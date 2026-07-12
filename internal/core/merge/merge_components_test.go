package merge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
)

func TestCompileComponentTargetAndInput(t *testing.T) {
	program := compileInline(t, `
component "tools" {
  input "repo_uri" {
    type = string
  }

  apt {
    repository "tools_repo" {
      uris       = [input.repo_uri]
      suites     = [target.platform.codename]
      components = ["main"]
    }
  }

  packages {
    package "example-tool" {
      repositories = ["tools_repo"]
    }
  }
}

host "server1" {
  component "tools" {
    source = component.tools

    inputs = {
      repo_uri = "https://repo.example/debian"
    }
  }

  platform {
    codename = "trixie"
  }
}
`)

	host := program.Hosts[0]
	if len(host.Components) != 1 {
		t.Fatalf("components = %d, want 1", len(host.Components))
	}
	component := host.Components[0]
	repository := component.APT.Repositories["tools_repo"]
	if !reflect.DeepEqual(repository.URIs, []string{"https://repo.example/debian"}) {
		t.Fatalf("repository uris = %#v", repository.URIs)
	}
	if !reflect.DeepEqual(repository.Suites, []string{"trixie"}) {
		t.Fatalf("repository suites = %#v", repository.Suites)
	}
	if got := component.Packages.Install[0].Repositories; !reflect.DeepEqual(got, []string{"tools_repo"}) {
		t.Fatalf("package repositories = %#v", got)
	}
}

func TestCompileComponentCanMountOnMultipleHosts(t *testing.T) {
	program := compileInline(t, `
component "tools" {
  apt {
    repository "tools_repo" {
      uris       = ["https://repo.example/debian"]
      suites     = [target.platform.codename]
      components = ["main"]
    }
  }
}

host "bookworm1" {
  components = [component.tools]

  platform {
    codename = "bookworm"
  }
}

host "trixie1" {
  components = [component.tools]

  platform {
    codename = "trixie"
  }
}
`)

	if len(program.Hosts) != 2 {
		t.Fatalf("hosts = %d, want 2", len(program.Hosts))
	}
	got := map[string]string{}
	for _, host := range program.Hosts {
		got[host.Name] = host.Components[0].APT.Repositories["tools_repo"].Suites[0]
	}
	want := map[string]string{"bookworm1": "bookworm", "trixie1": "trixie"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suites = %#v, want %#v", got, want)
	}
}

func TestCompileComponentBinaryArtifactSelectsArchitecture(t *testing.T) {
	program := compileInline(t, `
component "rclone" {
  type    = "binary"
  version = "1.66.0"

  source "amd64" {
    url    = "https://downloads.example/rclone-amd64.zip"
    sha256 = "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
  }

  source "arm64" {
    url    = "https://downloads.example/rclone-arm64.zip"
    sha256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
  }

  extract {
    strip_components = 1
    include          = "rclone"
  }

  install {
    path = "/usr/local/bin/rclone"
  }
}

host "tool1" {
  components = [component.rclone]

  platform {
    architecture = "arm64"
  }
}
`)

	component := program.Hosts[0].Components[0]
	if component.ArtifactType != "binary" || component.Version != "1.66.0" {
		t.Fatalf("artifact = %#v", component)
	}
	if component.SelectedSource == nil || component.SelectedSource.Architecture != "arm64" {
		t.Fatalf("selected source = %#v", component.SelectedSource)
	}
	if component.SelectedSource.URL != "https://downloads.example/rclone-arm64.zip" {
		t.Fatalf("selected url = %q", component.SelectedSource.URL)
	}
	if component.Extract == nil || component.Extract.Format != "zip" || component.Extract.StripComponents != 1 {
		t.Fatalf("extract = %#v", component.Extract)
	}
	if component.Install == nil || component.Install.Owner != "root" || component.Install.Group != "root" || component.Install.Mode != "0755" {
		t.Fatalf("install defaults = %#v", component.Install)
	}
}

func TestCompileComponentBinaryArtifactInfersTarXZ(t *testing.T) {
	program := compileInline(t, `
component "tool" {
  type = "binary"

  source "amd64" {
    url    = "https://downloads.example/tool-v1.0.0-x86_64.tar.xz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  extract {
    include = "tool"
  }

  install {
    path = "/usr/local/bin/tool"
  }
}

host "server1" {
  components = [component.tool]

  platform {
    architecture = "amd64"
  }
}
`)

	component := program.Hosts[0].Components[0]
	if component.Extract == nil || component.Extract.Format != "tar.xz" {
		t.Fatalf("extract = %#v, want tar.xz", component.Extract)
	}
}

