package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
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
	if err == nil || !strings.Contains(err.Error(), `unknown v2 top-level block "banana"`) {
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

func TestParseRunnableV2ExamplesGolden(t *testing.T) {
	summaries := []parsedExampleSummary{}
	for _, fixture := range runnableV2ExampleFixtures() {
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
	assertGolden(t, "../testdata/parser/runnable-v2-examples.golden.json", string(data)+"\n")
}

func runnableV2ExampleFixtures() []string {
	return []string{
		"../../../examples/v2-bbr.dbf.hcl",
		"../../../examples/v2-apt-source-file.dbf.hcl",
		"../../../examples/v2-apt-repository.dbf.hcl",
		"../../../examples/v2-bird2.dbf.hcl",
		"../../../examples/v2-component-binary.dbf.hcl",
		"../../../examples/v2-component-inputs.dbf.hcl",
		"../../../examples/v2-component-source-build.dbf.hcl",
		"../../../examples/v2-files-plan-preview.dbf.hcl",
		"../../../examples/v2-nftables.dbf.hcl",
		"../../../examples/v2-plan-preview.dbf.hcl",
		"../../../examples/v2-profile-merge.dbf.hcl",
		"../../../examples/v2-shadowsocks-rust.dbf.hcl",
		"../../../examples/v2-systemd-service.dbf.hcl",
		"../../../examples/v2-systemd-service-unit.dbf.hcl",
		"../../../examples/v2-user-group.dbf.hcl",
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
