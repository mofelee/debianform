package graph

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestCompileBIRD2ResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/bird2.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/bird2.golden.json", got)

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
	resourceGraph := compileGraphFixture(t, "../../../examples/component-binary.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-binary.golden.json", got)

	installDeps := dependsOnFor(resourceGraph, `host.tool1.components.rclone.artifact.install["/usr/local/bin/rclone"]`)
	if !containsString(installDeps, `host.tool1.components.rclone.artifact.download["amd64"]`) {
		t.Fatalf("rclone install deps = %#v, want artifact download", installDeps)
	}
}

func TestCompileComponentSourceBuildResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/component-source-build.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-source-build.golden.json", got)

	installDeps := dependsOnFor(resourceGraph, `host.build1.components.hello_from_source.artifact.install["/usr/local/bin/hello-from-source"]`)
	foundBuild := false
	for _, dep := range installDeps {
		if strings.HasPrefix(dep, `host.build1.components.hello_from_source.artifact.build[`) {
			foundBuild = true
			break
		}
	}
	if !foundBuild {
		t.Fatalf("source install deps = %#v, want artifact build", installDeps)
	}
}

func TestCompileComponentStagingProviderPayloads(t *testing.T) {
	config := func(hostRoot string) string {
		return `
component "compressed" {
  type = "binary"
  source {
    url    = "https://downloads.example/tool.gz"
    sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
  }
  extract { format = "gz" }
  install { path = "/usr/local/bin/compressed" }
}

component "plain" {
  type = "binary"
  source {
    url    = "https://downloads.example/plain"
    sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
  }
  install { path = "/usr/local/bin/plain" }
}

component "archive" {
  type = "archive"
  source {
    url    = "https://downloads.example/app.tar.gz"
    sha256 = "3333333333333333333333333333333333333333333333333333333333333333"
  }
  extract { format = "tar.gz" }
  install { path = "/opt/app" }
}

component "source_build" {
  type = "source"
  source {
    url    = "https://downloads.example/main.c"
    sha256 = "4444444444444444444444444444444444444444444444444444444444444444"
  }
  build {
    commands   = [["cc", "-o", "tool", "main.c"]]
    output     = "tool"
    source_name = "main.c"
  }
  install { path = "/usr/local/bin/source-tool" }
}

host "custom" {
  staging { root = "` + hostRoot + `" }
  components = [component.compressed, component.plain]
  component "source_build" {
    source       = component.source_build
    staging_root = "/srv/component-staging"
  }
}

host "automatic" {
  components = [component.archive]
}
`
	}

	first := compileGraphInline(t, config("/srv/host-staging-a"))
	compressed := nodeFor(first, `host.custom.components.compressed.artifact.install["/usr/local/bin/compressed"]`)
	if compressed == nil || compressed.ProviderPayload["staging_root"] != "/srv/host-staging-a" {
		t.Fatalf("compressed provider payload = %#v", compressed)
	}
	if _, ok := compressed.Desired["staging_root"]; ok {
		t.Fatalf("compressed desired persisted operational staging root: %#v", compressed.Desired)
	}
	plain := nodeFor(first, `host.custom.components.plain.artifact.install["/usr/local/bin/plain"]`)
	if _, ok := plain.ProviderPayload["staging_root"]; ok {
		t.Fatalf("plain binary unexpectedly allocated staging: %#v", plain.ProviderPayload)
	}
	archive := nodeFor(first, `host.automatic.components.archive.artifact.install["/opt/app"]`)
	if _, ok := archive.ProviderPayload["staging_root"]; ok {
		t.Fatalf("automatic archive root should be resolved by provider: %#v", archive.ProviderPayload)
	}
	var build *Node
	for i := range first.Nodes {
		if strings.HasPrefix(first.Nodes[i].Address, `host.custom.components.source_build.artifact.build[`) {
			build = &first.Nodes[i]
			break
		}
	}
	if build == nil || build.ProviderPayload["staging_root"] != "/srv/component-staging" {
		t.Fatalf("source build provider payload = %#v", build)
	}
	if _, ok := build.Desired["staging_root"]; ok {
		t.Fatalf("source build desired persisted operational staging root: %#v", build.Desired)
	}
	for _, node := range first.Nodes {
		if node.Kind == "component_download" {
			if _, ok := node.ProviderPayload["staging_root"]; ok {
				t.Fatalf("download node received staging root: %#v", node.ProviderPayload)
			}
		}
	}

	second := compileGraphInline(t, config("/srv/host-staging-b"))
	secondCompressed := nodeFor(second, compressed.Address)
	if secondCompressed == nil || !reflect.DeepEqual(compressed.Desired, secondCompressed.Desired) {
		t.Fatalf("staging policy changed desired resource: first=%#v second=%#v", compressed.Desired, secondCompressed)
	}
	if secondCompressed.ProviderPayload["staging_root"] != "/srv/host-staging-b" {
		t.Fatalf("updated provider staging root = %#v", secondCompressed.ProviderPayload)
	}
}

