package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileSystemdServiceResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/systemd-service.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/systemd-service.golden.json", got)

	serviceDeps := dependsOnFor(resourceGraph, `host.service1.services.service["myapp"]`)
	for _, want := range []string{
		`host.service1.systemd.unit["myapp.service"]`,
		`host.service1.systemd.daemon_reload`,
	} {
		if !containsString(serviceDeps, want) {
			t.Fatalf("myapp service deps = %#v, want %q", serviceDeps, want)
		}
	}
}

func TestCompileServiceRestartOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  systemd {
    unit "worker.service" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }

  services {
    service "worker" {
      state = "restarted"
    }
  }
}
`)

	if !hasOperation(resourceGraph, `host.server1.services.service["worker"].restart`) {
		t.Fatalf("restart operation missing: %#v", resourceGraph.Operations)
	}
}

func TestCompileServiceUnitDependency(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  systemd {
    service_unit "worker" {
      run = ["/bin/true"]
    }
  }

  services {
    service "worker" {
      enabled = true
      state   = "running"
    }
  }
}
`)

	serviceDeps := dependsOnFor(resourceGraph, `host.server1.services.service["worker"]`)
	for _, want := range []string{
		`host.server1.systemd.unit["worker.service"]`,
		`host.server1.systemd.daemon_reload`,
	} {
		if !containsString(serviceDeps, want) {
			t.Fatalf("service deps = %#v, want %q", serviceDeps, want)
		}
	}
}

func TestCompileSystemdTimerResolvedAndJournaldGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  packages {
    install = ["systemd-resolved"]
  }

  systemd {
    timer "cleanup" {
      enable = true
      state  = "running"

      timer = {
        OnCalendar = "daily"
      }
    }

    resolved {
      enable = true

      resolve = {
        DNS = ["1.1.1.1", "9.9.9.9"]
      }
    }

    journald {
      state = "reloaded"

      journal = {
        SystemMaxUse = "1G"
      }
    }
  }
}
`)

	timerAddress := `host.server1.systemd.timer["cleanup.timer"]`
	timer := nodeFor(resourceGraph, timerAddress)
	if timer == nil || timer.Kind != "systemd_unit" || timer.Desired["name"] != "cleanup.timer" {
		t.Fatalf("timer node = %#v", timer)
	}
	content, _ := timer.Desired["content"].(string)
	for _, want := range []string{"[Timer]", "OnCalendar=daily", "WantedBy=timers.target"} {
		if !strings.Contains(content, want) {
			t.Fatalf("timer content missing %q:\n%s", want, content)
		}
	}

	daemonReload := operationFor(resourceGraph, "host.server1.systemd.daemon_reload")
	if daemonReload == nil || !containsString(daemonReload.TriggeredBy, timerAddress) {
		t.Fatalf("daemon reload = %#v, want timer trigger", daemonReload)
	}
	timerServiceAddress := `host.server1.systemd.timer["cleanup.timer"].service`
	timerService := nodeFor(resourceGraph, timerServiceAddress)
	if timerService == nil || timerService.Desired["enabled"] != true || timerService.Desired["state"] != "running" {
		t.Fatalf("timer service = %#v", timerService)
	}
	for _, want := range []string{timerAddress, "host.server1.systemd.daemon_reload"} {
		if !containsString(timerService.DependsOn, want) {
			t.Fatalf("timer service deps = %#v, want %q", timerService.DependsOn, want)
		}
	}

	resolvedAddress := "host.server1.systemd.resolved"
	resolved := nodeFor(resourceGraph, resolvedAddress)
	if resolved == nil || resolved.Kind != "file" || resolved.Desired["path"] != "/etc/systemd/resolved.conf.d/debianform.conf" {
		t.Fatalf("resolved node = %#v", resolved)
	}
	resolvedContent, _ := resolved.Desired["content"].(string)
	if !strings.Contains(resolvedContent, "DNS=1.1.1.1") || !strings.Contains(resolvedContent, "DNS=9.9.9.9") {
		t.Fatalf("resolved content = %q", resolvedContent)
	}
	resolvedPackage := `host.server1.packages.install["systemd-resolved"]`
	if !containsString(resolved.DependsOn, resolvedPackage) {
		t.Fatalf("resolved deps = %#v, want %q", resolved.DependsOn, resolvedPackage)
	}
	resolvedService := nodeFor(resourceGraph, "host.server1.systemd.resolved.service")
	if resolvedService == nil || resolvedService.Desired["enabled"] != true {
		t.Fatalf("resolved service = %#v", resolvedService)
	}
	resolvedRestart := operationFor(resourceGraph, "host.server1.systemd.resolved.restart")
	if resolvedRestart == nil || resolvedRestart.CommandPreview != "systemctl restart systemd-resolved.service" {
		t.Fatalf("resolved restart = %#v", resolvedRestart)
	}
	if !containsString(resolvedRestart.TriggeredBy, resolvedAddress) || !containsString(resolvedRestart.DependsOn, resolvedService.Address) {
		t.Fatalf("resolved restart deps=%#v triggered_by=%#v", resolvedRestart.DependsOn, resolvedRestart.TriggeredBy)
	}

	journaldAddress := "host.server1.systemd.journald"
	journald := nodeFor(resourceGraph, journaldAddress)
	if journald == nil || journald.Kind != "file" || journald.Desired["path"] != "/etc/systemd/journald.conf.d/debianform.conf" {
		t.Fatalf("journald node = %#v", journald)
	}
	journaldService := nodeFor(resourceGraph, "host.server1.systemd.journald.service")
	if journaldService == nil || journaldService.Desired["state"] != "reloaded" {
		t.Fatalf("journald service = %#v", journaldService)
	}
	if operationFor(resourceGraph, "host.server1.systemd.journald.restart") != nil {
		t.Fatalf("journald state reloaded should not also generate config restart operation")
	}
}

func TestCompileSystemdNetworkdWireGuardGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
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
}
`)

	netdev := nodeFor(resourceGraph, `host.server1.systemd.networkd.netdev["10-wg0"]`)
	if netdev == nil || netdev.Kind != "networkd_netdev" {
		t.Fatalf("networkd netdev node = %#v", netdev)
	}
	if _, ok := netdev.Desired["content"]; !ok {
		t.Fatalf("networkd netdev desired content missing: %#v", netdev.Desired)
	}
	networkDeps := dependsOnFor(resourceGraph, `host.server1.systemd.networkd.network["20-wg0"]`)
	if !containsString(networkDeps, `host.server1.systemd.networkd.netdev["10-wg0"]`) {
		t.Fatalf("network deps = %#v, want netdev dependency", networkDeps)
	}
	reload := operationFor(resourceGraph, "host.server1.systemd.networkd.restart")
	if reload == nil {
		t.Fatalf("networkd reload operation missing")
	}
	if !containsString(reload.TriggeredBy, `host.server1.systemd.networkd.netdev["10-wg0"]`) ||
		!containsString(reload.TriggeredBy, `host.server1.systemd.networkd.network["20-wg0"]`) {
		t.Fatalf("reload triggered_by = %#v", reload.TriggeredBy)
	}
	if !strings.Contains(reload.CommandPreview, "networkctl reload") {
		t.Fatalf("reload command = %q", reload.CommandPreview)
	}
	if nodeFor(resourceGraph, `host.server1.packages.install["wireguard-tools"]`) != nil {
		t.Fatalf("networkd WireGuard graph should not install wireguard-tools")
	}
}
