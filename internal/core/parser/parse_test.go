package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/zclconf/go-cty/cty"
)

func TestParseHostProfileNestedBlocksAndSourceLine(t *testing.T) {
	file := writeConfig(t, `
profile "base" {
  packages {
    install = ["curl"]
  }
}

host "web1" {
  imports = [profile.base]

  ssh {
    host = "10.0.0.10"
  }

  kernel {
    modules = ["tcp_bbr"]
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cfg.Profiles["base"]; !ok {
		t.Fatalf("profile base was not parsed")
	}
	host, ok := cfg.Hosts["web1"]
	if !ok {
		t.Fatalf("host web1 was not parsed")
	}
	if !reflect.DeepEqual(host.Imports, []string{"base"}) {
		t.Fatalf("host imports = %#v, want base", host.Imports)
	}

	kernel := host.Body.Map["kernel"]
	modules := kernel.Map["modules"]
	if modules.Source.File != file {
		t.Fatalf("modules source file = %q, want %q", modules.Source.File, file)
	}
	if modules.Source.Line != 15 {
		t.Fatalf("modules source line = %d, want 15", modules.Source.Line)
	}
	if modules.Source.Path != "host.web1.kernel.modules" {
		t.Fatalf("modules source path = %q", modules.Source.Path)
	}
}

func TestParseRejectsUnknownTopLevelBlock(t *testing.T) {
	file := writeConfig(t, `
banana {}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), `unknown top-level block "banana"`) {
		t.Fatalf("ParseFiles() error = %v, want unknown top-level block", err)
	}
}

