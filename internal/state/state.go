package state

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
)

type State struct {
	Version   int                      `json:"version"`
	Resources map[string]ResourceState `json:"resources"`
}

type ResourceState map[string]any

type SSHBackend struct {
	config config.StateConfig
	runner *sshx.Runner
}

type Lock struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	done   chan error
	cancel context.CancelFunc
}

func NewSSHBackend(cfg config.StateConfig, runner *sshx.Runner) *SSHBackend {
	return &SSHBackend{config: cfg, runner: runner}
}

func (b *SSHBackend) Lock(ctx context.Context, timeout time.Duration) (*Lock, error) {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	lockCtx, cancel := context.WithCancel(ctx)
	script := fmt.Sprintf(`set -eu
mkdir -p "$(dirname %s)" "$(dirname %s)"
touch %s
exec 9>%s
flock -x 9
printf '__DBF_LOCKED__\n'
while IFS= read -r line; do
  [ "$line" = "__DBF_UNLOCK__" ] && exit 0
done
`, sshx.ShellQuote(b.config.Path), sshx.ShellQuote(b.config.LockPath), sshx.ShellQuote(b.config.Path), sshx.ShellQuote(b.config.LockPath))

	args := append(b.runner.SSHArgs(b.config.Host), "sh", "-c", script)
	cmd := exec.CommandContext(lockCtx, "ssh", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	done := make(chan error, 1)
	go func() {
		errText, _ := io.ReadAll(stderr)
		if err := cmd.Wait(); err != nil {
			done <- fmt.Errorf("%w: %s", err, strings.TrimSpace(string(errText)))
			return
		}
		done <- nil
	}()

	locked := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				locked <- err
				return
			}
			if strings.TrimSpace(line) == "__DBF_LOCKED__" {
				locked <- nil
				return
			}
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-locked:
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to acquire state lock %s: %w", b.config.LockPath, err)
		}
		return &Lock{cmd: cmd, stdin: stdin, done: done, cancel: cancel}, nil
	case err := <-done:
		cancel()
		return nil, fmt.Errorf("state lock process exited before lock: %w", err)
	case <-timer.C:
		cancel()
		return nil, fmt.Errorf("timed out waiting for state lock %s", b.config.LockPath)
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}

func (l *Lock) Unlock() error {
	if l == nil {
		return nil
	}
	_, _ = io.WriteString(l.stdin, "__DBF_UNLOCK__\n")
	_ = l.stdin.Close()
	err := <-l.done
	l.cancel()
	return err
}

func (b *SSHBackend) Read(ctx context.Context) (State, error) {
	script := fmt.Sprintf(`set -eu
if [ -f %s ]; then
  cat %s
else
  printf '{"version":1,"resources":{}}'
fi
`, sshx.ShellQuote(b.config.Path), sshx.ShellQuote(b.config.Path))
	result, err := b.runner.Run(ctx, b.config.Host, script)
	if err != nil {
		return State{}, err
	}
	var st State
	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		return emptyState(), nil
	}
	if idx := strings.Index(output, "{"); idx >= 0 {
		output = output[idx:]
	}
	if err := json.Unmarshal([]byte(output), &st); err != nil {
		return State{}, fmt.Errorf("decode state %s: %w", b.config.Path, err)
	}
	if st.Version == 0 {
		st.Version = 1
	}
	if st.Resources == nil {
		st.Resources = map[string]ResourceState{}
	}
	return st, nil
}

func (b *SSHBackend) Write(ctx context.Context, st State) error {
	if st.Version == 0 {
		st.Version = 1
	}
	if st.Resources == nil {
		st.Resources = map[string]ResourceState{}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	payload := base64.StdEncoding.EncodeToString(data)
	tmp := b.config.Path + ".tmp"
	script := fmt.Sprintf(`set -eu
mkdir -p "$(dirname %s)"
base64 -d > %s <<'__DBF_STATE__'
%s
__DBF_STATE__
mv %s %s
`, sshx.ShellQuote(b.config.Path), sshx.ShellQuote(tmp), payload, sshx.ShellQuote(tmp), sshx.ShellQuote(b.config.Path))
	_, err = b.runner.Run(ctx, b.config.Host, script)
	return err
}

func emptyState() State {
	return State{Version: 1, Resources: map[string]ResourceState{}}
}
