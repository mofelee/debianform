package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/mofelee/debianform/internal/v2/ir"
	v2state "github.com/mofelee/debianform/internal/v2/state"
)

type SSHBackend struct {
	Runner Runner
	Owner  string
	Now    func() time.Time
}

func NewSSHBackend(runner Runner) SSHBackend {
	return SSHBackend{Runner: runner, Owner: "dbf"}
}

func (b SSHBackend) Read(ctx context.Context, host ir.HostSpec) (v2state.State, error) {
	if b.Runner == nil {
		return v2state.State{}, fmt.Errorf("ssh backend runner is required")
	}
	script := fmt.Sprintf(`set -eu
if [ -f %s ]; then
  cat %s
fi
`, shellQuote(host.State.Path), shellQuote(host.State.Path))
	result, err := b.Runner.Run(ctx, host.Name, script)
	if err != nil {
		return v2state.State{}, err
	}
	data := strings.TrimSpace(result.Stdout)
	if data == "" {
		return v2state.Empty(host.Name), nil
	}
	idx := strings.Index(data, "{")
	if idx > 0 {
		data = data[idx:]
	}
	return v2state.Decode([]byte(data), host.Name)
}

func (b SSHBackend) Write(ctx context.Context, host ir.HostSpec, st v2state.State) error {
	if b.Runner == nil {
		return fmt.Errorf("ssh backend runner is required")
	}
	data, err := v2state.Encode(st)
	if err != nil {
		return err
	}
	payload := base64.StdEncoding.EncodeToString(data)
	tmp := host.State.Path + ".tmp"
	script := fmt.Sprintf(`set -eu
mkdir -p "$(dirname %s)"
base64 -d > %s <<'__DBF_V2_STATE__'
%s
__DBF_V2_STATE__
mv %s %s
`, shellQuote(host.State.Path), shellQuote(tmp), payload, shellQuote(tmp), shellQuote(host.State.Path))
	_, err = b.Runner.Run(ctx, host.Name, script)
	return err
}

func (b SSHBackend) Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	if b.Runner == nil {
		return nil, fmt.Errorf("ssh backend runner is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	expiresAt := now().Add(timeout).UTC()
	owner := b.Owner
	if owner == "" {
		owner = "dbf"
	}
	lockDir := host.State.LockPath + ".d"
	script := fmt.Sprintf(`set -eu
lock_path=%s
lock_dir=%s
deadline=$(( $(date +%%s) + %d ))
expires_at_unix=%d
while true; do
  mkdir -p "$(dirname "$lock_path")"
  if mkdir "$lock_dir" 2>/dev/null; then
    cat > "$lock_path" <<__DBF_V2_LOCK__
{"owner":%q,"pid":"$$","token":%q,"expires_at":%q,"expires_at_unix":%d}
__DBF_V2_LOCK__
    exit 0
  fi
  now=$(date +%%s)
  current_expires=0
  if [ -f "$lock_path" ]; then
    current_expires=$(sed -n 's/.*"expires_at_unix":[ ]*\([0-9][0-9]*\).*/\1/p' "$lock_path" | tail -n 1)
    current_expires=${current_expires:-0}
  fi
  if [ "$current_expires" -gt 0 ] && [ "$now" -ge "$current_expires" ]; then
    printf 'taking over stale lock %%s\n' "$lock_path" >&2
    rm -rf -- "$lock_dir"
    continue
  fi
  if [ "$now" -ge "$deadline" ]; then
    printf 'timed out waiting for state lock %%s\n' "$lock_path" >&2
    exit 1
  fi
  sleep 1
done
`, shellQuote(host.State.LockPath), shellQuote(lockDir), int(timeout.Seconds()), expiresAt.Unix(), owner, token, expiresAt.Format(time.RFC3339), expiresAt.Unix())
	if _, err := b.Runner.Run(ctx, host.Name, script); err != nil {
		return nil, err
	}
	return sshLock{backend: b, host: host, token: token}, nil
}

type sshLock struct {
	backend SSHBackend
	host    ir.HostSpec
	token   string
}

func (l sshLock) Unlock(ctx context.Context) error {
	lockDir := l.host.State.LockPath + ".d"
	script := fmt.Sprintf(`set -eu
lock_path=%s
lock_dir=%s
token=%s
if [ ! -f "$lock_path" ]; then
  exit 0
fi
if grep -F "\"token\":\"$token\"" "$lock_path" >/dev/null 2>&1; then
  rm -f -- "$lock_path"
  rmdir -- "$lock_dir" 2>/dev/null || rm -rf -- "$lock_dir"
else
  printf 'state lock token mismatch for %%s\n' "$lock_path" >&2
  exit 1
fi
`, shellQuote(l.host.State.LockPath), shellQuote(lockDir), shellQuote(l.token))
	_, err := l.backend.Runner.Run(ctx, l.host.Name, script)
	return err
}

func randomToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
