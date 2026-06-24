package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

type MemoryBackend struct {
	mu     sync.Mutex
	states map[string]corestate.State
	locked map[string]bool
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		states: map[string]corestate.State{},
		locked: map[string]bool{},
	}
}

func (b *MemoryBackend) Read(ctx context.Context, host ir.HostSpec) (corestate.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if st, ok := b.states[host.Name]; ok {
		return cloneState(st), nil
	}
	return corestate.Empty(host.Name), nil
}

func (b *MemoryBackend) Write(ctx context.Context, host ir.HostSpec, st corestate.State) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	corestate.Normalize(&st, host.Name)
	b.states[host.Name] = cloneState(st)
	return nil
}

func (b *MemoryBackend) Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.locked[host.Name] {
		return nil, fmt.Errorf("state for host %s is already locked", host.Name)
	}
	b.locked[host.Name] = true
	return memoryLock{backend: b, host: host.Name}, nil
}

type memoryLock struct {
	backend *MemoryBackend
	host    string
}

func (l memoryLock) Unlock(ctx context.Context) error {
	l.backend.mu.Lock()
	defer l.backend.mu.Unlock()
	delete(l.backend.locked, l.host)
	return nil
}

type MemoryProvider struct {
	mu          sync.Mutex
	Observed    map[string]Observed
	Applied     []string
	Destroyed   []string
	Operations  []string
	FailApplyAt string
}

func NewMemoryProvider() *MemoryProvider {
	return &MemoryProvider{Observed: map[string]Observed{}}
}

func (p *MemoryProvider) Plan(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	observed, ok := p.Observed[node.Address]
	if !ok && prior != nil {
		observed = Observed{
			Exists:        true,
			DesiredDigest: prior.DesiredDigest,
			Values:        prior.Observed,
		}
		ok = true
	}
	if !ok {
		observed = Observed{Exists: false}
	}
	return Compare(node, prior, observed), nil
}

func (p *MemoryProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if step.Address == p.FailApplyAt {
		return nil, fmt.Errorf("injected failure")
	}
	p.Applied = append(p.Applied, step.Address)
	if step.Action == ActionDelete {
		p.Observed[step.Address] = Observed{Exists: false}
		return map[string]any{"exists": false}, nil
	}
	observed := map[string]any{
		"exists":         true,
		"desired_digest": corestate.DesiredDigest(step.Node.Desired),
	}
	if step.Node.Kind == "docker_compose_project" {
		state := stringMapValue(step.Node.Desired, "state")
		observed["state"] = state
		observed["project"] = stringMapValue(step.Node.Desired, "project")
		observed["services"] = map[string]any{"total": 1, "running": 1, "stopped": 0, "expected": []string{"web"}, "actual": []string{"web"}}
		observed["containers"] = map[string]any{"total": 1}
		observed["orphan_count"] = 0
		observed["orphan_services"] = []string{}
		if observed["state"] == "stopped" {
			observed["services"] = map[string]any{"total": 1, "running": 0, "stopped": 1, "expected": []string{"web"}, "actual": []string{"web"}}
		}
		if observed["state"] == "absent" {
			observed["exists"] = false
			observed["services"] = map[string]any{"total": 0, "running": 0, "stopped": 0, "expected": []string{"web"}, "actual": []string{}}
			observed["containers"] = map[string]any{"total": 0}
		}
	}
	p.Observed[step.Address] = Observed{
		Exists:        true,
		DesiredDigest: corestate.DesiredDigest(step.Node.Desired),
		Values:        observed,
	}
	return observed, nil
}

func (p *MemoryProvider) Destroy(ctx context.Context, step Step) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if step.Address == p.FailApplyAt {
		return fmt.Errorf("injected failure")
	}
	p.Destroyed = append(p.Destroyed, step.Address)
	p.Observed[step.Address] = Observed{Exists: false}
	return nil
}

func (p *MemoryProvider) RunOperation(ctx context.Context, operation graph.Operation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Operations = append(p.Operations, operation.Address)
	return nil
}

func cloneState(st corestate.State) corestate.State {
	out := st
	if st.Facts != nil {
		facts := *st.Facts
		out.Facts = &facts
	}
	out.Resources = make(map[string]corestate.Resource, len(st.Resources))
	for address, resource := range st.Resources {
		resource.Desired = cloneMap(resource.Desired)
		resource.Observed = cloneMap(resource.Observed)
		resource.Lifecycle = cloneLifecycle(resource.Lifecycle)
		out.Resources[address] = resource
	}
	return out
}
