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

func TestNftablesPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-nftables.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-nftables.dbf.hcl",
		Host:        "edge1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-nftables.golden.json", got)

	if doc.Summary.Create != 6 {
		t.Fatalf("create count = %d, want 6", doc.Summary.Create)
	}
	if doc.Summary.Operations != 2 {
		t.Fatalf("operations = %d, want 2", doc.Summary.Operations)
	}
	if !hasOperation(doc, "host.edge1.nftables.validate") || !hasOperation(doc, "host.edge1.nftables.activate") {
		t.Fatalf("nftables operations missing: %#v", doc.Operations)
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

func TestComponentBinaryPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-component-binary.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-component-binary.dbf.hcl",
		Host:        "tool1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-component-binary.golden.json", got)

	if doc.Summary.Create != 2 {
		t.Fatalf("create count = %d, want 2", doc.Summary.Create)
	}
}

func TestComponentInputsPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-component-inputs.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-component-inputs.dbf.hcl",
		Host:        "input1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-component-inputs.golden.json", got)

	if doc.Summary.Create != 2 {
		t.Fatalf("create count = %d, want 2", doc.Summary.Create)
	}
}

func TestProfileMergePlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-profile-merge.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-profile-merge.dbf.hcl",
		Host:        "merge1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-profile-merge.golden.json", got)

	for _, want := range []string{
		`host.merge1.packages.install["curl"]`,
		`host.merge1.packages.install["sudo"]`,
		`host.merge1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`,
	} {
		if !hasChange(doc, want) {
			t.Fatalf("profile merge plan missing change %q", want)
		}
	}
}

func TestSystemdServicePlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-systemd-service.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-systemd-service.dbf.hcl",
		Host:        "service1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-systemd-service.golden.json", got)

	if !hasOperation(doc, "host.service1.systemd.daemon_reload") {
		t.Fatalf("systemd daemon reload operation missing: %#v", doc.Operations)
	}
	if !hasChange(doc, `host.service1.services.service["myapp"]`) {
		t.Fatalf("myapp service change missing: %#v", doc.Changes)
	}
}

func TestUserGroupPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-user-group.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-user-group.dbf.hcl",
		Host:        "users1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/v2-user-group.golden.json", got)

	for _, want := range []string{
		`host.users1.groups.group["deploy"]`,
		`host.users1.users.user["deploy"]`,
	} {
		if !hasChange(doc, want) {
			t.Fatalf("user/group plan missing change %q", want)
		}
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

func TestNftablesPlanPreviewDoesNotLeakSecret(t *testing.T) {
	doc := planFixture(t, "../../../examples/v2-plan-preview.dbf.hcl", Options{
		CommandFile: "../../../examples/v2-plan-preview.dbf.hcl",
		Host:        "preview1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	if doc.Summary.Create != 4 || doc.Summary.Operations != 2 {
		t.Fatalf("summary = %#v, want 4 creates and 2 operations", doc.Summary)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "not-a-real-secret-token") {
		t.Fatalf("nftables preview leaked secret content: %s", data)
	}
	if !hasChange(doc, `host.preview1.nftables.file["20-services"]`) {
		t.Fatalf("nftables snippet change missing: %#v", doc.Changes)
	}
	if !hasChange(doc, `host.preview1.secrets.file["/etc/app/token"]`) {
		t.Fatalf("secret change missing: %#v", doc.Changes)
	}
}

func TestNftablesPortChangeTextDiffGolden(t *testing.T) {
	source := ir.SourceRef{
		File: "examples/v2-plan-preview.dbf.hcl",
		Line: 29,
		Path: `host.preview1.nftables.file["20-services"]`,
	}
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/v2-plan-preview.dbf.hcl", Host: "preview1"},
		Summary:       Summary{Update: 1, Operations: 2},
		Changes: []Change{
			{
				Address: `host.preview1.nftables.file["20-services"]`,
				Action:  "update",
				Summary: "update nftables snippet 20-services",
				Source:  source,
				Diff: BuildDiff("update",
					map[string]any{"content": "add rule inet filter input tcp dport { 22, 80 } accept\n"},
					map[string]any{"content": "add rule inet filter input tcp dport { 22, 80, 443 } accept\n"},
				),
			},
		},
		Operations: []OperationNode{
			{
				Address:        "host.preview1.nftables.validate",
				Action:         "run",
				Summary:        "validate nftables ruleset",
				TriggeredBy:    []string{`host.preview1.nftables.file["20-services"]`},
				CommandPreview: "nft -c -f /etc/nftables.conf",
				Source:         source,
			},
			{
				Address:        "host.preview1.nftables.activate",
				Action:         "run",
				Summary:        "activate nftables ruleset",
				DependsOn:      []string{"host.preview1.nftables.validate"},
				TriggeredBy:    []string{`host.preview1.nftables.file["20-services"]`},
				CommandPreview: "nft -f /etc/nftables.conf",
				Source:         source,
			},
		},
		Diagnostics: []Diagnostic{},
	}
	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/v2-nftables-port-change.golden.txt", text.String())
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
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := graph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return New(resourceGraph, opts)
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

func hasOperation(doc Document, address string) bool {
	for _, operation := range doc.Operations {
		if operation.Address == address {
			return true
		}
	}
	return false
}

func hasChange(doc Document, address string) bool {
	for _, change := range doc.Changes {
		if change.Address == address {
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
