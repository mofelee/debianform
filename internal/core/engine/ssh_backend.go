package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

const (
	defaultSSHLockLeaseTTL      = 2 * time.Minute
	defaultSSHLockRenewInterval = 30 * time.Second
	defaultSSHLockRenewTimeout  = 20 * time.Second
	defaultSSHLockRecoveryGrace = 5 * time.Minute
)

type SSHBackend struct {
	Runner        Runner
	Owner         string
	Warnings      io.Writer
	LeaseTTL      time.Duration
	RenewInterval time.Duration
	RenewTimeout  time.Duration
	RecoveryGrace time.Duration
}

func NewSSHBackend(runner Runner) SSHBackend {
	return SSHBackend{Runner: runner, Owner: "dbf", Warnings: os.Stderr}
}

func (b SSHBackend) Read(ctx context.Context, host ir.HostSpec) (corestate.State, error) {
	if b.Runner == nil {
		return corestate.State{}, fmt.Errorf("ssh backend runner is required")
	}
	script := fmt.Sprintf(`set -eu
if [ -f %s ]; then
  cat %s
fi
`, shellQuote(host.State.Path), shellQuote(host.State.Path))
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "state read",
		Action:  "read",
		Summary: host.State.Path,
	})
	result, err := b.Runner.Run(callCtx, host.Name, script)
	if err != nil {
		return corestate.State{}, err
	}
	data := strings.TrimSpace(result.Stdout)
	if data == "" {
		return corestate.Empty(host.Name), nil
	}
	idx := strings.Index(data, "{")
	if idx > 0 {
		data = data[idx:]
	}
	return corestate.Decode([]byte(data), host.Name)
}

