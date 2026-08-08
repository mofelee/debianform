package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestExecutionWavesReverseDependenciesForActiveDeletes(t *testing.T) {
	packageAddress := `host.server1.packages.install["vault"]`
	fileAddress := `host.server1.files.file["/etc/vault.d/vault.hcl"]`
	serviceAddress := `host.server1.services.service["vault"]`
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		{Host: "server1", Address: packageAddress, Kind: "package"},
		{Host: "server1", Address: fileAddress, Kind: "file", DependsOn: []string{packageAddress}},
		{Host: "server1", Address: serviceAddress, Kind: "service", DependsOn: []string{fileAddress}},
	}}
	plan := Plan{Steps: []Step{
		{Address: packageAddress, Host: "server1", Action: ActionDelete, Node: resourceGraph.Nodes[0]},
		{Address: fileAddress, Host: "server1", Action: ActionDelete, Node: resourceGraph.Nodes[1]},
		{Address: serviceAddress, Host: "server1", Action: ActionDelete, Node: resourceGraph.Nodes[2]},
	}}
	waves, err := executionWaves(resourceGraph, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := executionWaveAddresses(waves); !reflect.DeepEqual(got, [][]string{{serviceAddress}, {fileAddress}, {packageAddress}}) {
		t.Fatalf("delete waves = %#v", got)
	}
}

func TestExecutionWavesReversePersistedDependenciesForOrphans(t *testing.T) {
	packageAddress := `host.server1.packages.install["vault"]`
	fileAddress := `host.server1.files.file["/etc/vault.d/vault.hcl"]`
	serviceAddress := `host.server1.services.service["vault"]`
	plan := Plan{Steps: []Step{
		{Address: packageAddress, Host: "server1", Action: ActionDestroy, Prior: &corestate.Resource{Kind: "package"}},
		{Address: fileAddress, Host: "server1", Action: ActionDestroy, Prior: &corestate.Resource{Kind: "file", DependsOn: []string{packageAddress}}},
		{Address: serviceAddress, Host: "server1", Action: ActionDestroy, Prior: &corestate.Resource{Kind: "service", DependsOn: []string{fileAddress}}},
	}}
	waves, err := executionWaves(&graph.ResourceGraph{}, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got := executionWaveAddresses(waves); !reflect.DeepEqual(got, [][]string{{serviceAddress}, {fileAddress}, {packageAddress}}) {
		t.Fatalf("orphan destroy waves = %#v", got)
	}
}

func TestResourceStatePersistsDependencyOrdering(t *testing.T) {
	dependency := `host.server1.packages.install["vault"]`
	step := Step{Node: graph.Node{DependsOn: []string{dependency}, Desired: map[string]any{"path": "/tmp/app"}}}
	state := resourceStateForStep(step, nil, "2026-08-08T00:00:00Z")
	if !reflect.DeepEqual(state.DependsOn, []string{dependency}) {
		t.Fatalf("state depends_on = %#v", state.DependsOn)
	}
}

func TestApplySynchronizesDependenciesForNoOpResources(t *testing.T) {
	host := ir.HostSpec{Name: "server1"}
	packageAddress := `host.server1.packages.install["vault"]`
	fileAddress := `host.server1.files.file["/etc/vault.d/vault.hcl"]`
	staleAddress := `host.server1.packages.install["old-vault"]`
	packageDesired := map[string]any{"name": "vault", "ensure": "present"}
	fileDesired := map[string]any{"path": "/etc/vault.d/vault.hcl", "ensure": "present"}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		{Host: host.Name, Address: packageAddress, Kind: "package", Desired: packageDesired},
		{Host: host.Name, Address: fileAddress, Kind: "file", Desired: fileDesired, DependsOn: []string{packageAddress}},
	}}
	st := corestate.Empty(host.Name)
	st.Resources[packageAddress] = noOpDependencyState(host.Name, "package", packageDesired, []string{staleAddress})
	st.Resources[fileAddress] = noOpDependencyState(host.Name, "file", fileDesired, []string{staleAddress})
	backend := NewMemoryBackend()
	if _, err := backend.Write(context.Background(), host, st); err != nil {
		t.Fatal(err)
	}
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	plan, err := engine.Apply(context.Background(), &ir.Program{Hosts: []ir.HostSpec{host}}, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 0 || len(provider.Applied) != 0 || len(provider.Destroyed) != 0 {
		t.Fatalf("dependency-only apply caused provider work: plan=%#v applied=%#v destroyed=%#v", plan, provider.Applied, provider.Destroyed)
	}
	got, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{packageAddress}; !reflect.DeepEqual(got.Resources[fileAddress].DependsOn, want) {
		t.Fatalf("persisted dependencies = %#v, want %#v", got.Resources[fileAddress].DependsOn, want)
	}
	if dependencies := got.Resources[packageAddress].DependsOn; len(dependencies) != 0 {
		t.Fatalf("removed dependencies remained in state: %#v", dependencies)
	}
}

func noOpDependencyState(host, kind string, desired map[string]any, dependsOn []string) corestate.Resource {
	digest := corestate.DesiredDigest(desired)
	return corestate.Resource{
		Host:          host,
		Kind:          kind,
		Ownership:     "managed",
		Desired:       desired,
		DesiredDigest: digest,
		Observed:      map[string]any{"exists": true, "desired_digest": digest},
		DependsOn:     dependsOn,
	}
}

func executionWaveAddresses(waves [][]executionItem) [][]string {
	out := make([][]string, 0, len(waves))
	for _, wave := range waves {
		addresses := make([]string, 0, len(wave))
		for _, item := range wave {
			addresses = append(addresses, item.address)
		}
		out = append(out, addresses)
	}
	return out
}
