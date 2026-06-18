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