func (b SSHBackend) Write(ctx context.Context, host ir.HostSpec, st corestate.State) error {
	if b.Runner == nil {
		return fmt.Errorf("ssh backend runner is required")
	}
	var err error
	st, err = corestate.Normalize(st, host.Name)
	if err != nil {
		return err
	}
	data, err := corestate.Encode(st)
	if err != nil {
		return err
	}
	payload := base64.StdEncoding.EncodeToString(data)
	tmp := host.State.Path + ".tmp"
	script := fmt.Sprintf(`set -eu
mkdir -p "$(dirname %s)"
base64 -d > %s <<'__DBF_STATE__'
%s
__DBF_STATE__
mv %s %s
`, shellQuote(host.State.Path), shellQuote(tmp), payload, shellQuote(tmp), shellQuote(host.State.Path))
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "state write",
		Action:  "write",
		Summary: host.State.Path,
	})
	_, err = b.Runner.Run(callCtx, host.Name, script)
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
	owner := b.Owner
	if owner == "" {
		owner = "dbf"
	}
	ownerEncoded := base64.StdEncoding.EncodeToString([]byte(owner))
	leaseTTL := b.lockLeaseTTL()
	recoveryGrace := b.lockRecoveryGrace()
	lockDir := host.State.LockPath + ".d"
	script := fmt.Sprintf(`set -eu
	umask 077
	lock_path=%s
	lock_dir=%s
	marker_path="$lock_dir/owner.v2"
	guard_path=%s
	token=%s
	owner_encoded=%s
	lease_ttl_seconds=%d
	recovery_grace_seconds=%d
	deadline_ns=$(( $(date +%%s%%N) + %d ))
	mkdir -p "$(dirname "$lock_path")"
	if ! command -v flock >/dev/null 2>&1 || ! command -v sha256sum >/dev/null 2>&1; then
	  printf 'state lock requires flock and sha256sum\n' >&2
	  exit 1
	fi
	if [ -L "$guard_path" ]; then
	  printf 'refusing state lock symlink guard %%s\n' "$guard_path" >&2
	  exit 1
	fi
	exec 9>"$guard_path"
	tmp_path="${lock_path}.tmp.${token}"
	cleanup() {
	  rm -f -- "$tmp_path"
	}
	trap cleanup EXIT HUP INT TERM
	unknown_warning=0
	read_v2_record() {
	  record=$(cat "$lock_path" 2>/dev/null || true)
	  record_protocol=$(sed -n 's/^{"protocol_version":\([0-9][0-9]*\),.*/\1/p' "$lock_path" | tail -n 1)
	  record_owner=$(sed -n 's/.*"owner":"\([A-Za-z0-9+/=]*\)".*/\1/p' "$lock_path" | tail -n 1)
	  record_pid=$(sed -n 's/.*"pid":"\([0-9][0-9]*\)".*/\1/p' "$lock_path" | tail -n 1)
	  record_token=$(sed -n 's/.*"token":"\([0-9a-f][0-9a-f]*\)".*/\1/p' "$lock_path" | tail -n 1)
	  record_expires=$(sed -n 's/.*"lease_expires_at_unix":\([0-9][0-9]*\).*/\1/p' "$lock_path" | tail -n 1)
	  record_checksum=$(sed -n 's/.*"checksum":"\([0-9a-f][0-9a-f]*\)"}.*/\1/p' "$lock_path" | tail -n 1)
	  if [ "$record_protocol" != 2 ] || [ -z "$record_owner" ] || [ -z "$record_pid" ] || [ -z "$record_token" ] || [ -z "$record_expires" ] || [ -z "$record_checksum" ]; then
	    return 1
	  fi
	  payload="2|${record_owner}|${record_pid}|${record_token}|${record_expires}|0"
	  expected_checksum=$(printf '%%s' "$payload" | sha256sum | awk '{print $1}')
	  expected_record=$(printf '{"protocol_version":2,"owner":"%%s","owner_encoding":"base64","pid":"%%s","token":"%%s","lease_expires_at_unix":%%s,"expires_at_unix":0,"checksum":"%%s"}' \
	    "$record_owner" "$record_pid" "$record_token" "$record_expires" "$record_checksum")
	  [ "$record_checksum" = "$expected_checksum" ] && [ "$record" = "$expected_record" ]
	}
	write_v2_record() {
	  expires_at_unix=$1
	  payload="2|${owner_encoded}|$$|${token}|${expires_at_unix}|0"
	  checksum=$(printf '%%s' "$payload" | sha256sum | awk '{print $1}')
	  printf '{"protocol_version":2,"owner":"%%s","owner_encoding":"base64","pid":"%%s","token":"%%s","lease_expires_at_unix":%%s,"expires_at_unix":0,"checksum":"%%s"}\n' \
	    "$owner_encoded" "$$" "$token" "$expires_at_unix" "$checksum" > "$tmp_path"
	}
	read_v2_marker() {
	  marker=$(cat "$marker_path" 2>/dev/null || true)
	  marker_protocol=$(sed -n 's/^{"protocol_version":\([0-9][0-9]*\),.*/\1/p' "$marker_path" | tail -n 1)
	  marker_token=$(sed -n 's/.*"token":"\([0-9a-f][0-9a-f]*\)".*/\1/p' "$marker_path" | tail -n 1)
	  marker_checksum=$(sed -n 's/.*"checksum":"\([0-9a-f][0-9a-f]*\)"}.*/\1/p' "$marker_path" | tail -n 1)
	  if [ "$marker_protocol" != 2 ] || [ -z "$marker_token" ] || [ -z "$marker_checksum" ]; then
	    return 1
	  fi
	  marker_payload="2|${marker_token}"
	  expected_marker_checksum=$(printf '%%s' "$marker_payload" | sha256sum | awk '{print $1}')
	  expected_marker=$(printf '{"protocol_version":2,"token":"%%s","checksum":"%%s"}' "$marker_token" "$marker_checksum")
	  [ "$marker_checksum" = "$expected_marker_checksum" ] && [ "$marker" = "$expected_marker" ]
	}
	write_v2_marker() {
	  marker_payload="2|${token}"
	  marker_checksum=$(printf '%%s' "$marker_payload" | sha256sum | awk '{print $1}')
	  printf '{"protocol_version":2,"token":"%%s","checksum":"%%s"}\n' "$token" "$marker_checksum" > "$marker_path"
	}
	while true; do
	  if ! flock -n 9; then
	    now_ns=$(date +%%s%%N)
	    if [ "$now_ns" -ge "$deadline_ns" ]; then
	      printf 'timed out waiting for state lock %%s\n' "$lock_path" >&2
	      exit 1
	    fi
	    sleep 0.1
	    continue
	  fi
	  now_ns=$(date +%%s%%N)
	  if [ "$now_ns" -ge "$deadline_ns" ]; then
	    flock -u 9
	    printf 'timed out waiting for state lock %%s\n' "$lock_path" >&2
	    exit 1
	  fi
	  now=$(( now_ns / 1000000000 ))
	  claim=0
	  create_lock_dir=0
	  warning=
	  if [ -L "$lock_path" ] || { [ -e "$lock_path" ] && [ ! -f "$lock_path" ]; }; then
	    flock -u 9
	    printf 'refusing non-regular state lock path %%s\n' "$lock_path" >&2
	    exit 1
	  elif [ -L "$lock_dir" ] || { [ -e "$lock_dir" ] && [ ! -d "$lock_dir" ]; }; then
	    flock -u 9
	    printf 'refusing non-directory state lock path %%s\n' "$lock_dir" >&2
	    exit 1
	  elif [ -f "$lock_path" ]; then
	    if read_v2_record; then
	      if read_v2_marker && [ "$marker_token" = "$record_token" ]; then
	        current_expires=$record_expires
	        if [ "$now" -ge "$current_expires" ]; then
	          claim=1
	          warning="taking over stale lock $lock_path"
	        fi
	      else
	        modified=$(stat -c %%Y "$lock_path" 2>/dev/null || printf '%%s' "$now")
	        if [ $(( now - modified )) -ge "$recovery_grace_seconds" ]; then
	          claim=1
	          warning="recovering state lock with missing or mismatched v2 marker $lock_path"
	        fi
	      fi
	    else
	      case "$record" in
	        '{"protocol_version":2,'*)
	        modified=$(stat -c %%Y "$lock_path" 2>/dev/null || printf '%%s' "$now")
	        if [ $(( now - modified )) -ge "$recovery_grace_seconds" ]; then
	          claim=1
	          warning="recovering malformed state lock $lock_path"
	        fi
	          ;;
	        *)
	          if [ "$unknown_warning" -eq 0 ]; then
	            printf 'refusing automatic takeover of legacy or unknown state lock %%s; verify the holder and remove it manually\n' "$lock_path" >&2
	            unknown_warning=1
	          fi
	          ;;
	      esac
	    fi
	  elif [ -d "$lock_dir" ]; then
	    if read_v2_marker; then
	      modified=$(stat -c %%Y "$marker_path" 2>/dev/null || printf '%%s' "$now")
	      if [ $(( now - modified )) -ge "$recovery_grace_seconds" ]; then
	        claim=1
	        warning="recovering incomplete v2 state lock $lock_path"
	      fi
	    elif [ "$unknown_warning" -eq 0 ]; then
	      printf 'refusing automatic takeover of unmarked legacy or unknown state lock directory %%s; verify the holder and remove it manually\n' "$lock_dir" >&2
	      unknown_warning=1
	    fi
	  else
	    claim=1
	    create_lock_dir=1
	  fi
	  if [ "$claim" -eq 1 ]; then
	    expires_at_unix=$(( now + lease_ttl_seconds ))
	    write_v2_record "$expires_at_unix"
	    if [ ! -d "$lock_dir" ]; then
	      create_lock_dir=1
	    fi
	    if [ "$create_lock_dir" -eq 1 ] && ! mkdir "$lock_dir" 2>/dev/null; then
	      rm -f -- "$tmp_path"
	      flock -u 9
	      now_ns=$(date +%%s%%N)
	      if [ "$now_ns" -ge "$deadline_ns" ]; then
	        printf 'timed out waiting for state lock %%s\n' "$lock_path" >&2
	        exit 1
	      fi
	      sleep 0.1
	      continue
	    fi
	    write_v2_marker
	    mv -f -- "$tmp_path" "$lock_path"
	    flock -u 9
	    if [ -n "$warning" ]; then
	      printf '%%s\n' "$warning" >&2
	    fi
	    trap - EXIT HUP INT TERM
	    exit 0
	  fi
	  flock -u 9
	  if [ "$now_ns" -ge "$deadline_ns" ]; then
	    printf 'timed out waiting for state lock %%s\n' "$lock_path" >&2
	    exit 1
	  fi
	  sleep 0.1
	done
	`, shellQuote(host.State.LockPath), shellQuote(lockDir), shellQuote(host.State.LockPath+".guard"), shellQuote(token), shellQuote(ownerEncoded), durationSecondsCeil(leaseTTL), durationSecondsCeil(recoveryGrace), timeout.Nanoseconds())
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "state lock",
		Action:  "lock",
		Summary: host.State.LockPath,
	})
	result, err := b.Runner.Run(callCtx, host.Name, script)
	if err != nil {
		return nil, err
	}
	if b.Warnings != nil && strings.TrimSpace(result.Stderr) != "" {
		fmt.Fprintln(b.Warnings, strings.TrimSpace(result.Stderr))
	}
	lock := &sshLock{
		backend:       b,
		host:          host,
		token:         token,
		leaseTTL:      leaseTTL,
		renewInterval: b.lockRenewInterval(leaseTTL),
		renewTimeout:  b.lockRenewTimeout(leaseTTL),
		lost:          make(chan struct{}),
		renewDone:     make(chan struct{}),
	}
	if err := lock.renew(ctx); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lock.renewTimeout)
		_ = lock.unlockRemote(cleanupCtx)
		cancel()
		return nil, fmt.Errorf("validate acquired state lock %s: %w", host.State.LockPath, err)
	}
	lock.startRenewal(ctx)
	return lock, nil
}