func TestParseRejectsWrongLabelCount(t *testing.T) {
	file := writeConfig(t, `
profile {}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "profile block requires exactly one label") {
		t.Fatalf("ParseFiles() error = %v, want label count error", err)
	}
}

func TestParseAcceptsValidHCLIdentifierLabels(t *testing.T) {
	file := writeConfig(t, `
profile "配置一" {}

component "模板一" {}

host "edge-1" {
  component "实例一" {
    source = component.模板一
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Profiles["配置一"]; !ok {
		t.Fatalf("Unicode profile label was not parsed: %#v", cfg.Profiles)
	}
	if _, ok := cfg.Components["模板一"]; !ok {
		t.Fatalf("Unicode component label was not parsed: %#v", cfg.Components)
	}
	host, ok := cfg.Hosts["edge-1"]
	if !ok || len(host.Components) != 1 || host.Components[0].Name != "实例一" {
		t.Fatalf("hyphen host or Unicode component instance was not parsed: %#v", cfg.Hosts)
	}
}

func TestParseRejectsInvalidLabeledBlockIdentifiers(t *testing.T) {
	tests := []struct {
		name  string
		hcl   string
		label string
	}{
		{name: "empty", hcl: `host "" {}`, label: ""},
		{name: "fqdn", hcl: `host "web.example.com" {}`, label: "web.example.com"},
		{name: "single dot", hcl: `profile "." {}`, label: "."},
		{name: "double dot", hcl: `profile ".." {}`, label: ".."},
		{name: "parent path", hcl: `component "../base" {}`, label: "../base"},
		{name: "slash", hcl: `component "web/prod" {}`, label: "web/prod"},
		{name: "quote", hcl: `host "web\"prod" {}`, label: `web"prod`},
		{
			name: "component instance",
			hcl: `component "base" {}
host "server1" {
  component "bad/instance" {
    source = component.base
  }
}`,
			label: "bad/instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeConfig(t, tt.hcl)
			_, err := ParseFiles([]string{file})
			if err == nil || !strings.Contains(err.Error(), "must be a valid HCL identifier") {
				t.Fatalf("ParseFiles() error = %v, want invalid label %q", err, tt.label)
			}
		})
	}
}

func TestParseRejectsDuplicateHost(t *testing.T) {
	file := writeConfig(t, `
host "web1" {}
host "web1" {}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), `duplicate host "web1"`) {
		t.Fatalf("ParseFiles() error = %v, want duplicate host error", err)
	}
}

func TestParseVariableRichTypesAndMetadata(t *testing.T) {
	file := writeConfig(t, `
variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "prod"
  nullable    = false
  sensitive   = true
  ephemeral   = true
  const       = true
  deprecated  = "Use deployment_environment instead."

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be valid"
  }
}

variable "listeners" {
  type = list(object({
    name = string
    port = number
    tls  = optional(bool, false)
    tags = optional(map(string), {})
  }))

  default = [
    {
      name = "http"
      port = 80
    },
  ]
}

variable "labels" {
  type    = map(string)
  default = {}
}

variable "ports" {
  type    = set(number)
  default = [443, 80, 443]
}

variable "pair" {
  type    = tuple([string, number, bool])
  default = ["https", 443, true]
}

host "server1" {}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	environment := cfg.Variables["environment"]
	if environment.Type != "string" || environment.Description != "Deployment environment." {
		t.Fatalf("environment variable = %#v", environment)
	}
	if environment.Default == nil || environment.Default.String != "prod" {
		t.Fatalf("environment default = %#v", environment.Default)
	}
	if environment.Nullable || !environment.Sensitive || !environment.Ephemeral || !environment.Const {
		t.Fatalf("environment booleans = nullable:%v sensitive:%v ephemeral:%v const:%v", environment.Nullable, environment.Sensitive, environment.Ephemeral, environment.Const)
	}
	if environment.Deprecated != "Use deployment_environment instead." {
		t.Fatalf("deprecated = %q", environment.Deprecated)
	}
	if len(environment.Validations) != 1 || environment.Validations[0].Source.Path != `variable["environment"].validation[0]` {
		t.Fatalf("validations = %#v", environment.Validations)
	}

	listeners := cfg.Variables["listeners"]
	wantType := `list(object({name=string,port=number,tags=optional(map(string),{}),tls=optional(bool,false)}))`
	if listeners.Type != wantType {
		t.Fatalf("listeners type = %q, want %q", listeners.Type, wantType)
	}
	if listeners.TypeSpec.Kind != ComponentInputTypeList || listeners.TypeSpec.Element == nil || listeners.TypeSpec.Element.Kind != ComponentInputTypeObject {
		t.Fatalf("listeners type spec = %#v", listeners.TypeSpec)
	}
	attrs := listeners.TypeSpec.Element.Attributes
	if !attrs["tls"].Optional || attrs["tls"].Default == nil || attrs["tls"].Default.Bool {
		t.Fatalf("tls attr = %#v", attrs["tls"])
	}
	if !attrs["tags"].Optional || attrs["tags"].Default == nil || attrs["tags"].Default.Kind != KindMap {
		t.Fatalf("tags attr = %#v", attrs["tags"])
	}
	for _, name := range []string{"labels", "ports", "pair"} {
		if _, ok := cfg.Variables[name]; !ok {
			t.Fatalf("variable %q was not parsed", name)
		}
	}
}

func TestParseRejectsInvalidVariableBlocks(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "no label",
			hcl: `
variable {
  type = string
}
`,
			want: "variable block requires exactly one label",
		},
		{
			name: "two labels",
			hcl: `
variable "one" "two" {
  type = string
}
`,
			want: "variable block requires exactly one label",
		},
		{
			name: "duplicate",
			hcl: `
variable "environment" {
  type = string
}

variable "environment" {
  type = string
}
`,
			want: `duplicate variable "environment"`,
		},
		{
			name: "unknown attribute",
			hcl: `
variable "environment" {
  type    = string
  unknown = true
}
`,
			want: `unsupported attribute variable["environment"].unknown`,
		},
		{
			name: "wrong type expression",
			hcl: `
variable "environment" {
  type = array(string)
}
`,
			want: "array(T) is not supported; use list(T)",
		},
		{
			name: "wrong bool attribute type",
			hcl: `
variable "environment" {
  type      = string
  sensitive = "yes"
}
`,
			want: `variable["environment"].sensitive must be a boolean`,
		},
		{
			name: "validation label",
			hcl: `
variable "environment" {
  type = string

  validation "range" {
    condition     = true
    error_message = "ok"
  }
}
`,
			want: `variable["environment"].validation[0] block must not have labels`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeConfig(t, tt.hcl)
			_, err := ParseFiles([]string{file})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseFiles error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseVariableDefaultReferences(t *testing.T) {
	file := writeConfig(t, `
locals {
  default_message = "hello"
}

variable "message" {
  type    = string
  default = local.default_message
}

host "server1" {
  files {
    file "/etc/message" {
      content = var.message
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.VariableValues["message"].String; got != "hello" {
		t.Fatalf("var.message = %q, want hello", got)
	}
	content := cfg.Hosts["server1"].Body.Map["files"].Map["file"].Map["/etc/message"].Map["content"]
	if content.String != "hello" {
		t.Fatalf("file content = %#v", content)
	}
}

func TestParseLocalsCanReferenceVariablesAndOtherLocals(t *testing.T) {
	file := writeConfig(t, `
variable "app" {
  type    = string
  default = "firecrawl"
}

variable "settings" {
  type = object({
    domain = string
  })
  default = {
    domain = "example.test"
  }
}

variable "ports" {
  type    = list(number)
  default = [3000]
}

variable "token" {
  type      = string
  sensitive = true
  default   = "not-a-real-local-secret"
}

variable "runtime_token" {
  type      = string
  sensitive = true
  ephemeral = true
  default   = "not-a-real-local-runtime-token"
}

locals {
  labels       = jsonencode({ app = local.app_name })
  env          = <<-ENV
    APP=${local.app_name}
    DOMAIN=${var.settings.domain}
    PORT=${var.ports[0]}
  ENV
  secret_env   = "TOKEN=${var.token}\n"
  runtime_copy = var.runtime_token
  app_name     = var.app
}

host "server1" {
  files {
    file "/etc/app.env" {
      content = local.env
    }
  }
}
`)

	cfg, err := ParseFilesWithOptions([]string{file}, ParseOptions{VariableValues: []ExternalVariableValue{
		{Name: "app", Value: "api", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Locals["app_name"].String; got != "api" {
		t.Fatalf("local.app_name = %q, want api", got)
	}
	if got := cfg.Locals["labels"].String; got != `{"app":"api"}` {
		t.Fatalf("local.labels = %q, want encoded api label", got)
	}
	env := cfg.Locals["env"].String
	for _, want := range []string{"APP=api", "DOMAIN=example.test", "PORT=3000"} {
		if !strings.Contains(env, want) {
			t.Fatalf("local.env = %q, missing %q", env, want)
		}
	}
	content := cfg.Hosts["server1"].Body.Map["files"].Map["file"].Map["/etc/app.env"].Map["content"]
	if content.String != env {
		t.Fatalf("host file content = %q, want local.env %q", content.String, env)
	}
	if !cfg.Locals["secret_env"].Sensitive {
		t.Fatalf("local.secret_env did not inherit sensitive mark")
	}
	runtimeCopy := cfg.Locals["runtime_copy"]
	if !runtimeCopy.Sensitive || !runtimeCopy.Ephemeral {
		t.Fatalf("local.runtime_copy marks = sensitive:%v ephemeral:%v, want both", runtimeCopy.Sensitive, runtimeCopy.Ephemeral)
	}
}

func TestParseRejectsInvalidLocalReferences(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "unknown variable",
			hcl: `
locals {
  app = var.missing
}

host "server1" {}
`,
			want: `local.app: unknown variable var.missing`,
		},
		{
			name: "unknown local",
			hcl: `
locals {
  app = local.missing
}

host "server1" {}
`,
			want: `local.app: unknown local local.missing`,
		},
		{
			name: "cycle",
			hcl: `
locals {
  a = local.b
  b = local.a
}

host "server1" {}
`,
			want: `locals cycle detected: local.a -> local.b -> local.a`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeConfig(t, tt.hcl)
			_, err := ParseFiles([]string{file})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseFiles error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidVariableDefaultsAndReferences(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "required variable",
			hcl: `
variable "message" {
  type = string
}

host "server1" {}
`,
			want: `variable "message" is required`,
		},
		{
			name: "default reads var",
			hcl: `
variable "message" {
  type    = string
  default = var.other
}

host "server1" {}
`,
			want: "variable default cannot reference var",
		},
		{
			name: "default reads path",
			hcl: `
variable "message" {
  type    = string
  default = path.module
}

host "server1" {}
`,
			want: "variable default cannot reference path",
		},
		{
			name: "optional default reads path",
			hcl: `
variable "message" {
  type = object({
    value = optional(string, path.module)
  })
  default = {}
}

host "server1" {}
`,
			want: "variable default cannot reference path",
		},
		{
			name: "unknown variable reference",
			hcl: `
host "server1" {
  files {
    file "/etc/message" {
      content = var.message
    }
  }
}
`,
			want: "Unsupported attribute",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeConfig(t, tt.hcl)
			_, err := ParseFiles([]string{file})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseFiles error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseExternalVariableValues(t *testing.T) {
	file := writeConfig(t, `
variable "environment" {
  type    = string
  default = "dev"
}

variable "replicas" {
  type    = number
  default = 1
}

variable "enabled" {
  type    = bool
  default = false
}

variable "ports" {
  type    = list(number)
  default = []
}

variable "labels" {
  type = object({
    tier   = string
    canary = optional(bool, false)
  })
  default = {
    tier = "backend"
  }
}

host "server1" {}
`)

	cfg, err := ParseFilesWithOptions([]string{file}, ParseOptions{VariableValues: []ExternalVariableValue{
		{Name: "environment", Value: "prod", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}},
		{Name: "environment", Value: "staging", Source: ir.SourceRef{File: "cli", Line: 2, Path: "cli.var[1]"}},
		{Name: "replicas", Value: "3", Source: ir.SourceRef{File: "cli", Line: 3, Path: "cli.var[2]"}},
		{Name: "enabled", Value: "true", Source: ir.SourceRef{File: "cli", Line: 4, Path: "cli.var[3]"}},
		{Name: "ports", Value: "[80,443]", Source: ir.SourceRef{File: "cli", Line: 5, Path: "cli.var[4]"}},
		{Name: "labels", Value: `{"tier":"frontend"}`, Source: ir.SourceRef{File: "cli", Line: 6, Path: "cli.var[5]"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.VariableValues["environment"].String; got != "staging" {
		t.Fatalf("environment = %q, want staging", got)
	}
	if got := cfg.VariableValues["replicas"].Number; got != "3" {
		t.Fatalf("replicas = %q, want 3", got)
	}
	if !cfg.VariableValues["enabled"].Bool {
		t.Fatalf("enabled = %#v", cfg.VariableValues["enabled"])
	}
	if got := len(cfg.VariableValues["ports"].List); got != 2 {
		t.Fatalf("ports length = %d, want 2", got)
	}
	labels := cfg.VariableValues["labels"].Map
	if labels["tier"].String != "frontend" {
		t.Fatalf("labels = %#v", labels)
	}
	if labels["canary"].Kind != KindBool || labels["canary"].Bool {
		t.Fatalf("labels canary = %#v", labels["canary"])
	}
}

func TestParseRejectsInvalidExternalVariableValues(t *testing.T) {
	tests := []struct {
		name string
		vars []ExternalVariableValue
		want string
	}{
		{
			name: "unknown",
			vars: []ExternalVariableValue{{Name: "missing", Value: "value", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}}},
			want: `unknown variable "missing"`,
		},
		{
			name: "type mismatch",
			vars: []ExternalVariableValue{{Name: "replicas", Value: "many", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}}},
			want: `invalid value for variable "replicas"`,
		},
		{
			name: "invalid complex JSON",
			vars: []ExternalVariableValue{{Name: "labels", Value: `{"tier":"frontend"} trailing`, Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}}},
			want: `invalid JSON value for variable "labels"`,
		},
		{
			name: "nullable false",
			vars: []ExternalVariableValue{{Name: "required_any", Value: "null", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}}},
			want: `variable "required_any" must not be null`,
		},
		{
			name: "sensitive value redacted",
			vars: []ExternalVariableValue{{Name: "token_seed", Value: "not-a-real-cli-secret", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}}},
			want: `invalid value for sensitive variable "token_seed"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFilesWithOptions([]string{"../testdata/fixtures/variable-cli.dbf.hcl"}, ParseOptions{VariableValues: tt.vars})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseFiles error = %v, want %q", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "not-a-real-cli-secret") {
				t.Fatalf("sensitive value leaked in error: %v", err)
			}
		})
	}
}

