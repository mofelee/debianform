package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/merge"
	"github.com/mofelee/debianform/internal/core/parser"
)

func compileGraphFixture(t *testing.T, fixture string) *ResourceGraph {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return resourceGraph
}

func testHostFacts() map[string]ir.HostFacts {
	out := map[string]ir.HostFacts{}
	for _, name := range []string{
		"apt1",
		"bbr1",
		"compose1",
		"docker-daemon1",
		"docker1",
		"docker-users1",
		"edge1",
		"foundation1",
		"input1",
		"merge1",
		"preview1",
		"router1",
		"server1",
		"server2",
		"service1",
		"tool1",
		"users1",
	} {
		out[name] = ir.HostFacts{System: ir.SystemFacts{
			Hostname:     name,
			Architecture: "amd64",
			Codename:     "trixie",
		}}
	}
	return out
}

func compileGraphInline(t *testing.T, content string) *ResourceGraph {
	t.Helper()

	return compileGraphInlineWithFiles(t, content, nil)
}

func compileGraphInlineWithFiles(t *testing.T, content string, files map[string]string) *ResourceGraph {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return compileGraphFixture(t, file)
}

func compileGraphInlineError(t *testing.T, content string) error {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(program)
	return err
}

func dependsOnFor(resourceGraph *ResourceGraph, address string) []string {
	for _, node := range resourceGraph.Nodes {
		if node.Address == address {
			return node.DependsOn
		}
	}
	return nil
}

func nodeFor(resourceGraph *ResourceGraph, address string) *Node {
	for i := range resourceGraph.Nodes {
		if resourceGraph.Nodes[i].Address == address {
			return &resourceGraph.Nodes[i]
		}
	}
	return nil
}

func operationFor(resourceGraph *ResourceGraph, address string) *Operation {
	for i := range resourceGraph.Operations {
		if resourceGraph.Operations[i].Address == address {
			return &resourceGraph.Operations[i]
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func topologicalOrder(t *testing.T, resourceGraph *ResourceGraph) map[string]int {
	t.Helper()

	items, err := resourceGraph.TopologicalSort()
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]int, len(items))
	for i, item := range items {
		out[item.Address] = i
	}
	return out
}

func assertBefore(t *testing.T, order map[string]int, before string, after string) {
	t.Helper()

	beforeIndex, ok := order[before]
	if !ok {
		t.Fatalf("topological order missing %q", before)
	}
	afterIndex, ok := order[after]
	if !ok {
		t.Fatalf("topological order missing %q", after)
	}
	if beforeIndex >= afterIndex {
		t.Fatalf("topological order puts %q at %d after %q at %d", before, beforeIndex, after, afterIndex)
	}
}

func hasOperation(resourceGraph *ResourceGraph, address string) bool {
	for _, operation := range resourceGraph.Operations {
		if operation.Address == address {
			return true
		}
	}
	return false
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
