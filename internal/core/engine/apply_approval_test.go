package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestApplyBeforeExecuteRejectsNewDestroyWithoutWriting(t *testing.T) {
	host := ir.HostSpec{
		Name: "server1",
		Facts: ir.HostFacts{System: ir.SystemFacts{
			Hostname:     "server1",
			Architecture: "amd64",
			Codename:     "trixie",
			DetectedAt:   "2026-07-10T12:00:00Z",
		}},
	}
	program := &ir.Program{Hosts: []ir.HostSpec{host}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{fileNode(host.Name, "/tmp/approval-race", nil)}}
	backend := newApprovalTrackingBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}

	preview, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Steps) != 1 || preview.Steps[0].Action != ActionCreate {
		t.Fatalf("preview steps = %#v, want one create", preview.Steps)
	}

	orphanAddress := `host.server1.packages.install["curl"]`
	if _, err := backend.Write(context.Background(), host, corestate.State{
		Version: corestate.Version,
		Host:    host.Name,
		Resources: map[string]corestate.Resource{
			orphanAddress: {
				Host:          host.Name,
				Kind:          "package",
				Ownership:     "managed",
				DesiredDigest: "old",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	backend.resetWrites()

	rejected := errors.New("locked plan rejected")
	callbackCalls := 0
	actual, err := engine.Apply(context.Background(), program, resourceGraph, Options{
		BeforeExecute: func(_ context.Context, plan Plan) error {
			callbackCalls++
			if !backend.isLocked(host.Name) {
				t.Fatal("BeforeExecute ran without the host state lock")
			}
			if backend.writeCount() != 0 {
				t.Fatalf("state writes before approval = %d, want 0", backend.writeCount())
			}
			if !approvalPlanHasStep(plan, orphanAddress, ActionDestroy) {
				t.Fatalf("locked plan does not contain the new destroy for %s", orphanAddress)
			}
			return rejected
		},
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("apply error = %v, want approval rejection", err)
	}
	if callbackCalls != 1 {
		t.Fatalf("BeforeExecute calls = %d, want 1", callbackCalls)
	}
	if !approvalPlanHasStep(actual, orphanAddress, ActionDestroy) {
		t.Fatal("returned plan does not contain the rejected locked destroy")
	}
	if writes := backend.writeCount(); writes != 0 {
		t.Fatalf("state writes after rejection = %d, want 0", writes)
	}
	if len(provider.Applied) != 0 || len(provider.Destroyed) != 0 || len(provider.Operations) != 0 {
		t.Fatalf("provider mutations after rejection: applied=%#v destroyed=%#v operations=%#v", provider.Applied, provider.Destroyed, provider.Operations)
	}
	st, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; !ok {
		t.Fatalf("rejected orphan was removed from state: %#v", st.Resources)
	}
	if _, ok := st.Resources[preview.Steps[0].Address]; ok {
		t.Fatalf("rejected create was added to state: %#v", st.Resources)
	}
	lock, err := backend.Lock(context.Background(), host, time.Second)
	if err != nil {
		t.Fatalf("state lock was not released after rejection: %v", err)
	}
	if err := lock.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestApplyBeforeExecuteReviewsNoOpBeforePersistingFacts(t *testing.T) {
	host := ir.HostSpec{
		Name: "server1",
		Facts: ir.HostFacts{System: ir.SystemFacts{
			Hostname:     "server1",
			Architecture: "amd64",
			Codename:     "trixie",
			DetectedAt:   "2026-07-10T12:00:00Z",
		}},
	}
	backend := newApprovalTrackingBackend()
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	callbackCalls := 0

	plan, err := engine.Apply(context.Background(), &ir.Program{Hosts: []ir.HostSpec{host}}, &graph.ResourceGraph{}, Options{
		BeforeExecute: func(_ context.Context, plan Plan) error {
			callbackCalls++
			if len(plan.Steps) != 0 || len(plan.Operations) != 0 {
				t.Fatalf("no-op plan = %#v", plan)
			}
			if writes := backend.writeCount(); writes != 0 {
				t.Fatalf("state writes before no-op approval = %d, want 0", writes)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if callbackCalls != 1 {
		t.Fatalf("BeforeExecute calls = %d, want 1", callbackCalls)
	}
	if len(plan.Steps) != 0 || len(plan.Operations) != 0 {
		t.Fatalf("apply plan = %#v, want no-op", plan)
	}
	if writes := backend.writeCount(); writes != 1 {
		t.Fatalf("facts state writes = %d, want 1 after approval", writes)
	}
	st, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if st.Facts == nil || st.Facts.System.Codename != "trixie" {
		t.Fatalf("persisted facts = %#v", st.Facts)
	}
}

type approvalTrackingBackend struct {
	*MemoryBackend
	mu     sync.Mutex
	writes int
}

func newApprovalTrackingBackend() *approvalTrackingBackend {
	return &approvalTrackingBackend{MemoryBackend: NewMemoryBackend()}
}

func (b *approvalTrackingBackend) Write(ctx context.Context, host ir.HostSpec, st corestate.State) (corestate.State, error) {
	b.mu.Lock()
	b.writes++
	b.mu.Unlock()
	return b.MemoryBackend.Write(ctx, host, st)
}

func (b *approvalTrackingBackend) resetWrites() {
	b.mu.Lock()
	b.writes = 0
	b.mu.Unlock()
}

func (b *approvalTrackingBackend) writeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writes
}

func (b *approvalTrackingBackend) isLocked(host string) bool {
	b.MemoryBackend.mu.Lock()
	defer b.MemoryBackend.mu.Unlock()
	return b.MemoryBackend.locked[host]
}

func approvalPlanHasStep(plan Plan, address, action string) bool {
	for _, step := range plan.Steps {
		if step.Address == address && step.Action == action {
			return true
		}
	}
	return false
}
