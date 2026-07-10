package merge

import (
	"strings"
	"testing"
)

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