func TestCompileComponentBinaryArtifactInfersGzip(t *testing.T) {
	program := compileInline(t, `
component "tool" {
  type = "binary"

  source "amd64" {
    url    = "https://downloads.example/tool-v1.0.0-linux-amd64.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  extract {}

  install {
    path = "/usr/local/bin/tool"
  }
}

host "server1" {
  components = [component.tool]

  platform {
    architecture = "amd64"
  }
}
`)

	component := program.Hosts[0].Components[0]
	if component.Extract == nil || component.Extract.Format != "gz" {
		t.Fatalf("extract = %#v, want gz", component.Extract)
	}
}

func TestCompileComponentSourceArtifactBuild(t *testing.T) {
	program := compileInline(t, `
component "hello" {
  type    = "source"
  version = "1.0.0"

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

host "tool1" {
  components = [component.hello]

  platform {
    architecture = "amd64"
  }
}
`)

	component := program.Hosts[0].Components[0]
	if component.ArtifactType != "source" || component.Version != "1.0.0" {
		t.Fatalf("artifact = %#v", component)
	}
	if component.Build == nil {
		t.Fatalf("build was not compiled")
	}
	if got := component.Build.Commands[0]; !reflect.DeepEqual(got, []string{"cc", "-O2", "-o", "hello", "hello.c"}) {
		t.Fatalf("build command = %#v", got)
	}
	if !reflect.DeepEqual(component.Build.Packages, []string{"gcc"}) {
		t.Fatalf("build packages = %#v", component.Build.Packages)
	}
	if component.Build.Output != "hello" || component.Build.SourceName != "hello.c" {
		t.Fatalf("build attrs = %#v", component.Build)
	}
	if component.Install == nil || component.Install.Mode != "0755" {
		t.Fatalf("install defaults = %#v", component.Install)
	}
}

func TestCompileComponentTemplateSpecAndArtifactTypes(t *testing.T) {
	program := compileInline(t, `
component "artifact_file" {
  type    = "file"
  version = "1.0.0"

  input "path" {
    type      = string
    default   = "/etc/example.conf"
    sensitive = true
  }

  source {
    url    = "https://downloads.example/example.conf"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/etc/example.conf"
  }
}

component "artifact_archive" {
  type = "archive"

  source "amd64" {
    url    = "https://downloads.example/app.tar.gz"
    sha256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
  }

  extract {
    strip_components = 1
  }

  install {
    path = "/opt/app"
  }
}

component "company_ca" {
  type = "ca_certificate"

  source {
    url    = "https://downloads.example/ca.crt"
    sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
  }

  install {
    path = "/usr/local/share/ca-certificates/company-ca.crt"
  }
}

host "server1" {
  components = [component.artifact_file, component.artifact_archive, component.company_ca]

  platform {
    architecture = "amd64"
  }
}
`)

	template := program.Components["artifact_file"]
	if template.Name != "artifact_file" || template.ArtifactType != "file" || template.Version != "1.0.0" {
		t.Fatalf("template = %#v", template)
	}
	if template.Inputs["path"].Default != "/etc/example.conf" || !template.Inputs["path"].Sensitive {
		t.Fatalf("template input = %#v", template.Inputs["path"])
	}
	host := program.Hosts[0]
	if host.Components[0].Install.Mode != "0644" {
		t.Fatalf("file artifact mode = %q, want 0644", host.Components[0].Install.Mode)
	}
	if host.Components[1].Extract == nil || host.Components[1].Extract.Format != "tar.gz" {
		t.Fatalf("archive extract = %#v", host.Components[1].Extract)
	}
	if host.Components[2].Install.Mode != "0644" {
		t.Fatalf("ca certificate mode = %q, want 0644", host.Components[2].Install.Mode)
	}
}