type sshLock struct {
	backend       SSHBackend
	host          ir.HostSpec
	token         string
	leaseTTL      time.Duration
	renewInterval time.Duration
	renewTimeout  time.Duration

	renewCancel context.CancelFunc
	renewDone   chan struct{}
	stopOnce    sync.Once
	lostOnce    sync.Once
	lost        chan struct{}
	errMu       sync.Mutex
	err         error
}

func (l *sshLock) Unlock(ctx context.Context) error {
	l.stopRenewal()
	unlockErr := l.unlockRemote(ctx)
	return errors.Join(l.Err(), unlockErr)
}

func (l *sshLock) unlockRemote(ctx context.Context) error {
	lockDir := l.host.State.LockPath + ".d"
	script := fmt.Sprintf(`set -eu
	umask 077
	lock_path=%s
	lock_dir=%s
	marker_path="$lock_dir/owner.v2"
	guard_path=%s
	token=%s
	mkdir -p "$(dirname "$lock_path")"
	if ! command -v flock >/dev/null 2>&1 || ! command -v sha256sum >/dev/null 2>&1; then
	  printf 'state lock requires flock and sha256sum\n' >&2
	  exit 1
	fi
	if [ -L "$guard_path" ]; then
	  printf 'refusing state lock symlink guard %%s\n' "$guard_path" >&2
	  exit 1
	fi
	exec 9>"$guard_path"
	flock -x 9
	if [ -L "$lock_path" ] || { [ -e "$lock_path" ] && [ ! -f "$lock_path" ]; }; then
	  printf 'refusing non-regular state lock path %%s\n' "$lock_path" >&2
	  flock -u 9
	  exit 1
	fi
	if [ ! -f "$lock_path" ]; then
	  flock -u 9
	  exit 0
	fi
	record=$(cat "$lock_path" 2>/dev/null || true)
	record_protocol=$(sed -n 's/^{"protocol_version":\([0-9][0-9]*\),.*/\1/p' "$lock_path" | tail -n 1)
	record_owner=$(sed -n 's/.*"owner":"\([A-Za-z0-9+/=]*\)".*/\1/p' "$lock_path" | tail -n 1)
	record_pid=$(sed -n 's/.*"pid":"\([0-9][0-9]*\)".*/\1/p' "$lock_path" | tail -n 1)
	record_token=$(sed -n 's/.*"token":"\([0-9a-f][0-9a-f]*\)".*/\1/p' "$lock_path" | tail -n 1)
	record_expires=$(sed -n 's/.*"lease_expires_at_unix":\([0-9][0-9]*\).*/\1/p' "$lock_path" | tail -n 1)
	record_checksum=$(sed -n 's/.*"checksum":"\([0-9a-f][0-9a-f]*\)"}.*/\1/p' "$lock_path" | tail -n 1)
	payload="2|${record_owner}|${record_pid}|${record_token}|${record_expires}|0"
	expected_checksum=$(printf '%%s' "$payload" | sha256sum | awk '{print $1}')
	expected_record=$(printf '{"protocol_version":2,"owner":"%%s","owner_encoding":"base64","pid":"%%s","token":"%%s","lease_expires_at_unix":%%s,"expires_at_unix":0,"checksum":"%%s"}' \
	  "$record_owner" "$record_pid" "$record_token" "$record_expires" "$record_checksum")
	if [ "$record_protocol" != 2 ] || [ -z "$record_owner" ] || [ -z "$record_pid" ] || [ -z "$record_token" ] || [ -z "$record_expires" ] || [ -z "$record_checksum" ] || [ "$record_checksum" != "$expected_checksum" ] || [ "$record" != "$expected_record" ]; then
	  printf 'state lock lease is malformed for %%s\n' "$lock_path" >&2
	  flock -u 9
	  exit 1
	fi
	marker=$(cat "$marker_path" 2>/dev/null || true)
	marker_protocol=$(sed -n 's/^{"protocol_version":\([0-9][0-9]*\),.*/\1/p' "$marker_path" | tail -n 1)
	marker_token=$(sed -n 's/.*"token":"\([0-9a-f][0-9a-f]*\)".*/\1/p' "$marker_path" | tail -n 1)
	marker_checksum=$(sed -n 's/.*"checksum":"\([0-9a-f][0-9a-f]*\)"}.*/\1/p' "$marker_path" | tail -n 1)
	marker_payload="2|${marker_token}"
	expected_marker_checksum=$(printf '%%s' "$marker_payload" | sha256sum | awk '{print $1}')
	expected_marker=$(printf '{"protocol_version":2,"token":"%%s","checksum":"%%s"}' "$marker_token" "$marker_checksum")
	if [ "$marker_protocol" != 2 ] || [ -z "$marker_token" ] || [ -z "$marker_checksum" ] || [ "$marker_checksum" != "$expected_marker_checksum" ] || [ "$marker" != "$expected_marker" ] || [ "$marker_token" != "$record_token" ]; then
	  printf 'state lock ownership marker mismatch for %%s\n' "$lock_path" >&2
	  flock -u 9
	  exit 1
	fi
	if [ "$record_token" = "$token" ]; then
	  rm -f -- "$lock_path"
	  rm -f -- "$marker_path"
	  rmdir -- "$lock_dir" 2>/dev/null || true
	  flock -u 9
	else
	  printf 'state lock token mismatch for %%s\n' "$lock_path" >&2
	  flock -u 9
	  exit 1
	fi
	`, shellQuote(l.host.State.LockPath), shellQuote(lockDir), shellQuote(l.host.State.LockPath+".guard"), shellQuote(l.token))
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "state unlock",
		Action:  "unlock",
		Summary: l.host.State.LockPath,
		Cleanup: true,
	})
	_, unlockErr := l.backend.Runner.Run(callCtx, l.host.Name, script)
	return unlockErr
}

