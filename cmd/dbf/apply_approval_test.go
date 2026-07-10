package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	coreengine "github.com/mofelee/debianform/internal/core/engine"
	"github.com/mofelee/debianform/internal/core/graph"
	coreplan "github.com/mofelee/debianform/internal/core/plan"
)

func TestSameApprovedPlanIgnoresGeneratedAtButChecksExecutionPayload(t *testing.T) {
	preview := approvalTestPlan("preview payload")
	matching := approvalTestPlan("preview payload")
	previewDoc := approvalTestDocument(preview, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	matchingDoc := approvalTestDocument(matching, time.Date(2026, 7, 10, 12, 1, 0, 0, time.UTC))

	if !sameVisiblePlanDocument(previewDoc, matchingDoc) {
		t.Fatal("documents that differ only by GeneratedAt should match")
	}
	if !sameApprovedPlan(preview, matching, previewDoc, matchingDoc) {
		t.Fatal("equivalent execution plans should reuse approval")
	}

	changed := approvalTestPlan("changed hidden payload")
	changedDoc := approvalTestDocument(changed, time.Date(2026, 7, 10, 12, 2, 0, 0, time.UTC))
	if !sameVisiblePlanDocument(previewDoc, changedDoc) {
		t.Fatal("test setup: ProviderPayload should not change the rendered plan document")
	}
	if sameApprovedPlan(preview, changed, previewDoc, changedDoc) {
		t.Fatal("hidden execution payload change incorrectly reused approval")
	}
}

func TestReviewExecutionPlanReusesMatchingApproval(t *testing.T) {
	preview := approvalTestPlan("same payload")
	actual := approvalTestPlan("same payload")
	previewDoc := approvalTestDocument(preview, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	actualDoc := approvalTestDocument(actual, time.Date(2026, 7, 10, 12, 1, 0, 0, time.UTC))
	confirmCalls := 0
	var output bytes.Buffer

	err := reviewExecutionPlan(preview, actual, previewDoc, actualDoc, false, &output, coreplan.TextOptions{}, func() bool {
		confirmCalls++
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalls != 0 {
		t.Fatalf("confirmation calls = %d, want 0 for matching locked plan", confirmCalls)
	}
	if strings.Count(output.String(), "Plan:") != 1 || !strings.Contains(output.String(), "Execution plan (state lock held):") {
		t.Fatalf("locked plan output = %q", output.String())
	}
}

func TestReviewExecutionPlanAutoApproveStillPrintsChangedActualPlan(t *testing.T) {
	preview := approvalTestPlan("preview payload")
	actual := approvalTestPlan("changed hidden payload")
	previewDoc := approvalTestDocument(preview, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	actualDoc := approvalTestDocument(actual, time.Date(2026, 7, 10, 12, 1, 0, 0, time.UTC))
	confirmCalls := 0
	var output bytes.Buffer

	err := reviewExecutionPlan(preview, actual, previewDoc, actualDoc, true, &output, coreplan.TextOptions{}, func() bool {
		confirmCalls++
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalls != 0 {
		t.Fatalf("confirmation calls = %d, want 0 with auto-approve", confirmCalls)
	}
	if strings.Count(output.String(), "Plan:") != 1 || !strings.Contains(output.String(), actual.Steps[0].Address) {
		t.Fatalf("auto-approved locked plan output = %q", output.String())
	}
}

func TestReviewExecutionPlanChangedPlanCanBeApprovedAgain(t *testing.T) {
	preview := approvalTestPlan("preview payload")
	actual := approvalTestPlan("changed hidden payload")
	previewDoc := approvalTestDocument(preview, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	actualDoc := approvalTestDocument(actual, time.Date(2026, 7, 10, 12, 1, 0, 0, time.UTC))
	confirmCalls := 0
	var output bytes.Buffer

	err := reviewExecutionPlan(preview, actual, previewDoc, actualDoc, false, &output, coreplan.TextOptions{}, func() bool {
		confirmCalls++
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalls != 1 {
		t.Fatalf("confirmation calls = %d, want 1 for changed locked plan", confirmCalls)
	}
	if !strings.Contains(output.String(), "approval is required again") {
		t.Fatalf("changed plan output = %q", output.String())
	}
}

func TestReviewExecutionPlanChangedToNoOpRequiresApprovalAgain(t *testing.T) {
	preview := approvalTestPlan("preview payload")
	actual := coreengine.Plan{Summary: coreplan.Summary{NoOp: 1}}
	previewDoc := approvalTestDocument(preview, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	actualDoc := approvalTestDocument(actual, time.Date(2026, 7, 10, 12, 1, 0, 0, time.UTC))
	confirmCalls := 0
	var output bytes.Buffer

	err := reviewExecutionPlan(preview, actual, previewDoc, actualDoc, false, &output, coreplan.TextOptions{}, func() bool {
		confirmCalls++
		return false
	})
	if err == nil || !strings.Contains(err.Error(), "apply cancelled") {
		t.Fatalf("review error = %v, want cancellation", err)
	}
	if confirmCalls != 1 {
		t.Fatalf("confirmation calls = %d, want 1 for changed no-op plan", confirmCalls)
	}
	for _, want := range []string{"No changes.", "approval is required again"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("changed no-op output missing %q: %q", want, output.String())
		}
	}
}

func approvalTestPlan(providerPayload string) coreengine.Plan {
	address := `host.server1.files.file["/tmp/approval"]`
	return coreengine.Plan{
		Steps: []coreengine.Step{{
			Address: address,
			Host:    "server1",
			Action:  coreengine.ActionCreate,
			Summary: "create approval fixture",
			Node: graph.Node{
				Address:         address,
				Host:            "server1",
				Kind:            "file",
				Summary:         "create approval fixture",
				Desired:         map[string]any{"path": "/tmp/approval", "content": "redacted display"},
				ProviderPayload: map[string]any{"content": providerPayload},
			},
		}},
		Summary: coreplan.Summary{Create: 1},
	}
}

func approvalTestDocument(plan coreengine.Plan, now time.Time) coreplan.Document {
	return plan.Document(coreplan.Options{
		CommandFile: "main.dbf.hcl",
		Host:        "server1",
		Now:         func() time.Time { return now },
	})
}
