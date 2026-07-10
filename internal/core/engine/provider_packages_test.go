package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestNativeProviderPackageInstallRefreshesMissingAPTCache(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.packages.install[\"nftables\"]",
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{
			"name":   "nftables",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: ""}, {Stdout: "package\tnftables\n"}}}
	provider := NewNativeProvider(runner)

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[0]
	for _, want := range []string{
		"apt-cache policy 'nftables'",
		"apt-get update",
		"apt-get install -y 'nftables'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("package install script missing %q:\n%s", want, applied)
		}
	}
	if strings.Index(applied, "apt-get update") > strings.Index(applied, "apt-get install -y 'nftables'") {
		t.Fatalf("package install should refresh apt cache before install:\n%s", applied)
	}
}

func TestNativeProviderPackagePlanUsesInstalledProviderForVirtualPackage(t *testing.T) {
	node := graph.Node{
		Address: `host.server1.packages.install["dnsutils"]`,
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{
			"name":   "dnsutils",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "provider\tbind9-dnsutils\n"}}}
	provider := NewNativeProvider(runner)
	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(node.Desired)}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("virtual package action = %q, want no-op; observed=%#v", got.Action, got.Observed)
	}
	if got.Observed["installed"] != true || got.Observed["package"] != "bind9-dnsutils" || got.Observed["virtual"] != true {
		t.Fatalf("virtual package observed = %#v, want installed provider", got.Observed)
	}
	if !strings.Contains(got.Summary, "provided by bind9-dnsutils") {
		t.Fatalf("virtual package summary = %q, want provider detail", got.Summary)
	}
	if !strings.Contains(runner.scripts[0], "${Provides}") {
		t.Fatalf("package detection should inspect Provides:\n%s", runner.scripts[0])
	}
}

func TestNativeProviderPackagePlanTreatsDifferentBinaryPackageAsVirtualProvider(t *testing.T) {
	node := graph.Node{
		Address: `host.server1.packages.install["dnsutils"]`,
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{
			"name":   "dnsutils",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "provider\tbind9-dnsutils\n"}}}
	provider := NewNativeProvider(runner)
	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(node.Desired)}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("virtual package action = %q, want no-op; observed=%#v", got.Action, got.Observed)
	}
	if got.Observed["package"] != "bind9-dnsutils" || got.Observed["virtual"] != true {
		t.Fatalf("virtual package observed = %#v, want bind9-dnsutils virtual provider", got.Observed)
	}
	if !strings.Contains(runner.scripts[0], `pkg == target || base == target`) {
		t.Fatalf("package detection should distinguish target packages from providers:\n%s", runner.scripts[0])
	}
}

func TestNativeProviderPackagePlanCreatesWhenNeitherPackageNorProviderInstalled(t *testing.T) {
	node := graph.Node{
		Address: `host.server1.packages.install["dnsutils"]`,
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{
			"name":   "dnsutils",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: ""}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing virtual package action = %q, want create; observed=%#v", got.Action, got.Observed)
	}
	if got.Observed["installed"] != false {
		t.Fatalf("missing virtual package observed = %#v, want installed=false", got.Observed)
	}
}

func TestNativeProviderPackageAbsentDoesNotRemoveVirtualProvider(t *testing.T) {
	node := graph.Node{
		Address: `host.server1.packages.install["dnsutils"]`,
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{
			"name":   "dnsutils",
			"ensure": "absent",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "provider\tbind9-dnsutils\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("absent virtual package action = %q, want no-op; observed=%#v", got.Action, got.Observed)
	}
	if got.Observed["installed"] != false || got.Observed["satisfied_by"] != "bind9-dnsutils" {
		t.Fatalf("absent virtual package observed = %#v, want absent with satisfied_by provider", got.Observed)
	}
}

func TestNativeProviderPackageApplyRecordsVirtualProvider(t *testing.T) {
	node := graph.Node{
		Address: `host.server1.packages.install["dnsutils"]`,
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{
			"name":   "dnsutils",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: ""}, {Stdout: "provider\tbind9-dnsutils\n"}}}
	provider := NewNativeProvider(runner)

	observed, err := provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if observed["installed"] != true || observed["package"] != "bind9-dnsutils" || observed["virtual"] != true {
		t.Fatalf("apply observed = %#v, want virtual provider", observed)
	}
	if observed["desired_digest"] != corestate.DesiredDigest(node.Desired) {
		t.Fatalf("apply observed desired digest = %#v, want current digest", observed["desired_digest"])
	}
}

