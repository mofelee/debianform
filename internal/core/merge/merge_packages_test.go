package merge

import (
	"strings"
	"testing"
)

func TestCompileRejectsDuplicatePackage(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  packages {
    install = ["curl", "curl"]
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `duplicate package "curl"`) || !strings.Contains(err.Error(), "packages.install[1]") {
		t.Fatalf("error = %v, want duplicate package with source path", err)
	}
}
