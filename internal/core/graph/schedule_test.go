package graph

import (
	"reflect"
	"strings"
	"testing"
)

func TestResourceGraphWaves(t *testing.T) {
	resourceGraph := &ResourceGraph{
		Nodes: []Node{
			{Address: "host.server1.files.file[\"/tmp/a\"]", Host: "server1", Kind: "file"},
			{Address: "host.server1.files.file[\"/tmp/b\"]", Host: "server1", Kind: "file"},
			{
				Address: "host.server1.services.service[\"app\"]",
				Host:    "server1",
				Kind:    "service",
				DependsOn: []string{
					"host.server1.files.file[\"/tmp/a\"]",
					"host.server1.files.file[\"/tmp/b\"]",
				},
			},
			{Address: "host.server2.files.file[\"/tmp/independent\"]", Host: "server2", Kind: "file"},
		},
		Operations: []Operation{
			{
				Address:     "host.server1.services.service[\"app\"].restart",
				DependsOn:   []string{"host.server1.services.service[\"app\"]"},
				TriggeredBy: []string{"host.server1.services.service[\"app\"]"},
			},
		},
	}

	waves, err := resourceGraph.Waves()
	if err != nil {
		t.Fatal(err)
	}
	got := waveAddresses(waves)
	want := [][]string{
		{
			"host.server1.files.file[\"/tmp/a\"]",
			"host.server1.files.file[\"/tmp/b\"]",
			"host.server2.files.file[\"/tmp/independent\"]",
		},
		{"host.server1.services.service[\"app\"]"},
		{"host.server1.services.service[\"app\"].restart"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("waves = %#v, want %#v", got, want)
	}
	if !waves[0][0].SafeParallel {
		t.Fatalf("file resource should be marked safe_parallel")
	}
	if waves[2][0].SafeParallel {
		t.Fatalf("operation should not be marked safe_parallel")
	}
}

func TestResourceGraphActiveWavesIgnoreInactiveDependencies(t *testing.T) {
	resourceGraph := &ResourceGraph{
		Nodes: []Node{
			{Address: "host.server1.files.file[\"/tmp/base\"]", Host: "server1", Kind: "file"},
			{
				Address:   "host.server1.services.service[\"app\"]",
				Host:      "server1",
				Kind:      "service",
				DependsOn: []string{"host.server1.files.file[\"/tmp/base\"]"},
			},
		},
	}

	waves, err := resourceGraph.ActiveWaves(map[string]bool{
		"host.server1.services.service[\"app\"]": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := waveAddresses(waves)
	want := [][]string{{"host.server1.services.service[\"app\"]"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active waves = %#v, want %#v", got, want)
	}
	if len(waves[0][0].DependsOn) != 0 {
		t.Fatalf("active item depends_on = %#v, want inactive dependency omitted", waves[0][0].DependsOn)
	}
}

func TestResourceGraphActiveWavesWithOperationAliases(t *testing.T) {
	baseOperation := `host.server1.components.app.script["reload"]`
	firstFile := `host.server1.components.app.files.file["/tmp/a"]`
	secondFile := `host.server1.components.app.files.file["/tmp/b"]`
	firstAlias := baseOperation + `.trigger["` + firstFile + `"]`
	secondAlias := baseOperation + `.trigger["` + secondFile + `"]`
	resourceGraph := &ResourceGraph{
		Nodes: []Node{
			{Address: firstFile, Host: "server1", Kind: "file"},
			{Address: secondFile, Host: "server1", Kind: "file"},
		},
		Operations: []Operation{
			{
				Address:     baseOperation,
				DependsOn:   []string{firstFile, secondFile},
				TriggeredBy: []string{firstFile, secondFile},
			},
		},
	}

	waves, err := resourceGraph.ActiveWavesWithAliases(
		map[string]bool{
			firstFile:   true,
			secondFile:  true,
			firstAlias:  true,
			secondAlias: true,
		},
		map[string]string{
			firstAlias:  baseOperation,
			secondAlias: baseOperation,
		},
		map[string][]string{
			firstAlias:  []string{firstFile},
			secondAlias: []string{secondFile},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := waveAddresses(waves)
	want := [][]string{
		{firstFile, secondFile},
		{firstAlias, secondAlias},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active alias waves = %#v, want %#v", got, want)
	}
	wantDeps := map[string][]string{
		firstAlias:  []string{firstFile},
		secondAlias: []string{secondFile},
	}
	for _, item := range waves[1] {
		if !reflect.DeepEqual(item.DependsOn, wantDeps[item.Address]) {
			t.Fatalf("alias item %s deps = %#v, want %#v", item.Address, item.DependsOn, wantDeps[item.Address])
		}
	}
}

func TestResourceGraphRejectsDuplicateAddress(t *testing.T) {
	resourceGraph := &ResourceGraph{
		Nodes: []Node{
			{Address: "host.server1.files.file[\"/tmp/a\"]", Host: "server1", Kind: "file"},
		},
		Operations: []Operation{
			{Address: "host.server1.files.file[\"/tmp/a\"]"},
		},
	}

	err := resourceGraph.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate resource graph address") {
		t.Fatalf("error = %v, want duplicate address", err)
	}
}

func TestResourceGraphRejectsUnknownDependency(t *testing.T) {
	resourceGraph := &ResourceGraph{
		Nodes: []Node{
			{
				Address:   "host.server1.services.service[\"app\"]",
				Host:      "server1",
				Kind:      "service",
				DependsOn: []string{"host.server1.files.file[\"/tmp/missing\"]"},
			},
		},
	}

	err := resourceGraph.Validate()
	if err == nil || !strings.Contains(err.Error(), "depends on unknown resource graph address") {
		t.Fatalf("error = %v, want unknown dependency", err)
	}
}

func TestResourceGraphRejectsUnknownTrigger(t *testing.T) {
	resourceGraph := &ResourceGraph{
		Operations: []Operation{
			{
				Address:     "host.server1.systemd.daemon_reload",
				TriggeredBy: []string{"host.server1.systemd.unit[\"missing\"]"},
			},
		},
	}

	err := resourceGraph.Validate()
	if err == nil || !strings.Contains(err.Error(), "triggered by unknown resource graph address") {
		t.Fatalf("error = %v, want unknown trigger", err)
	}
}

func TestResourceGraphRejectsCycleWithPath(t *testing.T) {
	resourceGraph := &ResourceGraph{
		Nodes: []Node{
			{
				Address:   "host.server1.files.file[\"/tmp/a\"]",
				Host:      "server1",
				Kind:      "file",
				DependsOn: []string{"host.server1.files.file[\"/tmp/b\"]"},
			},
			{
				Address:   "host.server1.files.file[\"/tmp/b\"]",
				Host:      "server1",
				Kind:      "file",
				DependsOn: []string{"host.server1.files.file[\"/tmp/c\"]"},
			},
			{
				Address:   "host.server1.files.file[\"/tmp/c\"]",
				Host:      "server1",
				Kind:      "file",
				DependsOn: []string{"host.server1.files.file[\"/tmp/a\"]"},
			},
		},
	}

	err := resourceGraph.Validate()
	if err == nil || !strings.Contains(err.Error(), "resource graph dependency cycle") {
		t.Fatalf("error = %v, want cycle", err)
	}
	for _, want := range []string{
		"host.server1.files.file[\"/tmp/a\"]",
		"host.server1.files.file[\"/tmp/b\"]",
		"host.server1.files.file[\"/tmp/c\"]",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("cycle error = %v, missing %s", err, want)
		}
	}
}

func waveAddresses(waves [][]ScheduleItem) [][]string {
	out := make([][]string, 0, len(waves))
	for _, wave := range waves {
		addresses := make([]string, 0, len(wave))
		for _, item := range wave {
			addresses = append(addresses, item.Address)
		}
		out = append(out, addresses)
	}
	return out
}

func containsScheduleString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
