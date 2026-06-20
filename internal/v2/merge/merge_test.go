package merge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func TestCompileMergesImportsListsMapsAndScalars(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  packages {
    install = ["curl", "vim"]
  }

  kernel {
    sysctl = {
      "net.core.default_qdisc" = "fq"
    }
  }

  system {
    timezone = "UTC"
  }
}

profile "bbr" {
  imports = [profile.base]

  packages {
    install = ["curl", "htop"]
  }

  kernel {
    sysctl = {
      "net.core.default_qdisc"          = "fq_codel"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  system {
    timezone = "Asia/Tokyo"
  }
}

host "server1" {
  imports = [profile.bbr]

  packages {
    install = ["sudo"]
  }
}
`)

	host := program.Hosts[0]
	gotPackages := packageNames(host.Packages.Install)
	wantPackages := []string{"curl", "vim", "htop", "sudo"}
	if !reflect.DeepEqual(gotPackages, wantPackages) {
		t.Fatalf("packages = %#v, want %#v", gotPackages, wantPackages)
	}
	if got := host.Kernel.Sysctl["net.core.default_qdisc"].Value; got != "fq_codel" {
		t.Fatalf("default_qdisc = %q, want fq_codel", got)
	}
	if got := host.Kernel.Sysctl["net.ipv4.tcp_congestion_control"].Value; got != "bbr" {
		t.Fatalf("tcp_congestion_control = %q, want bbr", got)
	}
	if host.System.Timezone != "Asia/Tokyo" {
		t.Fatalf("timezone = %q, want Asia/Tokyo", host.System.Timezone)
	}
}

func TestCompileMergeModifiers(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  packages {
    install = ["curl", "vim"]
  }

  kernel {
    sysctl = {
      keep   = "yes"
      remove = "old"
    }
  }
}

profile "ordered" {
  imports = [profile.base]

  packages {
    install = before(["ca-certificates"])
  }
}

profile "forced" {
  packages {
    install = force(["nftables"])
  }
}

host "server1" {
  imports = [profile.ordered, profile.forced]

  packages {
    install = after(["sudo"])
  }

  kernel {
    sysctl = {
      remove = unset()
    }
  }
}
`)

	host := program.Hosts[0]
	gotPackages := packageNames(host.Packages.Install)
	wantPackages := []string{"nftables", "sudo"}
	if !reflect.DeepEqual(gotPackages, wantPackages) {
		t.Fatalf("packages = %#v, want %#v", gotPackages, wantPackages)
	}
	if _, exists := host.Kernel.Sysctl["remove"]; exists {
		t.Fatalf("sysctl remove should have been unset")
	}
	if got := host.Kernel.Sysctl["keep"].Value; got != "yes" {
		t.Fatalf("sysctl keep = %q, want yes", got)
	}
}

func TestCompileBeforeAfterWithoutForce(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  packages {
    install = ["vim"]
  }
}

profile "ordered" {
  imports = [profile.base]

  packages {
    install = before(["curl"])
  }
}

host "server1" {
  imports = [profile.ordered]

  packages {
    install = after(["sudo"])
  }
}
`)

	gotPackages := packageNames(program.Hosts[0].Packages.Install)
	wantPackages := []string{"curl", "vim", "sudo"}
	if !reflect.DeepEqual(gotPackages, wantPackages) {
		t.Fatalf("packages = %#v, want %#v", gotPackages, wantPackages)
	}
}

func TestCompileRejectsProfileImportCycle(t *testing.T) {
	cfg := parseInline(t, `
profile "a" {
  imports = [profile.b]
}

profile "b" {
  imports = [profile.a]
}

host "server1" {
  imports = [profile.a]
}
`)

	_, err := Compile(cfg)
	if err == nil || !strings.Contains(err.Error(), "profile.a -> profile.b -> profile.a") {
		t.Fatalf("Compile() error = %v, want profile import cycle", err)
	}
}

func TestCompileBBRHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-bbr.dbf.hcl", "../testdata/hostspec/v2-bbr.golden.json")
}

func TestCompileProfileMergeHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/v2-profile-merge.dbf.hcl", "../testdata/hostspec/v2-profile-merge.golden.json")
}

func assertHostSpecGolden(t *testing.T, fixture string, golden string) {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(program.Hosts))
	}
	data, err := json.MarshalIndent(program.Hosts[0], "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("HostSpec golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func compileInline(t *testing.T, content string) *ir.Program {
	t.Helper()

	cfg := parseInline(t, content)
	program, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func parseInline(t *testing.T, content string) *parser.Config {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parser.ParseFiles([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func packageNames(items []ir.PackageItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
