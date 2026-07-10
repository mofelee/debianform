package graph

import (
	"encoding/json"
	"testing"
)

func TestCompileUserGroupResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/user-group.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/user-group.golden.json", got)

	userDeps := dependsOnFor(resourceGraph, `host.users1.users.user["deploy"]`)
	if !containsString(userDeps, `host.users1.groups.group["deploy"]`) {
		t.Fatalf("deploy user deps = %#v, want deploy group dependency", userDeps)
	}
}
