package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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
	first, err := backend.Write(context.Background(), host, state)
	if err != nil {
		t.Fatal(err)
	}
	if first.Serial != 1 {
		t.Fatalf("first committed serial = %d, want 1", first.Serial)
	}
	if _, err := os.Stat(host.State.Path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary state file still exists: %v", err)
	}

	readFirst, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if readFirst.Serial != first.Serial {
		t.Fatalf("first read serial = %d, want committed %d", readFirst.Serial, first.Serial)
	}

	second, err := backend.Write(context.Background(), host, first)
	if err != nil {
		t.Fatal(err)
	}
	if second.Serial != 2 {
		t.Fatalf("second committed serial = %d, want 2", second.Serial)
	}
	got, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if got.Serial != 2 {
		t.Fatalf("second read serial = %d, want 2", got.Serial)
	}
	if got.Resources[address].Desired["path"] != "/tmp/example" {
		t.Fatalf("state resources = %#v", got.Resources)
	}
}

func TestSSHBackendFailedWriteDoesNotAdvanceRevision(t *testing.T) {
	host := testBackendHost(t)
	address := `host.server1.files.file["/tmp/example"]`
	initial := corestate.Empty(host.Name)
	initial.Serial = 7
	initial.Resources[address] = corestate.Resource{
		Kind:          "file",
		Ownership:     "managed",
		DesiredDigest: "digest",
	}
	runner := &failOnceLocalBackendRunner{}
	backend := SSHBackend{Runner: runner, Owner: "test"}

	failed, err := backend.Write(context.Background(), host, initial)
	if err == nil || !strings.Contains(err.Error(), "injected state write failure") {
		t.Fatalf("first Write() error = %v, want injected failure", err)
	}
	if failed.Serial != 0 {
		t.Fatalf("failed committed state = %#v, want zero value", failed)
	}
	if initial.Serial != 7 || initial.Resources[address].Host != "" {
		t.Fatalf("failed Write mutated input state: %#v", initial)
	}
	if _, err := os.Stat(host.State.Path); !os.IsNotExist(err) {
		t.Fatalf("state file exists after failed write: %v", err)
	}

	committed, err := backend.Write(context.Background(), host, initial)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Serial != 8 || committed.Resources[address].Host != host.Name {
		t.Fatalf("retry committed state = %#v, want serial 8 with normalized host", committed)
	}
	read, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if read.Serial != 8 || read.Resources[address].Host != host.Name {
		t.Fatalf("retry read state = %#v, want serial 8 with normalized host", read)
	}
}

type failOnceLocalBackendRunner struct {
	failed bool
}

func (r *failOnceLocalBackendRunner) Run(ctx context.Context, host, script string) (Result, error) {
	if !r.failed {
		r.failed = true
		return Result{}, errors.New("injected state write failure")
	}
	return localShellRunner{}.Run(ctx, host, script)
}

func (r *failOnceLocalBackendRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return localShellRunner{}.RunInput(ctx, host, remoteCommand, input)
}

func (r *failOnceLocalBackendRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return localShellRunner{}.RunCommand(ctx, host, remoteCommand)
}

