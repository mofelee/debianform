package merge

import "testing"

func TestCompileNetworkdRawContentPreservesLifecycleAndSensitivity(t *testing.T) {
	program := compileInline(t, `
variable "netdev" {
  type      = string
  sensitive = true
  default   = "[NetDev]\nName=dummy0\nKind=dummy\n"
}

host "router" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        content = var.netdev
        lifecycle {
          prevent_destroy = true
        }
      }
      network "20-dummy0" {
        content = "[Match]\nName=dummy0\n\n[Network]\nAddress=192.0.2.1/32\n"
      }
    }
  }
}
`)
	networkd := program.Hosts[0].Systemd.Networkd
	netdev := networkd.NetDevs["10-dummy0"]
	if netdev.ContentMode != "raw" || netdev.Name != "dummy0" || !netdev.Sensitive || netdev.Mode != "0600" {
		t.Fatalf("raw sensitive netdev = %#v", netdev)
	}
	if netdev.Lifecycle == nil || !netdev.Lifecycle.PreventDestroy {
		t.Fatalf("raw netdev lifecycle = %#v", netdev.Lifecycle)
	}
	network := networkd.Networks["20-dummy0"]
	if network.ContentMode != "raw" || network.Sensitive || network.Mode != "0644" || network.Summary.Bytes == 0 {
		t.Fatalf("raw network = %#v", network)
	}
}

func TestCompileNetworkdRawSourceResolvesRelativePath(t *testing.T) {
	program, err := parseOrCompileInlineWithFiles(t, `
host "router" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        source    = "dummy.netdev"
        sensitive = true
      }
    }
  }
}
`, map[string]string{"dummy.netdev": "[NetDev]\nName=dummy0\nKind=dummy\n"})
	if err != nil {
		t.Fatal(err)
	}
	netdev := program.Hosts[0].Systemd.Networkd.NetDevs["10-dummy0"]
	if netdev.SourcePath == "" || netdev.Content != "" || netdev.Name != "dummy0" || netdev.Summary.Bytes == 0 || !netdev.Sensitive {
		t.Fatalf("raw source netdev = %#v", netdev)
	}
}
