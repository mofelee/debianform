package merge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestCompileVariableDeclarationsIntoProgramIR(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/variable-declarations.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileWithOptions(cfg, CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Variables) != 3 {
		t.Fatalf("variables = %#v", program.Variables)
	}

	environment := program.Variables["environment"]
	if environment.Type != "string" || environment.Default != "prod" || environment.Nullable {
		t.Fatalf("environment variable = %#v", environment)
	}
	if len(environment.Validations) != 1 || environment.Validations[0].Message != "environment must be dev, staging, or prod." {
		t.Fatalf("environment validations = %#v", environment.Validations)
	}

	listeners := program.Variables["listeners"]
	defaults, ok := listeners.Default.([]any)
	if !ok || len(defaults) != 1 {
		t.Fatalf("listeners default = %#v", listeners.Default)
	}
	listener, ok := defaults[0].(map[string]any)
	if !ok {
		t.Fatalf("listener default = %#v", defaults[0])
	}
	if listener["tls"] != false {
		t.Fatalf("optional tls default = %#v", listener)
	}

	token := program.Variables["app_token"]
	if !token.Sensitive || !token.Ephemeral || token.Default != testassert.SensitiveVariableDefault {
		t.Fatalf("token variable = %#v", token)
	}
	data, err := json.MarshalIndent(program, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "program variable JSON", string(data))
	var decoded struct {
		Variables map[string]struct {
			Default any `json:"default"`
		} `json:"variables"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Variables["app_token"].Default != "<sensitive>" {
		t.Fatalf("program variable JSON did not redact sensitive default:\n%s", data)
	}
}

func TestCompileRejectsInvalidVariableDefault(t *testing.T) {
	_, err := parseOrCompileInline(t, `
variable "environment" {
  type    = string
  default = 42
}

host "server1" {}
`)
	if err == nil || !strings.Contains(err.Error(), `variable "environment" must be string`) {
		t.Fatalf("compile error = %v, want variable type mismatch", err)
	}
}

func TestCompileRejectsNonNullableVariableDefaultNull(t *testing.T) {
	_, err := parseOrCompileInline(t, `
variable "environment" {
  type     = string
  default  = null
  nullable = false
}

host "server1" {}
`)
	if err == nil || !strings.Contains(err.Error(), `variable "environment" must not be null`) {
		t.Fatalf("compile error = %v, want non-null variable error", err)
	}
}

func TestCompileVariableDefaultsIntoHostAndComponent(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/variable-defaults.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileWithOptions(cfg, CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(program.Hosts))
	}
	host := program.Hosts[0]
	if host.System.Hostname != "vars1" {
		t.Fatalf("hostname = %q, want vars1", host.System.Hostname)
	}
	file := host.Files.Files["/etc/debianform/message.txt"]
	if file.Content != "hello from variable default" {
		t.Fatalf("file content = %q", file.Content)
	}
	profileFile := host.Files.Files["/etc/debianform/profile-message.txt"]
	if profileFile.Content != "hello from variable default" {
		t.Fatalf("profile file content = %q", profileFile.Content)
	}
	unit := host.Components[0].Systemd.Units["message.service"]
	if !strings.Contains(unit.Content, "Description=Variable backed service") ||
		!strings.Contains(unit.Content, "ExecStart=/bin/echo \"hello from variable default\"") {
		t.Fatalf("unit content did not include variable defaults:\n%s", unit.Content)
	}
}

func TestCompileVariableValidations(t *testing.T) {
	program := compileInline(t, `
variable "environment" {
  type    = string
  default = "prod"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

host "server1" {}
`)
	if got := program.Variables["environment"].Default; got != "prod" {
		t.Fatalf("environment default = %#v", got)
	}
}

func TestCompileRejectsInvalidVariableValidations(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "validation failure",
			hcl: `
variable "environment" {
  type    = string
  default = "qa"
  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

host "server1" {}
`,
			want: `validation failed for variable "environment": environment must be dev, staging, or prod.`,
		},
		{
			name: "condition non bool",
			hcl: `
variable "environment" {
  type    = string
  default = "prod"
  validation {
    condition     = var.environment
    error_message = "bad"
  }
}

host "server1" {}
`,
			want: `variable validation condition must evaluate to a boolean`,
		},
		{
			name: "other variable",
			hcl: `
variable "other" {
  type    = string
  default = "x"
}

variable "environment" {
  type    = string
  default = "prod"
  validation {
    condition     = var.other == "x"
    error_message = "bad"
  }
}

host "server1" {}
`,
			want: `variable validation can only read var.environment`,
		},
		{
			name: "path reference",
			hcl: `
variable "environment" {
  type    = string
  default = "prod"
  validation {
    condition     = path.module != ""
    error_message = "bad"
  }
}

host "server1" {}
`,
			want: `variable validation can only read var.environment`,
		},
		{
			name: "target reference",
			hcl: `
variable "environment" {
  type    = string
  default = "prod"
  validation {
    condition     = target.platform.codename == "trixie"
    error_message = "bad"
  }
}

host "server1" {}
`,
			want: `variable validation can only read var.environment`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrCompileInline(t, tt.hcl)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileDeprecatedVariableWarnings(t *testing.T) {
	cfg := parseInlineWithOptions(t, `
variable "environment" {
  type       = string
  default    = "prod"
  deprecated = "Use deployment_environment instead."
}

host "server1" {}
`, parser.ParseOptions{VariableValues: []parser.ExternalVariableValue{
		{Name: "environment", Value: "staging", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}},
	}})
	warnings := []ir.Warning{}
	if _, err := CompileWithOptions(cfg, CompileOptions{Warnings: &warnings}); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want 1", warnings)
	}
	if !strings.Contains(warnings[0].Message, `variable "environment" is deprecated`) {
		t.Fatalf("warning = %#v", warnings[0])
	}
	if warnings[0].Source.Path != "cli.var[0]" {
		t.Fatalf("warning source = %#v", warnings[0].Source)
	}

	defaultOnly := parseInline(t, `
variable "environment" {
  type       = string
  default    = "prod"
  deprecated = "Use deployment_environment instead."
}

host "server1" {}
`)
	warnings = []ir.Warning{}
	if _, err := CompileWithOptions(defaultOnly, CompileOptions{Warnings: &warnings}); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("default-only warnings = %#v, want none", warnings)
	}
}

func TestCompileSensitiveVariablePropagatesToResources(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/sensitive-variable-files.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]
	for _, path := range []string{
		"/etc/debianform/token.txt",
		"/etc/debianform/config.json",
		"/etc/debianform/template.txt",
	} {
		file := host.Files.Files[path]
		if !file.Sensitive {
			t.Fatalf("%s sensitive = false", path)
		}
		if !strings.Contains(file.Content, "not-a-real-variable-secret") {
			t.Fatalf("%s in-memory content missing secret: %q", path, file.Content)
		}
	}
	publicFile := host.Files.Files["/etc/debianform/public.txt"]
	if publicFile.Sensitive {
		t.Fatalf("public file sensitive = true")
	}
	if publicFile.Content != "prod" {
		t.Fatalf("public file content = %q", publicFile.Content)
	}

	for _, name := range []string{"raw-token.service", "structured-token.service"} {
		unit := host.Systemd.Units[name]
		if !unit.Sensitive {
			t.Fatalf("%s sensitive = false", name)
		}
		if !strings.Contains(unit.Content, "not-a-real-variable-secret") {
			t.Fatalf("%s in-memory content missing secret: %q", name, unit.Content)
		}
	}

	data, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "not-a-real-variable-secret") {
		t.Fatalf("Program JSON leaked variable secret: %s", data)
	}
}

func TestCompileEphemeralVariableAllowedForFileContent(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/ephemeral-variable-content.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]
	for _, path := range []string{
		"/etc/debianform/runtime-token.txt",
		"/etc/debianform/runtime-token.json",
	} {
		file := host.Files.Files[path]
		if !file.Sensitive {
			t.Fatalf("%s sensitive = false", path)
		}
		if !strings.Contains(file.Content, testassert.EphemeralVariableValue) {
			t.Fatalf("%s in-memory content missing ephemeral value: %q", path, file.Content)
		}
	}
	data, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "ephemeral HostSpec JSON", string(data))
}

func TestCompileRejectsEphemeralFileContentWithoutVersion(t *testing.T) {
	_, err := parseOrCompileInline(t, `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = var.runtime_token
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "requires content_version") {
		t.Fatalf("error = %v, want missing content_version", err)
	}
	if err != nil && strings.Contains(err.Error(), testassert.EphemeralVariableValue) {
		t.Fatalf("ephemeral value leaked in error: %v", err)
	}

	_, err = parseOrCompileInline(t, `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

locals {
  token_file = var.runtime_token
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = local.token_file
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "requires content_version") {
		t.Fatalf("local error = %v, want missing content_version", err)
	}
	if err != nil && strings.Contains(err.Error(), testassert.EphemeralVariableValue) {
		t.Fatalf("ephemeral local value leaked in error: %v", err)
	}
}

func TestCompileRejectsSensitiveFileContentVersion(t *testing.T) {
	_, err := parseOrCompileInline(t, `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

variable "runtime_token_version" {
  type      = string
  sensitive = true
  default   = "v1"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content         = var.runtime_token
      content_version = var.runtime_token_version
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "content_version must not be sensitive") {
		t.Fatalf("error = %v, want sensitive content_version rejection", err)
	}
	if err != nil && strings.Contains(err.Error(), testassert.EphemeralVariableValue) {
		t.Fatalf("ephemeral value leaked in error: %v", err)
	}
}

func TestCompileRejectsEphemeralVariableInStructuralFields(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "file owner",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = "ok"
      owner   = var.runtime_token
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "local file owner",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

locals {
  owner = var.runtime_token
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = "ok"
      owner   = local.owner
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "file source",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      source = var.runtime_token
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "path attribute",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  nftables {
    file "runtime" {
      path    = var.runtime_token
      content = "flush ruleset\n"
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "apt source file content",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  apt {
    source_file "private" {
      path    = "/etc/apt/sources.list.d/private.list"
      content = var.runtime_token
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "apt repository signing key content",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  apt {
    repository "private" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        content = var.runtime_token
      }
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "nftables main content",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  nftables {
    main {
      content = var.runtime_token
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "nftables content",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  nftables {
    file "private" {
      content = var.runtime_token
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "package list",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  packages {
    install = [var.runtime_token]
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "lifecycle",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = "ok"
      lifecycle {
        prevent_destroy = var.runtime_token
      }
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "structured systemd run",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  systemd {
    service_unit "runtime" {
      run = var.runtime_token
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "structured systemd environment",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  systemd {
    service_unit "runtime" {
      run = "/bin/true"
      environment = {
        TOKEN = var.runtime_token
      }
    }
  }
}
`,
			want: "ephemeral value is not allowed in this field",
		},
		{
			name: "depends_on",
			hcl: `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content    = "ok"
      depends_on = [var.runtime_token]
    }
  }
}
`,
			want: "unsupported attribute",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrCompileInline(t, tt.hcl)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), testassert.EphemeralVariableValue) {
				t.Fatalf("ephemeral value leaked in error: %v", err)
			}
		})
	}
}
