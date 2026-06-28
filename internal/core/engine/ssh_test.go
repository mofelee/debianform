package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
