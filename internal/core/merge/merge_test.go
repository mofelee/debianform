package merge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/mofelee/debianform/internal/core/testassert"
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
    locale   = "C.UTF-8"
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
	if !host.System.TimezoneSet {
		t.Fatalf("timezone should be marked explicit")
	}
	if host.System.Locale != "C.UTF-8" {
		t.Fatalf("locale = %q, want C.UTF-8", host.System.Locale)
	}
	if !host.System.LocaleSet {
		t.Fatalf("locale should be marked explicit")
	}
}

func TestCompileAllowsHostToUnsetProfileSystemTimezoneAndLocale(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  system {
    timezone = "UTC"
    locale   = "C.UTF-8"
  }
}

host "server1" {
  imports = [profile.base]

  system {
    timezone = unset()
    locale   = unset()
  }
}
`)

	host := program.Hosts[0]
	if host.System.TimezoneSet || host.System.Timezone != "" {
		t.Fatalf("timezone = %q set=%v, want unset", host.System.Timezone, host.System.TimezoneSet)
	}
	if host.System.LocaleSet || host.System.Locale != "" {
		t.Fatalf("locale = %q set=%v, want unset", host.System.Locale, host.System.LocaleSet)
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

func TestCompileRejectsNonRootSSHUser(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  ssh {
    user = "debian"
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `ssh.user must be "root" or omitted`) {
		t.Fatalf("Compile error = %v, want non-root ssh.user rejection", err)
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

  script "reload" {
    run = "systemctl reload tools-${target.platform.codename}.service"
  }

  apt {
    repository "tools_repo" {
      uris       = ["https://repo.example/debian"]
      suites     = [target.platform.codename]
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
	if host.Platform == nil {
		t.Fatalf("platform facts were not applied: platform is nil")
	}
	if host.Platform.Architecture != "amd64" || host.Platform.Codename != "trixie" {
		t.Fatalf("platform facts were not applied: %#v", host.Platform)
	}
	component := host.Components[0]
	if component.SelectedSource == nil || component.SelectedSource.Architecture != "amd64" {
		t.Fatalf("selected source = %#v", component.SelectedSource)
	}
	if got := component.APT.Repositories["tools_repo"].Suites; !reflect.DeepEqual(got, []string{"trixie"}) {
		t.Fatalf("repository suites = %#v", got)
	}
}

func TestCompilePlatformBlockExposesTargetAndSelf(t *testing.T) {
	cfg := parseInline(t, `
component "tools" {
  apt {
    repository "platform_repo" {
      uris       = ["https://repo.example/debian"]
      suites     = [target.platform.codename]
      components = ["main"]
    }

  }
}

host "server1" {
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  components = [component.tools]

  assert {
    condition = self.platform.architecture == "amd64" && self.platform.codename == "trixie"
    message   = "platform facts should resolve"
  }
}
`)
	program, err := CompileWithOptions(cfg, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]
	if host.Platform == nil {
		t.Fatalf("platform = nil, want explicit platform spec")
	}
	if host.Platform.Architecture != "amd64" || host.Platform.Codename != "trixie" {
		t.Fatalf("platform = %#v, want amd64/trixie", host.Platform)
	}
	component := host.Components[0]
	if got := component.APT.Repositories["platform_repo"].Suites; !reflect.DeepEqual(got, []string{"trixie"}) {
		t.Fatalf("platform repository suites = %#v", got)
	}
}

func TestCompileRejectsLegacySystemPlatformFacts(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
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
			want: `system.architecture is no longer supported; use platform.architecture`,
		},
		{
			name: "codename",
			body: `
host "server1" {
  system {
    codename = "bookworm"
  }
}
`,
			want: `system.codename is no longer supported; use platform.codename`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseInline(t, tt.body)
			_, err := CompileWithOptions(cfg, CompileOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CompileWithOptions error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileRejectsLegacySystemPlatformExpressions(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "target codename",
			body: `
