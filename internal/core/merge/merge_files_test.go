package merge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func TestCompileSecretFileDeprecationWarnings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte("not-a-real-secret-token"), 0644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(`
host "server1" {
  secrets {
    file "/etc/app/token" {
      source = "token.txt"
    }
  }
}
`, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	warnings := []ir.Warning{}
	if _, err := CompileWithOptions(cfg, CompileOptions{Warnings: &warnings}); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want 1", warnings)
	}
	if !strings.Contains(warnings[0].Message, "secrets.file is deprecated") {
		t.Fatalf("warning = %#v", warnings[0])
	}
	if warnings[0].Source.Path != `host.server1.secrets.file["/etc/app/token"]` {
		t.Fatalf("warning source = %#v", warnings[0].Source)
	}

	warnings = []ir.Warning{}
	if _, err := CompileWithOptions(cfg, CompileOptions{
		Warnings:                             &warnings,
		SuppressSecretFileDeprecationWarning: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("suppressed warnings = %#v, want none", warnings)
	}
}

func TestCompileFileAndSecretPathAttribute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte("not-a-real-secret-token"), 0644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(`
host "server1" {
  files {
    file "app_config" {
      path    = "/etc/app/config"
      content = "ok"
    }
  }

  secrets {
    file "app_token" {
      path   = "/etc/app/token"
      source = "token.txt"
    }
  }
}
`, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileWithOptions(cfg, CompileOptions{SuppressSecretFileDeprecationWarning: true})
	if err != nil {
		t.Fatal(err)
	}
	host := program.Hosts[0]
	if _, ok := host.Files.Files["/etc/app/config"]; !ok {
		t.Fatalf("files = %#v", host.Files.Files)
	}
	if _, ok := host.Secrets.Files["/etc/app/token"]; !ok {
		t.Fatalf("secrets = %#v", host.Secrets.Files)
	}
}

func TestCompileRejectsDuplicateExplicitFilePath(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  files {
    file "one" {
      path    = "/etc/app/config"
      content = "one"
    }

    file "two" {
      path    = "/etc/app/config"
      content = "two"
    }
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `file path "/etc/app/config" conflicts with file declared`) {
		t.Fatalf("error = %v, want duplicate file path rejection", err)
	}
}