func TestSSHBackendReadRejectsIncompatibleOrForeignStateWithoutRewriting(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "missing version",
			data:    `{"host":"server1","resources":{}}`,
			wantErr: "unsupported version 0",
		},
		{
			name:    "old version",
			data:    `{"version":1,"host":"server1","resources":{}}`,
			wantErr: "unsupported version 1",
		},
		{
			name:    "newer version",
			data:    `{"version":3,"host":"server1","resources":{}}`,
			wantErr: "newer version 3",
		},
		{
			name:    "foreign host",
			data:    `{"version":2,"host":"server2","resources":{}}`,
			wantErr: `state host "server2" does not match requested host "server1"`,
		},
		{
			name:    "foreign resource host",
			data:    `{"version":2,"host":"server1","resources":{"host.server1.files.file[\"/tmp/example\"]":{"host":"server2","kind":"file","ownership":"managed","desired_digest":"digest"}}}`,
			wantErr: `belongs to host "server2", expected "server1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := testBackendHost(t)
			if err := os.MkdirAll(filepath.Dir(host.State.Path), 0755); err != nil {
				t.Fatal(err)
			}
			original := []byte(tt.data)
			if err := os.WriteFile(host.State.Path, original, 0600); err != nil {
				t.Fatal(err)
			}
			backend := SSHBackend{Runner: localShellRunner{}, Owner: "test"}

			_, err := backend.Read(context.Background(), host)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Read() error = %v, want containing %q", err, tt.wantErr)
			}
			got, err := os.ReadFile(host.State.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("state file changed after rejected read:\n got: %s\nwant: %s", got, original)
			}
		})
	}
}

func TestSSHBackendWriteRejectsIncompatibleOrForeignState(t *testing.T) {
	host := testBackendHost(t)
	address := `host.server1.files.file["/tmp/example"]`
	tests := []struct {
		name  string
		state corestate.State
	}{
		{
			name:  "newer version",
			state: corestate.State{Version: corestate.Version + 1, Host: host.Name, Resources: map[string]corestate.Resource{}},
		},
		{
			name:  "foreign host",
			state: corestate.State{Version: corestate.Version, Host: "server2", Resources: map[string]corestate.Resource{}},
		},
		{
			name: "foreign resource host",
			state: corestate.State{Version: corestate.Version, Host: host.Name, Resources: map[string]corestate.Resource{
				address: {Host: "server2", Kind: "file", Ownership: "managed", DesiredDigest: "digest"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &countingBackendRunner{}
			backend := SSHBackend{Runner: runner, Owner: "test"}
			if _, err := backend.Write(context.Background(), host, tt.state); err == nil {
				t.Fatal("Write() succeeded, want state validation error")
			}
			if runner.calls != 0 {
				t.Fatalf("runner calls = %d, want 0", runner.calls)
			}
			if _, err := os.Stat(host.State.Path); !os.IsNotExist(err) {
				t.Fatalf("state file exists after rejected write: %v", err)
			}
		})
	}
}

type countingBackendRunner struct {
	calls int
}

func (r *countingBackendRunner) Run(context.Context, string, string) (Result, error) {
	r.calls++
	return Result{}, nil
}

func (r *countingBackendRunner) RunInput(context.Context, string, string, io.Reader) (Result, error) {
	r.calls++
	return Result{}, nil
}

func (r *countingBackendRunner) RunCommand(context.Context, string, string) (Result, error) {
	r.calls++
	return Result{}, nil
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
	data = testLockRecord("other", "999", strings.Repeat("b", 32), int64(record["lease_expires_at_unix"].(float64)))
	if err := os.WriteFile(host.State.LockPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	writeTestLockMarker(t, host.State.LockPath+".d", strings.Repeat("b", 32))

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
	if err := os.WriteFile(host.State.LockPath, testLockRecord("stale", "1", strings.Repeat("a", 32), 1), 0600); err != nil {
		t.Fatal(err)
	}
	writeTestLockMarker(t, lockDir, strings.Repeat("a", 32))

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
	if _, err := os.Stat(host.State.LockPath + ".guard"); err != nil {
		t.Fatalf("stable guard file was removed after unlock: %v", err)
	}
}

func TestSSHBackendLockUsesHighPrecisionDeadline(t *testing.T) {
	host := testBackendHost(t)
	runner := &recordingRunner{}
	backend := SSHBackend{
		Runner: runner,
		Owner:  "test",
	}

	lock, err := backend.Lock(context.Background(), host, 1500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock(context.Background())
	if len(runner.scripts) != 2 {
		t.Fatalf("lock scripts = %d, want acquire and synchronous validation", len(runner.scripts))
	}
	script := runner.scripts[0]
	for _, want := range []string{
		"deadline_ns=$(( $(date +%s%N) + 1500000000 ))",
		"now_ns=$(date +%s%N)",
		"lease_ttl_seconds=120",
		"flock -n 9",
		`"lease_expires_at_unix":%s,"expires_at_unix":0`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("lock script missing %q:\n%s", want, script)
		}
	}
}

func TestSSHBackendLeaseTimingDefaultsAndClamps(t *testing.T) {
	backend := SSHBackend{}
	if got := backend.lockRenewInterval(defaultSSHLockLeaseTTL); got != defaultSSHLockRenewInterval {
		t.Fatalf("default renew interval = %s, want %s", got, defaultSSHLockRenewInterval)
	}
	if got := backend.lockRenewInterval(6 * time.Second); got != 2*time.Second {
		t.Fatalf("short lease renew interval = %s, want TTL/3", got)
	}
	if got := backend.lockRenewTimeout(defaultSSHLockLeaseTTL); got != defaultSSHLockRenewTimeout {
		t.Fatalf("default renew timeout = %s, want %s", got, defaultSSHLockRenewTimeout)
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

func TestSSHBackendLeaseStartsAfterWaitingForLock(t *testing.T) {
	host := testBackendHost(t)
	backend := testLeaseBackend(localShellRunner{})
	backend.LeaseTTL = 5 * time.Second
	backend.RenewInterval = time.Second

	first, err := backend.Lock(context.Background(), host, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	type lockResult struct {
		lock Lock
		err  error
	}
	resultCh := make(chan lockResult, 1)
	go func() {
		lock, err := backend.Lock(context.Background(), host, 2*time.Second)
		resultCh <- lockResult{lock: lock, err: err}
	}()
	time.Sleep(900 * time.Millisecond)
	if err := first.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}

	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.lock.Unlock(context.Background())
	record := readTestLockRecord(t, host.State.LockPath)
	remaining := record.LeaseExpiresAtUnix - time.Now().Unix()
	if remaining < 4 {
		t.Fatalf("lease remaining after wait = %ds, want a fresh 5s lease", remaining)
	}
	if record.ExpiresAtUnix != 0 {
		t.Fatalf("legacy expires_at_unix = %d, want 0 so old clients cannot take over v2 leases", record.ExpiresAtUnix)
	}
}

func TestSSHBackendLeaseRenewsDuringLongTask(t *testing.T) {
	host := testBackendHost(t)
	backend := testLeaseBackend(localShellRunner{})
	lock, err := backend.Lock(context.Background(), host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock(context.Background())
	initial := readTestLockRecord(t, host.State.LockPath).LeaseExpiresAtUnix

	time.Sleep(2300 * time.Millisecond)
	current := readTestLockRecord(t, host.State.LockPath).LeaseExpiresAtUnix
	if current <= initial {
		t.Fatalf("lease expiry did not advance: initial=%d current=%d", initial, current)
	}
	if lease := lock.(*sshLock); lease.Err() != nil {
		t.Fatalf("lease renewal error = %v", lease.Err())
	}

	if _, err := backend.Lock(context.Background(), host, 300*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out waiting for state lock") {
		t.Fatalf("contender error = %v, want timeout while long-running holder is renewed", err)
	}
}

func TestSSHBackendStaleTakeoverIsAtomicAcrossContenders(t *testing.T) {
	host := testBackendHost(t)
	if err := os.MkdirAll(host.State.LockPath+".d", 0755); err != nil {
		t.Fatal(err)
	}
	writeTestLockRecord(t, host.State.LockPath, "stale", "1", strings.Repeat("a", 32), 1)
	writeTestLockMarker(t, host.State.LockPath+".d", strings.Repeat("a", 32))

	backend := testLeaseBackend(localShellRunner{})
	backend.Warnings = io.Discard
	backend.LeaseTTL = 5 * time.Second
	backend.RenewInterval = time.Second
	start := make(chan struct{})
	results := make(chan struct {
		lock Lock
		err  error
	}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			lock, err := backend.Lock(context.Background(), host, 500*time.Millisecond)
			results <- struct {
				lock Lock
				err  error
			}{lock: lock, err: err}
		}()
	}
	close(start)

	var winner *sshLock
	var loserErr error
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err == nil {
			if winner != nil {
				t.Fatalf("both contenders acquired the stale lock")
			}
			winner = result.lock.(*sshLock)
		} else {
			loserErr = result.err
		}
	}
	if winner == nil || loserErr == nil || !strings.Contains(loserErr.Error(), "timed out waiting for state lock") {
		t.Fatalf("winner=%v loser error=%v, want one winner and one timeout", winner != nil, loserErr)
	}
	defer winner.Unlock(context.Background())
	record := readTestLockRecord(t, host.State.LockPath)
	if record.Token != winner.token {
		t.Fatalf("published token = %q, want winner token %q", record.Token, winner.token)
	}
}

func TestSSHBackendIncompleteLeaseRecoveryIsConservative(t *testing.T) {
	host := testBackendHost(t)
	lockDir := host.State.LockPath + ".d"
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestLockMarker(t, lockDir, strings.Repeat("a", 32))
	backend := testLeaseBackend(localShellRunner{})
	backend.RecoveryGrace = 5 * time.Second

	if _, err := backend.Lock(context.Background(), host, 250*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out waiting for state lock") {
		t.Fatalf("fresh incomplete lease error = %v, want timeout", err)
	}
	old := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(filepath.Join(lockDir, "owner.v2"), old, old); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	backend.Warnings = &warnings
	lock, err := backend.Lock(context.Background(), host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock(context.Background())
	if !strings.Contains(warnings.String(), "recovering incomplete v2 state lock") {
		t.Fatalf("warnings = %q, want incomplete lock recovery", warnings.String())
	}
}

func TestSSHBackendMalformedV2LeaseRequiresRecoveryGrace(t *testing.T) {
	host := testBackendHost(t)
	if err := os.MkdirAll(host.State.LockPath+".d", 0755); err != nil {
		t.Fatal(err)
	}
	valid := testLockRecord("broken", "1", strings.Repeat("a", 32), time.Now().Add(time.Minute).Unix())
	truncated := valid[:len(valid)-3]
	if err := os.WriteFile(host.State.LockPath, truncated, 0600); err != nil {
		t.Fatal(err)
	}
	backend := testLeaseBackend(localShellRunner{})
	backend.RecoveryGrace = 5 * time.Second
	if _, err := backend.Lock(context.Background(), host, 250*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out waiting for state lock") {
		t.Fatalf("fresh malformed lease error = %v, want timeout", err)
	}
	old := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(host.State.LockPath, old, old); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	backend.Warnings = &warnings
	lock, err := backend.Lock(context.Background(), host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock(context.Background())
	if !strings.Contains(warnings.String(), "recovering malformed state lock") {
		t.Fatalf("warnings = %q, want malformed v2 recovery", warnings.String())
	}
}

func TestSSHBackendRecoversAfterPublishFailsFollowingMkdir(t *testing.T) {
	host := testBackendHost(t)
	realMV, err := exec.LookPath("mv")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	marker := filepath.Join(fakeBin, "mv-failed-once")
	fakeMV := filepath.Join(fakeBin, "mv")
	script := fmt.Sprintf(`#!/bin/sh
marker=%s
if [ ! -e "$marker" ]; then
  : > "$marker"
  exit 73
fi
exec %s "$@"
`, shellQuote(marker), shellQuote(realMV))
	if err := os.WriteFile(fakeMV, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	backend := testLeaseBackend(localShellRunner{})
	backend.RecoveryGrace = 5 * time.Second
	if _, err := backend.Lock(context.Background(), host, time.Second); err == nil {
		t.Fatal("lock succeeded even though publishing the lease failed")
	}
	if _, err := os.Stat(host.State.LockPath + ".d"); err != nil {
		t.Fatalf("lock directory was not left by the injected post-mkdir failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(host.State.LockPath+".d", "owner.v2")); err != nil {
		t.Fatalf("v2 ownership marker was not published before the injected lease failure: %v", err)
	}
	if _, err := os.Stat(host.State.LockPath); !os.IsNotExist(err) {
		t.Fatalf("authoritative lease exists after failed publish: %v", err)
	}
	tmpFiles, err := filepath.Glob(host.State.LockPath + ".tmp.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpFiles) != 0 {
		t.Fatalf("temporary leases were not cleaned up: %v", tmpFiles)
	}

	if _, err := backend.Lock(context.Background(), host, 250*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out waiting for state lock") {
		t.Fatalf("fresh post-mkdir failure error = %v, want conservative timeout", err)
	}
	old := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(filepath.Join(host.State.LockPath+".d", "owner.v2"), old, old); err != nil {
		t.Fatal(err)
	}
	lock, err := backend.Lock(context.Background(), host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock(context.Background())
}

func TestSSHBackendRejectsNonRegularLockPath(t *testing.T) {
	host := testBackendHost(t)
	if err := os.MkdirAll(host.State.LockPath, 0755); err != nil {
		t.Fatal(err)
	}
	backend := testLeaseBackend(localShellRunner{})
	_, err := backend.Lock(context.Background(), host, time.Second)
	if err == nil || !strings.Contains(err.Error(), "refusing non-regular state lock path") {
		t.Fatalf("directory lock path error = %v, want non-regular path rejection", err)
	}
}

func TestSSHBackendLegacyLeaseIsNeverAutomaticallyTakenOver(t *testing.T) {
	host := testBackendHost(t)
	if err := os.MkdirAll(host.State.LockPath+".d", 0755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"owner":"old-dbf","token":"legacy","expires_at_unix":1}`)
	if err := os.WriteFile(host.State.LockPath, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(host.State.LockPath, old, old); err != nil {
		t.Fatal(err)
	}
	backend := testLeaseBackend(localShellRunner{})
	backend.RecoveryGrace = time.Second
	_, err := backend.Lock(context.Background(), host, 250*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "refusing automatic takeover of legacy or unknown state lock") {
		t.Fatalf("legacy lease error = %v, want manual-cleanup refusal", err)
	}
	data, readErr := os.ReadFile(host.State.LockPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(data, legacy) {
		t.Fatalf("legacy lease was modified: %q", data)
	}
}

func TestSSHBackendDoesNotTakeOverLiveUnmarkedLegacyWriter(t *testing.T) {
	host := testBackendHost(t)
	if err := os.MkdirAll(filepath.Dir(host.State.LockPath), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"owner":"old-dbf","token":"legacy","expires_at_unix":0}`)
	ready := make(chan struct{})
	writerErr := make(chan error, 1)
	go func() {
		if err := os.Mkdir(host.State.LockPath+".d", 0755); err != nil {
			writerErr <- err
			close(ready)
			return
		}
		close(ready)
		time.Sleep(150 * time.Millisecond)
		writerErr <- os.WriteFile(host.State.LockPath, legacy, 0600)
	}()
	<-ready

	backend := testLeaseBackend(localShellRunner{})
	backend.RecoveryGrace = 50 * time.Millisecond
	_, err := backend.Lock(context.Background(), host, 350*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "refusing automatic takeover of unmarked legacy or unknown state lock directory") {
		t.Fatalf("live legacy writer error = %v, want unmarked directory refusal", err)
	}
	if err := <-writerErr; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(host.State.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, legacy) {
		t.Fatalf("legacy writer metadata was replaced: %q", data)
	}
}

func TestSSHBackendFreshAcquireUsesAtomicLegacyBridge(t *testing.T) {
	host := testBackendHost(t)
	if err := os.MkdirAll(filepath.Dir(host.State.LockPath), 0755); err != nil {
		t.Fatal(err)
	}
	realMkdir, err := exec.LookPath("mkdir")
	if err != nil {
		t.Fatal(err)
	}
	realSleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	reached := filepath.Join(fakeBin, "v2-mkdir-reached")
	release := filepath.Join(fakeBin, "release-v2-mkdir")
	fakeMkdir := filepath.Join(fakeBin, "mkdir")
	lockDir := host.State.LockPath + ".d"
	script := fmt.Sprintf(`#!/bin/sh
target=%s
reached=%s
release=%s
last=
for arg in "$@"; do
  case "$arg" in
    -*) ;;
    *) last=$arg ;;
  esac
