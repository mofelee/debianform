package merge

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
)

func TestResourceDependenciesResolveAfterProfileMerge(t *testing.T) {
	program := compileInline(t, `
profile "package" {
  packages {
    package "vault" {}
  }
}

profile "configuration" {
  files {
    file "vault_config" {
      path       = "/etc/vault.d/vault.hcl"
      content    = "storage {}"
      depends_on = [package.vault]
    }
  }
  services {
    service "vault" {
      depends_on = [file.vault_config]
      state      = "running"
    }
  }
}

host "server1" {
  imports = [profile.package, profile.configuration]
}
`)
	want := []ir.ResourceDependencySpec{
		{From: `host.server1.files.file["/etc/vault.d/vault.hcl"]`, DependsOn: `host.server1.packages.install["vault"]`},
		{From: `host.server1.services.service["vault"]`, DependsOn: `host.server1.files.file["/etc/vault.d/vault.hcl"]`},
	}
	got := program.Hosts[0].ExplicitDependencies
	if len(got) != len(want) {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
	for i := range want {
		got[i].Source = ir.SourceRef{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
}

func TestResourceDependenciesResolveInsideComponentInstance(t *testing.T) {
	program := compileInline(t, `
component "vault" {
  packages {
    package "vault" {}
  }
  files {
    file "config" {
      path       = "/etc/vault.d/vault.hcl"
      content    = "storage {}"
      depends_on = [package.vault]
    }
  }
  services {
    service "vault" {
      depends_on = [file.config]
      state      = "running"
    }
  }
}

host "server1" {
  component "vault_server" { source = component.vault }
}
`)
	got := program.Hosts[0].Components[0].ExplicitDependencies
	if len(got) != 2 {
		t.Fatalf("component dependencies = %#v", got)
	}
	if got[0].From != `host.server1.components.vault_server.files.file["/etc/vault.d/vault.hcl"]` || got[0].DependsOn != `host.server1.components.vault_server.packages.install["vault"]` {
		t.Fatalf("component file dependency = %#v", got[0])
	}
	if got[1].From != `host.server1.components.vault_server.services.service["vault"]` || got[1].DependsOn != got[0].From {
		t.Fatalf("component service dependency = %#v", got[1])
	}
}

func TestResourceDependenciesRejectUnknownAndListFormTargets(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "unknown",
			hcl: `
host "server1" {
  files {
    file "/tmp/app" {
      content    = "ok"
      depends_on = [package.missing]
    }
  }
}`,
			want: "depends_on references unknown package.missing",
		},
		{
			name: "list package",
			hcl: `
host "server1" {
  packages { install = ["vault"] }
  files {
    file "/tmp/app" {
      content    = "ok"
      depends_on = [package.vault]
    }
  }
}`,
			want: "package.vault is list-form and cannot be referenced",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrCompileInline(t, tt.hcl)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), ".depends_on[0]") {
				t.Fatalf("error lacks depends_on source path: %v", err)
			}
		})
	}
}
