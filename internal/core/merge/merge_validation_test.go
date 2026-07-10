package merge

import (
	"strings"
	"testing"
)

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
