package merge

import (
	"strings"
	"testing"
)

func TestCompileRejectsInvalidNetworkdGenericAndRawContent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "mixed raw and generic", body: `content = "[Match]\nName=eth0\n"
section "match" {
  name = "Match"
  settings = { Name = "eth0" }
}`, want: "raw content/source is mutually exclusive"},
		{name: "mixed generic and compatibility", body: `match = { Name = "eth0" }
section "network" {
  name = "Network"
  settings = {}
}`, want: "section blocks are mutually exclusive"},
		{name: "missing section name", body: `section "match" {
  settings = { Name = "eth0" }
}`, want: "section name"},
		{name: "missing settings", body: `section "match" {
  name = "Match"
}`, want: "settings map is required"},
		{name: "invalid setting key", body: `section "match" {
  name = "Match"
  settings = { "Bad Key" = "value" }
}`, want: "section key"},
		{name: "unmarked inline key", body: `section "wireguard" {
  name = "WireGuard"
  settings = { PrivateKey = "secret" }
}`, want: "inline PrivateKey must use a sensitive value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrCompileInline(t, `
host "router" {
  systemd {
    networkd {
      network "test" {
`+tt.body+`
      }
    }
  }
}
`)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileRejectsRawNetworkdNetDevWithoutIdentity(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "router" {
  systemd {
    networkd {
      netdev "10-bad" {
        content = "[NetDev]\nName=bad0\n"
      }
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "requires [NetDev] Name and Kind") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileNetworkdPostReloadResolvesByDeclarationIdentity(t *testing.T) {
	program := compileInline(t, `
script "root_reload" {
  mode = "once"
  run  = "root"
}

component "routing" {
  script "local_reload" {
    mode = "once"
    run  = "local"
  }
  systemd {
    networkd {
      network "local" {
        content = "[Match]\nName=local0\n"
        activation {
          post_reload = script.local_reload
        }
      }
      network "root" {
        content = "[Match]\nName=root0\n"
        activation {
          post_reload = global.script.root_reload
        }
      }
    }
  }
}

host "router" {
  component "routing" {
    source = component.routing
  }
}
`)
	networks := program.Hosts[0].Components[0].Systemd.Networkd.Networks
	local := networks["local"].Activation.PostReload
	root := networks["root"].Activation.PostReload
	if local.Scope != "component" || local.DeclarationID == "" {
		t.Fatalf("local post_reload = %#v", local)
	}
	if root.Scope != "root" || root.DeclarationID == "" || root.DeclarationID == local.DeclarationID {
		t.Fatalf("root post_reload = %#v", root)
	}
}

func TestCompileRejectsUnknownNetworkdPostReloadReference(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "router" {
  systemd {
    networkd {
      network "test" {
        content = "[Match]\nName=test0\n"
        activation {
          post_reload = script.missing
        }
      }
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "activation.post_reload references unknown script.missing") {
		t.Fatalf("error = %v", err)
	}
}
