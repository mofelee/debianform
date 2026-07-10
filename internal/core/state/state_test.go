package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestStateDecodeEmptyReturnsCurrentState(t *testing.T) {
	st, err := Decode(nil, "server1")
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

func TestStateDecodeRejectsIncompatibleOrForeignState(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "missing version",
			data:    `{"host":"server1","resources":{}}`,
			wantErr: "unsupported version 0",
		},
		{
			name:    "old version",
			data:    `{"version":1,"host":"server1","resources":{}}`,
			wantErr: "unsupported version 1",
		},
		{
			name:    "newer version",
			data:    `{"version":3,"host":"server1","resources":{}}`,
			wantErr: "newer version 3",
		},
		{
			name:    "missing host",
			data:    `{"version":2,"resources":{}}`,
			wantErr: "state host is empty",
		},
		{
			name:    "foreign host",
			data:    `{"version":2,"host":"server2","resources":{}}`,
			wantErr: `state host "server2" does not match requested host "server1"`,
		},
		{
			name:    "foreign resource host",
			data:    `{"version":2,"host":"server1","resources":{"host.server1.files.file[\"/tmp/example\"]":{"host":"server2","kind":"file","ownership":"managed","desired_digest":"digest"}}}`,
			wantErr: `belongs to host "server2", expected "server1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.data), "server1")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Decode() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestStateDecodeNormalizesCompatibleResourceHost(t *testing.T) {
	address := `host.server1.files.file["/tmp/example"]`
	st, err := Decode([]byte(`{"version":2,"host":"server1","resources":{"host.server1.files.file[\"/tmp/example\"]":{"kind":"file","ownership":"managed","desired_digest":"digest"}}}`), "server1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Resources == nil {
		t.Fatal("resources map was not initialized")
	}
	if got := st.Resources[address].Host; got != "server1" {
		t.Fatalf("resource host = %q, want server1", got)
	}
}

func TestStateNormalizeDoesNotMutateInputResourceHost(t *testing.T) {
	address := `host.server1.files.file["/tmp/example"]`
	st := Empty("server1")
	st.Resources[address] = Resource{Kind: "file", Ownership: "managed", DesiredDigest: "digest"}

	normalized, err := Normalize(st, "server1")
	if err != nil {
		t.Fatal(err)
	}
	if got := normalized.Resources[address].Host; got != "server1" {
		t.Fatalf("normalized resource host = %q, want server1", got)
	}
	if got := st.Resources[address].Host; got != "" {
		t.Fatalf("input resource host = %q, want unchanged empty host", got)
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

func TestPrepareWriteAdvancesNormalizedCopy(t *testing.T) {
	address := `host.server1.files.file["/tmp/example"]`
	st := Empty("server1")
	st.Serial = 7
	st.UpdatedAt = "2026-07-09T12:00:00Z"
	st.Resources[address] = Resource{Kind: "file", Ownership: "managed", DesiredDigest: "digest"}

	committed, err := PrepareWrite(st, "server1")
	if err != nil {
		t.Fatal(err)
	}
	if committed.Serial != 8 {
		t.Fatalf("committed serial = %d, want 8", committed.Serial)
	}
	if _, err := time.Parse(time.RFC3339, committed.UpdatedAt); err != nil {
		t.Fatalf("committed updated_at = %q: %v", committed.UpdatedAt, err)
	}
	if got := committed.Resources[address].Host; got != "server1" {
		t.Fatalf("committed resource host = %q, want server1", got)
	}
	if st.Serial != 7 || st.UpdatedAt != "2026-07-09T12:00:00Z" || st.Resources[address].Host != "" {
		t.Fatalf("PrepareWrite mutated input state: %#v", st)
	}
}

func TestEncodeIsRevisionPure(t *testing.T) {
	address := `host.server1.files.file["/tmp/example"]`
	st := Empty("server1")
	st.Serial = 7
	st.UpdatedAt = "2026-07-09T12:00:00Z"
	st.Resources[address] = Resource{Kind: "file", Ownership: "managed", DesiredDigest: "digest"}

	data, err := Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	var encoded State
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	if encoded.Serial != st.Serial || encoded.UpdatedAt != st.UpdatedAt {
		t.Fatalf("encoded revision = serial %d updated_at %q, want serial %d updated_at %q", encoded.Serial, encoded.UpdatedAt, st.Serial, st.UpdatedAt)
	}
	if got := encoded.Resources[address].Host; got != "server1" {
		t.Fatalf("encoded resource host = %q, want normalized server1", got)
	}
	if st.Resources[address].Host != "" {
		t.Fatalf("Encode mutated input resource host: %#v", st.Resources[address])
	}
}
