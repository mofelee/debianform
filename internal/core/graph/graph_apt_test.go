package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileAPTRepositoryResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/apt-repository.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/apt-repository.golden.json", got)

	packageDeps := dependsOnFor(resourceGraph, `host.apt1.packages.install["example-tool"]`)
	for _, want := range []string{
		`host.apt1.apt.repository["example_tools"]`,
		`host.apt1.apt.cache_refresh`,
	} {
		if !containsString(packageDeps, want) {
			t.Fatalf("example-tool deps = %#v, want %q", packageDeps, want)
		}
	}
}

func TestCompileAPTRepositoryDependencies(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  apt {
    repository "tools" {
      uris          = ["https://repo.example/debian"]
      suites        = ["trixie"]
      components    = ["main"]
      architectures = ["amd64"]

      signing_key {
        content = "-----BEGIN PGP PUBLIC KEY BLOCK-----\nexample\n-----END PGP PUBLIC KEY BLOCK-----\n"
      }
    }
  }

  packages {
    install = ["curl"]

    package "example-tool" {
      repositories = ["tools"]
    }
  }
}
`)

	repositoryAddress := `host.server1.apt.repository["tools"]`
	keyAddress := `host.server1.apt.signing_key["tools"]`
	refreshAddress := `host.server1.apt.cache_refresh`
	packageAddress := `host.server1.packages.install["example-tool"]`

	repository := nodeFor(resourceGraph, repositoryAddress)
	if repository == nil {
		t.Fatalf("repository node missing")
	}
	if !containsString(repository.DependsOn, keyAddress) {
		t.Fatalf("repository deps = %#v, want signing key", repository.DependsOn)
	}
	content, _ := repository.Desired["content"].(string)
	if !strings.Contains(content, "Signed-By: /etc/apt/keyrings/tools.asc") {
		t.Fatalf("repository content missing Signed-By:\n%s", content)
	}
	if !strings.Contains(content, "Architectures: amd64") {
		t.Fatalf("repository content missing Architectures:\n%s", content)
	}

	operation := operationFor(resourceGraph, refreshAddress)
	if operation == nil {
		t.Fatalf("apt cache refresh operation missing")
	}
	for _, want := range []string{keyAddress, repositoryAddress} {
		if !containsString(operation.TriggeredBy, want) {
			t.Fatalf("refresh triggered_by = %#v, want %q", operation.TriggeredBy, want)
		}
	}

	packageDeps := dependsOnFor(resourceGraph, packageAddress)
	for _, want := range []string{repositoryAddress, refreshAddress} {
		if !containsString(packageDeps, want) {
			t.Fatalf("package deps = %#v, want %q", packageDeps, want)
		}
	}
	curlDeps := dependsOnFor(resourceGraph, `host.server1.packages.install["curl"]`)
	if containsString(curlDeps, refreshAddress) {
		t.Fatalf("unrelated package deps = %#v, did not want apt refresh", curlDeps)
	}
}

func TestCompileAPTSourceFileTriggersCacheRefresh(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  apt {
    source_file "main" {
      path       = "/etc/apt/sources.list"
      content    = "deb https://mirrors.aliyun.com/debian/ trixie main\n"
      on_destroy = "restore"
    }
  }
}
`)

	address := `host.server1.apt.source_file["main"]`
	node := nodeFor(resourceGraph, address)
	if node == nil {
		t.Fatalf("apt source_file node missing")
	}
	if node.Kind != "apt_source_file" || node.ProviderType != "apt_source_file" {
		t.Fatalf("node kind/provider = %s/%s", node.Kind, node.ProviderType)
	}
	if node.Desired["path"] != "/etc/apt/sources.list" || node.Desired["on_destroy"] != "restore" {
		t.Fatalf("desired = %#v", node.Desired)
	}

	operation := operationFor(resourceGraph, `host.server1.apt.cache_refresh`)
	if operation == nil {
		t.Fatalf("apt cache refresh operation missing")
	}
	if !containsString(operation.TriggeredBy, address) {
		t.Fatalf("refresh triggered_by = %#v, want %q", operation.TriggeredBy, address)
	}
}
