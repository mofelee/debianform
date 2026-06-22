package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	for address, resource := range st.Resources {
		if resource.ProviderAddress == "" {
			t.Fatalf("state resource %s has no provider address", address)
		}
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

func TestApplyForgetsAdoptedOrphanWithoutDestroy(t *testing.T) {
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
				Ownership:     "adopted",
				DesiredDigest: "old",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("adopted orphan still in state: %#v", st.Resources)
	}
}

func TestApplyForgetsAPTSourceFileOrphanWhenDestroyKeeps(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.apt.source_file["main"]`
	if err := backend.Write(context.Background(), program.Hosts[0], v2state.State{
		Version: v2state.Version,
		Host:    "bbr1",
		Resources: map[string]v2state.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "apt_source_file",
				Ownership:     "managed",
				DesiredDigest: "old",
				Desired:       map[string]any{"path": "/etc/apt/sources.list", "on_destroy": "keep"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
}

func TestBBRMemoryApplyIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	engine := Engine{Backend: NewMemoryBackend(), Provider: NewMemoryProvider()}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 {
		t.Fatalf("second BBR plan steps = %#v, want no-op", next.Steps)
	}
}

func TestPlanPropagatesObservedReadFailure(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	engine := Engine{
		Backend:  NewMemoryBackend(),
		Provider: planErrorProvider{MemoryProvider: NewMemoryProvider()},
	}

	_, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err == nil || !strings.Contains(err.Error(), "injected observed read failure") {
		t.Fatalf("plan error = %v, want observed read failure", err)
	}
}

func TestApplyRunsHostsInParallelWithDeterministicResultOrder(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server2" {
  files {
    file "/tmp/server2" {
      content = "server2"
    }
  }
}

host "server1" {
  files {
    file "/tmp/server1" {
      content = "server1"
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2})
	if err != nil {
		t.Fatal(err)
	}
	if provider.maxActive < 2 {
		t.Fatalf("max concurrent applies = %d, want at least 2", provider.maxActive)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v, want 2", plan.Steps)
	}
	if plan.Steps[0].Host != "server1" || plan.Steps[1].Host != "server2" {
		t.Fatalf("step host order = %s, %s; want server1, server2", plan.Steps[0].Host, plan.Steps[1].Host)
	}
	for _, host := range program.Hosts {
		st, err := backend.Read(context.Background(), host)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Resources) != 1 {
			t.Fatalf("host %s state resources = %#v, want 1", host.Name, st.Resources)
		}
	}
}

func TestApplyHonorsDefaultPerHostSerialExecution(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive != 1 {
		t.Fatalf("max concurrent applies = %d, want default per-host serial execution", provider.maxActive)
	}
}

func TestApplyAllowsSafeParallelWithinHostWhenConfigured(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2, PerHostParallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive < 2 {
		t.Fatalf("max concurrent applies = %d, want safe same-host resources to run in parallel", provider.maxActive)
	}
}

func TestApplyGlobalParallelLimit(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}, {Name: "server2"}, {Name: "server3"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/server1", nil),
		fileNode("server2", "/tmp/server2", nil),
		fileNode("server3", "/tmp/server3", nil),
	}}
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive != 2 {
		t.Fatalf("max concurrent applies = %d, want global limit 2", provider.maxActive)
	}
}

func TestApplySkipsDependentsAfterFailure(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	failing := `host.server1.files.file["/tmp/failing"]`
	dependent := `host.server1.files.file["/tmp/dependent"]`
	independent := `host.server1.files.file["/tmp/independent"]`
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/failing", nil),
		fileNode("server1", "/tmp/independent", nil),
		fileNode("server1", "/tmp/dependent", []string{failing}),
	}}
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	provider.FailApplyAt = failing
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2, PerHostParallel: 2})
	if err == nil || !strings.Contains(err.Error(), failing) {
		t.Fatalf("apply error = %v, want failing resource", err)
	}
	if containsString(provider.Applied, dependent) {
		t.Fatalf("dependent resource was applied after failed dependency: %#v", provider.Applied)
	}
	if !containsString(provider.Applied, independent) {
		t.Fatalf("independent resource was not applied after unrelated failure: %#v", provider.Applied)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[independent]; !ok {
		t.Fatalf("independent resource was not persisted: %#v", st.Resources)
	}
	if _, ok := st.Resources[dependent]; ok {
		t.Fatalf("dependent resource was persisted despite skipped dependency: %#v", st.Resources)
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

func twoFileProgramAndGraph(host string) (*ir.Program, *graph.ResourceGraph) {
	return &ir.Program{Hosts: []ir.HostSpec{{Name: host}}}, &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode(host, "/tmp/a", nil),
		fileNode(host, "/tmp/b", nil),
	}}
}

func fileNode(host, path string, deps []string) graph.Node {
	return graph.Node{
		Host:      host,
		Address:   "host." + host + ".files.file[" + fmt.Sprintf("%q", path) + "]",
		Kind:      "file",
		Summary:   "create file " + path,
		DependsOn: deps,
		Desired: map[string]any{
			"path":    path,
			"content": path,
			"owner":   "root",
			"group":   "root",
			"mode":    "0644",
			"ensure":  "present",
		},
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

type planErrorProvider struct {
	*MemoryProvider
}

func (p planErrorProvider) Plan(ctx context.Context, node graph.Node, prior *v2state.Resource) (ProviderPlan, error) {
	return ProviderPlan{}, fmt.Errorf("injected observed read failure")
}

type concurrencyProvider struct {
	*MemoryProvider
	mu        sync.Mutex
	active    int
	maxActive int
}

func (p *concurrencyProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.MemoryProvider.Apply(ctx, step)
}
