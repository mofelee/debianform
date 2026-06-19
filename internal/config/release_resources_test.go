package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReleaseBinaryAndSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}

debian_release_binary "tool" {
  host   = "server1"
  path   = "/usr/local/bin/tool"
  member = "tool"

  sources = {
    amd64 = {
      url            = "https://example.com/tool-amd64.tar.xz"
      archive_sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      binary_sha256  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    }
  }
}

debian_systemd_unit "tool" {
  host    = "server1"
  name    = "tool.service"
  content = "[Service]\nExecStart=/usr/local/bin/tool\n"
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Resources), 2; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	if got := cfg.Resources[0].Type; got != "debian_release_binary" {
		t.Fatalf("first resource type = %q", got)
	}
	sources, ok := cfg.Resources[0].Attrs["sources"].(map[string]any)
	if !ok {
		t.Fatalf("sources = %#v, want object", cfg.Resources[0].Attrs["sources"])
	}
	if _, ok := sources["amd64"].(map[string]any); !ok {
		t.Fatalf("sources.amd64 = %#v, want object", sources["amd64"])
	}
	if got := cfg.Resources[1].Attrs["name"]; got != "tool.service" {
		t.Fatalf("unit name = %#v", got)
	}
}

func TestReleaseBinaryRejectsInvalidChecksum(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}

debian_release_binary "tool" {
  host   = "server1"
  path   = "/usr/local/bin/tool"
  member = "tool"
  source = {
    url            = "https://example.com/tool.tar.xz"
    archive_sha256 = "not-a-checksum"
    binary_sha256  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  }
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load([]string{file})
	if err == nil || !strings.Contains(err.Error(), "archive_sha256") {
		t.Fatalf("Load error = %v, want archive_sha256 validation error", err)
	}
}
