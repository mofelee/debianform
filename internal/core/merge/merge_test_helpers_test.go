package merge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func assertHostSpecGolden(t *testing.T, fixture string, golden string) {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileWithOptions(cfg, CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(program.Hosts))
	}
	data, err := json.MarshalIndent(program.Hosts[0], "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("HostSpec golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func compileInline(t *testing.T, content string) *ir.Program {
	t.Helper()

	cfg := parseInline(t, content)
	program, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func parseInline(t *testing.T, content string) *parser.Config {
	t.Helper()

	return parseInlineWithOptions(t, content, parser.ParseOptions{})
}

func parseInlineWithOptions(t *testing.T, content string, opts parser.ParseOptions) *parser.Config {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFilesWithOptions([]string{file}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func parseOrCompileInline(t *testing.T, content string) (*ir.Program, error) {
	t.Helper()

	return parseOrCompileInlineWithFiles(t, content, nil)
}

func parseOrCompileInlineWithFiles(t *testing.T, content string, extraFiles map[string]string) (*ir.Program, error) {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	for name, data := range extraFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return parseOrCompileFiles([]string{file})
}

func parseOrCompileFiles(files []string) (*ir.Program, error) {
	cfg, err := parser.ParseFiles(files)
	if err != nil {
		return nil, err
	}
	return Compile(cfg)
}

func testHostFacts() map[string]ir.HostFacts {
	out := map[string]ir.HostFacts{}
	for _, name := range []string{
		"apt1",
		"bbr1",
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

func packageNames(items []ir.PackageItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