func TestParseVariableFileValues(t *testing.T) {
	vars, err := ParseVariableFile("../testdata/fixtures/variable-prod.dbfvars")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseFilesWithOptions([]string{"../testdata/fixtures/variable-cli.dbf.hcl"}, ParseOptions{VariableValues: vars})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.VariableValues["environment"].String; got != "prod" {
		t.Fatalf("environment = %q, want prod", got)
	}
	if got := cfg.VariableValues["replicas"].Number; got != "4" {
		t.Fatalf("replicas = %q, want 4", got)
	}
	if !cfg.VariableValues["enabled"].Bool {
		t.Fatalf("enabled = %#v, want true", cfg.VariableValues["enabled"])
	}
	if got := len(cfg.VariableValues["ports"].List); got != 2 {
		t.Fatalf("ports length = %d, want 2", got)
	}
	if got := cfg.VariableValues["labels"].Map["tier"].String; got != "frontend" {
		t.Fatalf("labels.tier = %q, want frontend", got)
	}
}

func TestParseJSONVariableFileValues(t *testing.T) {
	dir := t.TempDir()
	varFile := filepath.Join(dir, "prod.dbfvars.json")
	if err := os.WriteFile(varFile, []byte(`{"environment":"json","ports":[8080],"labels":{"tier":"api"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	vars, err := ParseVariableFile(varFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != 3 {
		t.Fatalf("vars length = %d, want 3", len(vars))
	}
	if vars[0].Name != "environment" || vars[0].ParsedValue == nil || vars[0].ParsedValue.String != "json" {
		t.Fatalf("environment var = %#v", vars[0])
	}
	if vars[2].Name != "ports" || vars[2].ParsedValue == nil || len(vars[2].ParsedValue.List) != 1 {
		t.Fatalf("ports var = %#v", vars[2])
	}
}

func TestParseExternalVariablePrecedenceAndIgnoredUnknownEnv(t *testing.T) {
	file := writeConfig(t, `
variable "environment" {
  type    = string
  default = "dev"
}

host "server1" {}
`)
	envValue := Value{Kind: KindString, String: "env", Source: ir.SourceRef{File: "env", Line: 1, Path: "DBF_VAR_environment"}}
	fileValue := Value{Kind: KindString, String: "file", Source: ir.SourceRef{File: "vars.dbfvars", Line: 1, Path: "varfile.environment"}}

	cfg, err := ParseFilesWithOptions([]string{file}, ParseOptions{VariableValues: []ExternalVariableValue{
		{Name: "environment", ParsedValue: &envValue, Source: envValue.Source, IgnoreUnknown: true},
		{Name: "unused_env", Value: "ignored", Source: ir.SourceRef{File: "env", Line: 1, Path: "DBF_VAR_unused_env"}, IgnoreUnknown: true},
		{Name: "environment", ParsedValue: &fileValue, Source: fileValue.Source},
		{Name: "environment", Value: "cli", Source: ir.SourceRef{File: "cli", Line: 1, Path: "cli.var[0]"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.VariableValues["environment"].String; got != "cli" {
		t.Fatalf("environment = %q, want cli", got)
	}
}

func TestEvalRejectsEphemeralMapKey(t *testing.T) {
	file := writeConfig(t, `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = jsonencode({
        (var.runtime_token) = "value"
      })
    }
  }
}
`)
	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "map key cannot use ephemeral values") {
		t.Fatalf("ParseFiles error = %v, want ephemeral map key", err)
	}
}

func TestEvalRejectsEphemeralSetElement(t *testing.T) {
	file := writeConfig(t, `
