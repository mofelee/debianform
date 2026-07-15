package engine

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/termstyle"
)

func TestDiscoverHostFacts(t *testing.T) {
	tests := []struct {
		name         string
		stdout       string
		distribution string
		version      string
		architecture string
		codename     string
	}{
		{
			name:         "Debian 12",
			stdout:       "hostname=server1\ndistribution=debian\nversion=12\narchitecture=x86_64\ncodename=bookworm\n",
			distribution: "debian",
			version:      "12",
			architecture: "amd64",
			codename:     "bookworm",
		},
		{
			name:         "Ubuntu 24.04",
			stdout:       "hostname=server1\ndistribution=ubuntu\nversion=24.04\narchitecture=amd64\ncodename=noble\n",
			distribution: "ubuntu",
			version:      "24.04",
			architecture: "amd64",
			codename:     "noble",
		},
		{
			name:         "Ubuntu 26.04",
			stdout:       "hostname=server1\ndistribution=ubuntu\nversion=26.04\narchitecture=x86_64\ncodename=resolute\n",
			distribution: "ubuntu",
			version:      "26.04",
			architecture: "amd64",
			codename:     "resolute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts, err := DiscoverHostFacts(context.Background(), factRunner{stdout: tt.stdout}, ir.HostSpec{Name: "server1"}, func() time.Time {
				return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			})
			if err != nil {
				t.Fatal(err)
			}
			system := facts.System
			if system.Hostname != "server1" || system.Distribution != tt.distribution || system.Version != tt.version || system.Architecture != tt.architecture || system.Codename != tt.codename {
				t.Fatalf("facts = %#v", facts)
			}
			if system.DetectedAt != "2026-06-20T12:00:00Z" {
				t.Fatalf("detected_at = %q", system.DetectedAt)
			}
		})
	}
}

func TestDiscoverHostFactsRejectsUnsupportedTargets(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		wantErr string
	}{
		{name: "missing distribution", stdout: "hostname=server1\nversion=24.04\narchitecture=amd64\ncodename=noble\n", wantErr: "distribution is empty"},
		{name: "Ubuntu 22.04", stdout: "hostname=server1\ndistribution=ubuntu\nversion=22.04\narchitecture=amd64\ncodename=jammy\n", wantErr: "unsupported target platform"},
		{name: "Ubuntu 26.04 with noble", stdout: "hostname=server1\ndistribution=ubuntu\nversion=26.04\narchitecture=amd64\ncodename=noble\n", wantErr: `reports codename "noble", want "resolute"`},
		{name: "Fedora", stdout: "hostname=server1\ndistribution=fedora\nversion=42\narchitecture=amd64\ncodename=rawhide\n", wantErr: "unsupported target platform"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DiscoverHostFacts(context.Background(), factRunner{stdout: tt.stdout}, ir.HostSpec{Name: "server1"}, time.Now)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("DiscoverHostFacts() error = %v, want containing %q", err, tt.wantErr)
			}
		})
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

func TestDiscoverProgramFactsDefaultParallelLimit(t *testing.T) {
	runner := &concurrencyFactRunner{}
	program := &ir.Program{Hosts: make([]ir.HostSpec, 8)}
	for i := range program.Hosts {
		program.Hosts[i] = ir.HostSpec{Name: "server" + string(rune('1'+i))}
	}

	facts, err := DiscoverProgramFacts(context.Background(), runner, program, func() time.Time {
		return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 8 {
		t.Fatalf("facts = %#v, want 8 hosts", facts)
	}
	if runner.maxActive != defaultHostParallel() {
		t.Fatalf("max concurrent discoveries = %d, want default %d", runner.maxActive, defaultHostParallel())
	}
}

func TestDiscoverProgramFactsHonorsParallelLimit(t *testing.T) {
	runner := &concurrencyFactRunner{}
	program := &ir.Program{Hosts: []ir.HostSpec{
		{Name: "server1"},
		{Name: "server2"},
		{Name: "server3"},
	}}

	facts, err := DiscoverProgramFactsWithOptions(context.Background(), runner, program, func() time.Time {
		return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	}, nil, termstyle.Options{}, DiscoverProgramFactsOptions{Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 3 {
		t.Fatalf("facts = %#v, want 3 hosts", facts)
	}
	if runner.maxActive != 1 {
		t.Fatalf("max concurrent discoveries = %d, want 1", runner.maxActive)
	}
}

func TestApplyPersistsRuntimeFactsWithoutResourceChanges(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		codename string
	}{
		{name: "Ubuntu 24.04", version: "24.04", codename: "noble"},
		{name: "Ubuntu 26.04", version: "26.04", codename: "resolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := ir.HostSpec{
				Name: "server1",
				Facts: ir.HostFacts{System: ir.SystemFacts{
					Hostname:     "server1",
					Distribution: "ubuntu",
					Version:      tt.version,
					Architecture: "amd64",
					Codename:     tt.codename,
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
			if st.Facts == nil || st.Facts.System.Distribution != "ubuntu" || st.Facts.System.Version != tt.version || st.Facts.System.Architecture != "amd64" || st.Facts.System.Codename != tt.codename {
				t.Fatalf("state facts = %#v", st.Facts)
			}
			if st.Serial != 1 {
				t.Fatalf("facts-only state serial = %d, want 1", st.Serial)
			}
		})
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
	return Result{Stdout: "hostname=" + host + "\ndistribution=debian\nversion=13\narchitecture=amd64\ncodename=trixie\n"}, nil
}

func (r *concurrencyFactRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

func (r *concurrencyFactRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}
