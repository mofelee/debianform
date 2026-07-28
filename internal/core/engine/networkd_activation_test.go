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
