package merge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/parser"
	"github.com/mofelee/debianform/internal/v2/testassert"
)

func TestCompileMergesImportsListsMapsAndScalars(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  packages {
    install = ["curl", "vim"]
  }

  kernel {
    sysctl = {
      "net.core.default_qdisc" = "fq"
    }
  }

  system {
    timezone = "UTC"
  }
}

profile "bbr" {
  imports = [profile.base]

  packages {
    install = ["curl", "htop"]
  }

  kernel {
    sysctl = {
      "net.core.default_qdisc"          = "fq_codel"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  system {
    timezone = "Asia/Tokyo"
  }
}

host "server1" {
  imports = [profile.bbr]

  packages {
    install = ["sudo"]
  }
}
`)

	host := program.Hosts[0]
	gotPackages := packageNames(host.Packages.Install)
	wantPackages := []string{"curl", "vim", "htop", "sudo"}
	if !reflect.DeepEqual(gotPackages, wantPackages) {
		t.Fatalf("packages = %#v, want %#v", gotPackages, wantPackages)
	}
	if got := host.Kernel.Sysctl["net.core.default_qdisc"].Value; got != "fq_codel" {
		t.Fatalf("default_qdisc = %q, want fq_codel", got)
	}
	if got := host.Kernel.Sysctl["net.ipv4.tcp_congestion_control"].Value; got != "bbr" {
		t.Fatalf("tcp_congestion_control = %q, want bbr", got)
	}
	if host.System.Timezone != "Asia/Tokyo" {
		t.Fatalf("timezone = %q, want Asia/Tokyo", host.System.Timezone)
	}
}

func TestCompileMergeModifiers(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  packages {
    install = ["curl", "vim"]
  }

  kernel {
    sysctl = {
      keep   = "yes"
      remove = "old"
    }
  }
}

profile "ordered" {
  imports = [profile.base]

  packages {
    install = before(["ca-certificates"])
  }
}

profile "forced" {
  packages {
    install = force(["nftables"])
  }
}

host "server1" {
  imports = [profile.ordered, profile.forced]

  packages {
    install = after(["sudo"])
  }

  kernel {
    sysctl = {
      remove = unset()
    }
  }
}
`)

	host := program.Hosts[0]
	gotPackages := packageNames(host.Packages.Install)
	wantPackages := []string{"nftables", "sudo"}
	if !reflect.DeepEqual(gotPackages, wantPackages) {
		t.Fatalf("packages = %#v, want %#v", gotPackages, wantPackages)
	}
	if _, exists := host.Kernel.Sysctl["remove"]; exists {
		t.Fatalf("sysctl remove should have been unset")
	}
	if got := host.Kernel.Sysctl["keep"].Value; got != "yes" {
		t.Fatalf("sysctl keep = %q, want yes", got)
	}
}

func TestCompileBeforeAfterWithoutForce(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  packages {
    install = ["vim"]
  }
}

profile "ordered" {
  imports = [profile.base]

  packages {
    install = before(["curl"])
  }
}

host "server1" {
  imports = [profile.ordered]

  packages {
    install = after(["sudo"])
  }
}
`)

	gotPackages := packageNames(program.Hosts[0].Packages.Install)
	wantPackages := []string{"curl", "vim", "sudo"}
	if !reflect.DeepEqual(gotPackages, wantPackages) {
		t.Fatalf("packages = %#v, want %#v", gotPackages, wantPackages)
	}
}

func TestCompileMergesLabeledPackageBlocks(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  apt {
    repository "base_repo" {
      uris       = ["https://repo.example/base"]
      suites     = ["trixie"]
      components = ["main"]
    }
  }

  packages {
    package "bird2" {
      repositories = ["base_repo"]
    }
  }
}

host "server1" {
  imports = [profile.base]

  apt {
    repository "host_repo" {
      uris       = ["https://repo.example/host"]
      suites     = ["trixie"]
      components = ["main"]
    }
  }

  packages {
    package "bird2" {
      repositories = ["host_repo"]
    }
  }
}
`)

	packages := program.Hosts[0].Packages.Install
	if len(packages) != 1 {
		t.Fatalf("package count = %d, want 1", len(packages))
	}
	if packages[0].Name != "bird2" {
		t.Fatalf("package name = %q, want bird2", packages[0].Name)
	}
	wantRepositories := []string{"base_repo", "host_repo"}
	if !reflect.DeepEqual(packages[0].Repositories, wantRepositories) {
		t.Fatalf("repositories = %#v, want %#v", packages[0].Repositories, wantRepositories)
	}
}

