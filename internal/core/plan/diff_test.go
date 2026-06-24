package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildDiffCreatesStructuredChildrenAndTextHunk(t *testing.T) {
	diff := BuildDiff("update",
		map[string]any{
			"content": "first\nold\nlast\n",
			"labels":  map[string]any{"env": "dev"},
			"mode":    "0644",
			"ports":   []any{80},
			"groups":  []any{"adm"},
		},
		map[string]any{
			"content": "first\nnew\nlast\n",
			"labels":  map[string]any{"env": "prod"},
			"mode":    "0600",
			"ports":   []any{80, 443},
			"groups":  []any{"adm", "sudo"},
		},
	)

	if diff.Kind != "object" || diff.Action != "update" {
		t.Fatalf("root diff = %#v", diff)
	}

	content := childAt(t, diff, "content")
	if content.Kind != "text" || len(content.Hunks) != 1 {
		t.Fatalf("content diff = %#v", content)
	}
	hunk := content.Hunks[0]
	if hunk.OldStart != 2 || hunk.OldLines != 1 || hunk.NewStart != 2 || hunk.NewLines != 1 {
		t.Fatalf("text hunk = %#v", hunk)
	}
	if len(hunk.Lines) != 2 || hunk.Lines[0].Op != "delete" || hunk.Lines[0].Text != "old" || hunk.Lines[1].Op != "create" || hunk.Lines[1].Text != "new" {
		t.Fatalf("text hunk lines = %#v", hunk.Lines)
	}

	if got := childAt(t, diff, "labels").Kind; got != "map" {
		t.Fatalf("labels kind = %q, want map", got)
	}
	if got := childAt(t, diff, "ports").Kind; got != "list" {
		t.Fatalf("ports kind = %q, want list", got)
	}
	if got := childAt(t, diff, "groups").Kind; got != "set" {
		t.Fatalf("groups kind = %q, want set", got)
	}
	mode := childAt(t, diff, "mode")
	if mode.Kind != "scalar" || mode.Before != "0644" || mode.After != "0600" {
		t.Fatalf("mode diff = %#v", mode)
	}
}

func TestBuildDiffSensitiveUsesSummariesWithoutLeakingValues(t *testing.T) {
	diff := BuildDiff("update",
		map[string]any{
			"content":     "old-secret",
			"source_path": "/tmp/old-secret",
			"sensitive":   true,
			"summary": map[string]any{
				"sha256": "old-sha",
				"bytes":  10,
			},
		},
		map[string]any{
			"content":     "new-secret",
			"source_path": "/tmp/new-secret",
			"sensitive":   true,
			"summary": map[string]any{
				"sha256": "new-sha",
				"bytes":  10,
			},
		},
	)

	content := childAt(t, diff, "content")
	if content.Kind != "sensitive" || !content.Sensitive {
		t.Fatalf("sensitive content diff = %#v", content)
	}
	if content.BeforeSummary["sha256"] != "old-sha" || content.AfterSummary["sha256"] != "new-sha" {
		t.Fatalf("sensitive summaries = before %#v after %#v", content.BeforeSummary, content.AfterSummary)
	}

	data, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"old-secret", "new-secret", "/tmp/old-secret", "/tmp/new-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sensitive diff leaked %q: %s", forbidden, text)
		}
	}
}

func TestBuildDiffPreservesDestroyAtRoot(t *testing.T) {
	diff := BuildDiff("destroy", map[string]any{"path": "/tmp/old"}, nil)
	if diff.Action != "destroy" {
		t.Fatalf("root action = %q, want destroy", diff.Action)
	}
	path := childAt(t, diff, "path")
	if path.Action != "delete" || path.Before != "/tmp/old" {
		t.Fatalf("path diff = %#v", path)
	}
}

func childAt(t *testing.T, node DiffNode, path ...string) DiffNode {
	t.Helper()
	for _, child := range node.Children {
		if len(child.Path) != len(path) {
			continue
		}
		match := true
		for i := range path {
			if child.Path[i] != path[i] {
				match = false
				break
			}
		}
		if match {
			return child
		}
	}
	t.Fatalf("child path %v not found in %#v", path, node.Children)
	return DiffNode{}
}
