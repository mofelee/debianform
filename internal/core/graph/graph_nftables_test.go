package graph

import (
	"encoding/json"
	"testing"
)

func TestCompileNftablesResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/nftables.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/nftables.golden.json", got)

	validate := operationFor(resourceGraph, "host.edge1.nftables.validate")
	if validate == nil {
		t.Fatal("nftables validate operation missing")
	}
	activate := operationFor(resourceGraph, "host.edge1.nftables.activate")
	if activate == nil {
		t.Fatal("nftables activate operation missing")
	}
	if !containsString(activate.DependsOn, "host.edge1.nftables.validate") {
		t.Fatalf("activate deps = %#v, want validate", activate.DependsOn)
	}
	for _, want := range []string{
		`host.edge1.nftables.file["main"]`,
		`host.edge1.nftables.file["10-base"]`,
		`host.edge1.nftables.file["20-services"]`,
		`host.edge1.nftables.file["30-wireguard"]`,
	} {
		if !containsString(validate.TriggeredBy, want) {
			t.Fatalf("validate triggered_by = %#v, want %q", validate.TriggeredBy, want)
		}
	}
	deps := dependsOnFor(resourceGraph, `host.edge1.nftables.file["20-services"]`)
	if !containsString(deps, `host.edge1.packages.install["nftables"]`) {
		t.Fatalf("nftables file deps = %#v, want package dependency", deps)
	}
	enableDeps := dependsOnFor(resourceGraph, "host.edge1.nftables.enable")
	if !containsString(enableDeps, "host.edge1.nftables.activate") {
		t.Fatalf("nftables enable deps = %#v, want activate dependency", enableDeps)
	}
}