func TestCompileAPTRepository(t *testing.T) {
	program := compileInline(t, `
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
    package "example-tool" {
      repositories = ["tools"]
    }
  }
}
`)

	host := program.Hosts[0]
	repository, ok := host.APT.Repositories["tools"]
	if !ok {
		t.Fatalf("apt repository tools was not compiled")
	}
	if repository.Name != "tools" || repository.Ensure != "present" {
		t.Fatalf("repository = %#v", repository)
	}
	if !reflect.DeepEqual(repository.URIs, []string{"https://repo.example/debian"}) {
		t.Fatalf("repository uris = %#v", repository.URIs)
	}
	if !reflect.DeepEqual(repository.Architectures, []string{"amd64"}) {
		t.Fatalf("repository architectures = %#v", repository.Architectures)
	}
	if repository.SigningKey == nil {
		t.Fatalf("signing key was not compiled")
	}
	if repository.SigningKey.Path != "/etc/apt/keyrings/tools.asc" {
		t.Fatalf("signing key path = %q", repository.SigningKey.Path)
	}
	if got := host.Packages.Install[0].Repositories; !reflect.DeepEqual(got, []string{"tools"}) {
		t.Fatalf("package repositories = %#v", got)
	}
}

func TestCompileHostSpecJSONDoesNotLeakCurrentSensitiveBaseline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
	}{
		{name: "secrets file", fixture: "../testdata/fixtures/v2-foundation.dbf.hcl"},
		{name: "sensitive file content", fixture: "../../../examples/v2-files-plan-preview.dbf.hcl"},
		{name: "sensitive component input", fixture: "../../../examples/v2-component-inputs.dbf.hcl"},
		{name: "sensitive service environment", fixture: "../testdata/fixtures/v2-sensitive-service-environment.dbf.hcl"},
		{name: "sensitive variable content", fixture: "../testdata/fixtures/v2-sensitive-variable-files.dbf.hcl"},
		{name: "ephemeral variable content", fixture: "../testdata/fixtures/v2-ephemeral-variable-content.dbf.hcl"},
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

func TestCompileStructuredServiceEnvironmentMarksUnitSensitive(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/v2-sensitive-service-environment.dbf.hcl"})
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

func TestCompileVariableDeclarationsIntoProgramIR(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/v2-variable-declarations.dbf.hcl"})
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
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/v2-variable-defaults.dbf.hcl"})
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

func TestCompileNftablesDefaults(t *testing.T) {
	program := compileInline(t, `
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

	host := program.Hosts[0]
	if host.Nftables.Enable == nil || !*host.Nftables.Enable {
		t.Fatalf("nftables enable = %#v, want true", host.Nftables.Enable)
	}
	if host.Nftables.Main == nil {
		t.Fatal("nftables main missing")
	}
	if host.Nftables.Main.Path != "/etc/nftables.conf" || !host.Nftables.Main.Validate || !host.Nftables.Main.Activate {
		t.Fatalf("nftables main defaults = %#v", host.Nftables.Main)
	}
	snippet := host.Nftables.Files["20-services"]
	if snippet.Path != "/etc/nftables.d/20-services.nft" || snippet.Owner != "root" || snippet.Group != "root" || snippet.Mode != "0644" {
		t.Fatalf("nftables snippet defaults = %#v", snippet)
	}
}

func TestCompileRejectsInvalidAPTRepository(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "missing referenced repository",
			hcl: `
host "server1" {
  packages {
    package "example-tool" {
      repositories = ["missing"]
    }
  }
}
`,
			want: `references missing apt.repository "missing"`,
		},
		{
			name: "url key without sha",
			hcl: `
host "server1" {
  apt {
    repository "tools" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        url = "https://repo.example/key.asc"
      }
    }
  }
}
`,
			want: "signing key url requires sha256",
		},
		{
			name: "invalid sha",
			hcl: `
host "server1" {
  apt {
    repository "tools" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        url    = "https://repo.example/key.asc"
        sha256 = "not-a-sha"
      }
    }
  }
}
`,
			want: "sha256 must be a 64 character hex string",
		},
		{
			name: "absent repository reference",
			hcl: `
host "server1" {
  apt {
    repository "tools" {
      ensure = "absent"
    }
  }

  packages {
    package "example-tool" {
      repositories = ["tools"]
    }
  }
}
`,
			want: `references absent apt.repository "tools"`,
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

func TestCompileComponentTargetAndInput(t *testing.T) {
	program := compileInline(t, `
component "tools" {
  input "repo_uri" {
    type = string
  }

  apt {
    repository "tools_repo" {
      uris       = [input.repo_uri]
      suites     = [target.system.codename]
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

  system {
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
      suites     = [target.system.codename]
      components = ["main"]
    }
  }
}

host "bookworm1" {
  components = [component.tools]

  system {
    codename = "bookworm"
  }
}

host "trixie1" {
  components = [component.tools]

  system {
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

  system {
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

  system {
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

  system {
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

  system {
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

func TestCompileUsesRuntimeFactsForTargetAndArtifactSelection(t *testing.T) {
	cfg := parseInline(t, `
component "tools" {
  type = "binary"

  source "amd64" {
    url    = "https://downloads.example/tools-amd64"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/bin/tools"
  }

  apt {
    repository "tools_repo" {
      uris       = ["https://repo.example/debian"]
      suites     = [target.system.codename]
      components = ["main"]
    }
  }
}

host "server1" {
  components = [component.tools]
}
`)
	program, err := CompileWithOptions(cfg, CompileOptions{
		HostFacts: map[string]ir.HostFacts{
			"server1": {System: ir.SystemFacts{
				Hostname:     "server1",
				Architecture: "amd64",
				Codename:     "trixie",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]
	if host.System.Architecture != "amd64" || host.System.Codename != "trixie" {
		t.Fatalf("system facts were not applied: %#v", host.System)
	}
	component := host.Components[0]
	if component.SelectedSource == nil || component.SelectedSource.Architecture != "amd64" {
		t.Fatalf("selected source = %#v", component.SelectedSource)
	}
	if got := component.APT.Repositories["tools_repo"].Suites; !reflect.DeepEqual(got, []string{"trixie"}) {
		t.Fatalf("repository suites = %#v", got)
	}
}

func TestCompileRejectsDeclaredRuntimeFactMismatch(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		facts ir.SystemFacts
		want  string
	}{
		{
			name: "architecture",
			body: `
host "server1" {
  system {
    architecture = "arm64"
  }
}
`,
			facts: ir.SystemFacts{Architecture: "amd64", Codename: "trixie"},
			want:  `declared architecture "arm64" does not match detected architecture "amd64"`,
		},
		{
			name: "codename",
			body: `
host "server1" {
  system {
    architecture = "amd64"
    codename     = "bookworm"
  }
}
`,
			facts: ir.SystemFacts{Architecture: "amd64", Codename: "trixie"},
			want:  `declared codename "bookworm" does not match detected codename "trixie"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseInline(t, tt.body)
			_, err := CompileWithOptions(cfg, CompileOptions{
				HostFacts: map[string]ir.HostFacts{
					"server1": {System: tt.facts},
				},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CompileWithOptions error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileKeepsDesiredHostnameSeparateFromObservedFacts(t *testing.T) {
	cfg := parseInline(t, `
host "server1" {
  system {
    hostname = "desired-hostname"
  }
}
`)
	program, err := CompileWithOptions(cfg, CompileOptions{
		HostFacts: map[string]ir.HostFacts{
			"server1": {System: ir.SystemFacts{
				Hostname:     "observed-hostname",
				Architecture: "amd64",
				Codename:     "trixie",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]
	if host.System.Hostname != "desired-hostname" {
		t.Fatalf("desired hostname = %q, want desired-hostname", host.System.Hostname)
	}
	if host.Facts.System.Hostname != "observed-hostname" {
		t.Fatalf("observed hostname fact = %q, want observed-hostname", host.Facts.System.Hostname)
	}
}

func TestValidateRuntimeTemplatesAllowsMissingRuntimeFacts(t *testing.T) {
	cfg := parseInline(t, `
component "tools" {
  type = "binary"

  source "amd64" {
    url    = "https://downloads.example/tools-amd64.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  source "arm64" {
    url    = "https://downloads.example/tools-arm64.tar.gz"
    sha256 = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
  }

  extract {
    include = "tools"
  }

  install {
    path = "/usr/local/bin/tools"
  }

  apt {
    repository "tools_repo" {
      uris       = ["https://repo.example/debian"]
      suites     = [target.system.codename]
      components = ["main"]
    }
  }
}

host "server1" {
  components = [component.tools]
}
`)
	if _, err := CompileWithOptions(cfg, CompileOptions{ValidateRuntimeTemplates: true}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRuntimeTemplatesChecksAllArtifactSources(t *testing.T) {
	cfg := parseInline(t, `
component "tools" {
  type = "binary"

  source "amd64" {
    url    = "https://downloads.example/tools-amd64.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  source "arm64" {
    url    = "https://downloads.example/tools-arm64.tar.gz"
    sha256 = "bad"
  }

  extract {
    include = "tools"
  }

  install {
    path = "/usr/local/bin/tools"
  }
}

host "server1" {
  components = [component.tools]
}
`)
	_, err := CompileWithOptions(cfg, CompileOptions{ValidateRuntimeTemplates: true})
	if err == nil || !strings.Contains(err.Error(), "sha256 must be a 64 character hex string") {
		t.Fatalf("CompileWithOptions error = %v, want sha256 validation", err)
	}
}

func TestValidateRuntimeTemplatesChecksRuntimeDependentBodyShape(t *testing.T) {
	cfg := parseInline(t, `
component "tools" {
  apt {
    repository "tools_repo" {
      uris       = ["https://repo.example/debian"]
      suites     = [target.system.codename]
      components = ["main"]
      invalid    = true
    }
  }
}

host "server1" {
  components = [component.tools]
}
`)
	_, err := CompileWithOptions(cfg, CompileOptions{ValidateRuntimeTemplates: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported attribute component.tools.apt.repository[\"tools_repo\"].invalid") {
		t.Fatalf("CompileWithOptions error = %v, want unsupported attribute validation", err)
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

  system {
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
			want: "must declare system.architecture",
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

  system {
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
      condition     = target.system.codename == "trixie"
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
    condition     = target.system.codename == "trixie"
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

func TestCompileSecretFileDeprecationWarnings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte("not-a-real-secret-token"), 0644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(`
host "server1" {
  secrets {
    file "/etc/app/token" {
      source = "token.txt"
    }
  }
}
`, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	warnings := []ir.Warning{}
	if _, err := CompileWithOptions(cfg, CompileOptions{Warnings: &warnings}); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want 1", warnings)
	}
	if !strings.Contains(warnings[0].Message, "secrets.file is deprecated") {
		t.Fatalf("warning = %#v", warnings[0])
	}
	if warnings[0].Source.Path != `host.server1.secrets.file["/etc/app/token"]` {
		t.Fatalf("warning source = %#v", warnings[0].Source)
	}

	warnings = []ir.Warning{}
	if _, err := CompileWithOptions(cfg, CompileOptions{
		Warnings:                             &warnings,
		SuppressSecretFileDeprecationWarning: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("suppressed warnings = %#v, want none", warnings)
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

func TestCompileSensitiveComponentInputPropagatesToSystemdUnits(t *testing.T) {
	program := compileInline(t, `
component "app" {
  input "token" {
    type      = string
    sensitive = true
  }

  systemd {
    unit "raw-token.service" {
      content = "TOKEN=${input.token}\n"
    }

    service_unit "structured-token" {
      run = "/usr/bin/true"
      environment = {
        API_TOKEN = input.token
      }
    }
  }
}

host "server1" {
  component "app" {
    source = component.app
    inputs = {
      token = "systemd-secret-token"
    }
  }
}
`)
	units := program.Hosts[0].Components[0].Systemd.Units
	rawUnit := units["raw-token.service"]
	if !rawUnit.Sensitive {
		t.Fatalf("raw unit sensitive = false")
	}
	if !strings.Contains(rawUnit.Content, "systemd-secret-token") {
		t.Fatalf("raw unit in-memory content missing secret: %q", rawUnit.Content)
	}
	structuredUnit := units["structured-token.service"]
	if !structuredUnit.Sensitive {
		t.Fatalf("structured unit sensitive = false")
	}
	if !strings.Contains(structuredUnit.Content, "systemd-secret-token") {
		t.Fatalf("structured unit in-memory content missing secret: %q", structuredUnit.Content)
	}

	data, err := json.Marshal(program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "systemd-secret-token") {
		t.Fatalf("HostSpec JSON leaked systemd secret: %s", data)
	}
}

func TestCompileSensitiveVariablePropagatesToResources(t *testing.T) {
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/v2-sensitive-variable-files.dbf.hcl"})
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
	cfg, err := parser.ParseFiles([]string{"../testdata/fixtures/v2-ephemeral-variable-content.dbf.hcl"})
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

func TestCompileRejectsProfileImportCycle(t *testing.T) {
	cfg := parseInline(t, `
profile "a" {
  imports = [profile.b]
}

profile "b" {
  imports = [profile.a]
}

host "server1" {
  imports = [profile.a]
}
`)

	_, err := Compile(cfg)
	if err == nil || !strings.Contains(err.Error(), "profile.a -> profile.b -> profile.a") {
		t.Fatalf("Compile() error = %v, want profile import cycle", err)
	}
}

func TestCompileRejectsProfileHostOnlyFields(t *testing.T) {
	_, err := parseOrCompileInline(t, `
profile "bad" {
  system {
    hostname = "bad"
  }
}

host "server1" {
  imports = [profile.bad]
}
`)
	if err == nil || !strings.Contains(err.Error(), "profile.bad.system.hostname is host-only") {
		t.Fatalf("error = %v, want host-only field error", err)
	}
}

func TestCompileRejectsDuplicatePackage(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  packages {
    install = ["curl", "curl"]
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `duplicate package "curl"`) || !strings.Contains(err.Error(), "packages.install[1]") {
		t.Fatalf("error = %v, want duplicate package with source path", err)
	}
}

func TestCompileRejectsEmptyModuleAndSysctlKey(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "empty module",
			hcl: `
host "server1" {
  kernel {
    modules = [""]
  }
}
`,
			want: "kernel module entries must be non-empty strings",
		},
		{
			name: "empty sysctl key",
			hcl: `
host "server1" {
  kernel {
    sysctl = {
      "" = "bad"
    }
  }
}
`,
			want: "sysctl key must be non-empty",
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

func TestCompileRejectsUnsetOnList(t *testing.T) {
	_, err := parseOrCompileInline(t, `
profile "base" {
  packages {
    install = ["curl"]
  }
}

host "server1" {
  imports = [profile.base]

  packages {
    install = unset()
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "unset() cannot be used on lists") || !strings.Contains(err.Error(), "packages.install") {
		t.Fatalf("error = %v, want unset list error with path", err)
	}
}

func TestCompileEvaluatesAssertAgainstMergedSelf(t *testing.T) {
	program := compileInline(t, `
profile "bbr" {
  kernel {
    modules = ["tcp_bbr"]
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "tcp_bbr should be inherited"
  }
}

host "server1" {
  imports = [profile.bbr]

  system {
    hostname = "server1"
  }

  assert {
    condition = self.system.hostname == "server1"
    message   = "hostname should have defaulted or been set"
  }
}
`)
	if got := program.Hosts[0].Name; got != "server1" {
		t.Fatalf("host = %q, want server1", got)
	}
}

func TestCompileRejectsAssertFailures(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "false condition",
			hcl: `
host "server1" {
  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires tcp_bbr"
  }
}
`,
			want: "assertion failed: BBR requires tcp_bbr",
		},
		{
			name: "empty message",
			hcl: `
host "server1" {
  assert {
    condition = true
    message   = ""
  }
}
`,
			want: "message must be a non-empty string",
		},
		{
			name: "illegal field",
			hcl: `
host "server1" {
  assert {
    condition = self.observed.ready
    message   = "remote runtime state is not available"
  }
}
`,
			want: "Unsupported attribute",
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

func TestCompileInvalidFixtureReportsSourcePath(t *testing.T) {
	_, err := parseOrCompileFiles([]string{"../testdata/invalid/profile-hostname.dbf.hcl"})
	if err == nil {
		t.Fatal("expected invalid fixture error")
	}
	for _, want := range []string{"profile.bad.system.hostname", "host-only"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want substring %q", err, want)
		}
	}
}

func TestCompileAPTSourceFile(t *testing.T) {
	program := compileInline(t, `
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

	got := program.Hosts[0].APT.SourceFiles["main"]
	if got.Path != "/etc/apt/sources.list" {
		t.Fatalf("path = %q", got.Path)
	}
	if got.Content != "deb https://mirrors.aliyun.com/debian/ trixie main\n" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.OnDestroy != "restore" {
		t.Fatalf("on_destroy = %q, want restore", got.OnDestroy)
	}
	if got.Owner != "root" || got.Group != "root" || got.Mode != "0644" {
		t.Fatalf("metadata = %s:%s %s, want root:root 0644", got.Owner, got.Group, got.Mode)
	}
}

func TestCompileBBRHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-bbr.dbf.hcl", "../testdata/hostspec/v2-bbr.golden.json")
}

func TestCompileProfileMergeHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-profile-merge.dbf.hcl", "../testdata/hostspec/v2-profile-merge.golden.json")
}

func TestCompileFoundationHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../testdata/fixtures/v2-foundation.dbf.hcl", "../testdata/hostspec/v2-foundation.golden.json")
}

func TestCompileAPTRepositoryHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-apt-repository.dbf.hcl", "../testdata/hostspec/v2-apt-repository.golden.json")
}

func TestCompileBIRD2HostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-bird2.dbf.hcl", "../testdata/hostspec/v2-bird2.golden.json")
}

func TestCompileComponentBinaryHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-component-binary.dbf.hcl", "../testdata/hostspec/v2-component-binary.golden.json")
}

func TestCompileComponentSourceBuildHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-component-source-build.dbf.hcl", "../testdata/hostspec/v2-component-source-build.golden.json")
}

func TestCompileComponentInputsHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-component-inputs.dbf.hcl", "../testdata/hostspec/v2-component-inputs.golden.json")
}

func TestCompileSystemdServiceHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-systemd-service.dbf.hcl", "../testdata/hostspec/v2-systemd-service.golden.json")
}

func TestCompileSystemdServiceUnitStructured(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    service_unit "myapp" {
      description   = "My App"
      run           = ["/opt/myapp/bin/myapp", "--config", "/etc/myapp/config.yaml"]
      type          = "simple"
      user          = "myapp"
      group         = "myapp"
      working_dir   = "/var/lib/myapp"
      restart       = "always"
      restart_delay = "5s"
      wants         = ["network-online.target"]
      after         = ["network-online.target"]
      stdout        = "journal"
      stderr        = "journal"

      environment = {
        MYAPP_ENV = "production"
        PATH      = "/usr/local/bin:/usr/bin:/bin"
      }
    }
  }

  services {
    service "myapp" {
      enabled = true
      state   = "running"
    }
  }
}
`)

	host := program.Hosts[0]
	unit, ok := host.Systemd.Units["myapp.service"]
	if !ok {
		t.Fatalf("myapp.service unit missing: %#v", host.Systemd.Units)
	}
	wantContent := `[Unit]
Description=My App
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=myapp
Group=myapp
WorkingDirectory=/var/lib/myapp
Environment=MYAPP_ENV=production
Environment=PATH=/usr/local/bin:/usr/bin:/bin
ExecStart=/opt/myapp/bin/myapp --config /etc/myapp/config.yaml
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`
	if unit.Content != wantContent {
		t.Fatalf("unit content mismatch\n--- got ---\n%s\n--- want ---\n%s", unit.Content, wantContent)
	}
	if unit.Path != "/etc/systemd/system/myapp.service" || unit.Owner != "root" || unit.Group != "root" || unit.Mode != "0644" {
		t.Fatalf("unit metadata = %#v", unit)
	}
	if got := host.Services.Services["myapp"].Unit; got != "myapp.service" {
		t.Fatalf("service unit = %q, want myapp.service", got)
	}
}

func TestCompileSystemdServiceUnitRawContent(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    service_unit "myapp" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }
}
`)

	unit, ok := program.Hosts[0].Systemd.Units["myapp.service"]
	if !ok {
		t.Fatalf("myapp.service unit missing")
	}
	if unit.Content != "[Service]\nExecStart=/bin/true\n" {
		t.Fatalf("unit content = %q", unit.Content)
	}
}

func TestCompileSystemdNetworkdWireGuard(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = 51820
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = "off"
        }

        wireguard_peer "server2" {
          PublicKey  = "peer-public-key"
          AllowedIPs = ["10.80.0.2/32"]
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address = ["10.80.0.1/30"]
        }
      }
    }
  }

  assert {
    condition = self.systemd.networkd.netdev["10-wg0"].wireguard.RouteTable == "off"
    message   = "RouteTable must stay off"
  }
}
`)

	host := program.Hosts[0]
	if host.Systemd.Networkd == nil {
		t.Fatal("networkd spec missing")
	}
	netdev := host.Systemd.Networkd.NetDevs["10-wg0"]
	wantNetdev := `[NetDev]
Kind=wireguard
Name=wg0

[WireGuard]
ListenPort=51820
PrivateKeyFile=/etc/wireguard/private.key
RouteTable=off

[WireGuardPeer]
AllowedIPs=10.80.0.2/32
PublicKey=peer-public-key
`
	if netdev.Content != wantNetdev {
		t.Fatalf("netdev content mismatch\n--- got ---\n%s\n--- want ---\n%s", netdev.Content, wantNetdev)
	}
	network := host.Systemd.Networkd.Networks["20-wg0"]
	wantNetwork := `[Match]
Name=wg0

[Network]
Address=10.80.0.1/30
`
	if network.Content != wantNetwork {
		t.Fatalf("network content mismatch\n--- got ---\n%s\n--- want ---\n%s", network.Content, wantNetwork)
	}
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

  input "peer" {
    type = object({
      public_key           = string
      allowed_ips          = list(string)
      endpoint             = string
      persistent_keepalive = optional(number, 25)
    })

    nullable = false

    validation {
      condition     = length(input.peer.allowed_ips) > 0
      error_message = "peer.allowed_ips must contain at least one CIDR."
    }
  }

  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = input.interface.name
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = input.interface.listen_port
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = input.interface.route_table
        }

        wireguard_peer "peer" {
          PublicKey           = input.peer.public_key
          AllowedIPs          = input.peer.allowed_ips
          Endpoint            = input.peer.endpoint
          PersistentKeepalive = input.peer.persistent_keepalive
        }
      }

      network "20-wg0" {
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
        address = "10.80.0.1/30"
      }
      peer = {
        public_key  = "peer-public-key"
        allowed_ips = ["10.80.0.2/32"]
        endpoint    = "server2.example.net:51820"
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
	netdev := component.Systemd.Networkd.NetDevs["10-wg0"]
	if got := firstSectionValue(netdev.WireGuard, "RouteTable"); got != "off" {
		t.Fatalf("RouteTable section value = %q, want off", got)
	}
	if !strings.Contains(netdev.Content, "RouteTable=off\n") {
		t.Fatalf("netdev content does not disable networkd route table writes:\n%s", netdev.Content)
	}
}

func TestCompileRejectsInlineWireGuardPrivateKey(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard = {
          PrivateKey = "inline-private-key"
        }
      }
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "use PrivateKeyFile instead of inline PrivateKey") {
		t.Fatalf("error = %v, want inline private key rejection", err)
	}
}

func TestCompileUserGroupHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-user-group.dbf.hcl", "../testdata/hostspec/v2-user-group.golden.json")
}

func TestCompileNftablesHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-nftables.dbf.hcl", "../testdata/hostspec/v2-nftables.golden.json")
}

func TestCompileRejectsLoop3InvalidInputs(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		hcl   string
		want  string
	}{
		{
			name: "file secret path conflict",
			files: map[string]string{
				"token.txt": "not-real-secret",
			},
			hcl: `
host "server1" {
  files {
    file "/etc/app/token" {
      content = "plain"
    }
  }

  secrets {
    file "/etc/app/token" {
      source = "token.txt"
    }
  }
}
`,
			want: "conflicts with secret",
		},
		{
			name: "illegal mode",
			hcl: `
host "server1" {
  files {
    file "/etc/app/config" {
      content = "hello"
      mode    = "9999"
    }
  }
}
`,
			want: "mode must be a four digit octal string",
		},
		{
			name: "service unit raw and structured",
			hcl: `
host "server1" {
  systemd {
    service_unit "myapp" {
      content = "[Service]\nExecStart=/bin/true\n"
      run     = ["/bin/true"]
    }
  }
}
`,
			want: "cannot combine content/source with structured service fields",
		},
		{
			name: "missing group reference",
			hcl: `
host "server1" {
  users {
    user "deploy" {
      group = "missing"
    }
  }
}
`,
			want: `references missing primary group "missing"`,
		},
		{
			name: "secret source missing",
			hcl: `
host "server1" {
  secrets {
    file "/etc/app/token" {
      source = "missing-token.txt"
    }
  }
}
`,
			want: "read source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrCompileInlineWithFiles(t, tt.hcl, tt.files)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileRejectsLoop7InvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "duplicate nftables path",
			hcl: `
host "edge1" {
  nftables {
    main {
      content = "flush ruleset\n"
    }

    file "same" {
      path    = "/etc/nftables.conf"
      content = "add rule inet filter input tcp dport 443 accept\n"
    }
  }
}
`,
			want: "nftables file path",
		},
		{
			name: "content and source",
			hcl: `
host "edge1" {
  nftables {
    file "20-services" {
      content = "add rule inet filter input tcp dport 443 accept\n"
      source  = "services.nft"
    }
  }
}
`,
			want: "requires exactly one of content or source",
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

func assertHostSpecGolden(t *testing.T, fixture string, golden string) {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
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
	data, err := json.MarshalIndent(program.Hosts[0], "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("HostSpec golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func compileInline(t *testing.T, content string) *ir.Program {
	t.Helper()

	cfg := parseInline(t, content)
	program, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func parseInline(t *testing.T, content string) *parser.Config {
	t.Helper()

	return parseInlineWithOptions(t, content, parser.ParseOptions{})
}

func parseInlineWithOptions(t *testing.T, content string, opts parser.ParseOptions) *parser.Config {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFilesWithOptions([]string{file}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func parseOrCompileInline(t *testing.T, content string) (*ir.Program, error) {
	t.Helper()

	return parseOrCompileInlineWithFiles(t, content, nil)
}

func parseOrCompileInlineWithFiles(t *testing.T, content string, extraFiles map[string]string) (*ir.Program, error) {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	for name, data := range extraFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return parseOrCompileFiles([]string{file})
}

func parseOrCompileFiles(files []string) (*ir.Program, error) {
	cfg, err := parser.ParseFiles(files)
	if err != nil {
		return nil, err
	}
	return Compile(cfg)
}

func testHostFacts() map[string]ir.HostFacts {
	out := map[string]ir.HostFacts{}
	for _, name := range []string{
		"apt1",
		"bbr1",
		"edge1",
		"foundation1",
		"input1",
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

func packageNames(items []ir.PackageItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
