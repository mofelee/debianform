package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/merge"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func TestCompileBBRResourceGraphGolden(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../../../examples/v2-bbr.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := Compile(program)
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-bbr.golden.json", got)

	dependsOn := dependsOnFor(resourceGraph, `host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`)
	want := []string{`host.bbr1.kernel.module["tcp_bbr"]`}
	if strings.Join(dependsOn, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tcp_congestion_control depends_on = %#v, want %#v", dependsOn, want)
	}
}

func dependsOnFor(resourceGraph *ResourceGraph, address string) []string {
	for _, node := range resourceGraph.Nodes {
		if node.Address == address {
			return node.DependsOn
		}
	}
	return nil
}

func assertGolden(t *testing.T, golden string, got string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