func TestCompileRejectsInvalidComponentArtifacts(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "invalid sha",
			hcl: `
component "rclone" {
  type = "binary"

  source {
    url    = "https://downloads.example/rclone"
    sha256 = "not-a-sha"
  }

  install {
    path = "/usr/local/bin/rclone"
  }
}

host "tool1" {
  components = [component.rclone]
}
`,
			want: "sha256 must be a 64 character hex string",
		},
		{
			name: "missing host architecture",
			hcl: `
component "rclone" {
  type = "binary"

  source "amd64" {
    url    = "https://downloads.example/rclone-amd64"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/rclone"
  }
}

host "tool1" {
  components = [component.rclone]
}
`,
			want: "must declare platform.architecture",
		},
		{
			name: "mixed source labels",
			hcl: `
component "rclone" {
  type = "binary"

  source {
    url    = "https://downloads.example/rclone"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  source "amd64" {
    url    = "https://downloads.example/rclone-amd64"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/rclone"
  }
}

host "tool1" {
  components = [component.rclone]

  platform {
    architecture = "amd64"
  }
}
`,
			want: "cannot mix unlabeled and architecture-labeled source blocks",
		},
		{
			name: "relative install path",
			hcl: `
component "rclone" {
  type = "binary"

  source {
    url    = "https://downloads.example/rclone"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "bin/rclone"
  }
}

host "tool1" {
  components = [component.rclone]
}
`,
			want: "install path must be absolute",
		},
		{
			name: "archive missing extract",
			hcl: `
component "app" {
  type = "archive"

  source {
    url    = "https://downloads.example/app.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/opt/app"
  }
}

host "tool1" {
  components = [component.app]
}
`,
			want: "archive component requires an extract block",
		},
		{
			name: "file with extract",
			hcl: `
component "config" {
  type = "file"

  source {
    url    = "https://downloads.example/config.zip"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  extract {
    format = "zip"
  }

  install {
    path = "/etc/config"
  }
}

host "tool1" {
  components = [component.config]
}
`,
			want: "file component does not support extract",
		},
		{
			name: "source missing build",
			hcl: `
component "hello" {
  type = "source"

  source {
    url    = "https://downloads.example/hello.c"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/hello"
  }
}

host "tool1" {
  components = [component.hello]
}
`,
			want: "source component requires a build block",
		},
		{
			name: "binary with build",
			hcl: `
component "hello" {
  type = "binary"

  source {
    url    = "https://downloads.example/hello"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  build {
    commands = [["cc", "-o", "hello", "hello.c"]]
    output   = "hello"
  }

  install {
    path = "/usr/local/bin/hello"
  }
}

host "tool1" {
  components = [component.hello]
}
`,
			want: "binary component does not support build",
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

func TestCompileRejectsInvalidComponentInputsAndConflicts(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "missing input",
			hcl: `
component "tools" {
  input "repo_uri" {
    type = string
  }
}

host "server1" {
  components = [component.tools]
}
`,
			want: `input "repo_uri" is required`,
		},
		{
			name: "unknown input",
			hcl: `
component "tools" {}

host "server1" {
  component "tools" {
    source = component.tools

    inputs = {
      missing = "value"
    }
  }
}
`,
			want: "unknown input",
		},
		{
			name: "identity conflict",
			hcl: `
component "tools" {
  packages {
    install = ["curl"]
  }
}

host "server1" {
  components = [component.tools]

  packages {
    install = ["curl"]
  }
}
`,
			want: `package "curl" conflicts`,
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

func TestCompileNormalizesRichComponentInputs(t *testing.T) {
	program := compileInline(t, `
component "proxy" {
  input "listeners" {
    type = list(object({
      name = string
      port = number
      tls  = optional(bool, false)
      note = optional(string)
      tags = optional(map(string), {})
    }))

    description = "Listener definitions."
    default     = []
    nullable    = false
  }

  files {
    file "/etc/proxy/listeners.json" {
      content = jsonencode(input.listeners)
    }
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy

    inputs = {
      listeners = [
        {
          name = "http"
          port = 80
        },
      ]
    }
  }
}
`)

	template := program.Components["proxy"].Inputs["listeners"]
	if template.Description != "Listener definitions." || !reflect.DeepEqual(template.Default, []any{}) || template.Nullable {
		t.Fatalf("template input = %#v", template)
	}
	component := program.Hosts[0].Components[0]
	listeners, ok := component.InputValues["listeners"].([]any)
	if !ok || len(listeners) != 1 {
		t.Fatalf("input listeners = %#v", component.InputValues["listeners"])
	}
	listener := listeners[0].(map[string]any)
	if listener["tls"] != false || listener["note"] != nil || !reflect.DeepEqual(listener["tags"], map[string]any{}) {
		t.Fatalf("normalized listener = %#v", listener)
	}
	file := component.Files.Files["/etc/proxy/listeners.json"]
	if !strings.Contains(file.Content, `"tls":false`) || !strings.Contains(file.Content, `"note":null`) || !strings.Contains(file.Content, `"tags":{}`) {
		t.Fatalf("file content = %s", file.Content)
	}
}

func TestCompileComponentInputValidations(t *testing.T) {
	program := compileInline(t, `
component "proxy" {
  input "listeners" {
    type     = list(object({ name = string, port = number }))
    nullable = false

    validation {
      condition     = alltrue([for listener in input.listeners : listener.port >= 1 && listener.port <= 65535])
      error_message = "Each listener.port must be between 1 and 65535."
    }

    validation {
      condition     = !contains([for listener in input.listeners : listener.name], "")
      error_message = "Listener names must be non-empty."
    }
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy

    inputs = {
      listeners = [
        { name = "http", port = 80 },
        { name = "https", port = 443 },
      ]
    }
  }
}
`)
	if got := program.Hosts[0].Components[0].InputValues["listeners"]; got == nil {
		t.Fatalf("listeners input missing")
	}
}

func TestCompileRejectsInvalidComponentInputValidations(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "validation failure",
			hcl: `
component "proxy" {
  input "listeners" {
    type = list(object({ port = number }))
    validation {
      condition     = alltrue([for listener in input.listeners : listener.port >= 1 && listener.port <= 65535])
      error_message = "Each listener.port must be between 1 and 65535."
    }
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = [{ port = 70000 }] }
  }
}
`,
			want: `validation failed for input "listeners": Each listener.port must be between 1 and 65535.`,
		},
		{
			name: "condition non bool",
			hcl: `
component "proxy" {
  input "listeners" {
    type = list(object({ port = number }))
    validation {
      condition     = length(input.listeners)
      error_message = "bad"
    }
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = [{ port = 80 }] }
  }
}
`,
			want: `input validation condition must evaluate to a boolean`,
		},
		{
			name: "other input",
			hcl: `
component "proxy" {
  input "other" {
    type    = string
    default = "x"
  }
  input "listeners" {
    type = list(object({ port = number }))
    validation {
      condition     = input.other == "x"
      error_message = "bad"
    }
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = [{ port = 80 }] }
  }
}
`,
			want: `input validation can only read input.listeners`,
		},
		{
			name: "target reference",
			hcl: `
component "proxy" {
  input "listeners" {
    type = list(object({ port = number }))
    validation {
      condition     = target.platform.codename == "trixie"
      error_message = "bad"
    }
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = [{ port = 80 }] }
  }
}
`,
			want: `input validation can only read input.listeners`,
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

func TestCompileComponentScriptOnChange(t *testing.T) {
	program := compileInline(t, `
component "app" {
  input "service_name" {
    type = string
  }

  script "reload" {
    outputs = ["/etc/app.rendered"]
    run     = "systemctl reload ${input.service_name}.service"
  }

  script "reindex" {
    mode        = "each"
    interpreter = ["/bin/bash", "-e"]
    commands    = [["/usr/local/bin/reindex", "/etc/app/index"]]
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
      service_name = "app"
    }
  }
}
`)

	template := program.Components["app"]
	if _, ok := template.Scripts["reload"]; !ok {
		t.Fatalf("template scripts = %#v", template.Scripts)
	}
	component := program.Hosts[0].Components[0]
	reload := component.Scripts["reload"]
	if reload.Mode != "once" || !reflect.DeepEqual(reload.Interpreter, []string{"/bin/sh", "-eu"}) {
		t.Fatalf("reload defaults = %#v", reload)
	}
	if reload.Run != "systemctl reload app.service" {
		t.Fatalf("reload run = %q", reload.Run)
	}
	if !reflect.DeepEqual(reload.Outputs, []string{"/etc/app.rendered"}) {
		t.Fatalf("reload outputs = %#v", reload.Outputs)
	}
	reindex := component.Scripts["reindex"]
	if reindex.Mode != "each" || !reflect.DeepEqual(reindex.Interpreter, []string{"/bin/bash", "-e"}) {
		t.Fatalf("reindex = %#v", reindex)
	}
	if got := reindex.Commands[0]; !reflect.DeepEqual(got, []string{"/usr/local/bin/reindex", "/etc/app/index"}) {
		t.Fatalf("commands = %#v", reindex.Commands)
	}
	file := component.Files.Files["/etc/app.conf"]
	if file.OnChange == nil || file.OnChange.Name != "reload" || file.OnChange.Scope != "component" {
		t.Fatalf("file on_change = %#v", file)
	}
	if file.OnChange.DeclarationID != `component.app.script["reload"]` {
		t.Fatalf("on_change declaration identity = %q", file.OnChange.DeclarationID)
	}
	if file.OnChange.Source.Path != `component.app.files.file["/etc/app.conf"].on_change` {
		t.Fatalf("on_change source = %#v", file.OnChange.Source)
	}
}

func TestCompileRootScriptReferencesResolveByDeclaration(t *testing.T) {
	program := compileInline(t, `
script "reload" {
  mode = "once"
  run  = "echo ${target.platform.codename}"
}

component "wan" {
  files {
    file "/etc/wan" {
      content   = "wan"
      on_change = script.reload
    }
  }
}

component "policy" {
  script "reload" {
    run = "local"
  }

  files {
    file "/etc/policy" {
      content   = "policy"
      on_change = global.script.reload
    }
    file "/etc/local" {
      content   = "local"
      on_change = script.reload
    }
  }
}

host "router" {
  components = [component.wan, component.policy]
  platform { codename = "trixie" }
}
`)
	host := program.Hosts[0]
	if script := host.Scripts["reload"]; script.Run != "echo trixie" || script.DeclarationID != `script["reload"]` {
		t.Fatalf("host script = %#v", script)
	}
	wan := host.Components[0].Files.Files["/etc/wan"].OnChange
	policy := host.Components[1].Files.Files["/etc/policy"].OnChange
	local := host.Components[1].Files.Files["/etc/local"].OnChange
	if wan == nil || policy == nil || wan.DeclarationID != policy.DeclarationID || wan.Scope != "root" {
		t.Fatalf("root references = %#v %#v", wan, policy)
	}
	if local == nil || local.Scope != "component" || local.DeclarationID == wan.DeclarationID {
		t.Fatalf("local collision reference = %#v", local)
	}
}

func TestCompileRejectsInvalidRootScriptsAndReferences(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "each mode",
			hcl: `script "reload" {
  mode = "each"
  run  = "true"
}
host "server1" {}`,
			want: "root script mode must be once",
		},
		{
			name: "explicit root does not fall back to local",
			hcl: `component "app" {
  script "reload" { run = "true" }
  files {
    file "/etc/app" {
      content   = "app"
      on_change = global.script.reload
    }
  }
}
host "server1" { components = [component.app] }`,
			want: "references unknown global.script.reload",
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

func TestCompileRejectsInvalidComponentScripts(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "unknown on_change script",
			hcl: `
component "app" {
  script "reload" {
    run = "systemctl reload app.service"
  }

  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.restart
    }
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `files.file.on_change references unknown script.restart`,
		},
		{
			name: "host on_change",
			hcl: `
host "app1" {
  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }
  }
}
`,
			want: `files.file.on_change is only supported inside component`,
		},
		{
			name: "profile on_change",
			hcl: `
profile "base" {
  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }
  }
}

host "app1" {
  imports = [profile.base]
}
`,
			want: `files.file.on_change is only supported inside component`,
		},
		{
			name: "script in host",
			hcl: `
host "app1" {
  script "reload" {
    run = "systemctl reload app.service"
  }
}
`,
			want: `unsupported block host.app1.script`,
		},
		{
			name: "script in profile",
			hcl: `
profile "base" {
  script "reload" {
    run = "systemctl reload app.service"
  }
}

host "app1" {
  imports = [profile.base]
}
`,
			want: `unsupported block profile.base.script`,
		},
		{
			name: "missing body",
			hcl: `
component "app" {
  script "reload" {
    mode = "once"
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `requires exactly one of run, content, or commands`,
		},
		{
			name: "two bodies",
			hcl: `
component "app" {
  script "reload" {
    run     = "systemctl reload app.service"
    content = "systemctl reload app.service"
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `requires exactly one of run, content, or commands`,
		},
		{
			name: "invalid mode",
			hcl: `
component "app" {
  script "reload" {
    mode = "always"
    run  = "systemctl reload app.service"
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `script mode must be once or each`,
		},
		{
			name: "empty interpreter",
			hcl: `
component "app" {
  script "reload" {
    interpreter = []
    run         = "systemctl reload app.service"
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `script interpreter must be a non-empty string list`,
		},
		{
			name: "relative output",
			hcl: `
component "app" {
  script "render" {
    outputs = ["tmp/app.conf"]
    run     = "cp input output"
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `script output path must be absolute and non-empty`,
		},
		{
			name: "empty command list",
			hcl: `
component "app" {
  script "reload" {
    commands = []
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `script commands must contain at least one command`,
		},
		{
			name: "empty command",
			hcl: `
component "app" {
  script "reload" {
    commands = [[]]
  }
}

host "app1" {
  components = [component.app]
}
`,
			want: `script command must contain at least one argument`,
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

func TestCompileDeprecatedComponentInputWarnings(t *testing.T) {
	cfg := parseInline(t, `
component "app" {
  input "listen_addr" {
    type       = string
    default    = "127.0.0.1:8080"
    deprecated = "Use listeners instead."
  }
}

host "default1" {
  components = [component.app]
}

host "explicit1" {
  component "app" {
    source = component.app
    inputs = {
      listen_addr = "0.0.0.0:8080"
    }
  }
}
`)
	warnings := []ir.Warning{}
	if _, err := CompileWithOptions(cfg, CompileOptions{Warnings: &warnings}); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want 1", warnings)
	}
	if !strings.Contains(warnings[0].Message, `input "listen_addr" is deprecated`) {
		t.Fatalf("warning = %#v", warnings[0])
	}
	if !strings.Contains(warnings[0].Source.Path, `inputs["listen_addr"]`) {
		t.Fatalf("warning source = %#v", warnings[0].Source)
	}
}

func TestCompileAllowsRepeatedComponentSharedDirectory(t *testing.T) {
	program, err := parseOrCompileInlineWithFiles(t, `
component "wireguard_networkd" {
  input "private_key_source" {
    type = string
  }

  input "interface" {
    type = object({
      name    = string
      address = string
    })
  }

  directories {
    directory "/etc/wireguard" {
      owner = "root"
      group = "systemd-network"
      mode  = "0750"
    }
  }

  secrets {
    file "private_key" {
      path   = "/etc/wireguard/${input.interface.name}.key"
      source = input.private_key_source
      owner  = "root"
      group  = "systemd-network"
      mode   = "0640"
    }
  }

  systemd {
    networkd {
      netdev "wireguard" {
        path = "/etc/systemd/network/10-${input.interface.name}.netdev"

        netdev = {
          Name = input.interface.name
          Kind = "wireguard"
        }

        wireguard = {
          PrivateKeyFile = "/etc/wireguard/${input.interface.name}.key"
        }
      }

      network "wireguard" {
        path = "/etc/systemd/network/20-${input.interface.name}.network"

        match = {
          Name = input.interface.name
        }

        network = {
          Address = [input.interface.address]
        }
      }
    }
  }
}

host "server1" {
  component "wg_prod" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "wg-prod.key"
      interface = {
        name    = "wg-prod"
        address = "10.80.0.1/30"
      }
    }
  }

  component "wg_backup" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "wg-backup.key"
      interface = {
        name    = "wg-backup"
        address = "10.81.0.1/30"
      }
    }
  }
}
`, map[string]string{
		"wg-prod.key":   "prod-key",
		"wg-backup.key": "backup-key",
	})
	if err != nil {
		t.Fatalf("compile repeated component shared directory: %v", err)
	}
	host := program.Hosts[0]
	if len(host.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(host.Components))
	}
	for _, component := range host.Components {
		if _, ok := component.Directories.Directories["/etc/wireguard"]; !ok {
			t.Fatalf("component %s missing shared wireguard directory", component.Name)
		}
	}
}

func TestCompileRejectsRepeatedComponentDirectoryWithDifferentAttributes(t *testing.T) {
	_, err := parseOrCompileInline(t, `
component "app" {
  input "mode" {
    type = string
  }

  directories {
    directory "/opt/app" {
      mode = input.mode
    }
  }
}

host "server1" {
  component "one" {
    source = component.app

    inputs = {
      mode = "0755"
    }
  }

  component "two" {
    source = component.app

    inputs = {
      mode = "0700"
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `component "two" directory "/opt/app" conflicts`) {
		t.Fatalf("error = %v, want conflicting shared directory rejection", err)
	}
}

func TestValidateRuntimeComponentsRejectsRepeatedNetworkdPath(t *testing.T) {
	cfg := parseInline(t, `
component "wireguard_networkd" {
  input "interface" {
    type = object({
      name = string
    })
  }

  systemd {
    networkd {
      netdev "wireguard" {
        path = "/etc/systemd/network/10-${input.interface.name}.netdev"

        netdev = {
          Name = input.interface.name
          Kind = "wireguard"
        }
      }
    }
  }
}

host "server1" {
  component "one" {
    source = component.wireguard_networkd

    inputs = {
      interface = {
        name = "wg-prod"
      }
    }
  }

  component "two" {
    source = component.wireguard_networkd

    inputs = {
      interface = {
        name = "wg-prod"
      }
    }
  }
}
`)
	_, err := CompileWithOptions(cfg, CompileOptions{ValidateRuntimeTemplates: true})
	if err == nil || !strings.Contains(err.Error(), `networkd netdev path "/etc/systemd/network/10-wg-prod.netdev" conflicts`) {
		t.Fatalf("error = %v, want repeated networkd path rejection", err)
	}
}

func TestCompileSensitiveComponentInputPropagatesToFileContent(t *testing.T) {
	program := compileInline(t, `
component "app" {
  input "environment" {
    type      = map(string)
    sensitive = true
    default   = {}
  }

  files {
    file "/etc/app/env.json" {
      content = jsonencode(input.environment)
    }
  }
}

host "server1" {
  component "app" {
    source = component.app
    inputs = {
      environment = {
        API_TOKEN = "super-secret-token"
      }
    }
  }
}
`)
	file := program.Hosts[0].Components[0].Files.Files["/etc/app/env.json"]
	if !file.Sensitive {
		t.Fatalf("file sensitive = false")
	}
	if !strings.Contains(file.Content, "super-secret-token") {
		t.Fatalf("compiled in-memory content missing secret: %q", file.Content)
	}
	data, err := json.Marshal(program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret-token") {
		t.Fatalf("HostSpec JSON leaked secret: %s", data)
	}
}

func TestCompileRejectsInvalidRichComponentInputs(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "nullable false",
			hcl: `
component "proxy" {
  input "listeners" {
    type     = list(object({ name = string }))
    nullable = false
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = null }
  }
}
`,
			want: `component input "listeners" must not be null`,
		},
		{
			name: "invalid input default",
			hcl: `
component "proxy" {
  input "listeners" {
    type    = list(object({ name = string }))
    default = [{ name = 123 }]
  }
}

host "edge1" {}
`,
			want: `component input "listeners"[0].name must be string`,
		},
		{
			name: "invalid optional default",
			hcl: `
component "proxy" {
  input "listeners" {
    type = list(object({ name = string, tls = optional(bool, "no") }))
  }
}

host "edge1" {}
`,
			want: `component input "listeners".tls must be bool`,
		},
		{
			name: "missing object attribute",
			hcl: `
component "proxy" {
  input "listeners" {
    type = list(object({ name = string, port = number }))
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = [{ name = "http" }] }
  }
}
`,
			want: `missing required attribute [0].port`,
		},
		{
			name: "extra object attribute",
			hcl: `
component "proxy" {
  input "listeners" {
    type = list(object({ name = string }))
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = [{ name = "http", typo = true }] }
  }
}
`,
			want: `unsupported attribute [0].typo`,
		},
		{
			name: "nested type mismatch",
			hcl: `
component "proxy" {
  input "listeners" {
    type = list(object({ name = string, port = number }))
  }
}

host "edge1" {
  component "proxy" {
    source = component.proxy
    inputs = { listeners = [{ name = "http", port = "eighty" }] }
  }
}
`,
			want: `component input "listeners"[0].port must be number`,
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

func TestCompileComponentBinaryHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/component-binary.dbf.hcl", "../testdata/hostspec/component-binary.golden.json")
}

func TestCompileComponentSourceBuildHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/component-source-build.dbf.hcl", "../testdata/hostspec/component-source-build.golden.json")
}

func TestCompileComponentInputsHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/component-inputs.dbf.hcl", "../testdata/hostspec/component-inputs.golden.json")
}

func TestCompileComponentScriptOnChangeHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../testdata/fixtures/component-script-on-change.dbf.hcl", "../testdata/hostspec/component-script-on-change.golden.json")
}

func TestCompileSharedNetworkdReloadHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/shared-networkd-reload.dbf.hcl", "../testdata/hostspec/shared-networkd-reload.golden.json")
}

