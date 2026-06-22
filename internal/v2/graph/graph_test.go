package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/merge"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func TestCompileBBRResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-bbr.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-bbr.golden.json", got)

	dependsOn := dependsOnFor(resourceGraph, `host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`)
	want := []string{`host.bbr1.kernel.module["tcp_bbr"]`}
	if strings.Join(dependsOn, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tcp_congestion_control depends_on = %#v, want %#v", dependsOn, want)
	}
}

func TestCompileFoundationResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/v2-foundation.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-foundation.golden.json", got)

	userDeps := dependsOnFor(resourceGraph, `host.foundation1.users.user["deploy"]`)
	if !containsString(userDeps, `host.foundation1.groups.group["deploy"]`) {
		t.Fatalf("user deps = %#v, want deploy group dependency", userDeps)
	}
	serviceDeps := dependsOnFor(resourceGraph, `host.foundation1.services.service["myapp"]`)
	for _, want := range []string{
		`host.foundation1.packages.install["curl"]`,
		`host.foundation1.systemd.unit["myapp.service"]`,
		`host.foundation1.systemd.daemon_reload`,
	} {
		if !containsString(serviceDeps, want) {
			t.Fatalf("service deps = %#v, want %q", serviceDeps, want)
		}
	}
}

func TestCompileAPTRepositoryResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-apt-repository.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-apt-repository.golden.json", got)

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

func TestCompileProfileMergeResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-profile-merge.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-profile-merge.golden.json", got)

	for _, want := range []string{
		`host.merge1.packages.install["curl"]`,
		`host.merge1.packages.install["vim"]`,
		`host.merge1.packages.install["htop"]`,
		`host.merge1.packages.install["sudo"]`,
		`host.merge1.kernel.module["tcp_bbr"]`,
	} {
		if nodeFor(resourceGraph, want) == nil {
			t.Fatalf("resource graph missing %q", want)
		}
	}
}

func TestCompileSystemdServiceResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-systemd-service.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-systemd-service.golden.json", got)

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

func TestCompileUserGroupResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-user-group.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-user-group.golden.json", got)

	userDeps := dependsOnFor(resourceGraph, `host.users1.users.user["deploy"]`)
	if !containsString(userDeps, `host.users1.groups.group["deploy"]`) {
		t.Fatalf("deploy user deps = %#v, want deploy group dependency", userDeps)
	}
}

func TestCompileNftablesResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-nftables.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-nftables.golden.json", got)

	validate := operationFor(resourceGraph, "host.edge1.nftables.validate")
	if validate == nil {
		t.Fatal("nftables validate operation missing")
	}
	activate := operationFor(resourceGraph, "host.edge1.nftables.activate")
	if activate == nil {
		t.Fatal("nftables activate operation missing")
	}
	if !containsString(activate.DependsOn, "host.edge1.nftables.validate") {
		t.Fatalf("activate deps = %#v, want validate", activate.DependsOn)
	}
	for _, want := range []string{
		`host.edge1.nftables.file["main"]`,
		`host.edge1.nftables.file["10-base"]`,
		`host.edge1.nftables.file["20-services"]`,
		`host.edge1.nftables.file["30-wireguard"]`,
	} {
		if !containsString(validate.TriggeredBy, want) {
			t.Fatalf("validate triggered_by = %#v, want %q", validate.TriggeredBy, want)
		}
	}
	deps := dependsOnFor(resourceGraph, `host.edge1.nftables.file["20-services"]`)
	if !containsString(deps, `host.edge1.packages.install["nftables"]`) {
		t.Fatalf("nftables file deps = %#v, want package dependency", deps)
	}
	enableDeps := dependsOnFor(resourceGraph, "host.edge1.nftables.enable")
	if !containsString(enableDeps, "host.edge1.nftables.activate") {
		t.Fatalf("nftables enable deps = %#v, want activate dependency", enableDeps)
	}
}

func TestCompileBIRD2ResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-bird2.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-bird2.golden.json", got)

	serviceDeps := dependsOnFor(resourceGraph, `host.router1.components.bird2.services.service["bird"]`)
	if !containsString(serviceDeps, `host.router1.components.bird2.packages.install["bird2"]`) {
		t.Fatalf("bird service deps = %#v, want bird2 package", serviceDeps)
	}
	packageDeps := dependsOnFor(resourceGraph, `host.router1.components.bird2.packages.install["bird2"]`)
	for _, want := range []string{
		`host.router1.components.bird2.apt.repository["cznic_bird2"]`,
		`host.router1.apt.cache_refresh`,
	} {
		if !containsString(packageDeps, want) {
			t.Fatalf("bird2 package deps = %#v, want %q", packageDeps, want)
		}
	}
}

func TestCompileComponentBinaryResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-component-binary.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-component-binary.golden.json", got)

	installDeps := dependsOnFor(resourceGraph, `host.tool1.components.rclone.artifact.install["/usr/local/bin/rclone"]`)
	if !containsString(installDeps, `host.tool1.components.rclone.artifact.download["amd64"]`) {
		t.Fatalf("rclone install deps = %#v, want artifact download", installDeps)
	}
}

func TestCompileComponentArtifactKindsAndCAOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "company_ca" {
  type = "ca_certificate"

  source {
    url    = "https://downloads.example/company-ca.crt"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/share/ca-certificates/company-ca.crt"
  }
}

component "config" {
  type = "file"

  source {
    url    = "https://downloads.example/config.yaml"
    sha256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
  }

  install {
    path = "/etc/myapp/config.yaml"
  }
}

component "myapp" {
  type = "archive"

  source "amd64" {
    url    = "https://downloads.example/myapp.tar.gz"
    sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
  }

  extract {
    format           = "tar.gz"
    strip_components = 1
  }

  install {
    path  = "/opt/myapp"
    owner = "myapp"
    group = "myapp"
  }

  groups {
    group "myapp" {
      system = true
    }
  }

  users {
    user "myapp" {
      system = true
      group  = "myapp"
    }
  }
}

host "server1" {
  components = [component.company_ca, component.config, component.myapp]

  system {
    architecture = "amd64"
  }
}
`)

	if node := nodeFor(resourceGraph, `host.server1.components.config.artifact.install["/etc/myapp/config.yaml"]`); node == nil || node.Kind != "component_file" {
		t.Fatalf("component file node = %#v", node)
	}
	caAddress := `host.server1.components.company_ca.artifact.install["/usr/local/share/ca-certificates/company-ca.crt"]`
	if node := nodeFor(resourceGraph, caAddress); node == nil || node.Kind != "component_ca_certificate" {
		t.Fatalf("ca certificate node = %#v", node)
	}
	operation := operationFor(resourceGraph, "host.server1.ca_certificates.update")
	if operation == nil {
		t.Fatalf("ca certificates update operation missing")
	}
	if !containsString(operation.TriggeredBy, caAddress) || operation.CommandPreview != "update-ca-certificates" {
		t.Fatalf("ca operation = %#v", operation)
	}
	archiveDeps := dependsOnFor(resourceGraph, `host.server1.components.myapp.artifact.install["/opt/myapp"]`)
	for _, want := range []string{
		`host.server1.components.myapp.artifact.download["amd64"]`,
		`host.server1.components.myapp.groups.group["myapp"]`,
		`host.server1.components.myapp.users.user["myapp"]`,
	} {
		if !containsString(archiveDeps, want) {
			t.Fatalf("archive deps = %#v, want %q", archiveDeps, want)
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

func compileGraphFixture(t *testing.T, fixture string) *ResourceGraph {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return resourceGraph
}

func testHostFacts() map[string]ir.HostFacts {
	out := map[string]ir.HostFacts{}
	for _, name := range []string{
		"apt1",
		"bbr1",
		"edge1",
		"foundation1",
		"merge1",
		"preview1",
		"router1",
		"server1",
		"server2",
		"service1",
		"tool1",
		"users1",
	} {
		out[name] = ir.HostFacts{System: ir.SystemFacts{
			Hostname:     name,
			Architecture: "amd64",
			Codename:     "trixie",
		}}
	}
	return out
}

func compileGraphInline(t *testing.T, content string) *ResourceGraph {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return compileGraphFixture(t, file)
}

func dependsOnFor(resourceGraph *ResourceGraph, address string) []string {
	for _, node := range resourceGraph.Nodes {
		if node.Address == address {
			return node.DependsOn
		}
	}
	return nil
}

func nodeFor(resourceGraph *ResourceGraph, address string) *Node {
	for i := range resourceGraph.Nodes {
		if resourceGraph.Nodes[i].Address == address {
			return &resourceGraph.Nodes[i]
		}
	}
	return nil
}

func operationFor(resourceGraph *ResourceGraph, address string) *Operation {
	for i := range resourceGraph.Operations {
		if resourceGraph.Operations[i].Address == address {
			return &resourceGraph.Operations[i]
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasOperation(resourceGraph *ResourceGraph, address string) bool {
	for _, operation := range resourceGraph.Operations {
		if operation.Address == address {
			return true
		}
	}
	return false
}

func assertGolden(t *testing.T, golden string, got string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