func (l *sshLock) Lost() <-chan struct{} {
	return l.lost
}

func (l *sshLock) Err() error {
	l.errMu.Lock()
	defer l.errMu.Unlock()
	return l.err
}

func (l *sshLock) startRenewal(parent context.Context) {
	renewCtx, cancel := context.WithCancel(context.WithoutCancel(parent))
	l.renewCancel = cancel
	go l.keepAlive(renewCtx)
}

func (l *sshLock) keepAlive(ctx context.Context) {
	defer close(l.renewDone)
	ticker := time.NewTicker(l.renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.renew(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				l.markLost(fmt.Errorf("renew state lock %s: %w", l.host.State.LockPath, err))
				return
			}
		}
	}
}

func (l *sshLock) renew(ctx context.Context) error {
	owner := l.backend.Owner
	if owner == "" {
		owner = "dbf"
	}
	ownerEncoded := base64.StdEncoding.EncodeToString([]byte(owner))
	script := fmt.Sprintf(`set -eu
	umask 077
	lock_path=%s
	lock_dir=%s
	marker_path="$lock_dir/owner.v2"
	guard_path=%s
	token=%s
	owner_encoded=%s
	lease_ttl_seconds=%d
	guard_wait_seconds=%d
	tmp_path="${lock_path}.tmp.${token}"
	cleanup() {
	  rm -f -- "$tmp_path"
	}
	trap cleanup EXIT HUP INT TERM
	mkdir -p "$(dirname "$lock_path")"
	if ! command -v flock >/dev/null 2>&1 || ! command -v sha256sum >/dev/null 2>&1; then
	  printf 'state lock requires flock and sha256sum\n' >&2
	  exit 1
	fi
	if [ -L "$guard_path" ]; then
	  printf 'refusing state lock symlink guard %%s\n' "$guard_path" >&2
	  exit 1
	fi
	exec 9>"$guard_path"
	if ! flock -w "$guard_wait_seconds" 9; then
	  printf 'timed out renewing state lock %%s\n' "$lock_path" >&2
	  exit 1
	fi
	if [ -L "$lock_path" ] || { [ -e "$lock_path" ] && [ ! -f "$lock_path" ]; }; then
	  printf 'refusing non-regular state lock path %%s\n' "$lock_path" >&2
	  exit 1
	fi
	if [ ! -f "$lock_path" ]; then
	  printf 'state lock disappeared for %%s\n' "$lock_path" >&2
	  exit 1
	fi
	record=$(cat "$lock_path" 2>/dev/null || true)
	record_protocol=$(sed -n 's/^{"protocol_version":\([0-9][0-9]*\),.*/\1/p' "$lock_path" | tail -n 1)
	record_owner=$(sed -n 's/.*"owner":"\([A-Za-z0-9+/=]*\)".*/\1/p' "$lock_path" | tail -n 1)
	record_pid=$(sed -n 's/.*"pid":"\([0-9][0-9]*\)".*/\1/p' "$lock_path" | tail -n 1)
	record_token=$(sed -n 's/.*"token":"\([0-9a-f][0-9a-f]*\)".*/\1/p' "$lock_path" | tail -n 1)
	record_expires=$(sed -n 's/.*"lease_expires_at_unix":\([0-9][0-9]*\).*/\1/p' "$lock_path" | tail -n 1)
	record_checksum=$(sed -n 's/.*"checksum":"\([0-9a-f][0-9a-f]*\)"}.*/\1/p' "$lock_path" | tail -n 1)
	payload="2|${record_owner}|${record_pid}|${record_token}|${record_expires}|0"
	expected_checksum=$(printf '%%s' "$payload" | sha256sum | awk '{print $1}')
	expected_record=$(printf '{"protocol_version":2,"owner":"%%s","owner_encoding":"base64","pid":"%%s","token":"%%s","lease_expires_at_unix":%%s,"expires_at_unix":0,"checksum":"%%s"}' \
	  "$record_owner" "$record_pid" "$record_token" "$record_expires" "$record_checksum")
	if [ "$record_protocol" != 2 ] || [ -z "$record_owner" ] || [ -z "$record_pid" ] || [ -z "$record_token" ] || [ -z "$record_expires" ] || [ -z "$record_checksum" ] || [ "$record_checksum" != "$expected_checksum" ] || [ "$record" != "$expected_record" ]; then
	  printf 'state lock lease is malformed for %%s\n' "$lock_path" >&2
	  exit 1
	fi
	marker=$(cat "$marker_path" 2>/dev/null || true)
	marker_protocol=$(sed -n 's/^{"protocol_version":\([0-9][0-9]*\),.*/\1/p' "$marker_path" | tail -n 1)
	marker_token=$(sed -n 's/.*"token":"\([0-9a-f][0-9a-f]*\)".*/\1/p' "$marker_path" | tail -n 1)
	marker_checksum=$(sed -n 's/.*"checksum":"\([0-9a-f][0-9a-f]*\)"}.*/\1/p' "$marker_path" | tail -n 1)
	marker_payload="2|${marker_token}"
	expected_marker_checksum=$(printf '%%s' "$marker_payload" | sha256sum | awk '{print $1}')
	expected_marker=$(printf '{"protocol_version":2,"token":"%%s","checksum":"%%s"}' "$marker_token" "$marker_checksum")
	if [ "$marker_protocol" != 2 ] || [ -z "$marker_token" ] || [ -z "$marker_checksum" ] || [ "$marker_checksum" != "$expected_marker_checksum" ] || [ "$marker" != "$expected_marker" ] || [ "$marker_token" != "$record_token" ]; then
	  printf 'state lock ownership marker mismatch for %%s\n' "$lock_path" >&2
	  exit 1
	fi
	now=$(date +%%s)
	if [ "$record_token" != "$token" ]; then
	  printf 'state lock token mismatch for %%s\n' "$lock_path" >&2
	  exit 1
	fi
	if [ "$now" -ge "$record_expires" ]; then
	  printf 'state lock lease expired for %%s\n' "$lock_path" >&2
	  exit 1
	fi
	expires_at_unix=$(( now + lease_ttl_seconds ))
	payload="2|${owner_encoded}|$$|${token}|${expires_at_unix}|0"
	checksum=$(printf '%%s' "$payload" | sha256sum | awk '{print $1}')
	printf '{"protocol_version":2,"owner":"%%s","owner_encoding":"base64","pid":"%%s","token":"%%s","lease_expires_at_unix":%%s,"expires_at_unix":0,"checksum":"%%s"}\n' \
	  "$owner_encoded" "$$" "$token" "$expires_at_unix" "$checksum" > "$tmp_path"
	mv -f -- "$tmp_path" "$lock_path"
	touch "$marker_path"
	flock -u 9
	trap - EXIT HUP INT TERM
	`, shellQuote(l.host.State.LockPath), shellQuote(l.host.State.LockPath+".d"), shellQuote(l.host.State.LockPath+".guard"), shellQuote(l.token), shellQuote(ownerEncoded), durationSecondsCeil(l.leaseTTL), durationSecondsCeil(l.renewTimeout))
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:       "state lock renew",
		Action:      "renew",
		Summary:     l.host.State.LockPath,
		Maintenance: true,
	})
	renewCtx, cancel := context.WithTimeout(callCtx, l.renewTimeout)
	defer cancel()
	_, err := l.backend.Runner.Run(renewCtx, l.host.Name, script)
	return err
}

