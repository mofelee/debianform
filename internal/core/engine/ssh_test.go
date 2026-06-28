package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestSSHRunnerSerializesOnlyInitialAuth(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeBin, 0755); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	if err := os.Mkdir(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
state=%s
mkdir -p "$state"
lock="$state/lock"
count="$state/count"
active="$state/active"
max_after="$state/max_after"
{
  flock 9
  n=0
  if [ -f "$count" ]; then n=$(cat "$count"); fi
  n=$((n + 1))
  printf '%%s\n' "$n" >"$count"
  if [ "$n" -eq 1 ]; then
    if [ -f "$active" ]; then
      echo "initial auth overlapped" >&2
      exit 9
    fi
    echo 1 >"$active"
  else
    a=0
    if [ -f "$active" ]; then a=$(cat "$active"); fi
    a=$((a + 1))
    printf '%%s\n' "$a" >"$active"
    m=0
    if [ -f "$max_after" ]; then m=$(cat "$max_after"); fi
    if [ "$a" -gt "$m" ]; then printf '%%s\n' "$a" >"$max_after"; fi
  fi
} 9>"$lock"
sleep 0.15
if [ "$n" -gt 1 ]; then
  i=0
  while [ "$i" -lt 100 ]; do
    c=0
    if [ -f "$count" ]; then c=$(cat "$count"); fi
    [ "$c" -ge 3 ] && break
    i=$((i + 1))
    sleep 0.01
  done
fi
{
  flock 9
  n=$(cat "$count")
  if [ "$n" -eq 1 ]; then
    rm -f "$active"
  else
    a=$(cat "$active")
    a=$((a - 1))
    if [ "$a" -le 0 ]; then rm -f "$active"; else printf '%%s\n' "$a" >"$active"; fi
  fi
} 9>"$lock"
cat
`, shellQuote(stateDir))
	sshPath := filepath.Join(fakeBin, "ssh")
	if err := os.WriteFile(sshPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
		"server2": {Address: "server2"},
		"server3": {Address: "server3"},
	})
	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	for _, host := range []string{"server1", "server2", "server3"} {
		host := host
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := runner.Run(context.Background(), host, "true\n")
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(filepath.Join(stateDir, "max_after"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "2" {
		t.Fatalf("post-auth max concurrency = %q, want 2", strings.TrimSpace(string(data)))
	}
}

func TestSSHRunnerUsesControlMasterConfigByDefault(t *testing.T) {
	t.Setenv("DBF_SSH_CONFIG", "/tmp/debianform-user-ssh-config")
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})

	args := runner.SSHArgs("server1")
	config := sshArgValue(t, args, "-F")
	if config == "/tmp/debianform-user-ssh-config" {
		t.Fatalf("ssh args used user config directly, want wrapper config: %#v", args)
	}
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"ControlMaster auto",
		"ControlPersist 10m",
		"ControlPath ",
		"BatchMode yes",
		"NumberOfPasswordPrompts 0",
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"Include /tmp/debianform-user-ssh-config",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("control config missing %q:\n%s", want, text)
		}
	}
	controlPath := sshControlPathFromConfig(t, text)
	if !strings.HasPrefix(controlPath, "/tmp/dbfssh-") {
		t.Fatalf("control path = %q, want short /tmp path", controlPath)
	}
	if got := sshControlPathExpandedLen(controlPath); got > sshControlPathMaxBytes {
		t.Fatalf("expanded control path length = %d, want <= %d: %s", got, sshControlPathMaxBytes, controlPath)
	}
}

func TestSSHRunnerUsesNonInteractiveAuthOptions(t *testing.T) {
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})

	args := runner.SSHArgs("server1")
	for _, want := range []string{
		"BatchMode=yes",
		"NumberOfPasswordPrompts=0",
		"PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no",
	} {
		if !hasSSHOption(args, want) {
			t.Fatalf("ssh args missing -o %s: %#v", want, args)
		}
	}
}

func TestSSHRunErrorIncludesTroubleshootingHint(t *testing.T) {
	err := sshRunError("server1", Result{Stderr: "Permission denied (publickey)"}, fmt.Errorf("exit status 255"))
	if err == nil {
		t.Fatal("sshRunError returned nil")
	}
	text := err.Error()
	for _, want := range []string{
		"Permission denied (publickey)",
		"check root SSH key/agent",
		"ProxyCommand/ProxyJump",
		"non-interactive login",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ssh error missing %q:\n%s", want, text)
		}
	}
}

func TestSSHRunnerUsesShortControlPathWithLongTMPDIR(t *testing.T) {
	t.Setenv("TMPDIR", "/var/folders/ct/pk5nt9t52bj6njgkdf6y2dq00000gn/T/")
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})

	args := runner.SSHArgs("server1")
	config := sshArgValue(t, args, "-F")
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	controlPath := sshControlPathFromConfig(t, string(data))
	if strings.HasPrefix(controlPath, "/var/folders/") {
		t.Fatalf("control path used long TMPDIR: %q", controlPath)
	}
	if got := sshControlPathExpandedLen(controlPath); got > sshControlPathMaxBytes {
		t.Fatalf("expanded control path length = %d, want <= %d: %s", got, sshControlPathMaxBytes, controlPath)
	}
}

func TestSSHRunnerSkipsOverlongConfiguredControlDir(t *testing.T) {
	base := t.TempDir()
	longDir := filepath.Join(base, strings.Repeat("x", 90))
	if err := os.Mkdir(longDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBF_SSH_CONTROL_DIR", longDir)
	t.Setenv("DBF_SSH_CONFIG", "/tmp/debianform-user-ssh-config")
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})

	args := runner.SSHArgs("server1")
	if got := sshArgValue(t, args, "-F"); got != "/tmp/debianform-user-ssh-config" {
		t.Fatalf("ssh config = %q, want fallback to user config when control dir is too long; args=%#v", got, args)
	}
}

func TestSSHRunnerControlMasterCanBeDisabled(t *testing.T) {
	t.Setenv("DBF_SSH_CONTROL_MASTER", "0")
	t.Setenv("DBF_SSH_CONFIG", "/tmp/debianform-user-ssh-config")
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})

	args := runner.SSHArgs("server1")
	if got := sshArgValue(t, args, "-F"); got != "/tmp/debianform-user-ssh-config" {
		t.Fatalf("ssh config = %q, want user config when control master is disabled; args=%#v", got, args)
	}
}

func TestSSHRunnerCloseRemovesControlMasterDirectory(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeBin, 0755); err != nil {
		t.Fatal(err)
	}
	sshPath := filepath.Join(fakeBin, "ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})
	args := runner.SSHArgs("server1")
	config := sshArgValue(t, args, "-F")
	controlDir := filepath.Dir(config)

	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(controlDir); !os.IsNotExist(err) {
		t.Fatalf("control dir stat error = %v, want removed", err)
	}
}

func sshArgValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	idx := slices.Index(args, flag)
	if idx < 0 || idx+1 >= len(args) {
		t.Fatalf("ssh args missing %s value: %#v", flag, args)
	}
	return args[idx+1]
}

func hasSSHOption(args []string, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-o" && args[i+1] == value {
			return true
		}
	}
	return false
}

func sshControlPathFromConfig(t *testing.T, config string) string {
	t.Helper()
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ControlPath ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ControlPath "))
		}
	}
	t.Fatalf("control config missing ControlPath:\n%s", config)
	return ""
}
