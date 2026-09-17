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

func TestResourceDependenciesUseResolvedServiceName(t *testing.T) {
	program := compileInline(t, `
component "app" {
  input "tag" {
    type = string
  }

  files {
    file "config" {
      path       = "/etc/app/${input.tag}.conf"
      content    = "ok"
      depends_on = [service.svc]
    }
  }

  services {
    service "svc" {
      name  = "app-${input.tag}"
      state = "running"
    }
  }
}

host "server1" {
  component "one" {
    source = component.app
    inputs = { tag = "one" }
  }
}
`)
	got := program.Hosts[0].Components[0].ExplicitDependencies
	want := ir.ResourceDependencySpec{
		From:      `host.server1.components.one.files.file["/etc/app/one.conf"]`,
		DependsOn: `host.server1.components.one.services.service["app-one"]`,
	}
	if len(got) != 1 {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
	got[0].Source = ir.SourceRef{}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
}

func TestResourceDependenciesResolveCrossComponentArtifactInstall(t *testing.T) {
	program := compileInline(t, `
component "zbin" {
  type    = "binary"
  version = "1.0.0"

  source "amd64" {
    url    = "https://example.invalid/tool-amd64.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  extract {
    format  = "tar.gz"
    include = "tool"
  }

  install {
    path = "/usr/local/bin/tool"
  }
}

component "link" {
  input "peer" {
    type = string
  }

  services {
    service "svc" {
      name       = "tool-${input.peer}"
      depends_on = [artifact["/usr/local/bin/tool"]]
      enabled    = true
      state      = "running"
    }
  }
}

host "server1" {
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  components = [component.zbin]

  component "alink" {
    source = component.link
    inputs = { peer = "alpha" }
  }
}
`)
	if len(program.Hosts[0].Components) != 2 {
		t.Fatalf("components = %#v", program.Hosts[0].Components)
	}
	got := program.Hosts[0].Components[1].ExplicitDependencies
	want := ir.ResourceDependencySpec{
		From:      `host.server1.components.alink.services.service["tool-alpha"]`,
		DependsOn: `host.server1.components.zbin.artifact.install["/usr/local/bin/tool"]`,
	}
	if len(got) != 1 {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
	got[0].Source = ir.SourceRef{}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
}

func TestResourceDependenciesResolveHostLevelArtifactInstall(t *testing.T) {
	program := compileInline(t, `
component "zbin" {
  type    = "binary"
  version = "1.0.0"

  source "amd64" {
    url    = "https://example.invalid/tool-amd64.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/tool"
  }
}

host "server1" {
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  components = [component.zbin]

  services {
    service "runner" {
      depends_on = [artifact["/usr/local/bin/tool"]]
      state      = "running"
    }
  }
}
`)
	got := program.Hosts[0].ExplicitDependencies
	want := ir.ResourceDependencySpec{
		From:      `host.server1.services.service["runner"]`,
		DependsOn: `host.server1.components.zbin.artifact.install["/usr/local/bin/tool"]`,
	}
	if len(got) != 1 {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
	got[0].Source = ir.SourceRef{}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("explicit dependencies = %#v, want %#v", got, want)
	}
}

func TestResourceDependenciesRejectUnknownAndAmbiguousArtifacts(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "unknown",
			hcl: `
component "link" {
  services {
    service "svc" {
      depends_on = [artifact["/usr/local/bin/missing"]]
      state      = "running"
    }
  }
}

host "server1" {
  component "alink" { source = component.link }
}`,
			want: `depends_on references unknown artifact["/usr/local/bin/missing"]`,
		},
		{
			name: "ambiguous",
			hcl: `
component "one" {
  type    = "binary"
  version = "1.0.0"

  source "amd64" {
    url    = "https://example.invalid/one.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/tool"
  }
}

component "two" {
  type    = "binary"
  version = "1.0.0"

  source "amd64" {
    url    = "https://example.invalid/two.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/tool"
  }
}

component "link" {
  services {
    service "svc" {
      depends_on = [artifact["/usr/local/bin/tool"]]
      state      = "running"
    }
  }
}

host "server1" {
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  components = [component.one, component.two]

  component "alink" { source = component.link }
}`,
			want: `depends_on references ambiguous artifact["/usr/local/bin/tool"]`,
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

func TestResourceDependenciesArtifactReferenceValidatedWithoutRuntimeFacts(t *testing.T) {
	cfg := parseInline(t, `
component "link" {
  services {
    service "svc" {
      depends_on = [artifact["/usr/local/bin/missing"]]
      state      = "running"
    }
  }
}

host "server1" {
  component "alink" { source = component.link }
}`)
	_, err := CompileWithOptions(cfg, CompileOptions{ValidateRuntimeTemplates: true})
	if err == nil || !strings.Contains(err.Error(), `depends_on references unknown artifact["/usr/local/bin/missing"]`) {
		t.Fatalf("validate error = %v", err)
	}
}