func TestCompileComponentArtifactInputsKeepInstanceAddressesAndRedactPayloads(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/component-artifact-inputs.dbf.hcl")
	for _, host := range []string{"mirror_a", "mirror_b"} {
		wantURLHost := strings.ReplaceAll(host, "_", "-")
		for _, instance := range []string{"binary", "archive", "source"} {
			address := `host.` + host + `.components.` + instance + `.artifact.download["amd64"]`
			node := nodeFor(resourceGraph, address)
			if node == nil {
				t.Fatalf("missing artifact input address %s", address)
			}
			url, _ := node.Desired["url"].(string)
			if !strings.Contains(url, wantURLHost+".example.invalid") {
				t.Fatalf("%s resolved url = %q", address, url)
			}
		}

		privateAddress := `host.` + host + `.components.private.artifact.download["amd64"]`
		private := nodeFor(resourceGraph, privateAddress)
		if private == nil {
			t.Fatalf("missing private artifact input address %s", privateAddress)
		}
		if private.Desired["sensitive"] != true || private.Desired["url"] != nil || private.Desired["url_digest"] == "" {
			t.Fatalf("private artifact desired = %#v", private.Desired)
		}
		if private.Desired["sha256"] != nil || private.Desired["sha256_digest"] == "" {
			t.Fatalf("private artifact checksum desired = %#v", private.Desired)
		}
		payloadURL, _ := private.ProviderPayload["url"].(string)
		if !strings.Contains(payloadURL, testassert.SensitiveVariableDefault) {
			t.Fatalf("private artifact provider payload = %#v", private.ProviderPayload)
		}
		payloadSHA, _ := private.ProviderPayload["sha256"].(string)
		if payloadSHA == "" || !strings.Contains(private.Desired["path"].(string), componentArtifactFieldDigest(payloadSHA)) || strings.Contains(private.Desired["path"].(string), payloadSHA) {
			t.Fatalf("private artifact cache path/payload = %q / %#v", private.Desired["path"], private.ProviderPayload)
		}
	}

	data, err := json.Marshal(resourceGraph)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "sensitive artifact resource graph", string(data))
	for _, secret := range []string{
		"5555555555555555555555555555555555555555555555555555555555555555",
		"6666666666666666666666666666666666666666666666666666666666666666",
	} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("sensitive artifact checksum leaked in resource graph: %s", data)
		}
	}
}

