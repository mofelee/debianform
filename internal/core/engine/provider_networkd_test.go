package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestNativeProviderDestroyNetworkdNetDevRemovesFileAndRuntimeLink(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	prior := &corestate.Resource{
		Kind: "networkd_netdev",
		Desired: map[string]any{
			"path": "/etc/systemd/network/10-dbf-test.netdev",
			"name": "dbf-test0",
		},
	}

	err := provider.Destroy(context.Background(), Step{
		Address: `host.test.systemd.networkd.netdev["10-dbf-test"]`,
		Host:    "test",
		Action:  ActionDestroy,
		Prior:   prior,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.scripts))
	}
	script := runner.scripts[0]
	for _, want := range []string{
		"netdev_name='dbf-test0'",
		"rm -f -- '/etc/systemd/network/10-dbf-test.netdev'",
		"systemctl start systemd-networkd.service",
		"networkctl reload",
		"ip link delete dev \"$netdev_name\" 2>/dev/null || true",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("destroy script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "sed -n") {
		t.Fatalf("state-backed destroy unexpectedly inspected file content:\n%s", script)
	}
	assertScriptOrder(t, script,
		"rm -f --",
		"systemctl start systemd-networkd.service",
		"networkctl reload",
		"ip link delete dev",
	)
}

func TestNativeProviderDestroyLegacyNetworkdNetDevReadsNameBeforeRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "10-dbf-test.netdev")
	if err := os.WriteFile(path, []byte("[NetDev]\nName=dbf-test0\nKind=dummy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	callsPath := filepath.Join(dir, "calls")
	command := []byte("#!/bin/sh\nprintf '%s %s\\n' \"$(basename \"$0\")\" \"$*\" >> \"$DBF_NETWORKD_TEST_CALLS\"\n")
	for _, name := range []string{"systemctl", "networkctl", "ip"} {
		if err := os.WriteFile(filepath.Join(binDir, name), command, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DBF_NETWORKD_TEST_CALLS", callsPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := &countingLocalRunner{}
	provider := NewNativeProvider(runner)
	prior := &corestate.Resource{
		Kind: "networkd_netdev",
		Desired: map[string]any{
			"path": path,
		},
	}

	if err := provider.Destroy(context.Background(), Step{Address: "legacy", Host: "test", Action: ActionDestroy, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	script := runner.scripts[0]
	if !strings.Contains(script, "sed -n") || !strings.Contains(script, "Name[[:space:]]*=") {
		t.Fatalf("legacy destroy script does not recover [NetDev] Name:\n%s", script)
	}
	assertScriptOrder(t, script, "sed -n", "rm -f --", "networkctl reload", "ip link delete dev")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy netdev file still exists after destroy: %v", err)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"systemctl start systemd-networkd.service",
		"networkctl reload",
		"ip link delete dev dbf-test0",
	} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("mock command log missing %q:\n%s", want, calls)
		}
	}
}

func assertScriptOrder(t *testing.T, script string, fragments ...string) {
	t.Helper()
	previous := -1
	for _, fragment := range fragments {
		index := strings.Index(script, fragment)
		if index < 0 {
			t.Fatalf("script missing %q:\n%s", fragment, script)
		}
		if index <= previous {
			t.Fatalf("script fragment %q is out of order:\n%s", fragment, script)
		}
		previous = index
	}
}