func TestCompileWireGuardComponentUsesStructuredInputsForRouteTable(t *testing.T) {
	program := compileInline(t, `
component "wireguard_networkd" {
  input "private_key_source" {
    type      = string
    sensitive = true
  }

  input "interface" {
    type = object({
      name        = optional(string, "wg0")
      address     = string
      listen_port = optional(number, 51820)
      route_table = optional(string, "off")
    })

    nullable = false
  }

  input "peers" {
    type = map(object({
      public_key           = string
      allowed_ips          = list(string)
      endpoint             = optional(string)
      persistent_keepalive = optional(number, 25)
    }))

    default  = {}
    nullable = false

    validation {
      condition     = alltrue([for peer in values(input.peers) : length(peer.allowed_ips) > 0])
      error_message = "each peer.allowed_ips must contain at least one CIDR."
    }
  }

  systemd {
    networkd {
      netdev "wireguard" {
        path = "/etc/systemd/network/10-${input.interface.name}.netdev"

        netdev = {
          Name = input.interface.name
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = input.interface.listen_port
          PrivateKeyFile = "/etc/wireguard/${input.interface.name}.key"
          RouteTable     = input.interface.route_table
        }

        wireguard_peer = {
          for name, peer in input.peers : name => {
            PublicKey           = peer.public_key
            AllowedIPs          = peer.allowed_ips
            Endpoint            = peer.endpoint
            PersistentKeepalive = peer.persistent_keepalive
          }
        }
      }

      network "wireguard" {
        path = "/etc/systemd/network/20-${input.interface.name}.network"

        match = {
          Name = input.interface.name
        }

        network = {
          Address = [input.interface.address]
        }
      }
    }
  }
}

host "server1" {
  component "wireguard" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/server1.key"
      interface = {
        name    = "wg-prod"
        address = "10.80.0.1/24"
      }
      peers = {
        server2 = {
          public_key  = "peer-public-key"
          allowed_ips = ["10.80.0.2/32"]
          endpoint    = "server2.example.net:51820"
        }
        laptop = {
          public_key  = "laptop-public-key"
          allowed_ips = ["10.80.0.10/32"]
        }
      }
    }
  }
}
`)

	component := program.Hosts[0].Components[0]
	inputs := component.InputValues
	if got := inputs["interface"].(map[string]any)["route_table"]; got != "off" {
		t.Fatalf("interface.route_table input = %#v, want off", got)
	}
	netdev := component.Systemd.Networkd.NetDevs["wireguard"]
	if netdev.Path != "/etc/systemd/network/10-wg-prod.netdev" {
		t.Fatalf("netdev path = %q", netdev.Path)
	}
	if got := firstSectionValue(netdev.WireGuard, "RouteTable"); got != "off" {
		t.Fatalf("RouteTable section value = %q, want off", got)
	}
	if got := firstSectionValue(netdev.WireGuard, "PrivateKeyFile"); got != "/etc/wireguard/wg-prod.key" {
		t.Fatalf("PrivateKeyFile section value = %q", got)
	}
	if !strings.Contains(netdev.Content, "RouteTable=off\n") {
		t.Fatalf("netdev content does not disable networkd route table writes:\n%s", netdev.Content)
	}
	if !strings.Contains(netdev.Content, "PublicKey=laptop-public-key\n") || !strings.Contains(netdev.Content, "PublicKey=peer-public-key\n") {
		t.Fatalf("netdev content does not contain both WireGuard peers:\n%s", netdev.Content)
	}
	network := component.Systemd.Networkd.Networks["wireguard"]
	if network.Path != "/etc/systemd/network/20-wg-prod.network" {
		t.Fatalf("network path = %q", network.Path)
	}
}

