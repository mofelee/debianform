package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
)

func TestNativeProviderSysctlAbsentRemovesManagedPersistence(t *testing.T) {
	key := "net.ipv4.ip_forward"
	value := "1"
	persistedHash := sha256Hex([]byte(key + " = " + value + "\n"))
	node := graph.Node{
		Address: "host.server1.kernel.sysctl[\"net.ipv4.ip_forward\"]",
		Host:    "server1",
		Kind:    "sysctl",
		Desired: map[string]any{"key": key, "value": value, "ensure": "absent"},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: value + "\n" + persistedHash + "\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionDelete {
		t.Fatalf("persisted absent sysctl action = %q, want delete", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionDelete}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	if strings.Contains(applied, "sysctl -w") {
		t.Fatalf("absent sysctl should not write runtime value:\n%s", applied)
	}
	if !strings.Contains(applied, "rm -f") {
		t.Fatalf("absent sysctl should remove persisted file:\n%s", applied)
	}
}
