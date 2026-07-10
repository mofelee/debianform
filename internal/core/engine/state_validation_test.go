package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestEngineRejectsIncompatibleOrForeignStateBeforeProviderOrWrite(t *testing.T) {
	address := `host.server1.files.file["/tmp/example"]`
	tests := []struct {
		name    string
		state   corestate.State
		wantErr string
	}{
		{
			name:    "missing version",
			state:   corestate.State{Host: "server1", Resources: map[string]corestate.Resource{}},
			wantErr: "unsupported version 0",
		},
		{
			name:    "old version",
			state:   corestate.State{Version: 1, Host: "server1", Resources: map[string]corestate.Resource{}},
			wantErr: "unsupported version 1",
		},
		{
			name:    "newer version",
			state:   corestate.State{Version: corestate.Version + 1, Host: "server1", Resources: map[string]corestate.Resource{}},
			wantErr: "newer version 3",
		},
		{
			name:    "foreign host",
			state:   corestate.State{Version: corestate.Version, Host: "server2", Resources: map[string]corestate.Resource{}},
			wantErr: `state host "server2" does not match requested host "server1"`,
		},
		{
			name: "foreign resource host",
			state: corestate.State{Version: corestate.Version, Host: "server1", Resources: map[string]corestate.Resource{
				address: {Host: "server2", Kind: "file", Ownership: "managed", DesiredDigest: "digest"},
			}},
			wantErr: `belongs to host "server2", expected "server1"`,
		},
	}

	for _, tt := range tests {
		for _, mode := range []string{"plan", "apply"} {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				backend := newStateValidationBackend(tt.state)
				provider := &stateValidationProvider{}
				engine := Engine{Backend: backend, Provider: provider}
				program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
				resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode("server1", "/tmp/example", nil)}}

				var err error
				if mode == "plan" {
					_, err = engine.Plan(context.Background(), program, resourceGraph, Options{})
				} else {
					_, err = engine.Apply(context.Background(), program, resourceGraph, Options{})
				}
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("%s error = %v, want containing %q", mode, err, tt.wantErr)
				}
				if got := backend.writeCount(); got != 0 {
					t.Fatalf("state writes = %d, want 0", got)
				}
				if got := provider.callCount(); got != 0 {
					t.Fatalf("provider calls = %d, want 0", got)
				}
			})
		}
	}
}

func TestApplyValidatesStateAgainBeforePersistingFacts(t *testing.T) {
	host := ir.HostSpec{
		Name: "server1",
		Facts: ir.HostFacts{System: ir.SystemFacts{
			Architecture: "amd64",
			Codename:     "trixie",
		}},
	}
	backend := newStateValidationBackend(
		corestate.Empty(host.Name),
		corestate.State{Version: corestate.Version + 1, Host: host.Name, Resources: map[string]corestate.Resource{}},
	)
	provider := &stateValidationProvider{}
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), &ir.Program{Hosts: []ir.HostSpec{host}}, &graph.ResourceGraph{}, Options{})
	if err == nil || !strings.Contains(err.Error(), "newer version 3") {
		t.Fatalf("Apply() error = %v, want newer state version", err)
	}
	if got := backend.writeCount(); got != 0 {
		t.Fatalf("state writes = %d, want 0", got)
	}
	if got := provider.callCount(); got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
}

func TestEngineNormalizesCompatibleEmptyResourceHost(t *testing.T) {
	address := `host.server1.files.file["/tmp/orphan"]`
	backend := newStateValidationBackend(corestate.State{
		Version: corestate.Version,
		Host:    "server1",
		Resources: map[string]corestate.Resource{
			address: {Kind: "file", Ownership: "managed", DesiredDigest: "digest"},
		},
	})
	engine := Engine{Backend: backend, Provider: &stateValidationProvider{}}

	plan, err := engine.Plan(
		context.Background(),
		&ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}},
		&graph.ResourceGraph{},
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Prior == nil {
		t.Fatalf("plan steps = %#v, want one orphan", plan.Steps)
	}
	if got := plan.Steps[0].Prior.Host; got != "server1" {
		t.Fatalf("normalized prior host = %q, want server1", got)
	}
}

func TestMemoryBackendWriteRejectsIncompatibleOrForeignState(t *testing.T) {
	host := ir.HostSpec{Name: "server1"}
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
			backend := NewMemoryBackend()
			if err := backend.Write(context.Background(), host, tt.state); err == nil {
				t.Fatal("Write() succeeded, want state validation error")
			}
			st, err := backend.Read(context.Background(), host)
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Resources) != 0 || st.Serial != 0 {
				t.Fatalf("stored state = %#v, want unchanged empty state", st)
			}
		})
	}
}

type stateValidationBackend struct {
	*MemoryBackend

	mu     sync.Mutex
	states []corestate.State
	reads  int
	writes int
}

func newStateValidationBackend(states ...corestate.State) *stateValidationBackend {
	return &stateValidationBackend{MemoryBackend: NewMemoryBackend(), states: states}
}

func (b *stateValidationBackend) Read(context.Context, ir.HostSpec) (corestate.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	index := b.reads
	if index >= len(b.states) {
		index = len(b.states) - 1
	}
	b.reads++
	return cloneState(b.states[index]), nil
}

func (b *stateValidationBackend) Write(context.Context, ir.HostSpec, corestate.State) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes++
	return nil
}

func (b *stateValidationBackend) writeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writes
}

type stateValidationProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *stateValidationProvider) record() {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
}

func (p *stateValidationProvider) Plan(context.Context, graph.Node, *corestate.Resource) (ProviderPlan, error) {
	p.record()
	return ProviderPlan{Action: ActionNoOp}, nil
}

func (p *stateValidationProvider) Apply(context.Context, Step) (map[string]any, error) {
	p.record()
	return nil, nil
}

func (p *stateValidationProvider) Destroy(context.Context, Step) error {
	p.record()
	return nil
}

func (p *stateValidationProvider) RunOperation(context.Context, graph.Operation) (OperationResult, error) {
	p.record()
	return OperationResult{}, nil
}

func (p *stateValidationProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

var _ Backend = (*stateValidationBackend)(nil)
var _ Provider = (*stateValidationProvider)(nil)
