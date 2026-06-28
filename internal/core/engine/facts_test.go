package engine

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
)

func TestDiscoverHostFacts(t *testing.T) {
	runner := factRunner{stdout: "hostname=server1\narchitecture=x86_64\ncodename=trixie\n"}
	facts, err := DiscoverHostFacts(context.Background(), runner, ir.HostSpec{Name: "server1"}, func() time.Time {
		return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	system := facts.System
	if system.Hostname != "server1" || system.Architecture != "amd64" || system.Codename != "trixie" {
		t.Fatalf("facts = %#v", facts)
	}
	if system.DetectedAt != "2026-06-20T12:00:00Z" {
		t.Fatalf("detected_at = %q", system.DetectedAt)
	}
}

func TestDiscoverProgramFactsRunsHostsInParallel(t *testing.T) {
	runner := &concurrencyFactRunner{}
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}, {Name: "server2"}}}

	facts, err := DiscoverProgramFacts(context.Background(), runner, program, func() time.Time {
		return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("facts = %#v, want 2 hosts", facts)
	}
	if runner.maxActive < 2 {
		t.Fatalf("max concurrent discoveries = %d, want hosts in parallel", runner.maxActive)
	}
}

func TestApplyPersistsRuntimeFactsWithoutResourceChanges(t *testing.T) {
	host := ir.HostSpec{
		Name: "server1",
		Facts: ir.HostFacts{System: ir.SystemFacts{
			Hostname:     "server1",
			Architecture: "amd64",
			Codename:     "trixie",
			DetectedAt:   "2026-06-20T12:00:00Z",
		}},
	}
	backend := NewMemoryBackend()
	engine := Engine{Backend: backend, Provider: NewMemoryProvider()}
	_, err := engine.Apply(context.Background(), &ir.Program{Hosts: []ir.HostSpec{host}}, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	st, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if st.Facts == nil || st.Facts.System.Architecture != "amd64" || st.Facts.System.Codename != "trixie" {
		t.Fatalf("state facts = %#v", st.Facts)
	}
}

type factRunner struct {
	stdout string
}

func (r factRunner) Run(ctx context.Context, host, script string) (Result, error) {
	return Result{Stdout: r.stdout}, nil
}

func (r factRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return Result{Stdout: r.stdout}, nil
}

func (r factRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

type concurrencyFactRunner struct {
	mu        sync.Mutex
	active    int
	maxActive int
}

func (r *concurrencyFactRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.mu.Lock()
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	return Result{Stdout: "hostname=" + host + "\narchitecture=amd64\ncodename=trixie\n"}, nil
}

func (r *concurrencyFactRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

func (r *concurrencyFactRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}
