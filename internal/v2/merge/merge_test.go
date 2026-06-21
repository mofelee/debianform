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

func TestCompileSystemdServiceHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-systemd-service.dbf.hcl", "../testdata/hostspec/v2-systemd-service.golden.json")
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

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFiles([]string{file})
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
