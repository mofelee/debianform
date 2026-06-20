package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/graph"
	v2state "github.com/mofelee/debianform/internal/v2/state"
)

type recordingRunner struct {
	outputs []Result
	scripts []string
}

func (r *recordingRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.scripts = append(r.scripts, script)
	if len(r.outputs) == 0 {
		return Result{}, nil
	}
	out := r.outputs[0]
	r.outputs = r.outputs[1:]
	return out, nil
}

func (r *recordingRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

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

	prior := &v2state.Resource{Ownership: "managed"}
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionDelete {
		t.Fatalf("missing absent file with prior action = %q, want delete to clear state", got.Action)
	}
}

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

func TestNativeProviderAPTSigningKeyURL(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.apt.signing_key[\"tools\"]",
		Host:    "server1",
		Kind:    "apt_signing_key",
		Desired: map[string]any{
			"path":   "/etc/apt/keyrings/tools.asc",
			"url":    "https://repo.example/key.asc",
			"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"owner":  "root",
			"group":  "root",
			"mode":   "0644",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing apt signing key action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"curl -fsSL 'https://repo.example/key.asc'",
		"sha256sum --check --status",
		"install -o 'root' -g 'root' -m '0644'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("apt signing key apply script missing %q:\n%s", want, applied)
		}
	}
}

func TestSSHRunnerExpandsHomeIdentityFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	runner := NewSSHRunner(map[string]Host{
		"server1": {
			Address:      "192.0.2.10",
			IdentityFile: "~/.ssh/id_ed25519",
		},
	})

	args := runner.SSHArgs("server1")
	want := filepath.Join(home, ".ssh", "id_ed25519")
	for _, arg := range args {
		if arg == want {
			return
		}
	}
	t.Fatalf("ssh args %q do not contain expanded identity file %q", strings.Join(args, " "), want)
}
