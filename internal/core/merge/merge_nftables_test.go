package merge

import (
	"testing"
)

func TestCompileNftablesHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/nftables.dbf.hcl", "../testdata/hostspec/nftables.golden.json")
}
