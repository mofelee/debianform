package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/v2/graph"
	"github.com/mofelee/debianform/internal/v2/merge"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func TestBBRPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-bbr.dbf.hcl", Options{
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

func TestFoundationPlanJSONGoldenDoesNotLeakSecrets(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/v2-foundation.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/v2-foundation.dbf.hcl",
		Host:        "foundation1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-foundation.golden.json", got)

	if strings.Contains(got, "not-a-real-secret-token") {
		t.Fatalf("plan JSON leaked secret content:\n%s", got)
	}
	if doc.Summary.Operations != 1 {
		t.Fatalf("operations = %d, want 1", doc.Summary.Operations)
	}
}

func TestPlanHTMLDoesNotLeakSecrets(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/v2-foundation.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/v2-foundation.dbf.hcl",
		Host:        "foundation1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	var out bytes.Buffer
	if err := PrintHTML(&out, doc); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		"DebianForm Plan",
		`host.foundation1.files.file[&#34;/etc/myapp/config.yaml&#34;]`,
		"host.foundation1.systemd.daemon_reload",
		"debianform.plan.v2alpha1",
		`id="action-filter"`,
		`id="host-filter"`,
		`id="search-filter"`,
		`value="foundation1"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML output does not contain %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "not-a-real-secret-token") {
		t.Fatalf("plan HTML leaked secret content:\n%s", html)
	}
}

func planFixture(t *testing.T, fixture string, opts Options) Document {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
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
	return New(resourceGraph, opts)
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
