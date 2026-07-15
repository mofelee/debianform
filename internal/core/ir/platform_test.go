package ir

import (
	"strings"
	"testing"
)

func TestValidateTargetPlatform(t *testing.T) {
	tests := []struct {
		name         string
		distribution string
		version      string
		architecture string
		codename     string
		wantErr      string
	}{
		{name: "Debian 12 amd64", distribution: "debian", version: "12", architecture: "amd64", codename: "bookworm"},
		{name: "Debian 13 arm64 preview", distribution: "debian", version: "13", architecture: "arm64", codename: "trixie"},
		{name: "Ubuntu 24.04 amd64", distribution: "ubuntu", version: "24.04", architecture: "amd64", codename: "noble"},
		{name: "Ubuntu 26.04 amd64", distribution: "ubuntu", version: "26.04", architecture: "amd64", codename: "resolute"},
		{name: "unknown distribution", distribution: "fedora", version: "42", architecture: "amd64", codename: "", wantErr: "unsupported target platform"},
		{name: "unsupported Ubuntu version", distribution: "ubuntu", version: "22.04", architecture: "amd64", codename: "jammy", wantErr: "unsupported target platform"},
		{name: "unsupported Ubuntu architecture", distribution: "ubuntu", version: "24.04", architecture: "arm64", codename: "noble", wantErr: "unsupported target platform"},
		{name: "Ubuntu 24.04 codename mismatch", distribution: "ubuntu", version: "24.04", architecture: "amd64", codename: "resolute", wantErr: `reports codename "resolute", want "noble"`},
		{name: "Ubuntu 26.04 codename mismatch", distribution: "ubuntu", version: "26.04", architecture: "amd64", codename: "noble", wantErr: `reports codename "noble", want "resolute"`},
		{name: "Debian version codename mismatch", distribution: "debian", version: "13", architecture: "amd64", codename: "bookworm", wantErr: `reports codename "bookworm", want "trixie"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTargetPlatform(tt.distribution, tt.version, tt.architecture, tt.codename)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateTargetPlatform() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