component "tools" {
  apt {
    repository "tools_repo" {
      uris       = ["https://repo.example/debian"]
      suites     = [target.system.codename]
      components = ["main"]
    }
  }
}

host "server1" {
  platform {
    codename = "trixie"
  }

  components = [component.tools]
}
`,
			want: `target.system.codename is no longer supported; use target.platform.codename`,
		},
		{
			name: "self architecture",
			body: `
host "server1" {
  platform {
    architecture = "amd64"
  }

  assert {
    condition = self.system.architecture == "amd64"
    message   = "must be amd64"
  }
}
`,
			want: `self.system.architecture is no longer supported; use self.platform.architecture`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseInline(t, tt.body)
			_, err := CompileWithOptions(cfg, CompileOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CompileWithOptions error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsPlatformFactsInProfile(t *testing.T) {
	_, err := parseOrCompileInline(t, `
profile "base" {
  platform {
    architecture = "amd64"
  }
}

host "server1" {
  imports = [profile.base]
}
`)
	if err == nil || !strings.Contains(err.Error(), "host-only and cannot be declared in profile") {
		t.Fatalf("parseOrCompileInline error = %v, want host-only platform error", err)
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
			name: "platform architecture",
			body: `
host "server1" {
  platform {
    architecture = "arm64"
  }
}
`,
			facts: ir.SystemFacts{Architecture: "amd64", Codename: "trixie"},
			want:  `declared platform.architecture "arm64" does not match detected architecture "amd64"`,
		},
		{
			name: "platform codename",
			body: `
host "server1" {
  platform {
    architecture = "amd64"
    codename     = "bookworm"
  }
}
`,
			facts: ir.SystemFacts{Architecture: "amd64", Codename: "trixie"},
			want:  `declared platform.codename "bookworm" does not match detected codename "trixie"`,
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
	if !host.System.HostnameSet {
		t.Fatalf("desired hostname should be marked explicit")
	}
	if host.Facts.System.Hostname != "observed-hostname" {
		t.Fatalf("observed hostname fact = %q, want observed-hostname", host.Facts.System.Hostname)
	}
}

func TestCompileDoesNotDefaultDesiredHostnameFromHostLabel(t *testing.T) {
	cfg := parseInline(t, `
host "server1" {}
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
	if host.System.HostnameSet {
		t.Fatalf("desired hostname should be unset")
	}
	if host.System.Hostname != "" {
		t.Fatalf("desired hostname = %q, want empty when unset", host.System.Hostname)
	}
	if host.Facts.System.Hostname != "observed-hostname" {
		t.Fatalf("observed hostname fact = %q, want observed-hostname", host.Facts.System.Hostname)
	}
	if host.SSH.Host != "server1" {
		t.Fatalf("ssh host = %q, want host label default", host.SSH.Host)
	}
	if !strings.Contains(host.State.Path, "server1.json") {
		t.Fatalf("state path = %q, want host label default", host.State.Path)
	}
}