func (l *sshLock) markLost(err error) {
	l.errMu.Lock()
	if l.err == nil {
		l.err = err
	}
	l.errMu.Unlock()
	l.lostOnce.Do(func() { close(l.lost) })
}

func (l *sshLock) stopRenewal() {
	l.stopOnce.Do(func() {
		l.renewCancel()
		<-l.renewDone
	})
}

func (b SSHBackend) lockLeaseTTL() time.Duration {
	if b.LeaseTTL > 0 {
		return b.LeaseTTL
	}
	return defaultSSHLockLeaseTTL
}

func (b SSHBackend) lockRenewInterval(leaseTTL time.Duration) time.Duration {
	interval := b.RenewInterval
	if interval <= 0 {
		interval = defaultSSHLockRenewInterval
	}
	if maximum := leaseTTL / 3; interval > maximum {
		interval = maximum
	}
	if interval <= 0 {
		return time.Millisecond
	}
	return interval
}

func (b SSHBackend) lockRecoveryGrace() time.Duration {
	if b.RecoveryGrace > 0 {
		return b.RecoveryGrace
	}
	return defaultSSHLockRecoveryGrace
}

func (b SSHBackend) lockRenewTimeout(leaseTTL time.Duration) time.Duration {
	timeout := b.RenewTimeout
	if timeout <= 0 {
		timeout = defaultSSHLockRenewTimeout
	}
	if maximum := leaseTTL / 3; timeout > maximum {
		timeout = maximum
	}
	if timeout <= 0 {
		return time.Millisecond
	}
	return timeout
}

func durationSecondsCeil(value time.Duration) int64 {
	seconds := value / time.Second
	if value%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return int64(seconds)
}

func randomToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
