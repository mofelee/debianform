package engine

import (
	"testing"

	"github.com/mofelee/debianform/internal/config"
)

func TestTopoSortKeepsDependenciesBeforeDependents(t *testing.T) {
	resources := []config.Resource{
		{
			Type:      "debian_service",
			Name:      "nginx",
			Address:   "debian_service.nginx",
			DependsOn: []string{"debian_file.nginx_default"},
			Order:     0,
		},
		{
			Type:    "debian_file",
			Name:    "nginx_default",
			Address: "debian_file.nginx_default",
			Order:   1,
		},
	}

	sorted, err := topoSort(resources)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sorted[0].Address, "debian_file.nginx_default"; got != want {
		t.Fatalf("first resource = %s, want %s", got, want)
	}
	if got, want := sorted[1].Address, "debian_service.nginx"; got != want {
		t.Fatalf("second resource = %s, want %s", got, want)
	}
}

func TestHandlerRunsDedupesAndUsesDeclarationOrder(t *testing.T) {
	e := &Engine{
		cfg: &config.Config{
			Handlers: []config.Handler{
				{Address: "handler.second", Host: "server1", Command: "echo second"},
				{Address: "handler.first", Host: "server1", Command: "echo first"},
			},
		},
	}
	changes := []Change{
		{
			Address: "debian_file.a",
			Resource: config.Resource{
				Notify: []string{"handler.first", "handler.second"},
			},
		},
		{
			Address: "debian_file.b",
			Resource: config.Resource{
				Notify: []string{"handler.first"},
			},
		},
	}

	handlers := e.handlerRuns(changes)
	if got, want := len(handlers), 2; got != want {
		t.Fatalf("len(handlers) = %d, want %d", got, want)
	}
	if got, want := handlers[0].Address, "handler.second"; got != want {
		t.Fatalf("first handler = %s, want %s", got, want)
	}
	if got, want := handlers[1].Address, "handler.first"; got != want {
		t.Fatalf("second handler = %s, want %s", got, want)
	}
	if got, want := len(handlers[1].Reasons), 2; got != want {
		t.Fatalf("len(first reasons) = %d, want %d", got, want)
	}
}
