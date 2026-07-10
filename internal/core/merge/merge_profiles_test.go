package merge

import (
	"strings"
	"testing"
)

func TestCompileRejectsProfileImportCycle(t *testing.T) {
	cfg := parseInline(t, `
profile "a" {
  imports = [profile.b]
}

profile "b" {
  imports = [profile.a]
}

host "server1" {
  imports = [profile.a]
}
`)

	_, err := Compile(cfg)
	if err == nil || !strings.Contains(err.Error(), "profile.a -> profile.b -> profile.a") {
		t.Fatalf("Compile() error = %v, want profile import cycle", err)
	}
}

func TestCompileRejectsProfileHostOnlyFields(t *testing.T) {
	_, err := parseOrCompileInline(t, `
profile "bad" {
  system {
    hostname = "bad"
  }
}

host "server1" {
  imports = [profile.bad]
}
`)
	if err == nil || !strings.Contains(err.Error(), "profile.bad.system.hostname is host-only") {
		t.Fatalf("error = %v, want host-only field error", err)
	}
}
