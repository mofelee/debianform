package merge

import (
	"reflect"
	"strings"
	"testing"
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
    locale   = "C.UTF-8"
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
	if !host.System.TimezoneSet {
		t.Fatalf("timezone should be marked explicit")
	}
	if host.System.Locale != "C.UTF-8" {
		t.Fatalf("locale = %q, want C.UTF-8", host.System.Locale)
	}
	if !host.System.LocaleSet {
		t.Fatalf("locale should be marked explicit")
	}
}

func TestCompileAllowsHostToUnsetProfileSystemTimezoneAndLocale(t *testing.T) {
	program := compileInline(t, `
profile "base" {
  system {
    timezone = "UTC"
    locale   = "C.UTF-8"
  }
}

host "server1" {
  imports = [profile.base]

  system {
    timezone = unset()
    locale   = unset()
  }
}
`)

	host := program.Hosts[0]
	if host.System.TimezoneSet || host.System.Timezone != "" {
		t.Fatalf("timezone = %q set=%v, want unset", host.System.Timezone, host.System.TimezoneSet)
	}
	if host.System.LocaleSet || host.System.Locale != "" {
		t.Fatalf("locale = %q set=%v, want unset", host.System.Locale, host.System.LocaleSet)
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
  apt {
    repository "base_repo" {
      uris       = ["https://repo.example/base"]
      suites     = ["trixie"]
      components = ["main"]
    }
  }

  packages {
    package "bird2" {
      repositories = ["base_repo"]
    }
  }
}

host "server1" {
  imports = [profile.base]

  apt {
    repository "host_repo" {
      uris       = ["https://repo.example/host"]
      suites     = ["trixie"]
      components = ["main"]
    }
  }

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

func TestCompileRejectsNonRootSSHUser(t *testing.T) {
	_, err := parseOrCompileInline(t, `
host "server1" {
  ssh {
    user = "debian"
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), `ssh.user must be "root" or omitted`) {
		t.Fatalf("Compile error = %v, want non-root ssh.user rejection", err)
	}
}

func TestCompileBBRHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/bbr.dbf.hcl", "../testdata/hostspec/bbr.golden.json")
}

func TestCompileProfileMergeHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/profile-merge.dbf.hcl", "../testdata/hostspec/profile-merge.golden.json")
}

func TestCompileFoundationHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../testdata/fixtures/foundation.dbf.hcl", "../testdata/hostspec/foundation.golden.json")
}

func TestCompilePlatformHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../testdata/fixtures/platform.dbf.hcl", "../testdata/hostspec/platform.golden.json")
}

func TestCompileBIRD2HostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/bird2.dbf.hcl", "../testdata/hostspec/bird2.golden.json")
}
