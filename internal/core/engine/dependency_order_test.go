package engine

import (
	"reflect"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
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
