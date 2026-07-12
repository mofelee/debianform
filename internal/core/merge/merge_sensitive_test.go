package merge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestCompileHostSpecJSONDoesNotLeakCurrentSensitiveBaseline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
	}{
		{name: "secrets file", fixture: "../testdata/fixtures/foundation.dbf.hcl"},
		{name: "sensitive file content", fixture: "../../../examples/files-plan-preview.dbf.hcl"},
		{name: "sensitive component input", fixture: "../../../examples/component-inputs.dbf.hcl"},
		{name: "sensitive service environment", fixture: "../testdata/fixtures/sensitive-service-environment.dbf.hcl"},
		{name: "sensitive variable content", fixture: "../testdata/fixtures/sensitive-variable-files.dbf.hcl"},
		{name: "sensitive apt and nftables content", fixture: "../testdata/fixtures/sensitive-apt-nftables-content.dbf.hcl"},
		{name: "ephemeral variable content", fixture: "../testdata/fixtures/ephemeral-variable-content.dbf.hcl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parser.ParseFiles([]string{tt.fixture})
			if err != nil {
				t.Fatal(err)
			}
			program, err := CompileWithOptions(cfg, CompileOptions{HostFacts: testHostFacts()})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.MarshalIndent(program, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			testassert.NoSecretLeak(t, tt.name+" HostSpec JSON", string(data))
		})
	}
}

func TestCompileSensitiveAPTAndNftablesContentPropagatesMarks(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/sensitive-apt-nftables-content.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileWithOptions(cfg, CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]

	sourceFile := host.APT.SourceFiles["private"]
	if !sourceFile.Sensitive || sourceFile.Content != testassert.SensitiveVariableDefault {
		t.Fatalf("apt source file sensitive/content = %v/%q", sourceFile.Sensitive, sourceFile.Content)
	}
	signingKey := host.APT.Repositories["private"].SigningKey
	if signingKey == nil || !signingKey.Sensitive || signingKey.Content != testassert.SensitiveVariableDefault {
		t.Fatalf("apt signing key = %#v", signingKey)
	}
	nftablesFile := host.Nftables.Files["private"]
	if !nftablesFile.Sensitive || nftablesFile.Content != testassert.SensitiveVariableDefault {
		t.Fatalf("nftables file sensitive/content = %v/%q", nftablesFile.Sensitive, nftablesFile.Content)
	}
	if host.Nftables.Main == nil || !host.Nftables.Main.Sensitive || host.Nftables.Main.Content != testassert.SensitiveVariableDefault {
		t.Fatalf("nftables main = %#v", host.Nftables.Main)
	}
	if len(host.Components) != 1 {
		t.Fatalf("components = %#v", host.Components)
	}
	component := host.Components[0]
	componentSource := component.APT.SourceFiles["component-private"]
	componentKey := component.APT.Repositories["component-private"].SigningKey
	if !componentSource.Sensitive || componentKey == nil || !componentKey.Sensitive {
		t.Fatalf("component apt sensitive marks = source %#v key %#v", componentSource, componentKey)
	}
}

func TestCompileRejectsSensitiveAPTSourceRestore(t *testing.T) {
	_, err := parseOrCompileInline(t, `
variable "managed_content" {
  type      = string
  sensitive = true
  default   = "not-a-real-variable-secret"
}

host "server1" {
  apt {
    source_file "private" {
      path       = "/etc/apt/sources.list.d/private.list"
      content    = var.managed_content
      on_destroy = "restore"
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "restore is not supported with sensitive content") {
		t.Fatalf("error = %v, want sensitive restore rejection", err)
	}
	if strings.Contains(err.Error(), testassert.SensitiveVariableDefault) {
		t.Fatalf("sensitive content leaked in error: %v", err)
	}
}

func TestCompileSensitiveComponentScriptRedactsHostSpecJSON(t *testing.T) {
	program := compileInline(t, `
component "app" {
  input "token" {
    type      = string
    sensitive = true
  }

  script "reload" {
    interpreter = ["/bin/sh", "-eu"]
    run         = "echo ${input.token}"
  }

  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }
  }
}

host "app1" {
  component "app" {
    source = component.app

    inputs = {
      token = "not-a-real-script-secret"
    }
  }
}
`)
	script := program.Hosts[0].Components[0].Scripts["reload"]
	if !script.Sensitive {
		t.Fatalf("script sensitive = false")
	}
	data, err := json.MarshalIndent(program, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "not-a-real-script-secret") {
		t.Fatalf("HostSpec JSON leaked script secret: %s", data)
	}
}

func TestCompileSensitiveRootScriptRedactsHostSpecJSON(t *testing.T) {
	program := compileInline(t, `
variable "token" {
  type      = string
  sensitive = true
  default   = "not-a-real-root-script-secret"
}

script "reload" {
  run = "echo ${var.token}"
}

component "app" {
  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }
  }
}

host "app1" { components = [component.app] }
`)
	script := program.Hosts[0].Scripts["reload"]
	if !script.Sensitive {
		t.Fatal("root script sensitive = false")
	}
	data, err := json.MarshalIndent(program, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "not-a-real-root-script-secret") {
		t.Fatalf("HostSpec JSON leaked root script secret: %s", data)
	}
}

func TestCompileStructuredServiceEnvironmentMarksUnitSensitive(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/sensitive-service-environment.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}

	unit := program.Hosts[0].Components[0].Systemd.Units["worker.service"]
	if !unit.Sensitive {
		t.Fatalf("structured service unit was not marked sensitive: %#v", unit)
	}
	data, err := json.MarshalIndent(program, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "structured service HostSpec JSON", string(data))
}
