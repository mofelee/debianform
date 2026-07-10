package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

type localShellRunner struct{}

func (localShellRunner) Run(ctx context.Context, host, script string) (Result, error) {
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Stdin = strings.NewReader(script)
	return localShellRunner{}.run(cmd)
}

func (localShellRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", remoteCommand)
	cmd.Stdin = input
	return localShellRunner{}.run(cmd)
}

func (localShellRunner) run(cmd *exec.Cmd) (Result, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return result, &runnerError{err: err, stderr: result.Stderr}
	}
	return result, nil
}

func (r localShellRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand+"\n")
}

type runnerError struct {
	err    error
	stderr string
}

func (e *runnerError) Error() string {
	return e.err.Error() + ": " + strings.TrimSpace(e.stderr)
}

func (e *runnerError) Unwrap() error {
	return e.err
}

func TestSSHBackendStateRoundTrip(t *testing.T) {
	host := testBackendHost(t)
	backend := SSHBackend{Runner: localShellRunner{}, Owner: "test"}

	empty, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Host != host.Name || len(empty.Resources) != 0 {
		t.Fatalf("empty state = %#v", empty)
	}

	address := `host.server1.files.file["/tmp/example"]`
	state := corestate.Empty(host.Name)
	state.Resources[address] = corestate.Resource{
		Host:          host.Name,
		Kind:          "file",
		Ownership:     "managed",
		DesiredDigest: "digest",
		Desired:       map[string]any{"path": "/tmp/example"},
	}
	if err := backend.Write(context.Background(), host, state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(host.State.Path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary state file still exists: %v", err)
	}

	got, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if got.Serial != 1 {
		t.Fatalf("serial = %d, want 1", got.Serial)
	}
	if got.Resources[address].Desired["path"] != "/tmp/example" {
		t.Fatalf("state resources = %#v", got.Resources)
	}
}

func TestSSHBackendLockRejectsTokenMismatch(t *testing.T) {
	host := testBackendHost(t)
	backend := SSHBackend{Runner: localShellRunner{}, Owner: "test"}

	lock, err := backend.Lock(context.Background(), host, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(host.State.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["token"] = "different-token"
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.State.LockPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	if err := lock.Unlock(context.Background()); err == nil || !strings.Contains(err.Error(), "token mismatch") {
		t.Fatalf("unlock error = %v, want token mismatch", err)
	}
	if _, err := os.Stat(host.State.LockPath + ".d"); err != nil {
		t.Fatalf("lock directory was removed after token mismatch: %v", err)
	}
}

func TestSSHBackendLockTakesOverStaleLockWithWarning(t *testing.T) {
	host := testBackendHost(t)
	lockDir := host.State.LockPath + ".d"
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(host.State.LockPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.State.LockPath, []byte(`{"token":"stale","expires_at_unix":1}`), 0600); err != nil {
		t.Fatal(err)
	}

	var warnings bytes.Buffer
	backend := SSHBackend{Runner: localShellRunner{}, Owner: "test", Warnings: &warnings}
	lock, err := backend.Lock(context.Background(), host, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warnings.String(), "taking over stale lock") {
		t.Fatalf("warnings = %q, want stale lock warning", warnings.String())
	}
	if err := lock.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(host.State.LockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still exists after unlock: %v", err)
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("lock directory still exists after unlock: %v", err)
	}
}

func TestSSHBackendLockUsesHighPrecisionDeadline(t *testing.T) {
	host := testBackendHost(t)
	runner := &recordingRunner{}
	backend := SSHBackend{
		Runner: runner,
		Owner:  "test",
		Now: func() time.Time {
			return time.Unix(100, 1)
		},
	}

	if _, err := backend.Lock(context.Background(), host, 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("lock scripts = %d, want 1", len(runner.scripts))
	}
	script := runner.scripts[0]
	for _, want := range []string{
		"deadline_ns=$(( $(date +%s%N) + 1500000000 ))",
		"now_ns=$(date +%s%N)",
		"expires_at_unix=102",
		`"expires_at_unix":102`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("lock script missing %q:\n%s", want, script)
		}
	}
}

func TestSSHBackendLockIsMutuallyExclusive(t *testing.T) {
	host := testBackendHost(t)
	backend := SSHBackend{Runner: localShellRunner{}, Owner: "test"}
	lock, err := backend.Lock(context.Background(), host, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock(context.Background())

	started := time.Now()
	_, err = backend.Lock(context.Background(), host, time.Second)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for state lock") {
		t.Fatalf("second lock error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("second lock returned too early after %s", elapsed)
	}
}

func testBackendHost(t *testing.T) ir.HostSpec {
	t.Helper()
	root := t.TempDir()
	return ir.HostSpec{
		Name: "server1",
		State: ir.StateSpec{
			Path:     filepath.Join(root, "state", "server1.json"),
			LockPath: filepath.Join(root, "lock", "server1.lock"),
		},
	}
}
