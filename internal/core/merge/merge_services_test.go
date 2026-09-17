package merge

import (
	"strings"
	"testing"
)

func TestCompileServiceNameOverride(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  services {
    service "app" {
      name    = "custom-app"
      enabled = true
      state   = "running"
    }
  }
}
`)
	services := program.Hosts[0].Services.Services
	service, ok := services["custom-app"]
	if !ok {
		t.Fatalf("services = %#v", services)
	}
	if service.Unit != "custom-app.service" {
		t.Fatalf("service unit = %q", service.Unit)
	}
}

func TestCompileRejectsDuplicateResolvedServiceName(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  services {
    service "one" {
      name = "same"
    }

    service "two" {
      name = "same"
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `service "same" conflicts with service declared`) {
		t.Fatalf("error = %v, want duplicate resolved service name rejection", err)
	}
}

func TestCompileRejectsEmptyServiceNameOverride(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  services {
    service "app" {
      name = ""
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "name must be non-empty") {
		t.Fatalf("error = %v, want empty name rejection", err)
	}
}
