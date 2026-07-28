package graph

import (
	"strings"
	"testing"
)

func TestCompileGenericNetworkdGraphRedactsSensitiveContent(t *testing.T) {
	const secret = "graph-inline-private-key"
	resourceGraph := compileGraphInline(t, `
variable "private_key" {
  type      = string
  sensitive = true
  default   = "`+secret+`"
}

host "server1" {
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
	node := nodeFor(resourceGraph, `host.server1.systemd.networkd.netdev["10-wg0"]`)
	if node == nil || node.Desired["content_mode"] != "structured" || node.Desired["sensitive"] != true || node.Desired["name"] != "wg0" {
		t.Fatalf("generic networkd node = %#v", node)
	}
	if _, exists := node.Desired["content"]; exists {
		t.Fatalf("generic networkd desired leaked content: %#v", node.Desired)
	}
	if content, _ := node.ProviderPayload["content"].(string); !strings.Contains(content, secret) {
		t.Fatalf("provider payload lost sensitive content")
	}
}

func TestCompileRawNetworkdSourceGraphKeepsNativeIdentity(t *testing.T) {
	resourceGraph := compileGraphInlineWithFiles(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        source = "dummy.netdev"
      }
    }
  }
}
`, map[string]string{"dummy.netdev": "[NetDev]\nName=dummy0\nKind=dummy\n"})
	node := nodeFor(resourceGraph, `host.server1.systemd.networkd.netdev["10-dummy0"]`)
	if node == nil || node.Kind != "networkd_netdev" || node.ProviderType != "file" || node.Desired["content_mode"] != "raw" || node.Desired["name"] != "dummy0" {
		t.Fatalf("raw networkd node = %#v", node)
	}
	if node.Desired["source_path"] == "" || node.ProviderPayload["source_path"] == "" {
		t.Fatalf("raw networkd source path missing: %#v / %#v", node.Desired, node.ProviderPayload)
	}
}