variable "runtime_token" {
  type      = string
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

host "server1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = jsonencode(toset([var.runtime_token]))
    }
  }
}
`)
	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "set element cannot use ephemeral values") {
		t.Fatalf("ParseFiles error = %v, want ephemeral set element", err)
	}
}

func TestValueToCtyPreservesEphemeralMark(t *testing.T) {
	value := Value{Kind: KindString, String: "secret", Ephemeral: true}
	converted, err := value.ToCty()
	if err != nil {
		t.Fatal(err)
	}
	if !converted.HasMark(EphemeralMark) {
		t.Fatalf("converted value missing ephemeral mark: %#v", converted)
	}
	roundTripped, err := ctyToValue(cty.StringVal("secret").Mark(EphemeralMark), ir.SourceRef{File: "test", Line: 1, Path: "value"})
	if err != nil {
		t.Fatal(err)
	}
	if !roundTripped.Ephemeral || !roundTripped.Sensitive {
		t.Fatalf("round-tripped value marks = sensitive:%v ephemeral:%v", roundTripped.Sensitive, roundTripped.Ephemeral)
	}
}

func TestParseLabeledObjectBlockSourcePath(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  packages {
    package "bird2" {
      repositories = ["cznic"]
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	packages := cfg.Hosts["web1"].Body.Map["packages"]
	pkg := packages.Map["package"].Map["bird2"]
	if pkg.Source.Path != `host.web1.packages.package["bird2"]` {
		t.Fatalf("package source path = %q", pkg.Source.Path)
	}
	repositories := pkg.Map["repositories"]
	if repositories.Source.Path != `host.web1.packages.package["bird2"].repositories` {
		t.Fatalf("repositories source path = %q", repositories.Source.Path)
	}
	if repositories.List[0].Source.Path != `host.web1.packages.package["bird2"].repositories[0]` {
		t.Fatalf("repository item source path = %q", repositories.List[0].Source.Path)
	}
}

func TestParseAPTSigningKeyBlockSourcePath(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  apt {
    repository "tools" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        url    = "https://repo.example/key.asc"
        sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      }
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	repository := cfg.Hosts["web1"].Body.Map["apt"].Map["repository"].Map["tools"]
	if repository.Source.Path != `host.web1.apt.repository["tools"]` {
		t.Fatalf("repository source path = %q", repository.Source.Path)
	}
	signingKey := repository.Map["signing_key"]
	if signingKey.Source.Path != `host.web1.apt.repository["tools"].signing_key` {
		t.Fatalf("signing key source path = %q", signingKey.Source.Path)
	}
	sha := signingKey.Map["sha256"]
	if sha.Source.Path != `host.web1.apt.repository["tools"].signing_key.sha256` {
		t.Fatalf("sha256 source path = %q", sha.Source.Path)
	}
}

func TestParseComponentReferences(t *testing.T) {
	file := writeConfig(t, `
