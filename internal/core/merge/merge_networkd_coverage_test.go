package merge

import (
	"strings"
	"testing"
)

func TestCompileNetworkdAllowsUnknownValidSectionsAndSettings(t *testing.T) {
	program := compileInline(t, `
host "router" {
  systemd {
    networkd {
      network "30-future" {
        section "future" {
          name = "FutureSection"
          settings = {
            FutureDirective = "enabled"
            FutureToggle    = true
          }
        }
      }
    }
  }
}
`)
	content := program.Hosts[0].Systemd.Networkd.Networks["30-future"].Content
	for _, want := range []string{"[FutureSection]", "FutureDirective=enabled", "FutureToggle=yes"} {
		if !strings.Contains(content, want) {
			t.Fatalf("future networkd content missing %q:\n%s", want, content)
		}
	}
}

func TestCompileRejectsNetworkdValidationEdges(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid section name",
			body: `section "bad" {
  name     = "Bad Section"
  settings = { Future = "value" }
}`,
			want: "section name",
		},
		{
			name: "invalid scalar shape",
			body: `section "bad" {
  name     = "FutureSection"
  settings = { Future = { nested = true } }
}`,
			want: "values must be strings, numbers, booleans, or lists",
		},
		{
			name: "newline injection",
			body: `section "bad" {
  name     = "FutureSection"
  settings = { Future = "first\nsecond" }
}`,
			want: "single-line strings",
		},
		{
			name: "unmarked inline preshared key",
			body: `section "peer" {
  name     = "WireGuardPeer"
  settings = { PresharedKey = "secret" }
}`,
			want: "inline PresharedKey must use a sensitive value",
		},
		{
			name: "duplicate reconfigure target",
			body: `content = "[Match]\nName=eth0\n"
activation {
  reconfigure = ["eth0", "eth0"]
}`,
			want: "duplicate",
		},
		{
			name: "raw content and source",
			body: `content = "[Match]\nName=eth0\n"
source  = "missing.network"`,
			want: "only one of content or source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrCompileInline(t, `
host "router" {
  systemd {
    networkd {
      network "test" {
`+tt.body+`
      }
    }
  }
}
`)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), `host.router.systemd.networkd.network["test"]`) {
				t.Fatalf("diagnostic is not source-oriented: %v", err)
			}
		})
	}
}
