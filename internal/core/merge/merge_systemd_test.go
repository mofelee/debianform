package merge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileSensitiveComponentInputPropagatesToSystemdUnits(t *testing.T) {
	program := compileInline(t, `
component "app" {
  input "token" {
    type      = string
    sensitive = true
  }

  systemd {
    unit "raw-token.service" {
      content = "TOKEN=${input.token}\n"
    }

    service_unit "structured-token" {
      run = "/usr/bin/true"
      environment = {
        API_TOKEN = input.token
      }
    }
  }
}

host "server1" {
  component "app" {
    source = component.app
    inputs = {
      token = "systemd-secret-token"
    }
  }
}
`)
	units := program.Hosts[0].Components[0].Systemd.Units
	rawUnit := units["raw-token.service"]
	if !rawUnit.Sensitive {
		t.Fatalf("raw unit sensitive = false")
	}
	if !strings.Contains(rawUnit.Content, "systemd-secret-token") {
		t.Fatalf("raw unit in-memory content missing secret: %q", rawUnit.Content)
	}
	structuredUnit := units["structured-token.service"]
	if !structuredUnit.Sensitive {
		t.Fatalf("structured unit sensitive = false")
	}
	if !strings.Contains(structuredUnit.Content, "systemd-secret-token") {
		t.Fatalf("structured unit in-memory content missing secret: %q", structuredUnit.Content)
	}

	data, err := json.Marshal(program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "systemd-secret-token") {
		t.Fatalf("HostSpec JSON leaked systemd secret: %s", data)
	}
}

func TestCompileSystemdServiceHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/systemd-service.dbf.hcl", "../testdata/hostspec/systemd-service.golden.json")
}

func TestCompileSystemdServiceUnitStructured(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    service_unit "myapp" {
      description   = "My App"
      run           = ["/opt/myapp/bin/myapp", "--config", "/etc/myapp/config.yaml"]
      type          = "simple"
      user          = "myapp"
      group         = "myapp"
      working_dir   = "/var/lib/myapp"
      restart       = "always"
      restart_delay = "5s"
      wants         = ["network-online.target"]
      after         = ["network-online.target"]
      stdout        = "journal"
      stderr        = "journal"

      environment = {
        MYAPP_ENV = "production"
        PATH      = "/usr/local/bin:/usr/bin:/bin"
      }

      service_config = {
        AmbientCapabilities = "CAP_NET_BIND_SERVICE"
        NoNewPrivileges     = true
      }
    }
  }

  services {
    service "myapp" {
      enabled = true
      state   = "running"
    }
  }
}
`)

	host := program.Hosts[0]
	unit, ok := host.Systemd.Units["myapp.service"]
	if !ok {
		t.Fatalf("myapp.service unit missing: %#v", host.Systemd.Units)
	}
	wantContent := `[Unit]
Description=My App
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=myapp
Group=myapp
WorkingDirectory=/var/lib/myapp
Environment=MYAPP_ENV=production
Environment=PATH=/usr/local/bin:/usr/bin:/bin
ExecStart=/opt/myapp/bin/myapp --config /etc/myapp/config.yaml
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal
AmbientCapabilities=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
`
	if unit.Content != wantContent {
		t.Fatalf("unit content mismatch\n--- got ---\n%s\n--- want ---\n%s", unit.Content, wantContent)
	}
	if unit.Path != "/etc/systemd/system/myapp.service" || unit.Owner != "root" || unit.Group != "root" || unit.Mode != "0644" {
		t.Fatalf("unit metadata = %#v", unit)
	}
	if got := host.Services.Services["myapp"].Unit; got != "myapp.service" {
		t.Fatalf("service unit = %q, want myapp.service", got)
	}
}

func TestCompileSystemdServiceUnitRawContent(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    service_unit "myapp" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }
}
`)

	unit, ok := program.Hosts[0].Systemd.Units["myapp.service"]
	if !ok {
		t.Fatalf("myapp.service unit missing")
	}
	if unit.Content != "[Service]\nExecStart=/bin/true\n" {
		t.Fatalf("unit content = %q", unit.Content)
	}
}

