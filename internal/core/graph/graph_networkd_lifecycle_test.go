package graph

import (
	"reflect"
	"strings"
	"testing"
)

func TestNetworkdContentModesKeepStableResourceAndProviderIdentity(t *testing.T) {
	const address = `host.server1.systemd.networkd.netdev["10-dummy0"]`
	configs := map[string]string{
		"compatibility": `
host "server1" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        netdev = {
          Name = "dummy0"
          Kind = "dummy"
        }
      }
    }
  }
}
`,
		"structured": `
host "server1" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        section "identity" {
          name = "NetDev"
          settings = {
            Name = "dummy0"
            Kind = "dummy"
          }
        }
      }
    }
  }
}
`,
		"raw": `
host "server1" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        content = "[NetDev]\nName=dummy0\nKind=dummy\n"
      }
    }
  }
}
`,
	}

	var providerAddress string
	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			node := nodeFor(compileGraphInline(t, config), address)
			if node == nil {
				t.Fatalf("node %s missing", address)
			}
			if node.Address != address || node.Kind != "networkd_netdev" || node.ProviderType != "file" || node.Desired["name"] != "dummy0" {
				t.Fatalf("networkd identity changed: %#v", node)
			}
			if providerAddress == "" {
				providerAddress = node.ProviderAddress
			} else if node.ProviderAddress != providerAddress {
				t.Fatalf("provider address = %q, want %q", node.ProviderAddress, providerAddress)
			}
		})
	}
}

func TestNetworkdNetworkContentModesKeepStableResourceAndProviderIdentity(t *testing.T) {
	const address = `host.server1.systemd.networkd.network["20-dummy0"]`
	configs := map[string]string{
		"compatibility": `
host "server1" {
  systemd {
    networkd {
      network "20-dummy0" {
        match   = { Name = "dummy0" }
        network = { Address = "192.0.2.1/32" }
      }
    }
  }
}
`,
		"structured": `
host "server1" {
  systemd {
    networkd {
      network "20-dummy0" {
        section "match" {
          name     = "Match"
          settings = { Name = "dummy0" }
        }
        section "network" {
          name     = "Network"
          settings = { Address = "192.0.2.1/32" }
        }
      }
    }
  }
}
`,
		"raw": `
host "server1" {
  systemd {
    networkd {
      network "20-dummy0" {
        content = "[Match]\nName=dummy0\n\n[Network]\nAddress=192.0.2.1/32\n"
      }
    }
  }
}
`,
	}

	var providerAddress string
	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			node := nodeFor(compileGraphInline(t, config), address)
			if node == nil {
				t.Fatalf("node %s missing", address)
			}
			if node.Address != address || node.Kind != "networkd_network" || node.ProviderType != "file" {
				t.Fatalf("networkd identity changed: %#v", node)
			}
			if providerAddress == "" {
				providerAddress = node.ProviderAddress
			} else if node.ProviderAddress != providerAddress {
				t.Fatalf("provider address = %q, want %q", node.ProviderAddress, providerAddress)
			}
		})
	}
}

func TestNetworkdNetDevDeleteRunsAfterReloadBeforeReconfigure(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-old0" {
        ensure = "absent"
        section "identity" {
          name = "NetDev"
          settings = {
            Name = "old0"
            Kind = "dummy"
          }
        }
      }
      network "20-wg0" {
        content = "[Match]\nName=wg0\n"
        activation {
          reconfigure = ["wg0"]
        }
      }
    }
  }
}
`)

	reloadAddress := "host.server1.systemd.networkd.restart"
	reconfigureAddress := `host.server1.systemd.networkd.reconfigure["wg0"]`
	reload := operationFor(resourceGraph, reloadAddress)
	reconfigure := operationFor(resourceGraph, reconfigureAddress)
	if reload == nil || reconfigure == nil {
		t.Fatalf("activation operations missing: reload=%#v reconfigure=%#v", reload, reconfigure)
	}
	fragments := []string{
		"systemctl start systemd-networkd.service",
		"networkctl reload",
		"ip link delete 'old0'",
	}
	positions := make([]int, len(fragments))
	for i, fragment := range fragments {
		positions[i] = strings.Index(reload.CommandPreview, fragment)
		if positions[i] < 0 {
			t.Fatalf("reload command missing %q: %q", fragment, reload.CommandPreview)
		}
	}
	if !(positions[0] < positions[1] && positions[1] < positions[2]) {
		t.Fatalf("reload/delete command order = %q", reload.CommandPreview)
	}
	if !reflect.DeepEqual(reconfigure.DependsOn, []string{reloadAddress}) {
		t.Fatalf("reconfigure dependencies = %#v, want reload", reconfigure.DependsOn)
	}
}