func TestCompileComponentFileSourceDependsOnManagedFile(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "local_tool" {
  type    = "file"
  version = "1.0.0"

  source {
    url    = "file:///var/lib/debianform/local-tool"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/local-tool"
  }
}

host "server1" {
  files {
    file "/var/lib/debianform/local-tool" {
      content = "local tool"
    }
  }

  components = [component.local_tool]
}
`)
	downloadAddress := `host.server1.components.local_tool.artifact.download["default"]`
	fileAddress := `host.server1.files.file["/var/lib/debianform/local-tool"]`
	if deps := dependsOnFor(resourceGraph, downloadAddress); !containsString(deps, fileAddress) {
		t.Fatalf("local component download deps = %#v, want managed file %q", deps, fileAddress)
	}
}

func TestCompileComponentInputsResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/component-inputs.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-inputs.golden.json", got)

	if node := nodeFor(resourceGraph, `host.input1.components.proxy.files.file["/etc/reverse-proxy/listeners.json"]`); node == nil {
		t.Fatalf("component input generated file node was not found")
	}
}

func TestCompileComponentScriptOnChangeResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/component-script-on-change.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-script-on-change.golden.json", got)

	fileAddress := `host.app1.components.app.files.file["/etc/managed-app/config.env"]`
	outputAddress := `host.app1.components.app.script["reload"].outputs["/etc/managed-app/rendered.env"]`
	scriptAddress := `host.app1.components.app.script["reload"]`
	if node := nodeFor(resourceGraph, fileAddress); node == nil {
		t.Fatalf("component file node %s was not found", fileAddress)
	}
	if node := nodeFor(resourceGraph, outputAddress); node == nil {
		t.Fatalf("component script output node %s was not found", outputAddress)
	} else if node.Desired["script_digest"] == "" {
		t.Fatalf("component script output missing script digest: %#v", node.Desired)
	}
	operation := operationFor(resourceGraph, scriptAddress)
	if operation == nil {
		t.Fatalf("script operation %s was not found", scriptAddress)
	}
	if operation.CommandPreview != "script reload (once)" {
		t.Fatalf("command preview = %q", operation.CommandPreview)
	}
	for _, want := range []string{fileAddress, outputAddress} {
		if !containsString(operation.TriggeredBy, want) || !containsString(operation.DependsOn, want) {
			t.Fatalf("script operation deps=%#v triggered_by=%#v, want %q", operation.DependsOn, operation.TriggeredBy, want)
		}
	}
}

func TestCompileSharedNetworkdReloadResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/shared-networkd-reload.dbf.hcl")
	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "../testdata/graph/shared-networkd-reload.golden.json", string(data)+"\n")
}

func TestCompileRootScriptAggregatesCrossComponentTriggers(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
script "reload" {
  run = "networkctl reload"
}

component "wan" {
  files {
    file "/etc/wan" {
      content   = "wan"
      on_change = script.reload
    }
    file "/etc/wan-extra" {
      content   = "wan-extra"
      on_change = script.reload
    }
  }
}

component "policy" {
  files {
    file "/etc/policy" {
      content   = "policy"
      on_change = script.reload
    }
  }
}

host "router" { components = [component.wan, component.policy] }
`)
	operation := operationFor(resourceGraph, `host.router.script["reload"]`)
	if operation == nil {
		t.Fatal("host-scoped root script operation missing")
	}
	for _, address := range []string{
		`host.router.components.wan.files.file["/etc/wan"]`,
		`host.router.components.wan.files.file["/etc/wan-extra"]`,
		`host.router.components.policy.files.file["/etc/policy"]`,
	} {
		if !containsString(operation.TriggeredBy, address) || !containsString(operation.DependsOn, address) {
			t.Fatalf("operation triggers/dependencies = %#v / %#v, want %s", operation.TriggeredBy, operation.DependsOn, address)
		}
	}
	if len(operation.TriggeredBy) != 3 || operation.ScriptPayload == nil || operation.ScriptPayload.ComponentName != "" {
		t.Fatalf("host operation = %#v", operation)
	}
}

func TestUnreferencedRootScriptDoesNotGenerateOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
script "unused" { run = "true" }
host "server1" {}
`)
	if operationFor(resourceGraph, `host.server1.script["unused"]`) != nil {
		t.Fatalf("unreferenced root definition generated operation: %#v", resourceGraph.Operations)
	}
}

func TestCompileRootScriptsKeepDeclarationIdentity(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
script "one" { run = "same command" }
script "two" { run = "same command" }

component "files" {
  files {
    file "/etc/one" {
      content   = "one"
      on_change = script.one
    }
    file "/etc/two" {
      content   = "two"
      on_change = script.two
    }
  }
}

host "a" { components = [component.files] }
host "b" { components = [component.files] }
`)
	for _, host := range []string{"a", "b"} {
		for _, name := range []string{"one", "two"} {
			if operationFor(resourceGraph, `host.`+host+`.script["`+name+`"]`) == nil {
				t.Fatalf("missing independent operation for host=%s script=%s", host, name)
			}
		}
	}
	if len(resourceGraph.Operations) != 4 {
		t.Fatalf("operations = %d, want 4", len(resourceGraph.Operations))
	}
}

