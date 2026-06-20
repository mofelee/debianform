package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/v2/graph"
	"github.com/mofelee/debianform/internal/v2/merge"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func TestBBRPlanJSONGolden(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../../../examples/v2-bbr.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := graph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	doc := New(resourceGraph, Options{
		CommandFile: "../../../examples/v2-bbr.dbf.hcl",
		Host:        "bbr1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-bbr.golden.json", got)

	if doc.FormatVersion != FormatVersion {
		t.Fatalf("format version = %q, want %q", doc.FormatVersion, FormatVersion)
	}
	if doc.Summary.Create != 3 {
		t.Fatalf("create count = %d, want 3", doc.Summary.Create)
	}
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
