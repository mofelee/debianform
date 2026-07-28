package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
)

func TestNetworkdActivationRunsOnceInDependencyOrder(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
script "reexport" {
  mode = "once"
  run  = "birdc reload out kernel4"
}

host "server1" {
  systemd {
    networkd {
      network "20-wg0" {
        content = "[Match]\nName=wg0\n"
        activation {
          reconfigure = ["wg1", "wg0"]
          post_reload = script.reexport
        }
      }
      network "21-wg1" {
        content = "[Match]\nName=wg1\n"
        activation {
          reconfigure = ["wg0"]
          post_reload = script.reexport
        }
      }
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	want := []string{
		"host.server1.systemd.networkd.restart",
		`host.server1.systemd.networkd.reconfigure["wg0"]`,
		`host.server1.systemd.networkd.reconfigure["wg1"]`,
		`host.server1.script["reexport"]`,
	}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(provider.Operations, want) {
		t.Fatalf("first apply operations = %#v, want %#v", provider.Operations, want)
	}
	if plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	} else if len(plan.Operations) != 0 || !reflect.DeepEqual(provider.Operations, want) {
		t.Fatalf("no-op apply operations = %#v / recorded %#v", plan.Operations, provider.Operations)
	}

	firstAddress := `host.server1.systemd.networkd.network["20-wg0"]`
	provider.Observed[firstAddress] = Observed{Exists: false}
	provider.Operations = nil
	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(provider.Operations, want) {
		t.Fatalf("single-file drift operations = %#v, want %#v", provider.Operations, want)
	}
}

func TestOperationForTriggersPreservesStaticDependencies(t *testing.T) {
	op := graph.Operation{
		Address:     "post",
		TriggeredBy: []string{"first", "second"},
		DependsOn:   []string{"first", "second", "reload"},
	}
	active := operationForTriggers(op, []string{"second"})
	if !reflect.DeepEqual(active.TriggeredBy, []string{"second"}) || !reflect.DeepEqual(active.DependsOn, []string{"second", "reload"}) {
		t.Fatalf("active operation = %#v", active)
	}
}

func TestNetworkdCheckPlansActivationWithoutExecutingIt(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
script "reexport" {
  mode = "once"
  run  = "birdc reload out kernel4"
}

host "server1" {
  systemd {
    networkd {
      network "20-wg0" {
        content = "[Match]\nName=wg0\n"
        activation {
          reconfigure = ["wg0"]
          post_reload = script.reexport
        }
      }
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Check(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || len(plan.Operations) != 3 {
		t.Fatalf("check plan = %d steps / %d operations, want 1/3", len(plan.Steps), len(plan.Operations))
	}
	if len(provider.Applied) != 0 || len(provider.Destroyed) != 0 || len(provider.Operations) != 0 {
		t.Fatalf("check mutated provider: applied=%v destroyed=%v operations=%v", provider.Applied, provider.Destroyed, provider.Operations)
	}
	state, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Resources) != 0 {
		t.Fatalf("check wrote state: %#v", state.Resources)
	}
}

func TestCompatibilityNetworkdConfigurationRemainsNoOp(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }
        wireguard = {
          PrivateKeyFile = "/etc/wireguard/wg0.key"
          RouteTable     = "off"
        }
      }
      network "20-wg0" {
        match   = { Name = "wg0" }
        network = { Address = ["192.0.2.1/32", "2001:db8::1/128"] }
      }
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	provider.Operations = nil
	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 0 || len(plan.Operations) != 0 {
		t.Fatalf("compatibility configuration churned: steps=%#v operations=%#v", plan.Steps, plan.Operations)
	}
}