component "rclone" {
  input "version" {
    type    = string
    default = "1.66.0"
  }
}

component "restic" {}

host "web1" {
  components = [
    component.rclone,
  ]

  component "backup" {
    source = component.restic

    inputs = {
      environment_source = "secrets/restic.env"
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	rclone, ok := cfg.Components["rclone"]
	if !ok {
		t.Fatalf("component rclone was not parsed")
	}
	input := rclone.Inputs["version"]
	if input.Type != "string" || input.Default == nil || input.Default.String != "1.66.0" {
		t.Fatalf("component input = %#v", input)
	}
	host := cfg.Hosts["web1"]
	if len(host.Components) != 2 {
		t.Fatalf("host components = %d, want 2", len(host.Components))
	}
	if host.Components[0].Name != "rclone" || host.Components[0].Template != "rclone" {
		t.Fatalf("shorthand component = %#v", host.Components[0])
	}
	if host.Components[1].Name != "backup" || host.Components[1].Template != "restic" {
		t.Fatalf("block component = %#v", host.Components[1])
	}
	if got := host.Components[1].Inputs["environment_source"].String; got != "secrets/restic.env" {
		t.Fatalf("component input value = %q", got)
	}
}

func TestParseComponentInputRichTypes(t *testing.T) {
	file := writeConfig(t, `
component "proxy" {
  input "listeners" {
    type = list(object({
      name = string
      port = number
      tls  = optional(bool, false)
      tags = optional(map(string), {})
    }))

    description = "Listener definitions."
    default     = []
    nullable    = false
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	input := cfg.Components["proxy"].Inputs["listeners"]
	if input.Type != `list(object({name=string,port=number,tags=optional(map(string),{}),tls=optional(bool,false)}))` {
		t.Fatalf("input type = %q", input.Type)
	}
	if input.Description != "Listener definitions." || input.Nullable {
		t.Fatalf("input metadata = %#v", input)
	}
	if input.TypeSpec.Kind != ComponentInputTypeList || input.TypeSpec.Element == nil || input.TypeSpec.Element.Kind != ComponentInputTypeObject {
		t.Fatalf("input type spec = %#v", input.TypeSpec)
	}
	attrs := input.TypeSpec.Element.Attributes
	if !attrs["tls"].Optional || attrs["tls"].Default == nil || attrs["tls"].Default.Bool {
		t.Fatalf("tls attr = %#v", attrs["tls"])
	}
	if !attrs["tags"].Optional || attrs["tags"].Default == nil || attrs["tags"].Default.Kind != KindMap {
		t.Fatalf("tags attr = %#v", attrs["tags"])
	}
}

func TestParseComponentInputValidationAndDeprecated(t *testing.T) {
	file := writeConfig(t, `
component "proxy" {
  input "listeners" {
    type       = list(object({ name = string, port = number }))
    deprecated = "Use endpoints instead."

    validation {
      condition     = alltrue([for listener in input.listeners : listener.port > 0])
      error_message = "ports must be positive"
    }

    validation {
      condition     = length(input.listeners) < 10
      error_message = "too many listeners"
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	input := cfg.Components["proxy"].Inputs["listeners"]
	if input.Deprecated != "Use endpoints instead." {
		t.Fatalf("deprecated = %q", input.Deprecated)
	}
	if len(input.Validations) != 2 {
		t.Fatalf("validations = %d, want 2", len(input.Validations))
	}
	if input.Validations[0].Source.Path != `component.proxy.input["listeners"].validation[0]` {
		t.Fatalf("validation source path = %q", input.Validations[0].Source.Path)
	}
	if input.Validations[0].Message != "ports must be positive" {
		t.Fatalf("validation message = %q", input.Validations[0].Message)
	}
}

func TestParseRejectsInvalidComponentInputTypes(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "array alias",
			hcl: `
component "bad" {
  input "ports" {
    type = array(number)
  }
}
`,
			want: "array(T) is not supported; use list(T)",
		},
		{
			name: "bare list",
			hcl: `
component "bad" {
  input "ports" {
    type = list
  }
}
`,
			want: "list requires an element type",
		},
		{
			name: "optional outside object",
			hcl: `
component "bad" {
  input "ports" {
    type = optional(number)
  }
}
`,
			want: "optional() is only allowed inside object attribute type declarations",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeConfig(t, tt.hcl)
			_, err := ParseFiles([]string{file})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseFiles error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidComponentInputValidationBlocks(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "validation label",
			hcl: `
component "bad" {
  input "ports" {
    type = list(number)
    validation "range" {
      condition     = true
      error_message = "ok"
    }
  }
}
`,
			want: "block must not have labels",
		},
		{
			name: "missing condition",
			hcl: `
component "bad" {
  input "ports" {
    type = list(number)
    validation {
      error_message = "ok"
    }
  }
}
`,
			want: ".condition is required",
		},
		{
			name: "missing message",
			hcl: `
component "bad" {
  input "ports" {
    type = list(number)
    validation {
      condition = true
    }
  }
}
`,
			want: ".error_message is required",
		},
		{
			name: "empty message",
			hcl: `
component "bad" {
  input "ports" {
    type = list(number)
    validation {
      condition     = true
      error_message = ""
    }
  }
}
`,
			want: "error_message must be a non-empty string",
		},
		{
			name: "empty deprecated",
			hcl: `
component "bad" {
  input "ports" {
    type       = list(number)
    deprecated = ""
  }
}
`,
			want: "deprecated must be a non-empty string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeConfig(t, tt.hcl)
			_, err := ParseFiles([]string{file})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseFiles error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseComponentArtifact(t *testing.T) {
	file := writeConfig(t, `
component "rclone" {
  type    = "binary"
  version = "1.66.0"

  source "amd64" {
    url    = "https://downloads.example/rclone-amd64.zip"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  extract {
    format           = "zip"
    strip_components = 1
    include          = "rclone"
  }

  build {
    packages = ["make"]

    commands = [
      ["make"],
      ["make", "install"],
    ]
    working_dir = "src"
    output      = "bin/rclone"
    source_name = "rclone.c"
  }

  install {
    path = "/usr/local/bin/rclone"
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	component := cfg.Components["rclone"]
	if component.Type != "binary" || component.Version != "1.66.0" {
		t.Fatalf("component artifact attrs = %#v", component)
	}
	source := component.Sources["amd64"]
	if source.URL != "https://downloads.example/rclone-amd64.zip" {
		t.Fatalf("source url = %q", source.URL)
	}
	if source.Source.Path != `component.rclone.source["amd64"]` {
		t.Fatalf("source path = %q", source.Source.Path)
	}
	if component.Extract == nil || component.Extract.StripComponents != 1 || component.Extract.Include != "rclone" {
		t.Fatalf("extract = %#v", component.Extract)
	}
	if component.Build == nil || len(component.Build.Commands) != 2 || component.Build.Commands[1][1] != "install" {
		t.Fatalf("build commands = %#v", component.Build)
	}
	if !reflect.DeepEqual(component.Build.Packages, []string{"make"}) {
		t.Fatalf("build packages = %#v", component.Build.Packages)
	}
	if component.Build.WorkingDir != "src" || component.Build.Output != "bin/rclone" || component.Build.SourceName != "rclone.c" {
		t.Fatalf("build attrs = %#v", component.Build)
	}
	if component.Install == nil || component.Install.Path != "/usr/local/bin/rclone" {
		t.Fatalf("install = %#v", component.Install)
	}
}

func TestParseComponentScriptAndFileOnChange(t *testing.T) {
	file := writeConfig(t, `
component "app" {
  script "reload" {
    mode        = "each"
    interpreter = ["/bin/bash", "-e"]
    outputs     = ["/etc/app.rendered"]
    run         = "systemctl reload app.service"
  }

  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	component := cfg.Components["app"]
	script := component.Scripts["reload"]
	if script.Name != "reload" || !script.ModeSet || !script.InterpreterSet || !script.OutputsSet || !script.RunSet {
		t.Fatalf("script = %#v", script)
	}
	evaluated, err := EvaluateComponentScript(script, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(evaluated.Outputs, []string{"/etc/app.rendered"}) {
		t.Fatalf("script outputs = %#v", evaluated.Outputs)
	}
	if script.Source.Path != `component.app.script["reload"]` {
		t.Fatalf("script source path = %q", script.Source.Path)
	}
	body, err := ParseComponentBody(component, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	fileBlock := body.Map["files"].Map["file"].Map["/etc/app.conf"]
	onChange := fileBlock.Map["on_change"]
	if onChange.String != "reload" {
		t.Fatalf("on_change = %#v", onChange)
	}
	if onChange.Source.Path != `component.app.files.file["/etc/app.conf"].on_change` {
		t.Fatalf("on_change source path = %q", onChange.Source.Path)
	}
}

func TestParseRejectsInvalidFileOnChangeTraversal(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
	}{
		{
			name: "string",
			hcl: `
component "app" {
  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = "reload"
    }
  }
}
`,
		},
		{
			name: "wrong root",
			hcl: `
component "app" {
  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = component.reload
    }
  }
}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeConfig(t, tt.hcl)
			cfg, err := ParseFiles([]string{file})
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseComponentBody(cfg.Components["app"], EvalContext{})
			if err == nil || !strings.Contains(err.Error(), "script reference must be script.<name>") {
				t.Fatalf("ParseComponentBody error = %v, want script traversal error", err)
			}
		})
	}
}

func TestParseRootScriptAndStructuredReferences(t *testing.T) {
	file := writeConfig(t, `
script "reload" {
  mode = "once"
  run  = "networkctl reload"
}

component "wan" {
  files {
    file "/etc/systemd/network/20-wan.network" {
      content   = "wan"
      on_change = script.reload
    }
  }
}

component "policy" {
  script "reload" {
    run = "local reload"
  }

  files {
    file "/etc/systemd/network/30-policy.network" {
      content   = "policy"
      on_change = global.script.reload
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if script := cfg.Scripts["reload"]; script.Source.Path != `script["reload"]` {
		t.Fatalf("root script = %#v", script)
	}
	wan, err := ParseComponentBody(cfg.Components["wan"], EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	wanRef := wan.Map["files"].Map["file"].Map["/etc/systemd/network/20-wan.network"].Map["on_change"].ScriptReference
	if wanRef == nil || wanRef.Scope != ScriptReferenceAuto || wanRef.Name != "reload" {
		t.Fatalf("wan reference = %#v", wanRef)
	}
	policy, err := ParseComponentBody(cfg.Components["policy"], EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	policyRef := policy.Map["files"].Map["file"].Map["/etc/systemd/network/30-policy.network"].Map["on_change"].ScriptReference
	if policyRef == nil || policyRef.Scope != ScriptReferenceGlobal || policyRef.Name != "reload" {
		t.Fatalf("policy reference = %#v", policyRef)
	}
}

func TestParseRejectsRootScriptComponentInputReference(t *testing.T) {
	file := writeConfig(t, `
script "reload" {
  run = "echo ${input.service}"
}
`)
	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "root script cannot reference component input.*") {
		t.Fatalf("ParseFiles() error = %v", err)
	}
}

func TestParseRejectsDuplicateRootScriptDeclarations(t *testing.T) {
	file := writeConfig(t, `
script "reload" { run = "one" }
script "reload" { run = "two" }
`)
	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), `duplicate root script "reload"`) || !strings.Contains(err.Error(), "first defined at") {
		t.Fatalf("ParseFiles() error = %v", err)
	}
}

func TestParseLifecycleBlock(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  files {
    file "/etc/protected.conf" {
      content = "managed"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	fileBlock := cfg.Hosts["web1"].Body.Map["files"].Map["file"].Map["/etc/protected.conf"]
	lifecycle := fileBlock.Map["lifecycle"]
	if lifecycle.Source.Path != `host.web1.files.file["/etc/protected.conf"].lifecycle` {
		t.Fatalf("lifecycle source path = %q", lifecycle.Source.Path)
	}
	preventDestroy := lifecycle.Map["prevent_destroy"]
	if preventDestroy.Kind != KindBool || !preventDestroy.Bool {
		t.Fatalf("prevent_destroy = %#v, want true", preventDestroy)
	}
	if preventDestroy.Source.Path != `host.web1.files.file["/etc/protected.conf"].lifecycle.prevent_destroy` {
		t.Fatalf("prevent_destroy source path = %q", preventDestroy.Source.Path)
	}
}

func TestParseRejectsUnknownLifecycleAttribute(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  files {
    file "/etc/protected.conf" {
      content = "managed"

      lifecycle {
        ignore_changes = true
      }
    }
  }
}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "unsupported attribute") || !strings.Contains(err.Error(), "lifecycle.ignore_changes") {
		t.Fatalf("ParseFiles() error = %v, want unsupported lifecycle attribute", err)
	}
}

func TestParseNftablesMainAndFileSourcePath(t *testing.T) {
	file := writeConfig(t, `
host "edge1" {
  nftables {
    enable = true

    main {
      content = "flush ruleset\n"
    }

    file "20-services" {
      content = "add rule inet filter input tcp dport 443 accept\n"
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	nftables := cfg.Hosts["edge1"].Body.Map["nftables"]
	if nftables.Map["enable"].Source.Path != "host.edge1.nftables.enable" {
		t.Fatalf("enable source path = %q", nftables.Map["enable"].Source.Path)
	}
	main := nftables.Map["main"]
	if main.Source.Path != "host.edge1.nftables.main" {
		t.Fatalf("main source path = %q", main.Source.Path)
	}
	snippet := nftables.Map["file"].Map["20-services"]
	if snippet.Source.Path != `host.edge1.nftables.file["20-services"]` {
		t.Fatalf("snippet source path = %q", snippet.Source.Path)
	}
	content := snippet.Map["content"]
	if content.Source.Path != `host.edge1.nftables.file["20-services"].content` {
		t.Fatalf("snippet content source path = %q", content.Source.Path)
	}
}

func TestParseDockerSourcePaths(t *testing.T) {
	file := writeConfig(t, `
host "docker1" {
  docker {
    enable = true

    package {
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
      gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
      gpg_sha256     = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }

    daemon {
      settings = {
        "log-driver" = "json-file"
      }
    }

    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      env_file "app" {
        path    = "/opt/app/.env"
        content = "TOKEN=example\n"
      }
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	docker := cfg.Hosts["docker1"].Body.Map["docker"]
	if docker.Map["enable"].Source.Path != "host.docker1.docker.enable" {
		t.Fatalf("enable source path = %q", docker.Map["enable"].Source.Path)
	}
	packageBlock := docker.Map["package"]
	if packageBlock.Map["repository_url"].Source.Path != "host.docker1.docker.package.repository_url" {
		t.Fatalf("package repository_url source path = %q", packageBlock.Map["repository_url"].Source.Path)
	}
	if packageBlock.Map["gpg_url"].Source.Path != "host.docker1.docker.package.gpg_url" {
		t.Fatalf("package gpg_url source path = %q", packageBlock.Map["gpg_url"].Source.Path)
	}
	if packageBlock.Map["gpg_sha256"].Source.Path != "host.docker1.docker.package.gpg_sha256" {
		t.Fatalf("package gpg_sha256 source path = %q", packageBlock.Map["gpg_sha256"].Source.Path)
	}
	daemon := docker.Map["daemon"]
	if daemon.Map["settings"].Source.Path != "host.docker1.docker.daemon.settings" {
		t.Fatalf("daemon settings source path = %q", daemon.Map["settings"].Source.Path)
	}
	compose := docker.Map["compose"].Map["app"]
	if compose.Source.Path != `host.docker1.docker.compose["app"]` {
		t.Fatalf("compose source path = %q", compose.Source.Path)
	}
	fileBlock := compose.Map["file"]
	if fileBlock.Map["content"].Source.Path != `host.docker1.docker.compose["app"].file.content` {
		t.Fatalf("compose file content source path = %q", fileBlock.Map["content"].Source.Path)
	}
	envFile := compose.Map["env_file"].Map["app"]
	if envFile.Map["path"].Source.Path != `host.docker1.docker.compose["app"].env_file["app"].path` {
		t.Fatalf("env file path source path = %q", envFile.Map["path"].Source.Path)
	}
}

func TestParseDockerMinimalAndMultipleEnvFiles(t *testing.T) {
	file := writeConfig(t, `
host "docker1" {
  docker {
    enable = true

    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      env_file "app" {
        path    = "/opt/app/.env"
        content = "APP_ENV=prod\n"
      }

      env_file "secret" {
        path    = "/opt/app/.env.secret"
        content = "TOKEN=example\n"
      }
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	docker := cfg.Hosts["docker1"].Body.Map["docker"]
	if docker.Map["enable"].Kind != KindBool || !docker.Map["enable"].Bool {
		t.Fatalf("docker enable = %#v, want true", docker.Map["enable"])
	}
	envFiles := docker.Map["compose"].Map["app"].Map["env_file"].Map
	if len(envFiles) != 2 {
		t.Fatalf("env files = %d, want 2", len(envFiles))
	}
}

func TestParseRejectsDuplicateDockerCompose(t *testing.T) {
	file := writeConfig(t, `
host "docker1" {
  docker {
    compose "app" {
      directory = "/opt/app"
    }

    compose "app" {
      directory = "/opt/app2"
    }
  }
}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), `duplicate host.docker1.docker.compose["app"]`) {
		t.Fatalf("ParseFiles() error = %v, want duplicate compose label", err)
	}
}

func TestParseRejectsDuplicateDockerComposeEnvFile(t *testing.T) {
	file := writeConfig(t, `
host "docker1" {
  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      env_file "app" {
        path    = "/opt/app/.env"
        content = "TOKEN=example\n"
      }

      env_file "app" {
        path    = "/opt/app/other.env"
        content = "TOKEN=example\n"
      }
    }
  }
}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), `duplicate host.docker1.docker.compose["app"].env_file["app"]`) {
		t.Fatalf("ParseFiles() error = %v, want duplicate compose env_file label", err)
	}
}

func TestParseRejectsMultipleDockerComposeFilesWithExplicitMessage(t *testing.T) {
	file := writeConfig(t, `
host "docker1" {
  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      file {
        path    = "/opt/app/compose.override.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "multiple compose file blocks are not supported yet") {
		t.Fatalf("ParseFiles() error = %v, want explicit multi-file rejection", err)
	}

	file = writeConfig(t, `
host "docker1" {
  docker {
    compose "app" {
      directory = "/opt/app"

      file "base" {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`)

	_, err = ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "multiple compose file blocks are not supported yet") {
		t.Fatalf("ParseFiles() error = %v, want explicit labeled file rejection", err)
	}
}

func TestParseRunnableExamplesGolden(t *testing.T) {
	summaries := []parsedExampleSummary{}
	for _, fixture := range runnableExampleFixtures() {
		cfg, err := ParseFiles([]string{fixture})
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Hosts) == 0 {
			t.Fatalf("%s hosts = 0, want at least 1", fixture)
		}
		summaries = append(summaries, summarizeParsedExample(fixture, cfg))
	}

	data, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "../testdata/parser/runnable-examples.golden.json", string(data)+"\n")
}

func runnableExampleFixtures() []string {
	return []string{
		"../../../examples/bbr.dbf.hcl",
		"../../../examples/apt-source-file.dbf.hcl",
		"../../../examples/apt-repository.dbf.hcl",
		"../../../examples/bird2.dbf.hcl",
		"../../../examples/component-binary.dbf.hcl",
		"../../../examples/component-inputs.dbf.hcl",
		"../../../examples/component-source-build.dbf.hcl",
		"../../../examples/shared-networkd-reload.dbf.hcl",
		"../../../examples/debian12-amd64.dbf.hcl",
		"../../../examples/docker-compose.dbf.hcl",
		"../../../examples/docker-daemon.dbf.hcl",
		"../../../examples/docker-minimal.dbf.hcl",
		"../../../examples/docker-official-mirror.dbf.hcl",
		"../../../examples/docker-users.dbf.hcl",
		"../../../examples/files-plan-preview.dbf.hcl",
		"../../../examples/fleet.dbf.hcl",
		"../../../examples/mihomo.dbf.hcl",
		"../../../examples/nftables.dbf.hcl",
		"../../../examples/plan-preview.dbf.hcl",
		"../../../examples/profile-merge.dbf.hcl",
		"../../../examples/realistic-systemd-app.dbf.hcl",
		"../../../examples/shadowsocks-rust.dbf.hcl",
		"../../../examples/systemd-service.dbf.hcl",
		"../../../examples/systemd-service-unit.dbf.hcl",
		"../../../examples/user-group.dbf.hcl",
		"../../../examples/variable-secret-file.dbf.hcl",
	}
}

type parsedExampleSummary struct {
	Fixture    string               `json:"fixture"`
	Locals     []string             `json:"locals,omitempty"`
	Variables  []string             `json:"variables,omitempty"`
	Profiles   []parsedBlockSummary `json:"profiles,omitempty"`
	Components []string             `json:"components,omitempty"`
	Hosts      []parsedBlockSummary `json:"hosts"`
}

type parsedBlockSummary struct {
	Name       string   `json:"name"`
	Imports    []string `json:"imports,omitempty"`
	Components []string `json:"components,omitempty"`
	BodyKeys   []string `json:"body_keys,omitempty"`
}

func summarizeParsedExample(fixture string, cfg *Config) parsedExampleSummary {
	summary := parsedExampleSummary{
		Fixture:    filepath.ToSlash(fixture),
		Locals:     sortedKeys(cfg.Locals),
		Variables:  sortedKeys(cfg.Variables),
		Components: sortedKeys(cfg.Components),
	}

	for _, name := range sortedKeys(cfg.Profiles) {
		profile := cfg.Profiles[name]
		summary.Profiles = append(summary.Profiles, parsedBlockSummary{
			Name:     profile.Name,
			Imports:  append([]string(nil), profile.Imports...),
			BodyKeys: sortedKeys(profile.Body.Map),
		})
	}

	for _, name := range sortedKeys(cfg.Hosts) {
		host := cfg.Hosts[name]
		summary.Hosts = append(summary.Hosts, parsedBlockSummary{
			Name:       host.Name,
			Imports:    append([]string(nil), host.Imports...),
			Components: componentInstanceSummaries(host.Components),
			BodyKeys:   sortedKeys(host.Body.Map),
		})
	}

	return summary
}

func componentInstanceSummaries(instances []ComponentInstance) []string {
	out := make([]string, 0, len(instances))
	for _, instance := range instances {
		out = append(out, instance.Name+"="+instance.Template)
	}
	return out
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}
