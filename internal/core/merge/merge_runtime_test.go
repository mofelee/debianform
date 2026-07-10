package merge

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
)

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
