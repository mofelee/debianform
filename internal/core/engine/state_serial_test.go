package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestMemoryBackendWriteReturnsCommittedSerial(t *testing.T) {
	host := ir.HostSpec{Name: "server1"}
	backend := NewMemoryBackend()
	initial := corestate.Empty(host.Name)
	initial.Serial = 4

	first, err := backend.Write(context.Background(), host, initial)
	if err != nil {
		t.Fatal(err)
	}
	if first.Serial != 5 || first.UpdatedAt == "" {
		t.Fatalf("first committed state = %#v, want serial 5 with updated_at", first)
	}
	if initial.Serial != 4 || initial.UpdatedAt != "" {
		t.Fatalf("Write mutated input state: %#v", initial)
	}
	readFirst, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if readFirst.Serial != first.Serial || readFirst.UpdatedAt != first.UpdatedAt {
		t.Fatalf("first read revision = %#v, want committed %#v", readFirst, first)
	}

	address := `host.server1.files.file["/tmp/example"]`
	first.Resources[address] = corestate.Resource{
		Host:          host.Name,
		Kind:          "file",
		Ownership:     "managed",
		DesiredDigest: "digest",
	}
	second, err := backend.Write(context.Background(), host, first)
	if err != nil {
		t.Fatal(err)
	}
	if second.Serial != 6 {
		t.Fatalf("second committed serial = %d, want 6", second.Serial)
	}
	readSecond, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if readSecond.Serial != 6 || readSecond.Resources[address].DesiredDigest != "digest" {
		t.Fatalf("second read state = %#v, want serial 6 with resource", readSecond)
	}
}

func TestExecuteResourceStepFailedWriteKeepsRevisionUntilSuccessfulRetry(t *testing.T) {
	host := ir.HostSpec{Name: "server1"}
	node := fileNode(host.Name, "/tmp/example", nil)
	address := node.Address
	oldDesired := cloneMap(node.Desired)
	oldDesired["content"] = "old"
	oldDigest := corestate.DesiredDigest(oldDesired)
	initial := corestate.Empty(host.Name)
	initial.Serial = 7
	initial.Resources[address] = corestate.Resource{
		Host:          host.Name,
		Kind:          node.Kind,
		Ownership:     "managed",
		Desired:       oldDesired,
		DesiredDigest: oldDigest,
		Observed:      map[string]any{"exists": true, "desired_digest": oldDigest},
	}
	backend := newScriptedCommitBackend(initial, 1)
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	states := map[string]corestate.State{host.Name: cloneState(initial)}
	statesLock := &sync.Mutex{}
	stateLocks := map[string]*sync.Mutex{host.Name: {}}
	step := Step{
		Address:   address,
		Host:      host.Name,
		Action:    ActionUpdate,
		Node:      node,
		Ownership: "managed",
	}

	err := engine.executeResourceStep(context.Background(), map[string]ir.HostSpec{host.Name: host}, states, statesLock, stateLocks, step)
	if err == nil || !errors.Is(err, errInjectedStateWrite) {
		t.Fatalf("first execute error = %v, want injected state write failure", err)
	}
	if states[host.Name].Serial != 7 || states[host.Name].Resources[address].DesiredDigest != oldDigest {
		t.Fatalf("caller state advanced after failed write: %#v", states[host.Name])
	}
	readFailed, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if readFailed.Serial != 7 || readFailed.Resources[address].DesiredDigest != oldDigest {
		t.Fatalf("backend state advanced after failed write: %#v", readFailed)
	}

	if err := engine.executeResourceStep(context.Background(), map[string]ir.HostSpec{host.Name: host}, states, statesLock, stateLocks, step); err != nil {
		t.Fatal(err)
	}
	readCommitted, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if readCommitted.Serial != 8 || states[host.Name].Serial != 8 {
		t.Fatalf("retry revisions = backend %d caller %d, want 8", readCommitted.Serial, states[host.Name].Serial)
	}
	if got := readCommitted.Resources[address].DesiredDigest; got != corestate.DesiredDigest(node.Desired) {
		t.Fatalf("retry resource digest = %q, want current desired digest", got)
	}
	if got := backend.candidateSerials(); len(got) != 2 || got[0] != 8 || got[1] != 8 {
		t.Fatalf("candidate serials = %#v, want [8 8]", got)
	}
}

