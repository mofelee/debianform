package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/v2/graph"
	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/merge"
	"github.com/mofelee/debianform/internal/v2/parser"
	v2state "github.com/mofelee/debianform/internal/v2/state"
)

func TestCompareActionMatrix(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.packages.install[\"curl\"]",
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{"name": "curl", "ensure": "present"},
	}
	digest := v2state.DesiredDigest(node.Desired)
	prior := &v2state.Resource{DesiredDigest: digest, Ownership: "managed"}

	tests := []struct {
		name     string
		prior    *v2state.Resource
		observed Observed
		want     string
	}{
		{name: "missing remote creates", observed: Observed{Exists: false}, want: ActionCreate},
		{name: "matching managed is no-op", prior: prior, observed: Observed{Exists: true, DesiredDigest: digest}, want: ActionNoOp},
		{name: "matching unmanaged adopts", observed: Observed{Exists: true, DesiredDigest: digest}, want: ActionAdopt},
		{name: "different remote updates", prior: prior, observed: Observed{Exists: true, DesiredDigest: "old"}, want: ActionUpdate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compare(node, tt.prior, tt.observed)
			if got.Action != tt.want {
				t.Fatalf("action = %q, want %q", got.Action, tt.want)
			}
		})
	}

	absent := node
	absent.Desired = map[string]any{"name": "curl", "ensure": "absent"}
	if got := Compare(absent, prior, Observed{Exists: true, DesiredDigest: digest}); got.Action != ActionDelete {
		t.Fatalf("absent desired action = %q, want delete", got.Action)
	}
}

func TestApplyWithMemoryProviderIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/v2-foundation.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Create == 0 {
		t.Fatalf("initial apply summary = %#v, want creates", plan.Summary)
	}
	if len(provider.Applied) == 0 {
		t.Fatalf("provider did not apply any resources")
	}
	if len(provider.Operations) != 1 || provider.Operations[0] != "host.foundation1.systemd.daemon_reload" {
		t.Fatalf("operations = %#v, want daemon_reload", provider.Operations)
	}

	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 || next.Summary.Create != 0 || next.Summary.Update != 0 || next.Summary.Delete != 0 {
		t.Fatalf("second plan should be no-op, got steps=%#v summary=%#v", next.Steps, next.Summary)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := v2state.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "not-a-real-secret-token") {
		t.Fatalf("state leaked secret content:\n%s", string(data))
	}
}

func TestApplyPersistsOnlySuccessfulStepsOnFailure(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	provider.FailApplyAt = `host.bbr1.kernel.sysctl["net.core.default_qdisc"]`
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err == nil {
		t.Fatal("apply succeeded, want injected failure")
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[`host.bbr1.kernel.module["tcp_bbr"]`]; !ok {
		t.Fatalf("successful dependency was not persisted: %#v", st.Resources)
	}
	if _, ok := st.Resources[`host.bbr1.kernel.sysctl["net.core.default_qdisc"]`]; ok {
		t.Fatalf("failed step was persisted: %#v", st.Resources)
	}
}

func TestApplyDestroysOrphanStateResource(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.packages.install["curl"]`
	if err := backend.Write(context.Background(), program.Hosts[0], v2state.State{
		Version: v2state.Version,
		Host:    "bbr1",
		Resources: map[string]v2state.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "package",
				Ownership:     "managed",
				DesiredDigest: "old",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Destroyed) != 1 || provider.Destroyed[0] != orphanAddress {
		t.Fatalf("destroyed = %#v, want %s", provider.Destroyed, orphanAddress)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("orphan still in state: %#v", st.Resources)
	}
}

func TestPreventDestroyBlocksOrphanDestroy(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  files {
    file "/tmp/protected" {
      content = "managed"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}

	emptyProgram, emptyGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {}
`))
	_, err := engine.Apply(context.Background(), emptyProgram, emptyGraph, Options{})
	if err == nil {
		t.Fatal("apply succeeded, want prevent_destroy error")
	}
	if !strings.Contains(err.Error(), "lifecycle.prevent_destroy") {
		t.Fatalf("error = %v, want prevent_destroy", err)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no destroy", provider.Destroyed)
	}
}

func TestPreventDestroyBlocksExplicitDelete(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  files {
    file "/tmp/protected" {
      ensure = "absent"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`))
	provider := NewMemoryProvider()
	address := `host.server1.files.file["/tmp/protected"]`
	provider.Observed[address] = Observed{Exists: true}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	_, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err == nil {
		t.Fatal("plan succeeded, want prevent_destroy error")
	}
	if !strings.Contains(err.Error(), "lifecycle.prevent_destroy") {
		t.Fatalf("error = %v, want prevent_destroy", err)
	}
}

func fixtureProgramAndGraph(t *testing.T, fixture string) (*ir.Program, *graph.ResourceGraph) {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := graph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return program, resourceGraph
}

func writeEngineConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}
