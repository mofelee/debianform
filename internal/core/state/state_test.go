package state

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestSanitizeDesiredRedactsContentAndSensitiveSource(t *testing.T) {
	desired := map[string]any{
		"path":        "/etc/app/token",
		"content":     "not-a-real-secret-token",
		"source_path": "fixtures/app-token.txt",
		"sensitive":   true,
	}

	got := SanitizeDesired(desired)
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	testassert.NoSecretLeak(t, "sanitized desired", text)
	if strings.Contains(text, "fixtures/app-token.txt") {
		t.Fatalf("sanitized desired leaked sensitive source path: %s", text)
	}
	if got["content_sha256"] == "" {
		t.Fatalf("content_sha256 missing from sanitized desired: %#v", got)
	}
	if got["content_bytes"] != len("not-a-real-secret-token") {
		t.Fatalf("content_bytes = %#v", got["content_bytes"])
	}
}

func TestSanitizeObservedUsesSensitiveRedaction(t *testing.T) {
	observed := map[string]any{
		"path":        "/etc/app/token",
		"content":     "not-a-real-secret-token",
		"source_path": "fixtures/app-token.txt",
		"sensitive":   true,
	}

	got := SanitizeObserved(observed)
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	testassert.NoSecretLeak(t, "sanitized observed", text)
	if strings.Contains(text, "fixtures/app-token.txt") {
		t.Fatalf("sanitized observed leaked sensitive source path: %s", text)
	}
	if got["content_sha256"] == "" {
		t.Fatalf("content_sha256 missing from sanitized observed: %#v", got)
	}
}

func TestStateDecodeDefaultsToVersionTwo(t *testing.T) {
	st, err := Decode([]byte(`{"resources":{}}`), "server1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != Version {
		t.Fatalf("version = %d, want %d", st.Version, Version)
	}
	if st.Host != "server1" {
		t.Fatalf("host = %q, want server1", st.Host)
	}
	if st.Resources == nil {
		t.Fatalf("resources map was not initialized")
	}
}

func TestStateEncodesRuntimeFacts(t *testing.T) {
	st := Empty("server1")
	st.Facts = &ir.HostFacts{System: ir.SystemFacts{
		Architecture: "amd64",
		Codename:     "trixie",
	}}
	data, err := Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"facts"`) || !strings.Contains(string(data), `"architecture": "amd64"`) {
		t.Fatalf("encoded state missing facts:\n%s", data)
	}
}
