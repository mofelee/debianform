package graph

import (
	"encoding/json"
	"strings"
	"testing"
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