func TestCompileHyphenHostUsesSafeDefaultTargets(t *testing.T) {
	program := compileInline(t, `host "edge-1" {}`)
	if len(program.Hosts) != 1 {
		t.Fatalf("hosts = %#v, want one", program.Hosts)
	}
	host := program.Hosts[0]
	if host.SSH.Host != "edge-1" {
		t.Fatalf("ssh host = %q, want edge-1", host.SSH.Host)
	}
	if host.State.Path != "/var/lib/debianform/state/edge-1.json" {
		t.Fatalf("state path = %q", host.State.Path)
	}
	if host.State.LockPath != "/var/lock/debianform/state/edge-1.lock" {
		t.Fatalf("state lock path = %q", host.State.LockPath)
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
      suites     = [target.platform.codename]
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
      suites     = [target.platform.codename]
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
	if file.OnChange != "reload" {
		t.Fatalf("file on_change = %#v", file)
	}
	if file.OnChangeSource == nil || file.OnChangeSource.Path != `component.app.files.file["/etc/app.conf"].on_change` {
		t.Fatalf("on_change source = %#v", file.OnChangeSource)
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

func TestCompileFileAndSecretPathAttribute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte("not-a-real-secret-token"), 0644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(`
host "server1" {
  files {
    file "app_config" {
      path    = "/etc/app/config"
      content = "ok"
    }
  }

  secrets {
    file "app_token" {
      path   = "/etc/app/token"
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
	program, err := CompileWithOptions(cfg, CompileOptions{SuppressSecretFileDeprecationWarning: true})
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]
	if _, ok := host.Files.Files["/etc/app/config"]; !ok {
		t.Fatalf("files = %#v", host.Files.Files)
	}
	if _, ok := host.Secrets.Files["/etc/app/token"]; !ok {
		t.Fatalf("secrets = %#v", host.Secrets.Files)
	}
}

func TestCompileRejectsDuplicateExplicitFilePath(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  files {
    file "one" {
      path    = "/etc/app/config"
      content = "one"
    }

    file "two" {
      path    = "/etc/app/config"
      content = "two"
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `file path "/etc/app/config" conflicts with file declared`) {
		t.Fatalf("error = %v, want duplicate file path rejection", err)
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

func TestCompileRejectsInvalidSystemTimezoneAndLocale(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty timezone",
			body: `
host "server1" {
  system {
    timezone = ""
  }
}
`,
			want: "system.timezone must be non-empty",
		},
		{
			name: "timezone path traversal",
			body: `
host "server1" {
  system {
    timezone = "Etc/../UTC"
  }
}
`,
			want: "system.timezone must not contain empty, current, or parent path segments",
		},
		{
			name: "absolute timezone",
			body: `
host "server1" {
  system {
    timezone = "/etc/localtime"
  }
}
`,
			want: "system.timezone must be a zoneinfo name",
		},
		{
			name: "empty locale",
			body: `
host "server1" {
  system {
    locale = ""
  }
}
`,
			want: "system.locale must be non-empty",
		},
		{
			name: "unsafe locale",
			body: `
host "server1" {
  system {
    locale = "en_US.UTF-8;rm"
  }
}
`,
			want: "system.locale must be a locale name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrCompileInline(t, tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
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
	assertHostSpecGolden(t, "../../../examples/bbr.dbf.hcl", "../testdata/hostspec/bbr.golden.json")
}

func TestCompileProfileMergeHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/profile-merge.dbf.hcl", "../testdata/hostspec/profile-merge.golden.json")
}

func TestCompileFoundationHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../testdata/fixtures/foundation.dbf.hcl", "../testdata/hostspec/foundation.golden.json")
}

func TestCompilePlatformHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../testdata/fixtures/platform.dbf.hcl", "../testdata/hostspec/platform.golden.json")
}

func TestCompileAPTRepositoryHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/apt-repository.dbf.hcl", "../testdata/hostspec/apt-repository.golden.json")
}

func TestCompileBIRD2HostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/bird2.dbf.hcl", "../testdata/hostspec/bird2.golden.json")
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

func TestCompileSystemdServiceHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/systemd-service.dbf.hcl", "../testdata/hostspec/systemd-service.golden.json")
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

      service_config = {
        AmbientCapabilities = "CAP_NET_BIND_SERVICE"
        NoNewPrivileges     = true
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
AmbientCapabilities=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes

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

func TestCompileSystemdTimerResolvedAndJournald(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    timer "cleanup" {
      description = "Cleanup cache"
      enable      = true
      state       = "running"

      timer = {
        OnCalendar = "daily"
        Persistent = true
      }
    }

    resolved {
      enable = true
      state  = "running"

      resolve = {
        DNS      = ["1.1.1.1", "9.9.9.9"]
        DNSSEC   = "allow-downgrade"
        DNSStubListener = false
      }
    }

    journald {
      state = "reloaded"

      journal = {
        SystemMaxUse = "1G"
        Compress     = true
      }
    }
  }

  assert {
    condition = self.systemd.timers["cleanup.timer"].timer.Persistent == "yes"
    message   = "timer assertion failed"
  }

  assert {
    condition = self.systemd.resolved.resolve.DNS[0] == "1.1.1.1"
    message   = "resolved assertion failed"
  }

  assert {
    condition = self.systemd.journald.journal.Compress == "yes"
    message   = "journald assertion failed"
  }
}
`)

	host := program.Hosts[0]
	timer, ok := host.Systemd.Timers["cleanup.timer"]
	if !ok {
		t.Fatalf("cleanup.timer missing: %#v", host.Systemd.Timers)
	}
	for _, want := range []string{"[Timer]", "OnCalendar=daily", "Persistent=yes", "WantedBy=timers.target"} {
		if !strings.Contains(timer.Unit.Content, want) {
			t.Fatalf("timer content missing %q:\n%s", want, timer.Unit.Content)
		}
	}
	if timer.Enable == nil || !*timer.Enable || timer.State != "running" {
		t.Fatalf("timer service settings = %#v", timer)
	}
	if host.Systemd.Resolved == nil ||
		!strings.Contains(host.Systemd.Resolved.Unit.Content, "DNS=1.1.1.1") ||
		!strings.Contains(host.Systemd.Resolved.Unit.Content, "DNS=9.9.9.9") ||
		!strings.Contains(host.Systemd.Resolved.Unit.Content, "DNSStubListener=no") {
		t.Fatalf("resolved content = %#v", host.Systemd.Resolved)
	}
	if host.Systemd.Journald == nil || !strings.Contains(host.Systemd.Journald.Unit.Content, "Compress=yes") || host.Systemd.Journald.State != "reloaded" {
		t.Fatalf("journald spec = %#v", host.Systemd.Journald)
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

func TestCompileNetworkdWireGuardPeerAttributeMap(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard_peer = {
          laptop = {
            PublicKey  = "laptop-public-key"
            AllowedIPs = ["10.80.0.10/32"]
          }
          server2 = {
            PublicKey  = "peer-public-key"
            AllowedIPs = ["10.80.0.2/32"]
          }
        }
      }
    }
  }
}
`)
	netdev := program.Hosts[0].Systemd.Networkd.NetDevs["10-wg0"]
	if len(netdev.WireGuardPeers) != 2 {
		t.Fatalf("wireguard peers = %#v", netdev.WireGuardPeers)
	}
	if !strings.Contains(netdev.Content, "PublicKey=laptop-public-key\n") || !strings.Contains(netdev.Content, "PublicKey=peer-public-key\n") {
		t.Fatalf("netdev content does not contain both WireGuard peers:\n%s", netdev.Content)
	}
}

func TestCompileRejectsDuplicateWireGuardPeerAttributeAndBlock(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard_peer = {
          server2 = {
            PublicKey  = "peer-public-key"
            AllowedIPs = ["10.80.0.2/32"]
          }
        }

        wireguard_peer "server2" {
          PublicKey  = "other-peer-public-key"
          AllowedIPs = ["10.80.0.3/32"]
        }
      }
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `duplicate host.server1.systemd.networkd.netdev["10-wg0"].wireguard_peer["server2"]`) {
		t.Fatalf("error = %v, want duplicate wireguard peer rejection", err)
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
	assertHostSpecGolden(t, "../../../examples/user-group.dbf.hcl", "../testdata/hostspec/user-group.golden.json")
}

func TestCompileNftablesHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/nftables.dbf.hcl", "../testdata/hostspec/nftables.golden.json")
}

func TestCompileDockerDefaults(t *testing.T) {
	program := compileInline(t, `
host "docker1" {
  docker {
    enable = true
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker == nil || !docker.Enable {
		t.Fatalf("docker = %#v, want enabled spec", docker)
	}
	if docker.Package.Source != "official" || docker.Package.Channel != "stable" || docker.Package.RemoveConflicts != "auto" {
		t.Fatalf("docker package defaults = %#v", docker.Package)
	}
	if docker.Package.RepositoryURL != ir.DockerOfficialRepositoryURL || docker.Package.GPGURL != ir.DockerOfficialGPGURL || docker.Package.GPGSHA256 != ir.DockerOfficialGPGSHA256 {
		t.Fatalf("docker package official URLs = %#v, want official defaults", docker.Package)
	}
	if !docker.Service.Enable || docker.Service.State != "running" || docker.Service.Name != "docker.service" {
		t.Fatalf("docker service defaults = %#v", docker.Service)
	}
}

func TestCompileDockerOfficialMirrorURLs(t *testing.T) {
	program := compileInline(t, `
host "docker1" {
  docker {
    enable = true

    package {
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
      gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker.Package.RepositoryURL != "https://mirrors.aliyun.com/docker-ce/linux/debian" {
		t.Fatalf("repository_url = %q", docker.Package.RepositoryURL)
	}
	if docker.Package.GPGURL != "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg" {
		t.Fatalf("gpg_url = %q", docker.Package.GPGURL)
	}
	if docker.Package.GPGSHA256 != "" {
		t.Fatalf("gpg_sha256 = %q, want empty for custom gpg_url without explicit hash", docker.Package.GPGSHA256)
	}
}

func TestCompileDockerOfficialMirrorGPGSHA256(t *testing.T) {
	program := compileInline(t, `
host "docker1" {
  docker {
    enable = true

    package {
      gpg_url    = "https://mirror.example/docker/gpg"
      gpg_sha256 = "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"
    }
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker.Package.GPGURL != "https://mirror.example/docker/gpg" {
		t.Fatalf("gpg_url = %q", docker.Package.GPGURL)
	}
	if docker.Package.GPGSHA256 != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("gpg_sha256 = %q, want normalized hash", docker.Package.GPGSHA256)
	}
}

func TestCompileDockerProfileOverride(t *testing.T) {
	program := compileInline(t, `
profile "docker_base" {
  docker {
    enable = true

    package {
      source = "official"
    }

    service {
      state = "running"
    }
  }
}

host "docker1" {
  imports = [profile.docker_base]

  docker {
    package {
      source = "none"
    }

    service {
      state = "stopped"
    }
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker == nil || !docker.Enable {
		t.Fatalf("docker = %#v, want enabled spec", docker)
	}
	if docker.Package.Source != "none" {
		t.Fatalf("package source = %q, want none", docker.Package.Source)
	}
	if docker.Service.State != "stopped" {
		t.Fatalf("service state = %q, want stopped", docker.Service.State)
	}
}

func TestCompileDockerComposeDefaults(t *testing.T) {
	program := compileInline(t, `
host "compose1" {
  docker {
    enable = true

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "100m"
        }
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
	docker := program.Hosts[0].Docker
	if docker == nil || docker.Daemon == nil {
		t.Fatalf("docker daemon missing: %#v", docker)
	}
	if docker.Daemon.Settings["log-driver"] != "json-file" {
		t.Fatalf("daemon settings = %#v", docker.Daemon.Settings)
	}
	compose := docker.Composes["app"]
	if !compose.Enable || compose.State != "running" || compose.Project != "app" {
		t.Fatalf("compose defaults = %#v", compose)
	}
	if compose.File == nil || compose.File.Owner != "root" || compose.File.Group != "root" || compose.File.Mode != "0644" {
		t.Fatalf("compose file defaults = %#v", compose.File)
	}
	env := compose.EnvFiles["app"]
	if env.Owner != "root" || env.Group != "root" || env.Mode != "0600" || !env.Sensitive {
		t.Fatalf("env file defaults = %#v", env)
	}
	if !reflect.DeepEqual(compose.After, []string{"docker.service", "network-online.target"}) {
		t.Fatalf("after = %#v", compose.After)
	}
	if !reflect.DeepEqual(compose.WantedBy, []string{"multi-user.target"}) {
		t.Fatalf("wanted_by = %#v", compose.WantedBy)
	}
}

func TestCompileDockerHostSpecGoldens(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/docker-minimal.dbf.hcl", "../testdata/hostspec/docker-minimal.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-daemon.dbf.hcl", "../testdata/hostspec/docker-daemon.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-compose.dbf.hcl", "../testdata/hostspec/docker-compose.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-package-sources.dbf.hcl", "../testdata/hostspec/docker-package-sources.golden.json")
	assertHostSpecGolden(t, "../testdata/fixtures/docker-package-source-none.dbf.hcl", "../testdata/hostspec/docker-package-source-none.golden.json")
	assertHostSpecGolden(t, "../testdata/fixtures/docker-package-source-custom.dbf.hcl", "../testdata/hostspec/docker-package-source-custom.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-users.dbf.hcl", "../testdata/hostspec/docker-users.golden.json")
}

func TestCompileRejectsDockerInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "invalid package source",
			hcl: `
host "docker1" {
  docker {
    enable = true

    package {
      source = "invalid"
    }
  }
}
`,
			want: "must be official, debian, none, or custom",
		},
		{
			name: "invalid remove conflicts",
			hcl: `
host "docker1" {
  docker {
    package {
      remove_conflicts = "sometimes"
    }
  }
}
`,
			want: "must be auto, true, or false",
		},
		{
			name: "repository url with debian source",
			hcl: `
host "docker1" {
  docker {
    package {
      source         = "debian"
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
    }
  }
}
`,
			want: `repository_url is only valid when source = "official"`,
		},
		{
			name: "gpg url with custom source",
			hcl: `
host "docker1" {
  docker {
    package {
      source  = "custom"
      gpg_url = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
`,
			want: `gpg_url is only valid when source = "official"`,
		},
		{
			name: "invalid docker gpg sha",
			hcl: `
host "docker1" {
  docker {
    package {
      gpg_sha256 = "not-a-sha"
    }
  }
}
`,
			want: "gpg_sha256 must be a 64 character hex string",
		},
		{
			name: "empty docker repository url",
			hcl: `
host "docker1" {
  docker {
    package {
      repository_url = ""
    }
  }
}
`,
			want: "repository_url must be non-empty",
		},
		{
			name: "empty docker user",
			hcl: `
host "docker1" {
  docker {
    users = [""]
  }
}
`,
			want: "docker users entries must be non-empty strings",
		},
		{
			name: "missing compose directory",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose directory must be absolute and non-empty",
		},
		{
			name: "compose content and source",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
        source  = "compose.yaml"
      }
    }
  }
}
`,
			want: "requires exactly one of content or source",
		},
		{
			name: "daemon settings sensitive",
			hcl: `
variable "mirror" {
  type      = string
  default   = "https://mirror.example.com"
  sensitive = true
}

host "docker1" {
  docker {
    daemon {
      settings = {
        "registry-mirrors" = [var.mirror]
      }
    }
  }
}
`,
			want: "docker daemon settings cannot contain sensitive",
		},
		{
			name: "compose file path conflict",
			hcl: `
host "compose1" {
  files {
    file "/opt/app/compose.yaml" {
      content = "plain\n"
    }
  }

  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose file path",
		},
		{
			name: "compose env file path conflicts with systemd unit",
			hcl: `
host "compose1" {
  systemd {
    unit "app.env" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }

  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      env_file "app" {
        path    = "/etc/systemd/system/app.env"
        content = "TOKEN=example\n"
      }
    }
  }
}
`,
			want: "docker compose env_file path",
		},
		{
			name: "invalid compose project",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"
      project   = "bad project"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose project",
		},
		{
			name: "invalid compose service name",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      service {
        name = "/bad"
      }
    }
  }
}
`,
			want: "docker compose service name",
		},
		{
			name: "empty compose after entry",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"
      after     = [""]

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "after entries must be non-empty strings",
		},
		{
			name: "empty compose wanted_by entry",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"
      wanted_by = [""]

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "wanted_by entries must be non-empty strings",
		},
		{
			name: "compose generated unit path conflicts with systemd unit",
			hcl: `
host "compose1" {
  systemd {
    unit "debianform-compose-app.service" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }

  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose systemd unit path",
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