func TestCompileWireGuardComponentCanBeMountedTwiceWithDifferentInterfaces(t *testing.T) {
	program, err := parseOrCompileInlineWithFiles(t, `
component "wireguard_networkd" {
  input "private_key_source" {
    type      = string
    sensitive = true
  }

  input "interface" {
    type = object({
      name        = string
      address     = string
      listen_port = optional(number, 51820)
      route_table = optional(string, "off")
    })

    nullable = false
  }

  secrets {
    file "private_key" {
      path   = "/etc/wireguard/${input.interface.name}.key"
      source = input.private_key_source
      owner  = "root"
      group  = "systemd-network"
      mode   = "0640"
    }
  }

  systemd {
    networkd {
      netdev "wireguard" {
        path = "/etc/systemd/network/10-${input.interface.name}.netdev"

        netdev = {
          Name = input.interface.name
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = input.interface.listen_port
          PrivateKeyFile = "/etc/wireguard/${input.interface.name}.key"
          RouteTable     = input.interface.route_table
        }
      }

      network "wireguard" {
        path = "/etc/systemd/network/20-${input.interface.name}.network"

        match = {
          Name = input.interface.name
        }

        network = {
          Address = [input.interface.address]
        }
      }
    }
  }
}

host "edge1" {
  component "wg_prod" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "prod.key"
      interface = {
        name    = "wg-prod"
        address = "10.80.0.10/24"
      }
    }
  }

  component "wg_backup" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "backup.key"
      interface = {
        name        = "wg-backup"
        address     = "10.90.0.10/24"
        listen_port = 51821
      }
    }
  }
}
`, map[string]string{
		"prod.key":   "prod-private-key",
		"backup.key": "backup-private-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	components := map[string]ir.ComponentInstanceSpec{}
	for _, component := range program.Hosts[0].Components {
		components[component.Name] = component
	}
	prod := components["wg_prod"]
	if _, ok := prod.Secrets.Files["/etc/wireguard/wg-prod.key"]; !ok {
		t.Fatalf("wg_prod secrets = %#v", prod.Secrets.Files)
	}
	if prod.Systemd.Networkd.NetDevs["wireguard"].Path != "/etc/systemd/network/10-wg-prod.netdev" {
		t.Fatalf("wg_prod netdev path = %q", prod.Systemd.Networkd.NetDevs["wireguard"].Path)
	}
	backup := components["wg_backup"]
	if _, ok := backup.Secrets.Files["/etc/wireguard/wg-backup.key"]; !ok {
		t.Fatalf("wg_backup secrets = %#v", backup.Secrets.Files)
	}
	if backup.Systemd.Networkd.NetDevs["wireguard"].Path != "/etc/systemd/network/10-wg-backup.netdev" {
		t.Fatalf("wg_backup netdev path = %q", backup.Systemd.Networkd.NetDevs["wireguard"].Path)
	}
	if got := firstSectionValue(backup.Systemd.Networkd.NetDevs["wireguard"].WireGuard, "ListenPort"); got != "51821" {
		t.Fatalf("wg_backup ListenPort = %q", got)
	}
}
