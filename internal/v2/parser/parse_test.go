package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseHostProfileNestedBlocksAndSourceLine(t *testing.T) {
	file := writeConfig(t, `
profile "base" {
  packages {
    install = ["curl"]
  }
}

host "web1" {
  imports = [profile.base]

  ssh {
    host = "10.0.0.10"
  }

  kernel {
    modules = ["tcp_bbr"]
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cfg.Profiles["base"]; !ok {
		t.Fatalf("profile base was not parsed")
	}
	host, ok := cfg.Hosts["web1"]
	if !ok {
		t.Fatalf("host web1 was not parsed")
	}
	if !reflect.DeepEqual(host.Imports, []string{"base"}) {
		t.Fatalf("host imports = %#v, want base", host.Imports)
	}

	kernel := host.Body.Map["kernel"]
	modules := kernel.Map["modules"]
	if modules.Source.File != file {
		t.Fatalf("modules source file = %q, want %q", modules.Source.File, file)
	}
	if modules.Source.Line != 15 {
		t.Fatalf("modules source line = %d, want 15", modules.Source.Line)
	}
	if modules.Source.Path != "host.web1.kernel.modules" {
		t.Fatalf("modules source path = %q", modules.Source.Path)
	}
}

func TestParseRejectsUnknownTopLevelBlock(t *testing.T) {
	file := writeConfig(t, `
banana {}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), `unknown v2 top-level block "banana"`) {
		t.Fatalf("ParseFiles() error = %v, want unknown top-level block", err)
	}
}

func TestParseRejectsWrongLabelCount(t *testing.T) {
	file := writeConfig(t, `
profile {}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "profile block requires exactly one label") {
		t.Fatalf("ParseFiles() error = %v, want label count error", err)
	}
}

func TestParseRejectsDuplicateHost(t *testing.T) {
	file := writeConfig(t, `
host "web1" {}
host "web1" {}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), `duplicate host "web1"`) {
		t.Fatalf("ParseFiles() error = %v, want duplicate host error", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}
