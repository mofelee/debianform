package state

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
)

func TestResolveMovesRebasesComponentStateWithoutMutatingRemoteIdentity(t *testing.T) {
	oldPrefix := "host.server1.components.old"
	newPrefix := "host.server1.components.current"
	fileSuffix := `.files.file["/etc/example.conf"]`
	outputSuffix := `.script["render"].outputs["/var/lib/example.out"]`
	oldFile := oldPrefix + fileSuffix
	oldOutput := oldPrefix + outputSuffix
	oldExternal := oldPrefix + `.services.service["example"]`
	boundary := "host.server1.components.oldish" + fileSuffix
	newFile := newPrefix + fileSuffix
	newOutput := newPrefix + outputSuffix
	newExternal := newPrefix + `.services.service["example"]`

	fileDesired := map[string]any{"component": "old", "path": "/etc/example.conf", "content": "same"}
	fileDigest := DesiredDigest(fileDesired)
	st := Empty("server1")
	st.Serial = 7
	st.Resources[oldFile] = Resource{
		Host:            "server1",
		Kind:            "file",
		ProviderType:    "file",
		ProviderAddress: "file.server1_old_etc_example_conf",
		Ownership:       "managed",
		Lifecycle:       &ir.LifecycleSpec{PreventDestroy: true, Source: ir.SourceRef{Path: "old.lifecycle"}},
		Desired:         fileDesired,
		DesiredDigest:   fileDigest,
		Observed:        map[string]any{"exists": true, "desired_digest": fileDigest, "sha256": "remote-sha"},
		UpdatedAt:       "2026-07-28T00:00:00Z",
		Order:           12,
	}
	st.Resources[oldOutput] = Resource{
		Host:            "server1",
		Kind:            "component_script_output",
		ProviderType:    "component_script_output",
		ProviderAddress: "component_script_output.server1_old_render",
		Ownership:       "adopted",
		Desired:         map[string]any{"component": "old", "path": "/var/lib/example.out", "script": "render", "script_digest": "same-script"},
		Observed:        map[string]any{"path": "/var/lib/example.out", "sha256": "output-sha"},
		Order:           13,
	}
	st.Resources[oldExternal] = Resource{
		Host:            "server1",
		Kind:            "service",
		ProviderType:    "service",
		ProviderAddress: "service.server1_old_example",
		Ownership:       "external",
		Desired:         map[string]any{"component": "old", "name": "example", "unit": "example.service"},
		Observed:        map[string]any{"active": true},
		Order:           14,
	}
	st.Resources[boundary] = Resource{Host: "server1", Kind: "file", Ownership: "managed", Desired: map[string]any{"component": "oldish", "path": "/etc/example.conf"}}
	before, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ResolveMoves(st, []ir.MovedSpec{moveSpec(oldPrefix, newPrefix)}, map[string]bool{newPrefix: true}, map[string]MoveTarget{
		newFile:     {ProviderAddress: "file.server1_current_etc_example_conf", Desired: map[string]any{"component": "current"}},
		newOutput:   {ProviderAddress: "component_script_output.server1_current_render", Desired: map[string]any{"component": "current"}},
		newExternal: {ProviderAddress: "service.server1_current_example", Desired: map[string]any{"component": "current"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("ResolveMoves mutated input state\ngot:  %s\nwant: %s", after, before)
	}
	if result.State.Serial != 7 {
		t.Fatalf("serial = %d, want unchanged 7", result.State.Serial)
	}
	if len(result.Moves) != 3 {
		t.Fatalf("moves = %#v, want 3", result.Moves)
	}
	wantOrder := []string{oldFile, oldOutput, oldExternal}
	for i, move := range result.Moves {
		if move.From != wantOrder[i] {
			t.Fatalf("move order = %#v, want source %q at %d", result.Moves, wantOrder[i], i)
		}
	}
	if _, ok := result.State.Resources[oldFile]; ok {
		t.Fatalf("old file address remains in state")
	}
	if _, ok := result.State.Resources[boundary]; !ok {
		t.Fatalf("segment-boundary neighbor was moved")
	}

	file := result.State.Resources[newFile]
	if file.Ownership != "managed" || file.Order != 12 || file.UpdatedAt != "2026-07-28T00:00:00Z" || file.Lifecycle == nil || !file.Lifecycle.PreventDestroy {
		t.Fatalf("file payload metadata changed: %#v", file)
	}
	if file.Desired["path"] != "/etc/example.conf" || file.Desired["content"] != "same" || file.Observed["sha256"] != "remote-sha" {
		t.Fatalf("remote identity fields changed: desired=%#v observed=%#v", file.Desired, file.Observed)
	}
	if file.Desired["component"] != "current" || file.ProviderAddress != "file.server1_current_etc_example_conf" {
		t.Fatalf("address-derived file metadata was not rebased: %#v", file)
	}
	if file.DesiredDigest != DesiredDigest(file.Desired) || file.Observed["desired_digest"] != file.DesiredDigest {
		t.Fatalf("file digests were not rebased: %#v", file)
	}
	output := result.State.Resources[newOutput]
	if output.Ownership != "adopted" || output.Desired["script_digest"] != "same-script" || output.Observed["sha256"] != "output-sha" || output.ProviderAddress != "component_script_output.server1_current_render" {
		t.Fatalf("script output payload changed unexpectedly: %#v", output)
	}
	external := result.State.Resources[newExternal]
	if external.Ownership != "external" || external.Desired["unit"] != "example.service" || external.Observed["active"] != true {
		t.Fatalf("external ownership payload changed unexpectedly: %#v", external)
	}
}

func TestResolveMovesAppliesHistoricalChainsInDependencyOrder(t *testing.T) {
	oldPrefix := "host.server1.components.old"
	middlePrefix := "host.server1.components.middle"
	currentPrefix := "host.server1.components.current"
	suffix := `.packages.install["bird2"]`
	st := Empty("server1")
	st.Resources[oldPrefix+suffix] = Resource{
		Host:            "server1",
		Kind:            "package",
		ProviderAddress: "package.server1_old_bird2",
		Ownership:       "managed",
		Desired:         map[string]any{"component": "old", "name": "bird2"},
	}

	result, err := ResolveMoves(st, []ir.MovedSpec{
		moveSpec(middlePrefix, currentPrefix),
		moveSpec(oldPrefix, middlePrefix),
	}, map[string]bool{currentPrefix: true}, map[string]MoveTarget{
		currentPrefix + suffix: {ProviderAddress: "package.server1_current_bird2", Desired: map[string]any{"component": "current"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Moves) != 2 || result.Moves[0].From != oldPrefix+suffix || result.Moves[0].To != middlePrefix+suffix || result.Moves[1].From != middlePrefix+suffix || result.Moves[1].To != currentPrefix+suffix {
		t.Fatalf("chain moves = %#v", result.Moves)
	}
	resource, ok := result.State.Resources[currentPrefix+suffix]
	if !ok || resource.Desired["component"] != "current" || resource.ProviderAddress != "package.server1_current_bird2" {
		t.Fatalf("final chained resource = %#v", resource)
	}
}

func TestResolveMovesIdempotentAndPartialHostSemantics(t *testing.T) {
	oldPrefix := "host.server1.components.old"
	newPrefix := "host.server1.components.current"
	suffix := `.files.file["/etc/example"]`
	declarations := []ir.MovedSpec{moveSpec(oldPrefix, newPrefix)}

	t.Run("already migrated", func(t *testing.T) {
		st := Empty("server1")
		st.Resources[newPrefix+suffix] = Resource{Host: "server1", Kind: "file", Ownership: "managed"}
		result, err := ResolveMoves(st, declarations, map[string]bool{newPrefix: true}, nil)
		if err != nil || len(result.Moves) != 0 || len(result.State.Resources) != 1 {
			t.Fatalf("result = %#v err = %v", result, err)
		}
	})

	t.Run("neither state entry exists", func(t *testing.T) {
		result, err := ResolveMoves(Empty("server1"), declarations, map[string]bool{newPrefix: true}, nil)
		if err != nil || len(result.Moves) != 0 || len(result.State.Resources) != 0 {
			t.Fatalf("result = %#v err = %v", result, err)
		}
	})

	t.Run("source component still desired", func(t *testing.T) {
		st := Empty("server1")
		st.Resources[oldPrefix+suffix] = Resource{Host: "server1", Kind: "file", Ownership: "managed"}
		result, err := ResolveMoves(st, declarations, map[string]bool{oldPrefix: true}, nil)
		if err != nil || len(result.Moves) != 0 {
			t.Fatalf("result = %#v err = %v", result, err)
		}
		if _, ok := result.State.Resources[oldPrefix+suffix]; !ok {
			t.Fatalf("source state moved during staggered source configuration")
		}
	})
}

func TestResolveMovesRejectsStateAndDesiredGraphConflicts(t *testing.T) {
	oldPrefix := "host.server1.components.old"
	newPrefix := "host.server1.components.current"
	suffix := `.files.file["/etc/example"]`
	declarations := []ir.MovedSpec{moveSpec(oldPrefix, newPrefix)}

	tests := []struct {
		name      string
		resources []string
		desired   map[string]bool
		want      string
	}{
		{name: "both desired", resources: []string{oldPrefix + suffix}, desired: map[string]bool{oldPrefix: true, newPrefix: true}, want: "both source and destination components are desired"},
		{name: "target missing", resources: []string{oldPrefix + suffix}, desired: map[string]bool{}, want: "destination component"},
		{name: "destination collision", resources: []string{oldPrefix + suffix, newPrefix + suffix}, desired: map[string]bool{newPrefix: true}, want: "destination state entry already exists"},
		{name: "destination collision while source remains desired", resources: []string{oldPrefix + suffix, newPrefix + suffix}, desired: map[string]bool{oldPrefix: true}, want: "destination state entry already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := Empty("server1")
			for _, address := range tt.resources {
				st.Resources[address] = Resource{Host: "server1", Kind: "file", Ownership: "managed"}
			}
			_, err := ResolveMoves(st, declarations, tt.desired, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "moved.from") {
				t.Fatalf("error = %v, want source-oriented %q", err, tt.want)
			}
		})
	}
}

func TestResolveMovesCycleDiagnosticIsDeterministic(t *testing.T) {
	a := "host.server1.components.a"
	b := "host.server1.components.b"
	for range 20 {
		_, err := ResolveMoves(Empty("server1"), []ir.MovedSpec{moveSpec(b, a), moveSpec(a, b)}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "cycle through "+a) {
			t.Fatalf("cycle diagnostic = %v, want deterministic source %s", err, a)
		}
	}
}

func TestResolveMovesRejectsInvalidMappingsDefensively(t *testing.T) {
	oldPrefix := "host.server1.components.old"
	newPrefix := "host.server1.components.current"
	otherPrefix := "host.server1.components.other"
	tests := []struct {
		name  string
		moves []ir.MovedSpec
		want  string
	}{
		{name: "cross host", moves: []ir.MovedSpec{moveSpec("host.server2.components.old", newPrefix)}, want: "not a component root"},
		{name: "leaf endpoint", moves: []ir.MovedSpec{moveSpec(oldPrefix+".files", newPrefix)}, want: "not a component root"},
		{name: "self", moves: []ir.MovedSpec{moveSpec(oldPrefix, oldPrefix)}, want: "cannot move to itself"},
		{name: "duplicate source", moves: []ir.MovedSpec{moveSpec(oldPrefix, newPrefix), moveSpec(oldPrefix, otherPrefix)}, want: "declared more than once"},
		{name: "many to one", moves: []ir.MovedSpec{moveSpec(oldPrefix, newPrefix), moveSpec(otherPrefix, newPrefix)}, want: "both target"},
		{name: "cycle", moves: []ir.MovedSpec{moveSpec(oldPrefix, newPrefix), moveSpec(newPrefix, oldPrefix)}, want: "cycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveMoves(Empty("server1"), tt.moves, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func moveSpec(from, to string) ir.MovedSpec {
	return ir.MovedSpec{
		From:       from,
		To:         to,
		Source:     ir.SourceRef{File: "main.dbf.hcl", Line: 1, Path: "moved"},
		FromSource: ir.SourceRef{File: "main.dbf.hcl", Line: 2, Path: "moved.from"},
		ToSource:   ir.SourceRef{File: "main.dbf.hcl", Line: 3, Path: "moved.to"},
	}
}
