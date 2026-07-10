package graph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestResourceGraphDesiredDoesNotLeakCurrentSensitiveBaseline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
	}{
		{name: "secrets file", fixture: "../testdata/fixtures/foundation.dbf.hcl"},
		{name: "sensitive file content", fixture: "../../../examples/files-plan-preview.dbf.hcl"},
		{name: "sensitive component input", fixture: "../../../examples/component-inputs.dbf.hcl"},
		{name: "sensitive service environment", fixture: "../testdata/fixtures/sensitive-service-environment.dbf.hcl"},
		{name: "sensitive variable content", fixture: "../testdata/fixtures/sensitive-variable-files.dbf.hcl"},
		{name: "sensitive apt and nftables content", fixture: "../testdata/fixtures/sensitive-apt-nftables-content.dbf.hcl"},
		{name: "ephemeral variable content", fixture: "../testdata/fixtures/ephemeral-variable-content.dbf.hcl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resourceGraph := compileGraphFixture(t, tt.fixture)
			desired := make(map[string]map[string]any, len(resourceGraph.Nodes))
			for _, node := range resourceGraph.Nodes {
				desired[node.Address] = node.Desired
			}
			data, err := json.MarshalIndent(desired, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			testassert.NoSecretLeak(t, tt.name+" ResourceGraph desired", string(data))
		})
	}
}

func TestSensitiveAPTAndNftablesContentStaysOutOfResourceGraphJSON(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/sensitive-apt-nftables-content.dbf.hcl")
	for _, address := range []string{
		`host.server1.apt.source_file["private"]`,
		`host.server1.apt.signing_key["private"]`,
		`host.server1.components.private_apt.apt.source_file["component-private"]`,
		`host.server1.components.private_apt.apt.signing_key["component-private"]`,
		`host.server1.nftables.file["main"]`,
		`host.server1.nftables.file["private"]`,
	} {
		node := nodeFor(resourceGraph, address)
		if node == nil {
			t.Fatalf("sensitive resource node missing: %s", address)
		}
		if node.Desired["sensitive"] != true {
			t.Fatalf("%s desired is not sensitive: %#v", address, node.Desired)
		}
		if _, ok := node.Desired["content"]; ok {
			t.Fatalf("%s desired contains sensitive content: %#v", address, node.Desired)
		}
		if node.ProviderPayload["content"] != testassert.SensitiveVariableDefault {
			t.Fatalf("%s provider payload lost sensitive content: %#v", address, node.ProviderPayload)
		}
	}
	for _, address := range []string{
		"host.server1.apt.cache_refresh",
		"host.server1.nftables.validate",
		"host.server1.nftables.activate",
	} {
		operation := operationFor(resourceGraph, address)
		if operation == nil || !operation.Sensitive {
			t.Fatalf("sensitive operation missing mark: %s: %#v", address, operation)
		}
	}

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "sensitive apt and nftables ResourceGraph JSON", string(data))
}

func TestWriteOnlyFileContentStaysOutOfDesired(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/ephemeral-variable-content.dbf.hcl")
	node := nodeFor(resourceGraph, `host.ephemeral1.files.file["/etc/debianform/runtime-token.txt"]`)
	if node == nil {
		t.Fatal("write-only file node missing")
	}
	for _, key := range []string{"content", "content_sha256", "content_bytes", "summary"} {
		if _, ok := node.Desired[key]; ok {
			t.Fatalf("desired contains %s: %#v", key, node.Desired)
		}
	}
	if node.Desired["content_version"] != "v1" {
		t.Fatalf("content_version = %#v, want v1", node.Desired["content_version"])
	}
	if node.Desired["content_write_only"] != true {
		t.Fatalf("content_write_only = %#v, want true", node.Desired["content_write_only"])
	}
	if node.ProviderPayload["content"] != testassert.EphemeralVariableValue {
		t.Fatalf("provider payload content = %#v, want ephemeral value", node.ProviderPayload["content"])
	}

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	testassert.NoSecretLeak(t, "write-only ResourceGraph JSON", text)
	if strings.Contains(text, "provider_payload") {
		t.Fatalf("ResourceGraph JSON exposed provider_payload:\n%s", text)
	}
}
