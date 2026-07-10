package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestNativeProviderWriteOnlyFileErrorRedactsPayload(t *testing.T) {
	node := writeOnlyFileNode("v1")
	runner := &recordingRunner{errors: []error{errors.New("remote stderr not-a-real-ephemeral-token")}}
	provider := NewNativeProvider(runner)
	_, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
	if err == nil {
		t.Fatal("apply succeeded, want injected runner failure")
	}
	if strings.Contains(err.Error(), "not-a-real-ephemeral-token") {
		t.Fatalf("error leaked payload: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("error = %v, want redacted marker", err)
	}
}

func TestNativeProviderSensitiveOperationRedactsRemoteError(t *testing.T) {
	runner := &recordingRunner{errors: []error{errors.New("remote failed with " + testassert.SensitiveVariableDefault)}}
	provider := NewNativeProvider(runner)
	_, err := provider.RunOperation(context.Background(), graph.Operation{
		Host:           "server1",
		Address:        "host.server1.nftables.validate",
		Action:         "run",
		Summary:        "validate nftables ruleset",
		Sensitive:      true,
		CommandPreview: "nft -c -f /etc/nftables.conf",
	})
	if err == nil {
		t.Fatal("sensitive operation succeeded, want injected failure")
	}
	testassert.NoSecretLeak(t, "sensitive operation error", err.Error())
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("sensitive operation error = %q, want redaction marker", err)
	}
}

func TestResourceStateForSensitiveAPTSourceDropsOriginalContent(t *testing.T) {
	content := testassert.SensitiveVariableDefault
	step := Step{Node: graph.Node{Desired: map[string]any{
		"path":      "/etc/apt/sources.list.d/private.list",
		"sensitive": true,
	}}}
	resource := resourceStateForStep(step, map[string]any{
		"exists":           true,
		"original_content": content,
	}, "2026-07-10T00:00:00Z")
	data, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "sensitive apt source state", string(data))
	if _, ok := resource.Observed["original_content"]; ok {
		t.Fatalf("sensitive state observed contains original content: %#v", resource.Observed)
	}
}
