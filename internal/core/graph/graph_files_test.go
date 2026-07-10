package graph

import (
	"testing"
)

func TestSecretFileCompilesAsFileProviderWithStableAddress(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/foundation.dbf.hcl")
	address := `host.foundation1.secrets.file["/etc/myapp/token"]`
	node := nodeFor(resourceGraph, address)
	if node == nil {
		t.Fatalf("secret file node %s was not found", address)
	}
	if node.Kind != "secret" {
		t.Fatalf("kind = %q, want secret", node.Kind)
	}
	if node.Address != address {
		t.Fatalf("address = %q, want %q", node.Address, address)
	}
	if node.ProviderType != "file" || node.ProviderAddress != "file.foundation1__etc_myapp_token" {
		t.Fatalf("provider = %s %s, want file provider for preserved secret address", node.ProviderType, node.ProviderAddress)
	}
	if node.Desired["sensitive"] != true {
		t.Fatalf("desired sensitive = %#v, want true", node.Desired["sensitive"])
	}
	if node.Desired["source_path"] == "" || node.ProviderPayload["source_path"] != node.Desired["source_path"] {
		t.Fatalf("source_path desired/payload mismatch: desired=%#v payload=%#v", node.Desired, node.ProviderPayload)
	}
	if _, ok := node.Desired["content"]; ok {
		t.Fatalf("secret desired unexpectedly contains content: %#v", node.Desired)
	}
}
