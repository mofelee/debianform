package merge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileNetworkdGenericSectionsInDeclarationOrder(t *testing.T) {
	program := compileInline(t, `
script "reexport" {
  mode = "once"
  run  = "birdc reload out kernel4"
}

host "router" {
  systemd {
    networkd {
      netdev "10-wg0" {
        section "netdev" {
          name = "NetDev"
          settings = {
            Name = "wg0"
            Kind = "wireguard"
          }
        }
        section "wireguard" {
          name = "WireGuard"
          settings = {
            PrivateKeyFile = "/etc/wireguard/wg0.key"
            RouteTable     = "off"
          }
        }
      }

      network "20-wg0" {
        section "match" {
          name     = "Match"
          settings = { Name = "wg0" }
        }
        section "network" {
          name     = "Network"
          settings = { LinkLocalAddressing = "ipv6" }
        }
        section "ipv4" {
          name = "Address"
          settings = {
            Address        = "10.2.0.0/31"
            AddPrefixRoute = false
            Optional       = null
          }
        }
        section "ipv6" {
          name     = "Address"
          settings = { Address = ["fd64:0:2::/127", "fe80::1/64"] }
        }
        section "peer" {
          name = "Route"
          settings = {
            Destination = "10.2.0.1/32"
            Scope       = "link"
          }
        }
        activation {
          reconfigure = ["wg0"]
          post_reload = script.reexport
        }
      }
    }
  }
}
`)
	networkd := program.Hosts[0].Systemd.Networkd
	netdev := networkd.NetDevs["10-wg0"]
	if netdev.Name != "wg0" || netdev.ContentMode != "structured" {
		t.Fatalf("generic netdev identity/mode = %q/%q", netdev.Name, netdev.ContentMode)
	}
	wantNetDev := "[NetDev]\nKind=wireguard\nName=wg0\n\n[WireGuard]\nPrivateKeyFile=/etc/wireguard/wg0.key\nRouteTable=off\n"
	if netdev.Content != wantNetDev {
		t.Fatalf("generic netdev content mismatch\n--- got ---\n%s\n--- want ---\n%s", netdev.Content, wantNetDev)
	}
	network := networkd.Networks["20-wg0"]
	wantNetwork := "[Match]\nName=wg0\n\n[Network]\nLinkLocalAddressing=ipv6\n\n[Address]\nAddPrefixRoute=no\nAddress=10.2.0.0/31\n\n[Address]\nAddress=fd64:0:2::/127\nAddress=fe80::1/64\n\n[Route]\nDestination=10.2.0.1/32\nScope=link\n"
	if network.Content != wantNetwork {
		t.Fatalf("generic network content mismatch\n--- got ---\n%s\n--- want ---\n%s", network.Content, wantNetwork)
	}
	if network.ContentMode != "structured" || network.Activation == nil || network.Activation.PostReload == nil || network.Activation.PostReload.DeclarationID == "" {
		t.Fatalf("generic network mode/activation = %#v", network)
	}
}

func TestCompileNetworkdSensitiveInlineKeyIsRedacted(t *testing.T) {
	const secret = "inline-private-key-material"
	program := compileInline(t, `
variable "private_key" {
  type      = string
  sensitive = true
  default   = "`+secret+`"
}

host "router" {
  systemd {
    networkd {
      netdev "10-wg0" {
        section "netdev" {
          name = "NetDev"
          settings = {
            Name = "wg0"
            Kind = "wireguard"
          }
        }
        section "wireguard" {
          name     = "WireGuard"
          settings = { PrivateKey = var.private_key }
        }
      }
    }
  }
}
`)
	netdev := program.Hosts[0].Systemd.Networkd.NetDevs["10-wg0"]
	if !netdev.Sensitive || netdev.Mode != "0600" || !strings.Contains(netdev.Content, secret) {
		t.Fatalf("sensitive generic netdev = %#v", netdev)
	}
	data, err := json.Marshal(program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "PrivateKey") {
		t.Fatalf("HostSpec JSON leaked inline key: %s", data)
	}
}
