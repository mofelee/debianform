package merge

import (
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestCompileNetworkdSensitiveDiagnosticDoesNotLeakValue(t *testing.T) {
	_, err := parseOrCompileInline(t, `
variable "private_key" {
  type      = string
  sensitive = true
  default   = "not-a-real-variable-secret"
}

host "router" {
  systemd {
    networkd {
      netdev "10-bad" {
        content = "[NetDev]\nName=${var.private_key}\n"
      }
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "requires [NetDev] Name and Kind") {
		t.Fatalf("error = %v, want raw netdev identity diagnostic", err)
	}
	testassert.NoSecretLeak(t, "sensitive networkd diagnostic", err.Error())
}
