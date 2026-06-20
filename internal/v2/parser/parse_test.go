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

func TestParseLabeledObjectBlockSourcePath(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  packages {
    package "bird2" {
      repositories = ["cznic"]
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	packages := cfg.Hosts["web1"].Body.Map["packages"]
	pkg := packages.Map["package"].Map["bird2"]
	if pkg.Source.Path != `host.web1.packages.package["bird2"]` {
		t.Fatalf("package source path = %q", pkg.Source.Path)
	}
	repositories := pkg.Map["repositories"]
	if repositories.Source.Path != `host.web1.packages.package["bird2"].repositories` {
		t.Fatalf("repositories source path = %q", repositories.Source.Path)
	}
	if repositories.List[0].Source.Path != `host.web1.packages.package["bird2"].repositories[0]` {
		t.Fatalf("repository item source path = %q", repositories.List[0].Source.Path)
	}
}

func TestParseAPTSigningKeyBlockSourcePath(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  apt {
    repository "tools" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        url    = "https://repo.example/key.asc"
        sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      }
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	repository := cfg.Hosts["web1"].Body.Map["apt"].Map["repository"].Map["tools"]
	if repository.Source.Path != `host.web1.apt.repository["tools"]` {
		t.Fatalf("repository source path = %q", repository.Source.Path)
	}
	signingKey := repository.Map["signing_key"]
	if signingKey.Source.Path != `host.web1.apt.repository["tools"].signing_key` {
		t.Fatalf("signing key source path = %q", signingKey.Source.Path)
	}
	sha := signingKey.Map["sha256"]
	if sha.Source.Path != `host.web1.apt.repository["tools"].signing_key.sha256` {
		t.Fatalf("sha256 source path = %q", sha.Source.Path)
	}
}

func TestParseLifecycleBlock(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  files {
    file "/etc/protected.conf" {
      content = "managed"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`)

	cfg, err := ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	fileBlock := cfg.Hosts["web1"].Body.Map["files"].Map["file"].Map["/etc/protected.conf"]
	lifecycle := fileBlock.Map["lifecycle"]
	if lifecycle.Source.Path != `host.web1.files.file["/etc/protected.conf"].lifecycle` {
		t.Fatalf("lifecycle source path = %q", lifecycle.Source.Path)
	}
	preventDestroy := lifecycle.Map["prevent_destroy"]
	if preventDestroy.Kind != KindBool || !preventDestroy.Bool {
		t.Fatalf("prevent_destroy = %#v, want true", preventDestroy)
	}
	if preventDestroy.Source.Path != `host.web1.files.file["/etc/protected.conf"].lifecycle.prevent_destroy` {
		t.Fatalf("prevent_destroy source path = %q", preventDestroy.Source.Path)
	}
}

func TestParseRejectsUnknownLifecycleAttribute(t *testing.T) {
	file := writeConfig(t, `
host "web1" {
  files {
    file "/etc/protected.conf" {
      content = "managed"

      lifecycle {
        ignore_changes = true
      }
    }
  }
}
`)

	_, err := ParseFiles([]string{file})
	if err == nil || !strings.Contains(err.Error(), "unsupported attribute") || !strings.Contains(err.Error(), "lifecycle.ignore_changes") {
		t.Fatalf("ParseFiles() error = %v, want unsupported lifecycle attribute", err)
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
