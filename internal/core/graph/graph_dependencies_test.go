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

func TestExplicitCrossComponentArtifactDependencyOrdersInstallFirst(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "zbin" {
  type    = "binary"
  version = "1.0.0"

  source "amd64" {
    url    = "https://example.invalid/tool-amd64.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/tool"
  }
}

component "link" {
  input "peer" {
    type = string
  }

  services {
    service "svc" {
      name       = "tool-${input.peer}"
      depends_on = [artifact["/usr/local/bin/tool"]]
      state      = "running"
    }
  }
}

host "server1" {
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  components = [component.zbin]

  component "alink" {
    source = component.link
    inputs = { peer = "alpha" }
  }
}
`)
	installAddress := `host.server1.components.zbin.artifact.install["/usr/local/bin/tool"]`
	serviceAddress := `host.server1.components.alink.services.service["tool-alpha"]`
	serviceNode := nodeFor(resourceGraph, serviceAddress)
	if serviceNode == nil || !containsString(serviceNode.DependsOn, installAddress) || !containsString(serviceNode.ExplicitDependsOn, installAddress) {
		t.Fatalf("service dependencies = %#v explicit=%#v", serviceNode.DependsOn, serviceNode.ExplicitDependsOn)
	}
	order := topologicalOrder(t, resourceGraph)
	assertBefore(t, order, installAddress, serviceAddress)
}
