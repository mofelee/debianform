package merge

import (
	"strings"
	"testing"
)

func TestPackageConffilePoliciesCompile(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  packages {
    install = ["curl"]
    package "keep-package" { conffile_policy = "keep" }
    package "replace-package" { conffile_policy = "replace" }
    package "error-package" { conffile_policy = "error" }
  }
}
`)
	got := map[string]string{}
	set := map[string]bool{}
	for _, item := range program.Hosts[0].Packages.Install {
		got[item.Name] = item.ConffilePolicy
		set[item.Name] = item.ConffilePolicySet
	}
	if got["curl"] != "" || set["curl"] {
		t.Fatalf("list package policy = %q set=%v, want implicit keep", got["curl"], set["curl"])
	}
	for _, policy := range []string{"keep", "replace", "error"} {
		name := policy + "-package"
		if got[name] != policy || !set[name] {
			t.Fatalf("%s policy = %q set=%v", name, got[name], set[name])
		}
	}
}

func TestPackageConffilePolicyRejectsInvalidValue(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  packages {
    package "vault" { conffile_policy = "prompt" }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "conffile_policy must be keep, replace, or error") || !strings.Contains(err.Error(), ".conffile_policy") {
		t.Fatalf("invalid conffile policy error = %v", err)
	}
}
