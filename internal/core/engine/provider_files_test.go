package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestNativeProviderAbsentMissingDoesNotAdopt(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.files.file[\"/tmp/dbf-absent\"]",
		Host:    "server1",
		Kind:    "file",
		Desired: map[string]any{"path": "/tmp/dbf-absent", "ensure": "absent"},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}, {Stdout: "missing\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("missing absent file action = %q, want no-op", got.Action)
	}

	prior := &corestate.Resource{Ownership: "managed"}
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionDelete {
		t.Fatalf("missing absent file with prior action = %q, want delete to clear state", got.Action)
	}
}

func TestNativeProviderWriteOnlyFileUsesVersionForPlan(t *testing.T) {
	node := writeOnlyFileNode("v1")
	prior := &corestate.Resource{
		DesiredDigest: corestate.DesiredDigest(node.Desired),
		Ownership:     "managed",
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n600\nremote-sha\n"}}}
	provider := NewNativeProvider(runner)
	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("matching write-only version action = %q, want no-op", got.Action)
	}
	if strings.Contains(anyMapText(got.Observed), "not-a-real-ephemeral-token") {
		t.Fatalf("provider plan observed leaked write-only content: %#v", got.Observed)
	}
	if _, ok := got.Observed["sha256"]; ok {
		t.Fatalf("provider plan observed retained sha256 for write-only file: %#v", got.Observed)
	}

	next := writeOnlyFileNode("current")
	runner = &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n600\nremote-sha\n"}}}
	provider = NewNativeProvider(runner)
	got, err = provider.Plan(context.Background(), next, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate {
		t.Fatalf("changed write-only version action = %q, want update", got.Action)
	}
}

func TestNativeProviderObservedModeUsesFourDigitDisplay(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.files.file[\"/etc/example.conf\"]",
		Host:    "server1",
		Kind:    "file",
		Desired: map[string]any{
			"path":      "/etc/example.conf",
			"content":   "hello\n",
			"owner":     "root",
			"group":     "root",
			"mode":      "0640",
			"ensure":    "present",
			"sensitive": false,
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n640\nold-sha\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Observed["mode"] != "0640" {
		t.Fatalf("observed mode = %#v, want 0640", got.Observed["mode"])
	}
}

func TestNativeProviderPlanHostBulkInspectsMultipleFilesWithOneRun(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(fileA, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	owner, group := currentUserAndGroup(t)
	nodes := []graph.Node{
		{
			Address: `host.server1.files.file["a"]`,
			Host:    "server1",
			Kind:    "file",
			Desired: map[string]any{"path": fileA, "content": "a", "owner": owner, "group": group, "mode": "0644"},
		},
		{
			Address: `host.server1.files.file["b"]`,
			Host:    "server1",
			Kind:    "file",
			Desired: map[string]any{"path": fileB, "content": "new", "owner": owner, "group": group, "mode": "0644"},
		},
	}
	runner := &countingLocalRunner{}
	provider := NewNativeProvider(runner)

	plans, err := provider.PlanHost(context.Background(), testBackendHost(t), nodes, map[string]*corestate.Resource{
		nodes[0].Address: {Ownership: "managed", DesiredDigest: corestate.DesiredDigest(nodes[0].Desired)},
		nodes[1].Address: {Ownership: "managed", DesiredDigest: corestate.DesiredDigest(nodes[1].Desired)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("runner calls = %d, want one bulk call", len(runner.scripts))
	}
	if !strings.Contains(runner.scripts[0], bulkPlanPrefix) {
		t.Fatalf("bulk script missing protocol marker:\n%s", runner.scripts[0])
	}
	if plans[nodes[0].Address].Action != ActionNoOp {
		t.Fatalf("file a action = %q, want no-op; observed=%#v", plans[nodes[0].Address].Action, plans[nodes[0].Address].Observed)
	}
	if plans[nodes[1].Address].Action != ActionUpdate {
		t.Fatalf("file b action = %q, want update; observed=%#v", plans[nodes[1].Address].Action, plans[nodes[1].Address].Observed)
	}
}

func currentUserAndGroup(t *testing.T) (string, string) {
	t.Helper()
	owner, err := exec.Command("id", "-un").Output()
	if err != nil {
		t.Fatal(err)
	}
	group, err := exec.Command("id", "-gn").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(owner)), strings.TrimSpace(string(group))
}

func TestNativeProviderWriteOnlyFileApplyUsesProviderPayload(t *testing.T) {
	node := writeOnlyFileNode("v1")
	runner := &recordingRunner{outputs: []Result{{Stdout: "not-a-real-ephemeral-token", Stderr: "not-a-real-ephemeral-token"}}}
	provider := NewNativeProvider(runner)
	observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(anyMapText(observed), "not-a-real-ephemeral-token") {
		t.Fatalf("observed leaked write-only content: %#v", observed)
	}
	if len(runner.inputs) == 0 || runner.inputs[0] != "not-a-real-ephemeral-token" {
		t.Fatalf("apply stdin = %#v, want write-only payload", runner.inputs)
	}
	if strings.Contains(strings.Join(runner.scripts, "\n---\n"), "not-a-real-ephemeral-token") ||
		strings.Contains(strings.Join(runner.scripts, "\n---\n"), base64.StdEncoding.EncodeToString([]byte("not-a-real-ephemeral-token"))) {
		t.Fatalf("apply script leaked write-only payload:\n%s", strings.Join(runner.scripts, "\n---\n"))
	}
	if !strings.Contains(runner.scripts[0], "mktemp") || !strings.Contains(runner.scripts[0], "trap 'rm -f --") {
		t.Fatalf("apply script missing controlled temp file cleanup:\n%s", runner.scripts[0])
	}
}

func TestNativeProviderSensitiveNftablesUsesProviderPayload(t *testing.T) {
	content := testassert.SensitiveVariableDefault
	for _, tt := range []struct {
		label string
		path  string
	}{
		{label: "main", path: "/etc/nftables.conf"},
		{label: "private", path: "/etc/nftables.d/private.nft"},
	} {
		t.Run(tt.label, func(t *testing.T) {
			node := graph.Node{
				Address: "host.server1.nftables.file[\"" + tt.label + "\"]",
				Host:    "server1",
				Kind:    "nftables_file",
				Desired: map[string]any{
					"label":          tt.label,
					"path":           tt.path,
					"content_sha256": sha256Hex([]byte(content)),
					"content_bytes":  len(content),
					"owner":          "root",
					"group":          "root",
					"mode":           "0644",
					"ensure":         "present",
					"sensitive":      true,
				},
			}
			node.ProviderPayload = cloneMap(node.Desired)
			node.ProviderPayload["content"] = content
			runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}}}
			provider := NewNativeProvider(runner)

			got, err := provider.Plan(context.Background(), node, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != ActionCreate {
				t.Fatalf("missing sensitive nftables action = %q, want create", got.Action)
			}
			observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
			if err != nil {
				t.Fatal(err)
			}
			if len(runner.inputs) != 1 || runner.inputs[0] != content {
				t.Fatalf("nftables provider input = %#v, want sensitive payload", runner.inputs)
			}
			data, err := json.Marshal(observed)
			if err != nil {
				t.Fatal(err)
			}
			output := strings.Join(runner.scripts, "\n") + string(data)
			testassert.NoSecretLeak(t, "sensitive nftables provider output", output)
			if strings.Contains(output, base64.StdEncoding.EncodeToString([]byte(content))) {
				t.Fatalf("sensitive nftables provider output contains encoded payload: %s", output)
			}
		})
	}
}
