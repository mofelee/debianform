package plan

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
)

func TestExplicitResourceDependenciesAppearInAllPlanFormats(t *testing.T) {
	packageAddress := `host.server1.packages.install["vault"]`
	fileAddress := `host.server1.files.file["/etc/vault.d/vault.hcl"]`
	doc := New(&graph.ResourceGraph{Nodes: []graph.Node{
		{Host: "server1", Address: packageAddress, Kind: "package", Summary: "install vault", Desired: map[string]any{"name": "vault"}},
		{Host: "server1", Address: fileAddress, Kind: "file", Summary: "manage vault config", Desired: map[string]any{"path": "/etc/vault.d/vault.hcl"}, DependsOn: []string{packageAddress}, ExplicitDependsOn: []string{packageAddress}},
	}}, Options{Now: func() time.Time { return time.Unix(0, 0).UTC() }})
	if len(doc.Changes) != 2 || len(doc.Changes[0].DependsOn)+len(doc.Changes[1].DependsOn) != 1 {
		t.Fatalf("plan changes = %#v", doc.Changes)
	}

	var text bytes.Buffer
	PrintText(&text, doc)
	if !strings.Contains(text.String(), "depends_on:") || !strings.Contains(text.String(), packageAddress) {
		t.Fatalf("text plan lacks dependency:\n%s", text.String())
	}

	var jsonOutput bytes.Buffer
	if err := PrintJSON(&jsonOutput, doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOutput.String(), `"depends_on"`) || !strings.Contains(jsonOutput.String(), `host.server1.packages.install[\"vault\"]`) {
		t.Fatalf("JSON plan lacks dependency:\n%s", jsonOutput.String())
	}

	var html bytes.Buffer
	if err := PrintHTML(&html, doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "depends_on:") || !strings.Contains(html.String(), "host.server1.packages.install") {
		t.Fatalf("HTML plan lacks dependency")
	}
}