func TestApplyAdvancesSerialForEverySuccessfulResourceWrite(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	backend := NewMemoryBackend()
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2, PerHostParallel: 2}); err != nil {
		t.Fatal(err)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if st.Serial != 2 || len(st.Resources) != 2 {
		t.Fatalf("state after two resource writes = serial %d resources %d, want 2 and 2", st.Serial, len(st.Resources))
	}
	if _, err := engine.Plan(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	next, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if next.Serial != 2 {
		t.Fatalf("serial after next read-only plan = %d, want 2", next.Serial)
	}
}

func TestRecordMultipleOperationOutputsUsesOneCommittedSerial(t *testing.T) {
	host := ir.HostSpec{Name: "server1"}
	initial := corestate.Empty(host.Name)
	initial.Serial = 10
	backend := newScriptedCommitBackend(initial, 0)
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	states := map[string]corestate.State{host.Name: cloneState(initial)}
	statesLock := &sync.Mutex{}
	stateLocks := map[string]*sync.Mutex{host.Name: {}}
	first := `host.server1.components.app.script["render"].outputs["/tmp/a"]`
	second := `host.server1.components.app.script["render"].outputs["/tmp/b"]`
	step := OperationStep{
		Address: `host.server1.components.app.script["render"]`,
		Operation: graph.Operation{
			Host:    "server1",
			Address: `host.server1.components.app.script["render"]`,
			ScriptPayload: &graph.ScriptPayload{
				Name:          "render",
				ComponentName: "app",
				Outputs: []graph.ScriptOutputPayload{
					{Address: first, Path: "/tmp/a", ProviderAddress: "component_script_output.server1_a"},
					{Address: second, Path: "/tmp/b", ProviderAddress: "component_script_output.server1_b"},
				},
			},
		},
	}
	result := OperationResult{Outputs: map[string]map[string]any{
		first:  {"exists": true, "path": "/tmp/a", "sha256": "a"},
		second: {"exists": true, "path": "/tmp/b", "sha256": "b"},
	}}

	if err := engine.recordOperationOutputs(context.Background(), map[string]ir.HostSpec{host.Name: host}, states, statesLock, stateLocks, step, result); err != nil {
		t.Fatal(err)
	}
	if got := backend.successfulWrites(); got != 1 {
		t.Fatalf("successful state writes = %d, want 1 for one operation result", got)
	}
	if states[host.Name].Serial != 11 || len(states[host.Name].Resources) != 2 {
		t.Fatalf("caller state = serial %d resources %d, want 11 and 2", states[host.Name].Serial, len(states[host.Name].Resources))
	}
	read, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if read.Serial != 11 || len(read.Resources) != 2 {
		t.Fatalf("backend state = serial %d resources %d, want 11 and 2", read.Serial, len(read.Resources))
	}
}

func TestRecordOperationOutputsUsesExplicitHost(t *testing.T) {
	host := ir.HostSpec{Name: "web.example.com"}
	initial := corestate.Empty(host.Name)
	backend := newScriptedCommitBackend(initial, 0)
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	states := map[string]corestate.State{host.Name: cloneState(initial)}
	statesLock := &sync.Mutex{}
	stateLocks := map[string]*sync.Mutex{host.Name: {}}
	outputAddress := `host.web.example.com.components.app.script["render"].outputs["/tmp/result"]`
	step := OperationStep{
		Address: `host.web.example.com.components.app.script["render"]`,
		Operation: graph.Operation{
			Host:    host.Name,
			Address: `host.web.example.com.components.app.script["render"]`,
		},
	}
	result := OperationResult{Outputs: map[string]map[string]any{
		outputAddress: {"exists": true, "path": "/tmp/result"},
	}}

	if err := engine.recordOperationOutputs(context.Background(), map[string]ir.HostSpec{host.Name: host}, states, statesLock, stateLocks, step, result); err != nil {
		t.Fatal(err)
	}
	resource := states[host.Name].Resources[outputAddress]
	if resource.Host != host.Name {
		t.Fatalf("output resource host = %q, want explicit host %q", resource.Host, host.Name)
	}
}

var errInjectedStateWrite = errors.New("injected state write failure")

type scriptedCommitBackend struct {
	*MemoryBackend

	mu                sync.Mutex
	state             corestate.State
	failuresRemaining int
	candidates        []int
	successes         int
}

func newScriptedCommitBackend(st corestate.State, failures int) *scriptedCommitBackend {
	return &scriptedCommitBackend{
		MemoryBackend:     NewMemoryBackend(),
		state:             cloneState(st),
		failuresRemaining: failures,
	}
}

func (b *scriptedCommitBackend) Read(context.Context, ir.HostSpec) (corestate.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return cloneState(b.state), nil
}

func (b *scriptedCommitBackend) Write(_ context.Context, host ir.HostSpec, st corestate.State) (corestate.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	committed, err := corestate.PrepareWrite(st, host.Name)
	if err != nil {
		return corestate.State{}, err
	}
	b.candidates = append(b.candidates, committed.Serial)
	if b.failuresRemaining > 0 {
		b.failuresRemaining--
		return corestate.State{}, errInjectedStateWrite
	}
	b.state = cloneState(committed)
	b.successes++
	return cloneState(committed), nil
}

func (b *scriptedCommitBackend) candidateSerials() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]int(nil), b.candidates...)
}

func (b *scriptedCommitBackend) successfulWrites() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.successes
}

var _ Backend = (*scriptedCommitBackend)(nil)
