package merge

import (
	"testing"
)

func TestCompileUserGroupHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/user-group.dbf.hcl", "../testdata/hostspec/user-group.golden.json")
}