done
if [ "$last" = "$target" ]; then
  : > "$reached"
  while [ ! -e "$release" ]; do
    %s 0.01
  done
fi
exec %s "$@"
`, shellQuote(lockDir), shellQuote(reached), shellQuote(release), shellQuote(realSleep), shellQuote(realMkdir))
	if err := os.WriteFile(fakeMkdir, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	defer os.WriteFile(release, nil, 0600)

	type lockResult struct {
		lock Lock
		err  error
	}
	resultCh := make(chan lockResult, 1)
	backend := testLeaseBackend(localShellRunner{})
	go func() {
		lock, err := backend.Lock(context.Background(), host, 400*time.Millisecond)
		resultCh <- lockResult{lock: lock, err: err}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(reached); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("v2 acquisition did not reach its lock directory publish mkdir")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Mkdir(lockDir, 0755); err != nil {
		t.Fatalf("legacy writer did not win the atomic lock directory mkdir: %v", err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}

	var result lockResult
	select {
	case result = <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("v2 acquisition did not finish after releasing fake mkdir")
	}
	if result.lock != nil {
		defer result.lock.Unlock(context.Background())
	}
	if result.err == nil || !strings.Contains(result.err.Error(), "refusing automatic takeover of unmarked legacy or unknown state lock directory") {
		t.Fatalf("v2 acquisition error = %v, want refusal after legacy wins mkdir", result.err)
	}
	if _, err := os.Stat(host.State.LockPath); !os.IsNotExist(err) {
		t.Fatalf("v2 lease was published after losing atomic mkdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lockDir, "owner.v2")); !os.IsNotExist(err) {
		t.Fatalf("v2 marker was published after losing atomic mkdir: %v", err)
	}
}

func TestSSHBackendValidatesLeaseAfterDelayedAcquireResponse(t *testing.T) {
	host := testBackendHost(t)
	runner := &delayedAcquireRunner{
		published: make(chan struct{}),
		release:   make(chan struct{}),
	}
	firstBackend := testLeaseBackend(runner)
	firstBackend.LeaseTTL = time.Second
	firstBackend.RenewInterval = 200 * time.Millisecond
	firstBackend.RenewTimeout = 300 * time.Millisecond
	type lockResult struct {
		lock Lock
		err  error
	}
	firstResult := make(chan lockResult, 1)
	go func() {
		lock, err := firstBackend.Lock(context.Background(), host, time.Second)
		firstResult <- lockResult{lock: lock, err: err}
	}()
	select {
	case <-runner.published:
	case <-time.After(time.Second):
		t.Fatal("first acquisition did not publish its remote lease")
	}

	contenderBackend := testLeaseBackend(localShellRunner{})
	contenderBackend.LeaseTTL = 2 * time.Second
	contender, err := contenderBackend.Lock(context.Background(), host, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer contender.Unlock(context.Background())
	close(runner.release)
	result := <-firstResult
	if result.err == nil || result.lock != nil || !strings.Contains(result.err.Error(), "validate acquired state lock") {
		t.Fatalf("delayed first acquisition = lock %v, error %v; want validation failure", result.lock != nil, result.err)
	}
	record := readTestLockRecord(t, host.State.LockPath)
	if record.Token != contender.(*sshLock).token {
		t.Fatalf("delayed acquisition cleanup replaced contender token: got %q want %q", record.Token, contender.(*sshLock).token)
	}
}

func TestSSHBackendLockWaitDeadlineIncludesGuardContention(t *testing.T) {
	host := testBackendHost(t)
	if err := os.MkdirAll(filepath.Dir(host.State.LockPath), 0755); err != nil {
		t.Fatal(err)
	}
	guard, err := os.OpenFile(host.State.LockPath+".guard", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := syscall.Flock(int(guard.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)

	backend := testLeaseBackend(localShellRunner{})
	started := time.Now()
	_, err = backend.Lock(context.Background(), host, 250*time.Millisecond)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for state lock") {
		t.Fatalf("guard contention error = %v, want lock timeout", err)
	}
	if elapsed > time.Second {
		t.Fatalf("guard contention ignored wait deadline and took %s", elapsed)
	}
}

func TestSSHBackendRenewalHasIndependentDeadline(t *testing.T) {
	host := testBackendHost(t)
	runner := &blockingRenewRunner{started: make(chan struct{})}
	backend := testLeaseBackend(runner)
	backend.RenewInterval = 100 * time.Millisecond
	backend.RenewTimeout = 150 * time.Millisecond
	lock, err := backend.Lock(context.Background(), host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lease := lock.(*sshLock)
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("renewal did not start")
	}
	select {
	case <-lease.Lost():
	case <-time.After(time.Second):
		t.Fatal("blocked renewal did not lose the lease before TTL")
	}
	if !errors.Is(lease.Err(), context.DeadlineExceeded) {
		t.Fatalf("renewal error = %v, want context deadline exceeded", lease.Err())
	}
	if err := lock.Unlock(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unlock error = %v, want renewal root cause", err)
	}
}

type testLockRecordData struct {
	ProtocolVersion    int    `json:"protocol_version"`
	Token              string `json:"token"`
	LeaseExpiresAtUnix int64  `json:"lease_expires_at_unix"`
	ExpiresAtUnix      int64  `json:"expires_at_unix"`
}

func readTestLockRecord(t *testing.T, path string) testLockRecordData {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record testLockRecordData
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.ProtocolVersion != 2 {
		t.Fatalf("lock protocol version = %d, want 2", record.ProtocolVersion)
	}
	return record
}

func testLockRecord(owner, pid, token string, expires int64) []byte {
	ownerEncoded := base64.StdEncoding.EncodeToString([]byte(owner))
	payload := fmt.Sprintf("2|%s|%s|%s|%d|0", ownerEncoded, pid, token, expires)
	checksum := sha256.Sum256([]byte(payload))
	return []byte(fmt.Sprintf(`{"protocol_version":2,"owner":%q,"owner_encoding":"base64","pid":%q,"token":%q,"lease_expires_at_unix":%d,"expires_at_unix":0,"checksum":"%x"}`+"\n", ownerEncoded, pid, token, expires, checksum))
}

func testLockMarker(token string) []byte {
	payload := "2|" + token
	checksum := sha256.Sum256([]byte(payload))
	return []byte(fmt.Sprintf(`{"protocol_version":2,"token":%q,"checksum":"%x"}`+"\n", token, checksum))
}

func writeTestLockMarker(t *testing.T, lockDir, token string) {
	t.Helper()
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "owner.v2"), testLockMarker(token), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeTestLockRecord(t *testing.T, path, owner, pid, token string, expires int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, testLockRecord(owner, pid, token, expires), 0600); err != nil {
		t.Fatal(err)
	}
}

func testLeaseBackend(runner Runner) SSHBackend {
	return SSHBackend{
		Runner:        runner,
		Owner:         "test",
		Warnings:      io.Discard,
		LeaseTTL:      2 * time.Second,
		RenewInterval: 500 * time.Millisecond,
		RenewTimeout:  300 * time.Millisecond,
		RecoveryGrace: time.Second,
	}
}

type blockingRenewRunner struct {
	started    chan struct{}
	once       sync.Once
	mu         sync.Mutex
	renewCalls int
}

type delayedAcquireRunner struct {
	published chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (r *delayedAcquireRunner) Run(ctx context.Context, host, script string) (Result, error) {
	result, err := localShellRunner{}.Run(ctx, host, script)
	if !strings.Contains(script, "deadline_ns=") || err != nil {
		return result, err
	}
	r.once.Do(func() { close(r.published) })
	select {
	case <-r.release:
		return result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (r *delayedAcquireRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return localShellRunner{}.RunInput(ctx, host, remoteCommand, input)
}

func (r *delayedAcquireRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return localShellRunner{}.RunCommand(ctx, host, remoteCommand)
}

func (r *blockingRenewRunner) Run(ctx context.Context, host, script string) (Result, error) {
	if strings.Contains(script, "guard_wait_seconds=") {
		r.mu.Lock()
		r.renewCalls++
		call := r.renewCalls
		r.mu.Unlock()
		if call == 1 {
			return localShellRunner{}.Run(ctx, host, script)
		}
		r.once.Do(func() { close(r.started) })
		<-ctx.Done()
		return Result{}, ctx.Err()
	}
	return localShellRunner{}.Run(ctx, host, script)
}

func (r *blockingRenewRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return localShellRunner{}.RunInput(ctx, host, remoteCommand, input)
}

func (r *blockingRenewRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return localShellRunner{}.RunCommand(ctx, host, remoteCommand)
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
