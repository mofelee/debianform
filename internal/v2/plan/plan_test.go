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
	"github.com/mofelee/debianform/internal/v2/ir"
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
	for _, change := range doc.Changes {
		if change.ProviderAddress != "" {
			t.Fatalf("default plan leaked provider address %q", change.ProviderAddress)
		}
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

func TestAPTRepositoryPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-apt-repository.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-apt-repository.dbf.hcl",
		Host:        "apt1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-apt-repository.golden.json", got)

	if doc.Summary.Create != 4 {
		t.Fatalf("create count = %d, want 4", doc.Summary.Create)
	}
	if doc.Summary.Operations != 1 {
		t.Fatalf("operations = %d, want 1", doc.Summary.Operations)
	}
}

func TestBIRD2PlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-bird2.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-bird2.dbf.hcl",
		Host:        "router1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-bird2.golden.json", got)

	if doc.Summary.Create != 4 {
		t.Fatalf("create count = %d, want 4", doc.Summary.Create)
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

func TestFilesPlanPreviewHasTextAndSensitiveDiffs(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-files-plan-preview.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-files-plan-preview.dbf.hcl",
		Host:        "preview1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	if len(doc.Changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(doc.Changes))
	}

	var textDiff, sensitiveDiff *DiffNode
	for i := range doc.Changes {
		for j := range doc.Changes[i].Diff.Children {
			child := &doc.Changes[i].Diff.Children[j]
			if child.Kind == "text" {
				textDiff = child
			}
			if child.Kind == "sensitive" {
				sensitiveDiff = child
			}
		}
	}
	if textDiff == nil || len(textDiff.Hunks) != 1 || textDiff.Hunks[0].NewLines != 2 {
		t.Fatalf("text diff = %#v", textDiff)
	}
	if sensitiveDiff == nil || sensitiveDiff.AfterSummary["bytes"] != float64(len("not-a-real-preview-secret")) {
		t.Fatalf("sensitive diff = %#v", sensitiveDiff)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "not-a-real-preview-secret") {
		t.Fatalf("preview plan leaked sensitive content: %s", data)
	}

	var text bytes.Buffer
	PrintText(&text, doc)
	rendered := text.String()
	for _, want := range []string{
		`source: ../../../examples/v2-files-plan-preview.dbf.hcl`,
		`+ content`,
		`@@ -1,0 +1,2 @@`,
		`+ listen = "127.0.0.1:8080"`,
		`<sensitive sha256=`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("text plan does not contain %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "not-a-real-preview-secret") {
		t.Fatalf("text plan leaked sensitive content:\n%s", rendered)
	}
}

func TestPlanDebugIncludesProviderAddress(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-bbr.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-bbr.dbf.hcl",
		Host:        "bbr1",
		Debug:       true,
	})
	if len(doc.Changes) == 0 {
		t.Fatal("debug plan has no changes")
	}
	for _, change := range doc.Changes {
		if change.ProviderAddress == "" {
			t.Fatalf("debug change %s has no provider address", change.Address)
		}
	}

	var text bytes.Buffer
	PrintText(&text, doc)
	if !strings.Contains(text.String(), "provider: kernel_module.bbr1_tcp_bbr") {
		t.Fatalf("debug text plan does not show provider address:\n%s", text.String())
	}
}

func TestActionMatrixJSONAndTerminalGolden(t *testing.T) {
	source := ir.SourceRef{
		File: "examples/action-matrix.dbf.hcl",
		Line: 12,
		Path: `host.server1.files.file["/tmp/example"]`,
	}
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/action-matrix.dbf.hcl", Host: "server1"},
		Summary:       Summary{Create: 1, Update: 1, Delete: 1, NoOp: 1, Operations: 1},
		Changes: []Change{
			{
				Address: `host.server1.files.file["/tmp/create"]`,
				Action:  "create",
				Summary: "create file /tmp/create",
				Source:  source,
				Diff:    BuildDiff("create", nil, map[string]any{"path": "/tmp/create", "mode": "0644"}),
			},
			{
				Address: `host.server1.files.file["/tmp/update"]`,
				Action:  "update",
				Summary: "update file /tmp/update",
				Source:  source,
				Diff: BuildDiff("update",
					map[string]any{"content": "old\n", "mode": "0644"},
					map[string]any{"content": "new\n", "mode": "0600"},
				),
			},
			{
				Address: `host.server1.files.file["/tmp/delete"]`,
				Action:  "delete",
				Summary: "delete file /tmp/delete",
				Source:  source,
				Diff:    BuildDiff("delete", map[string]any{"path": "/tmp/delete"}, nil),
			},
			{
				Address: `host.server1.files.file["/tmp/no-op"]`,
				Action:  "no-op",
				Summary: "no changes for file /tmp/no-op",
				Source:  source,
				Diff:    BuildDiff("no-op", map[string]any{"mode": "0644"}, map[string]any{"mode": "0644"}),
			},
		},
		Operations: []OperationNode{
			{
				Address:        "host.server1.systemd.daemon_reload",
				Action:         "run",
				Summary:        "reload systemd manager configuration",
				TriggeredBy:    []string{`host.server1.files.file["/tmp/update"]`},
				CommandPreview: "systemctl daemon-reload",
				Source:         source,
			},
		},
		Diagnostics: []Diagnostic{},
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "../testdata/plan/action-matrix.golden.json", string(data)+"\n")

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/action-matrix.golden.txt", text.String())
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
