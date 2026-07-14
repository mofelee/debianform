package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestNativeProviderPreflightHostScopesNetplanCheck(t *testing.T) {
	ubuntu := testPreflightHost("ubuntu")
	debian := testPreflightHost("debian")
	tests := []struct {
		name string
		host ir.HostSpec
		node graph.Node
	}{
		{name: "Debian structured networkd", host: debian, node: preflightNode("networkd_network", "/etc/systemd/network/20-wan.network")},
		{name: "Ubuntu unrelated file", host: ubuntu, node: preflightNode("file", "/etc/example.conf")},
		{name: "Ubuntu similar path", host: ubuntu, node: preflightNode("file", "/etc/systemd/networking/example.conf")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingRunner{}
			provider := NewNativeProvider(runner)
			if err := provider.PreflightHost(context.Background(), tt.host, []graph.Node{tt.node}); err != nil {
				t.Fatal(err)
			}
			if len(runner.scripts) != 0 {
				t.Fatalf("preflight runner calls = %d, want 0", len(runner.scripts))
			}
		})
	}
}

func TestNativeProviderPreflightHostAllowsNativeNetworkdUbuntu(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{}}}
	provider := NewNativeProvider(runner)
	node := preflightNode("networkd_netdev", "/etc/systemd/network/10-wg0.netdev")

	if err := provider.PreflightHost(context.Background(), testPreflightHost("ubuntu"), []graph.Node{node}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("preflight runner calls = %d, want 1", len(runner.scripts))
	}
	if runner.hosts[0] != "server1" {
		t.Fatalf("preflight host = %q, want server1", runner.hosts[0])
	}
	if runner.scripts[0] != netplanOwnershipScript {
		t.Fatalf("preflight script = %q, want fixed read-only ownership script", runner.scripts[0])
	}
	for _, forbidden := range []string{"netplan generate", "netplan apply", "rm ", "printf %s >"} {
		if strings.Contains(runner.scripts[0], forbidden) {
			t.Fatalf("preflight script contains forbidden mutation %q", forbidden)
		}
	}
}

func TestNativeProviderPreflightHostRejectsNetplanOwnership(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "/etc/netplan/50-cloud-init.yaml\n"}}}
	provider := NewNativeProvider(runner)
	nodes := []graph.Node{
		preflightNode("networkd_network", "/etc/systemd/network/20-wan.network"),
		preflightNode("file", "/etc/systemd/network/10-custom.network"),
	}

	err := provider.PreflightHost(context.Background(), testPreflightHost("ubuntu"), nodes)
	if err == nil {
		t.Fatal("preflight error = nil, want Netplan ownership conflict")
	}
	for _, want := range []string{
		`host "server1"`,
		"active Netplan ownership",
		"/etc/netplan/50-cloud-init.yaml",
		nodes[0].Address,
		nodes[1].Address,
		"outside DebianForm",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("preflight error %q does not contain %q", err, want)
		}
	}
}

func TestEnginePreflightRunsAfterStateReadBeforeProviderPlan(t *testing.T) {
	events := []string{}
	backend := &preflightOrderBackend{MemoryBackend: NewMemoryBackend(), events: &events}
	provider := &preflightOrderProvider{MemoryProvider: NewMemoryProvider(), events: &events}
	engine := Engine{Backend: backend, Provider: provider}
	host := testPreflightHost("ubuntu")
	node := preflightNode("file", "/tmp/example")

	if _, err := engine.Plan(context.Background(), &ir.Program{Hosts: []ir.HostSpec{host}}, &graph.ResourceGraph{Nodes: []graph.Node{node}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, ","); got != "read,preflight,plan" {
		t.Fatalf("engine events = %q, want read,preflight,plan", got)
	}
}

func TestEngineApplyStopsBeforeProviderMutationOnPreflightFailure(t *testing.T) {
	events := []string{}
	backend := &preflightOrderBackend{MemoryBackend: NewMemoryBackend(), events: &events}
	provider := &preflightOrderProvider{
		MemoryProvider: NewMemoryProvider(),
		events:         &events,
		preflightErr:   errors.New("Netplan ownership conflict"),
	}
	engine := Engine{Backend: backend, Provider: provider}
	host := testPreflightHost("ubuntu")
	node := preflightNode("networkd_network", "/etc/systemd/network/20-wan.network")

	_, err := engine.Apply(context.Background(), &ir.Program{Hosts: []ir.HostSpec{host}}, &graph.ResourceGraph{Nodes: []graph.Node{node}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "Netplan ownership conflict") {
		t.Fatalf("apply error = %v, want preflight failure", err)
	}
	if got := strings.Join(events, ","); got != "read,preflight" {
		t.Fatalf("engine events = %q, want read,preflight", got)
	}
	if len(provider.Applied) != 0 || len(provider.Destroyed) != 0 || len(provider.Operations) != 0 {
		t.Fatalf("provider mutations = applied %v destroyed %v operations %v", provider.Applied, provider.Destroyed, provider.Operations)
	}
}

func testPreflightHost(distribution string) ir.HostSpec {
	version := "24.04"
	codename := "noble"
	if distribution == "debian" {
		version = "13"
		codename = "trixie"
	}
	return ir.HostSpec{
		Name: "server1",
		Facts: ir.HostFacts{System: ir.SystemFacts{
			Distribution: distribution,
			Version:      version,
			Architecture: "amd64",
			Codename:     codename,
		}},
	}
}

func preflightNode(kind, path string) graph.Node {
	return graph.Node{
		Host:    "server1",
		Address: `host.server1.` + kind + `["` + path + `"]`,
		Kind:    kind,
		Summary: "manage " + path,
		Desired: map[string]any{"path": path, "ensure": "present"},
	}
}

type preflightOrderBackend struct {
	*MemoryBackend
	events *[]string
}

func (b *preflightOrderBackend) Read(ctx context.Context, host ir.HostSpec) (corestate.State, error) {
	*b.events = append(*b.events, "read")
	return b.MemoryBackend.Read(ctx, host)
}

type preflightOrderProvider struct {
	*MemoryProvider
	events       *[]string
	preflightErr error
}

func (p *preflightOrderProvider) PreflightHost(context.Context, ir.HostSpec, []graph.Node) error {
	*p.events = append(*p.events, "preflight")
	return p.preflightErr
}

func (p *preflightOrderProvider) Plan(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	*p.events = append(*p.events, "plan")
	return p.MemoryProvider.Plan(ctx, node, prior)
}
