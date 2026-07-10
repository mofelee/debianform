package merge

import (
	"reflect"
	"strings"
	"testing"
)

func TestCompileAPTRepository(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  apt {
    repository "tools" {
      uris          = ["https://repo.example/debian"]
      suites        = ["trixie"]
      components    = ["main"]
      architectures = ["amd64"]

      signing_key {
        content = "-----BEGIN PGP PUBLIC KEY BLOCK-----\nexample\n-----END PGP PUBLIC KEY BLOCK-----\n"
      }
    }
  }

  packages {
    package "example-tool" {
      repositories = ["tools"]
    }
  }
}
`)

	host := program.Hosts[0]
	repository, ok := host.APT.Repositories["tools"]
	if !ok {
		t.Fatalf("apt repository tools was not compiled")
	}
	if repository.Name != "tools" || repository.Ensure != "present" {
		t.Fatalf("repository = %#v", repository)
	}
	if !reflect.DeepEqual(repository.URIs, []string{"https://repo.example/debian"}) {
		t.Fatalf("repository uris = %#v", repository.URIs)
	}
	if !reflect.DeepEqual(repository.Architectures, []string{"amd64"}) {
		t.Fatalf("repository architectures = %#v", repository.Architectures)
	}
	if repository.SigningKey == nil {
		t.Fatalf("signing key was not compiled")
	}
	if repository.SigningKey.Path != "/etc/apt/keyrings/tools.asc" {
		t.Fatalf("signing key path = %q", repository.SigningKey.Path)
	}
	if got := host.Packages.Install[0].Repositories; !reflect.DeepEqual(got, []string{"tools"}) {
		t.Fatalf("package repositories = %#v", got)
	}
}

func TestCompileNftablesDefaults(t *testing.T) {
	program := compileInline(t, `
host "edge1" {
  nftables {
    enable = true

    main {
      content = "flush ruleset\n"
    }

    file "20-services" {
      content = "add rule inet filter input tcp dport 443 accept\n"
    }
  }
}
`)

	host := program.Hosts[0]
	if host.Nftables.Enable == nil || !*host.Nftables.Enable {
		t.Fatalf("nftables enable = %#v, want true", host.Nftables.Enable)
	}
	if host.Nftables.Main == nil {
		t.Fatal("nftables main missing")
	}
	if host.Nftables.Main.Path != "/etc/nftables.conf" || !host.Nftables.Main.Validate || !host.Nftables.Main.Activate {
		t.Fatalf("nftables main defaults = %#v", host.Nftables.Main)
	}
	snippet := host.Nftables.Files["20-services"]
	if snippet.Path != "/etc/nftables.d/20-services.nft" || snippet.Owner != "root" || snippet.Group != "root" || snippet.Mode != "0644" {
		t.Fatalf("nftables snippet defaults = %#v", snippet)
	}
}

func TestCompileRejectsInvalidAPTRepository(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "missing referenced repository",
			hcl: `
host "server1" {
  packages {
    package "example-tool" {
      repositories = ["missing"]
    }
  }
}
`,
			want: `references missing apt.repository "missing"`,
		},
		{
			name: "url key without sha",
			hcl: `
host "server1" {
  apt {
    repository "tools" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        url = "https://repo.example/key.asc"
      }
    }
  }
}
`,
			want: "signing key url requires sha256",
		},
		{
			name: "invalid sha",
			hcl: `
host "server1" {
  apt {
    repository "tools" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        url    = "https://repo.example/key.asc"
        sha256 = "not-a-sha"
      }
    }
  }
}
`,
			want: "sha256 must be a 64 character hex string",
		},
		{
			name: "absent repository reference",
			hcl: `
host "server1" {
  apt {
    repository "tools" {
      ensure = "absent"
    }
  }

  packages {
    package "example-tool" {
      repositories = ["tools"]
    }
  }
}
`,
			want: `references absent apt.repository "tools"`,
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

func TestCompileAPTSourceFile(t *testing.T) {
	program := compileInline(t, `
host "server1" {
  apt {
    source_file "main" {
      path       = "/etc/apt/sources.list"
      content    = "deb https://mirrors.aliyun.com/debian/ trixie main\n"
      on_destroy = "restore"
    }
  }
}
`)

	got := program.Hosts[0].APT.SourceFiles["main"]
	if got.Path != "/etc/apt/sources.list" {
		t.Fatalf("path = %q", got.Path)
	}
	if got.Content != "deb https://mirrors.aliyun.com/debian/ trixie main\n" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.OnDestroy != "restore" {
		t.Fatalf("on_destroy = %q, want restore", got.OnDestroy)
	}
	if got.Owner != "root" || got.Group != "root" || got.Mode != "0644" {
		t.Fatalf("metadata = %s:%s %s, want root:root 0644", got.Owner, got.Group, got.Mode)
	}
}

func TestCompileAPTRepositoryHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/apt-repository.dbf.hcl", "../testdata/hostspec/apt-repository.golden.json")
}
