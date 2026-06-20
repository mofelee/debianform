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

func TestCompileMergesLabeledPackageBlocks(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  packages {
    package "bird2" {
      repositories = ["base_repo"]
    }
  }
}

host "server1" {
  imports = [profile.base]

  packages {
    package "bird2" {
      repositories = ["host_repo"]
    }
  }
}
`)

	packages := program.Hosts[0].Packages.Install
	if len(packages) != 1 {
		t.Fatalf("package count = %d, want 1", len(packages))
	}
	if packages[0].Name != "bird2" {
		t.Fatalf("package name = %q, want bird2", packages[0].Name)
	}
	wantRepositories := []string{"base_repo", "host_repo"}
	if !reflect.DeepEqual(packages[0].Repositories, wantRepositories) {
		t.Fatalf("repositories = %#v, want %#v", packages[0].Repositories, wantRepositories)
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

func TestCompileRejectsProfileHostOnlyFields(t *testing.T) {
	_, err := parseOrCompileInline(t, `
profile "bad" {
  system {
    hostname = "bad"
  }
}

host "server1" {
  imports = [profile.bad]
}
`)
	if err == nil || !strings.Contains(err.Error(), "profile.bad.system.hostname is host-only") {
		t.Fatalf("error = %v, want host-only field error", err)
	}
}

func TestCompileRejectsDuplicatePackage(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  packages {
    install = ["curl", "curl"]
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `duplicate package "curl"`) || !strings.Contains(err.Error(), "packages.install[1]") {
		t.Fatalf("error = %v, want duplicate package with source path", err)
	}
}

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

func parseOrCompileInline(t *testing.T, content string) (*ir.Program, error) {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return parseOrCompileFiles([]string{file})
}

func parseOrCompileFiles(files []string) (*ir.Program, error) {
	cfg, err := parser.ParseFiles(files)
	if err != nil {
		return nil, err
	}
	return Compile(cfg)
}

func packageNames(items []ir.PackageItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
