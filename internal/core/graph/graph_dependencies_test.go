package graph

import (
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
)

func TestExplicitResourceDependenciesCompileAndDeduplicateInferredEdges(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  packages {
    package "vault" {}
  }
  files {
    file "config" {
      path       = "/etc/vault.d/vault.hcl"
      content    = "storage {}"
      depends_on = [package.vault]
    }
    file "/tmp/unrelated" { content = "independent" }
  }
  services {
    service "vault" {
      package    = "vault"
      depends_on = [file.config, package.vault]
      state      = "running"
    }
  }
}
`)
	packageAddress := `host.server1.packages.install["vault"]`
	fileAddress := `host.server1.files.file["/etc/vault.d/vault.hcl"]`
	serviceAddress := `host.server1.services.service["vault"]`
	fileNode := nodeFor(resourceGraph, fileAddress)
	if fileNode == nil || !containsString(fileNode.DependsOn, packageAddress) || !containsString(fileNode.ExplicitDependsOn, packageAddress) {
		t.Fatalf("file dependencies = %#v explicit=%#v", fileNode.DependsOn, fileNode.ExplicitDependsOn)
	}
	serviceNode := nodeFor(resourceGraph, serviceAddress)
	if serviceNode == nil || countDependency(serviceNode.DependsOn, packageAddress) != 1 || !containsString(serviceNode.DependsOn, fileAddress) {
		t.Fatalf("service dependencies = %#v", serviceNode.DependsOn)
	}
	if len(serviceNode.ExplicitDependsOn) != 2 {
		t.Fatalf("service explicit dependencies = %#v", serviceNode.ExplicitDependsOn)
	}

	items, err := resourceGraph.TopologicalSort()
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]int{}
	for i, item := range items {
		positions[item.Address] = i
	}
	if !(positions[packageAddress] < positions[fileAddress] && positions[fileAddress] < positions[serviceAddress]) {
		t.Fatalf("topological positions = %#v", positions)
	}
}

func TestExplicitResourceDependencyCycleReportsSourceAndCompletePath(t *testing.T) {
	err := compileGraphInlineError(t, `
host "server1" {
  files {
    file "config" {
      path       = "/etc/vault.d/vault.hcl"
      content    = "storage {}"
      depends_on = [service.vault]
    }
  }
  services {
    service "vault" {
      depends_on = [file.config]
      state      = "running"
    }
  }
}
`)
	if err == nil {
		t.Fatal("Compile() succeeded, want dependency cycle")
	}
	for _, want := range []string{
		`.depends_on[0]`,
		`resource graph dependency cycle`,
		`host.server1.files.file["/etc/vault.d/vault.hcl"]`,
		`host.server1.services.service["vault"]`,
		` -> `,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("cycle error = %v, want %q", err, want)
		}
	}
}

func TestApplyExplicitDependenciesRejectsCrossHostAddress(t *testing.T) {
	nodes := []Node{{Host: "server1", Address: `host.server1.files.file["/tmp/app"]`}}
	err := applyExplicitDependencies("server1", nodes, []ir.ResourceDependencySpec{{
		From:      nodes[0].Address,
		DependsOn: `host.server2.packages.install["app"]`,
		Source:    ir.SourceRef{File: "main.dbf.hcl", Line: 8, Path: `host.server1.files.file["/tmp/app"].depends_on[0]`},
	}})
	if err == nil || !strings.Contains(err.Error(), "main.dbf.hcl:8") || !strings.Contains(err.Error(), "crosses host scope") {
		t.Fatalf("cross-host error = %v", err)
	}
}

func countDependency(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