func TestNativeProviderDockerPackageApplyScript(t *testing.T) {
	node := graph.Node{
		Address: `host.docker1.docker.package["docker-ce"]`,
		Host:    "docker1",
		Kind:    "package",
		Desired: map[string]any{
			"name":         "docker-ce",
			"ensure":       "present",
			"repositories": []string{"docker-official"},
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: ""}, {Stdout: ""}, {Stdout: "package\tdocker-ce\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing docker package action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[1]
	for _, want := range []string{
		"apt-cache policy 'docker-ce'",
		"apt-get update",
		"apt-get install -y 'docker-ce'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("docker package script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerPackageConflictsPlanNoopWhenAbsent(t *testing.T) {
	node := dockerPackageConflictsNode("auto")
	runner := &recordingRunner{outputs: []Result{{Stdout: ""}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("conflict plan action = %q, want no-op; observed=%#v", got.Action, got.Observed)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "dpkg-query") {
		t.Fatalf("conflict detection script = %#v, want dpkg-query", runner.scripts)
	}
}

func TestNativeProviderDockerPackageConflictsPlanAutoAndTrueDelete(t *testing.T) {
	for _, tt := range []struct {
		removeConflicts string
		wantSummary     string
	}{
		{removeConflicts: "auto", wantSummary: "replace docker conflict packages docker.io, runc"},
		{removeConflicts: "true", wantSummary: "remove docker conflict packages docker.io, runc"},
	} {
		t.Run(tt.removeConflicts, func(t *testing.T) {
			node := dockerPackageConflictsNode(tt.removeConflicts)
			runner := &recordingRunner{outputs: []Result{{Stdout: "docker.io\tinstall ok installed\nrunc\tinstall ok installed\n"}}}
			provider := NewNativeProvider(runner)

			got, err := provider.Plan(context.Background(), node, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != ActionDelete || got.Summary != tt.wantSummary {
				t.Fatalf("conflict plan = %#v, want delete summary %q", got, tt.wantSummary)
			}
			installed := stringListMapValue(got.Observed, "installed")
			if strings.Join(installed, ",") != "docker.io,runc" {
				t.Fatalf("installed conflicts = %#v, want docker.io,runc", installed)
			}
		})
	}
}

func TestNativeProviderDockerPackageConflictsApplyRemovesInstalledPackages(t *testing.T) {
	node := dockerPackageConflictsNode("auto")
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)

	observed, err := provider.Apply(context.Background(), Step{
		Node:     node,
		Action:   ActionDelete,
		Observed: map[string]any{"installed": []string{"docker.io", "runc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stringListMapValue(observed, "removed"), ","); got != "docker.io,runc" {
		t.Fatalf("removed conflicts = %q, want docker.io,runc", got)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("scripts = %#v, want one removal script", runner.scripts)
	}
	for _, want := range []string{
		"export DEBIAN_FRONTEND=noninteractive",
		"'apt-get' 'remove' '-y' 'docker.io' 'runc'",
	} {
		if !strings.Contains(runner.scripts[0], want) {
			t.Fatalf("conflict removal script missing %q:\n%s", want, runner.scripts[0])
		}
	}
}

func TestNativeProviderDockerPackageConflictsPlanFalseErrors(t *testing.T) {
	node := dockerPackageConflictsNode("false")
	runner := &recordingRunner{outputs: []Result{{Stdout: "docker.io\tinstall ok installed\n"}}}
	provider := NewNativeProvider(runner)

	_, err := provider.Plan(context.Background(), node, nil)
	if err == nil || !strings.Contains(err.Error(), "docker conflict packages are installed: docker.io") || !strings.Contains(err.Error(), "remove_conflicts") {
		t.Fatalf("conflict false error = %v", err)
	}

	_, err = provider.Apply(context.Background(), Step{
		Node:     node,
		Action:   ActionDelete,
		Observed: map[string]any{"installed": []string{"docker.io"}},
	})
	if err == nil || !strings.Contains(err.Error(), "docker conflict packages are installed: docker.io") || !strings.Contains(err.Error(), "remove_conflicts") {
		t.Fatalf("conflict false apply error = %v", err)
	}
}