func TestCompileSystemdTimerResolvedAndJournald(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    timer "cleanup" {
      description = "Cleanup cache"
      enable      = true
      state       = "running"

      timer = {
        OnCalendar = "daily"
        Persistent = true
      }
    }

    resolved {
      enable = true
      state  = "running"

      resolve = {
        DNS      = ["1.1.1.1", "9.9.9.9"]
        DNSSEC   = "allow-downgrade"
        DNSStubListener = false
      }
    }

    journald {
      state = "reloaded"

      journal = {
        SystemMaxUse = "1G"
        Compress     = true
      }
    }
  }

  assert {
    condition = self.systemd.timers["cleanup.timer"].timer.Persistent == "yes"
    message   = "timer assertion failed"
  }

  assert {
    condition = self.systemd.resolved.resolve.DNS[0] == "1.1.1.1"
    message   = "resolved assertion failed"
  }

  assert {
    condition = self.systemd.journald.journal.Compress == "yes"
    message   = "journald assertion failed"
  }
}
`)

	host := program.Hosts[0]
	timer, ok := host.Systemd.Timers["cleanup.timer"]
	if !ok {
		t.Fatalf("cleanup.timer missing: %#v", host.Systemd.Timers)
	}
	for _, want := range []string{"[Timer]", "OnCalendar=daily", "Persistent=yes", "WantedBy=timers.target"} {
		if !strings.Contains(timer.Unit.Content, want) {
			t.Fatalf("timer content missing %q:\n%s", want, timer.Unit.Content)
		}
	}
	if timer.Enable == nil || !*timer.Enable || timer.State != "running" {
		t.Fatalf("timer service settings = %#v", timer)
	}
	if host.Systemd.Resolved == nil ||
		!strings.Contains(host.Systemd.Resolved.Unit.Content, "DNS=1.1.1.1") ||
		!strings.Contains(host.Systemd.Resolved.Unit.Content, "DNS=9.9.9.9") ||
		!strings.Contains(host.Systemd.Resolved.Unit.Content, "DNSStubListener=no") {
		t.Fatalf("resolved content = %#v", host.Systemd.Resolved)
	}
	if host.Systemd.Journald == nil || !strings.Contains(host.Systemd.Journald.Unit.Content, "Compress=yes") || host.Systemd.Journald.State != "reloaded" {
		t.Fatalf("journald spec = %#v", host.Systemd.Journald)
	}
}

func TestCompileSystemdNetworkdWireGuard(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = 51820
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = "off"
        }

        wireguard_peer "server2" {
          PublicKey  = "peer-public-key"
          AllowedIPs = ["10.80.0.2/32"]
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address = ["10.80.0.1/30"]
        }
      }
    }
  }

  assert {
    condition = self.systemd.networkd.netdev["10-wg0"].wireguard.RouteTable == "off"
    message   = "RouteTable must stay off"
  }
}
`)

	host := program.Hosts[0]
	if host.Systemd.Networkd == nil {
		t.Fatal("networkd spec missing")
	}
	netdev := host.Systemd.Networkd.NetDevs["10-wg0"]
	wantNetdev := `[NetDev]
Kind=wireguard
Name=wg0

[WireGuard]
ListenPort=51820
PrivateKeyFile=/etc/wireguard/private.key
RouteTable=off

[WireGuardPeer]
AllowedIPs=10.80.0.2/32
PublicKey=peer-public-key
`
	if netdev.Content != wantNetdev {
		t.Fatalf("netdev content mismatch\n--- got ---\n%s\n--- want ---\n%s", netdev.Content, wantNetdev)
	}
	network := host.Systemd.Networkd.Networks["20-wg0"]
	wantNetwork := `[Match]
Name=wg0

[Network]
Address=10.80.0.1/30
`
	if network.Content != wantNetwork {
		t.Fatalf("network content mismatch\n--- got ---\n%s\n--- want ---\n%s", network.Content, wantNetwork)
	}
}

func TestCompileNetworkdWireGuardPeerAttributeMap(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard_peer = {
          laptop = {
            PublicKey  = "laptop-public-key"
            AllowedIPs = ["10.80.0.10/32"]
          }
          server2 = {
            PublicKey  = "peer-public-key"
            AllowedIPs = ["10.80.0.2/32"]
          }
        }
      }
    }
  }
}
`)
	netdev := program.Hosts[0].Systemd.Networkd.NetDevs["10-wg0"]
	if len(netdev.WireGuardPeers) != 2 {
		t.Fatalf("wireguard peers = %#v", netdev.WireGuardPeers)
	}
	if !strings.Contains(netdev.Content, "PublicKey=laptop-public-key\n") || !strings.Contains(netdev.Content, "PublicKey=peer-public-key\n") {
		t.Fatalf("netdev content does not contain both WireGuard peers:\n%s", netdev.Content)
	}
}

func TestCompileRejectsDuplicateWireGuardPeerAttributeAndBlock(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard_peer = {
          server2 = {
            PublicKey  = "peer-public-key"
            AllowedIPs = ["10.80.0.2/32"]
          }
        }

        wireguard_peer "server2" {
          PublicKey  = "other-peer-public-key"
          AllowedIPs = ["10.80.0.3/32"]
        }
      }
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `duplicate host.server1.systemd.networkd.netdev["10-wg0"].wireguard_peer["server2"]`) {
		t.Fatalf("error = %v, want duplicate wireguard peer rejection", err)
	}
}

func TestCompileRejectsInlineWireGuardPrivateKey(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard = {
          PrivateKey = "inline-private-key"
        }
      }
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "use PrivateKeyFile instead of inline PrivateKey") {
		t.Fatalf("error = %v, want inline private key rejection", err)
	}
}
