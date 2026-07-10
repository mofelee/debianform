package merge

import (
	"strings"
	"testing"
)

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