func TestComponentLocalScriptsWithSameLabelAndCommandRemainDistinct(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "one" {
  script "reload" { run = "same command" }
  files {
    file "/etc/one" {
      content   = "one"
      on_change = script.reload
    }
  }
}
component "two" {
  script "reload" { run = "same command" }
  files {
    file "/etc/two" {
      content   = "two"
      on_change = script.reload
    }
  }
}
host "server1" { components = [component.one, component.two] }
`)
	for _, address := range []string{
		`host.server1.components.one.script["reload"]`,
		`host.server1.components.two.script["reload"]`,
	} {
		if operationFor(resourceGraph, address) == nil {
			t.Fatalf("missing component-local operation %s", address)
		}
	}
	if len(resourceGraph.Operations) != 2 {
		t.Fatalf("component-local operations = %#v", resourceGraph.Operations)
	}
}

func TestCompileComponentScriptWithoutOnChangeDoesNotGenerateOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "app" {
  script "unused" {
    run = "systemctl reload app.service"
  }

  files {
    file "/etc/app.conf" {
      content = "managed"
    }
  }
}

host "app1" {
  components = [component.app]
}
`)
	if operationFor(resourceGraph, `host.app1.components.app.script["unused"]`) != nil {
		t.Fatalf("unused component script generated operation: %#v", resourceGraph.Operations)
	}
}

func TestComponentScriptPayloadStaysOutOfResourceGraphJSON(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "app" {
  script "reload" {
    run = "printf '%s\n' not-a-real-script-secret"
  }

  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }
  }
}

host "app1" {
  components = [component.app]
}
`)
	operation := operationFor(resourceGraph, `host.app1.components.app.script["reload"]`)
	if operation == nil || operation.ScriptPayload == nil {
		t.Fatalf("script operation payload missing: %#v", resourceGraph.Operations)
	}

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "not-a-real-script-secret") || strings.Contains(text, "script_payload") {
		t.Fatalf("ResourceGraph JSON exposed script payload:\n%s", text)
	}
	if !strings.Contains(text, `"command_preview": "script reload (once)"`) {
		t.Fatalf("ResourceGraph JSON missing script preview:\n%s", text)
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

  platform {
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

func TestCompileComponentSourceBuildGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "hello" {
  type = "source"

  source "amd64" {
    url    = "https://downloads.example/hello.c"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-o", "hello", "hello.c"],
    ]
    output      = "hello"
    source_name = "hello.c"
  }

  install {
    path = "/usr/local/bin/hello"
  }
}

host "server1" {
  components = [component.hello]

  platform {
    architecture = "amd64"
  }
}
`)

	var build *Node
	for i := range resourceGraph.Nodes {
		if strings.HasPrefix(resourceGraph.Nodes[i].Address, `host.server1.components.hello.artifact.build[`) {
			build = &resourceGraph.Nodes[i]
			break
		}
	}
	if build == nil || build.Kind != "component_build" {
		t.Fatalf("build node = %#v", build)
	}
	if !containsString(build.DependsOn, `host.server1.components.hello.artifact.download["amd64"]`) {
		t.Fatalf("build deps = %#v, want download dependency", build.DependsOn)
	}
	if !containsString(build.DependsOn, `host.server1.components.hello.build.package["gcc"]`) {
		t.Fatalf("build deps = %#v, want gcc package dependency", build.DependsOn)
	}
	install := nodeFor(resourceGraph, `host.server1.components.hello.artifact.install["/usr/local/bin/hello"]`)
	if install == nil || install.Kind != "component_binary" {
		t.Fatalf("install node = %#v", install)
	}
	if !containsString(install.DependsOn, build.Address) {
		t.Fatalf("install deps = %#v, want build dependency %q", install.DependsOn, build.Address)
	}
	if got := install.Desired["cache_path"]; got != build.Desired["output_path"] {
		t.Fatalf("install cache_path = %#v, want build output %#v", got, build.Desired["output_path"])
	}
}
