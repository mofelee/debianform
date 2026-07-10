package merge

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
)

func TestCompileDockerDefaults(t *testing.T) {
	program := compileInline(t, `
host "docker1" {
  docker {
    enable = true
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker == nil || !docker.Enable {
		t.Fatalf("docker = %#v, want enabled spec", docker)
	}
	if docker.Package.Source != "official" || docker.Package.Channel != "stable" || docker.Package.RemoveConflicts != "auto" {
		t.Fatalf("docker package defaults = %#v", docker.Package)
	}
	if docker.Package.RepositoryURL != ir.DockerOfficialRepositoryURL || docker.Package.GPGURL != ir.DockerOfficialGPGURL || docker.Package.GPGSHA256 != ir.DockerOfficialGPGSHA256 {
		t.Fatalf("docker package official URLs = %#v, want official defaults", docker.Package)
	}
	if !docker.Service.Enable || docker.Service.State != "running" || docker.Service.Name != "docker.service" {
		t.Fatalf("docker service defaults = %#v", docker.Service)
	}
}

func TestCompileDockerOfficialMirrorURLs(t *testing.T) {
	program := compileInline(t, `
host "docker1" {
  docker {
    enable = true

    package {
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
      gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker.Package.RepositoryURL != "https://mirrors.aliyun.com/docker-ce/linux/debian" {
		t.Fatalf("repository_url = %q", docker.Package.RepositoryURL)
	}
	if docker.Package.GPGURL != "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg" {
		t.Fatalf("gpg_url = %q", docker.Package.GPGURL)
	}
	if docker.Package.GPGSHA256 != "" {
		t.Fatalf("gpg_sha256 = %q, want empty for custom gpg_url without explicit hash", docker.Package.GPGSHA256)
	}
}

func TestCompileDockerOfficialMirrorGPGSHA256(t *testing.T) {
	program := compileInline(t, `
host "docker1" {
  docker {
    enable = true

    package {
      gpg_url    = "https://mirror.example/docker/gpg"
      gpg_sha256 = "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"
    }
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker.Package.GPGURL != "https://mirror.example/docker/gpg" {
		t.Fatalf("gpg_url = %q", docker.Package.GPGURL)
	}
	if docker.Package.GPGSHA256 != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("gpg_sha256 = %q, want normalized hash", docker.Package.GPGSHA256)
	}
}

func TestCompileDockerProfileOverride(t *testing.T) {
	program := compileInline(t, `
profile "docker_base" {
  docker {
    enable = true

    package {
      source = "official"
    }

    service {
      state = "running"
    }
  }
}

host "docker1" {
  imports = [profile.docker_base]

  docker {
    package {
      source = "none"
    }

    service {
      state = "stopped"
    }
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker == nil || !docker.Enable {
		t.Fatalf("docker = %#v, want enabled spec", docker)
	}
	if docker.Package.Source != "none" {
		t.Fatalf("package source = %q, want none", docker.Package.Source)
	}
	if docker.Service.State != "stopped" {
		t.Fatalf("service state = %q, want stopped", docker.Service.State)
	}
}

func TestCompileDockerComposeDefaults(t *testing.T) {
	program := compileInline(t, `
host "compose1" {
  docker {
    enable = true

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "100m"
        }
      }
    }

    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      env_file "app" {
        path    = "/opt/app/.env"
        content = "TOKEN=example\n"
      }
    }
  }
}
`)
	docker := program.Hosts[0].Docker
	if docker == nil || docker.Daemon == nil {
		t.Fatalf("docker daemon missing: %#v", docker)
	}
	if docker.Daemon.Settings["log-driver"] != "json-file" {
		t.Fatalf("daemon settings = %#v", docker.Daemon.Settings)
	}
	compose := docker.Composes["app"]
	if !compose.Enable || compose.State != "running" || compose.Project != "app" {
		t.Fatalf("compose defaults = %#v", compose)
	}
	if compose.File == nil || compose.File.Owner != "root" || compose.File.Group != "root" || compose.File.Mode != "0644" {
		t.Fatalf("compose file defaults = %#v", compose.File)
	}
	env := compose.EnvFiles["app"]
	if env.Owner != "root" || env.Group != "root" || env.Mode != "0600" || !env.Sensitive {
		t.Fatalf("env file defaults = %#v", env)
	}
	if !reflect.DeepEqual(compose.After, []string{"docker.service", "network-online.target"}) {
		t.Fatalf("after = %#v", compose.After)
	}
	if !reflect.DeepEqual(compose.WantedBy, []string{"multi-user.target"}) {
		t.Fatalf("wanted_by = %#v", compose.WantedBy)
	}
}

func TestCompileDockerHostSpecGoldens(t *testing.T) {
	assertHostSpecGolden(t, "../../../examples/docker-minimal.dbf.hcl", "../testdata/hostspec/docker-minimal.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-daemon.dbf.hcl", "../testdata/hostspec/docker-daemon.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-compose.dbf.hcl", "../testdata/hostspec/docker-compose.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-package-sources.dbf.hcl", "../testdata/hostspec/docker-package-sources.golden.json")
	assertHostSpecGolden(t, "../testdata/fixtures/docker-package-source-none.dbf.hcl", "../testdata/hostspec/docker-package-source-none.golden.json")
	assertHostSpecGolden(t, "../testdata/fixtures/docker-package-source-custom.dbf.hcl", "../testdata/hostspec/docker-package-source-custom.golden.json")
	assertHostSpecGolden(t, "../../../examples/docker-users.dbf.hcl", "../testdata/hostspec/docker-users.golden.json")
}

func TestCompileRejectsDockerInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want string
	}{
		{
			name: "invalid package source",
			hcl: `
host "docker1" {
  docker {
    enable = true

    package {
      source = "invalid"
    }
  }
}
`,
			want: "must be official, debian, none, or custom",
		},
		{
			name: "invalid remove conflicts",
			hcl: `
host "docker1" {
  docker {
    package {
      remove_conflicts = "sometimes"
    }
  }
}
`,
			want: "must be auto, true, or false",
		},
		{
			name: "repository url with debian source",
			hcl: `
host "docker1" {
  docker {
    package {
      source         = "debian"
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
    }
  }
}
`,
			want: `repository_url is only valid when source = "official"`,
		},
		{
			name: "gpg url with custom source",
			hcl: `
host "docker1" {
  docker {
    package {
      source  = "custom"
      gpg_url = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
`,
			want: `gpg_url is only valid when source = "official"`,
		},
		{
			name: "invalid docker gpg sha",
			hcl: `
host "docker1" {
  docker {
    package {
      gpg_sha256 = "not-a-sha"
    }
  }
}
`,
			want: "gpg_sha256 must be a 64 character hex string",
		},
		{
			name: "empty docker repository url",
			hcl: `
host "docker1" {
  docker {
    package {
      repository_url = ""
    }
  }
}
`,
			want: "repository_url must be non-empty",
		},
		{
			name: "empty docker user",
			hcl: `
host "docker1" {
  docker {
    users = [""]
  }
}
`,
			want: "docker users entries must be non-empty strings",
		},
		{
			name: "missing compose directory",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose directory must be absolute and non-empty",
		},
		{
			name: "compose content and source",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
        source  = "compose.yaml"
      }
    }
  }
}
`,
			want: "requires exactly one of content or source",
		},
		{
			name: "daemon settings sensitive",
			hcl: `
variable "mirror" {
  type      = string
  default   = "https://mirror.example.com"
  sensitive = true
}

host "docker1" {
  docker {
    daemon {
      settings = {
        "registry-mirrors" = [var.mirror]
      }
    }
  }
}
`,
			want: "docker daemon settings cannot contain sensitive",
		},
		{
			name: "compose file path conflict",
			hcl: `
host "compose1" {
  files {
    file "/opt/app/compose.yaml" {
      content = "plain\n"
    }
  }

  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose file path",
		},
		{
			name: "compose env file path conflicts with systemd unit",
			hcl: `
host "compose1" {
  systemd {
    unit "app.env" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }

  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      env_file "app" {
        path    = "/etc/systemd/system/app.env"
        content = "TOKEN=example\n"
      }
    }
  }
}
`,
			want: "docker compose env_file path",
		},
		{
			name: "invalid compose project",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"
      project   = "bad project"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose project",
		},
		{
			name: "invalid compose service name",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }

      service {
        name = "/bad"
      }
    }
  }
}
`,
			want: "docker compose service name",
		},
		{
			name: "empty compose after entry",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"
      after     = [""]

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "after entries must be non-empty strings",
		},
		{
			name: "empty compose wanted_by entry",
			hcl: `
host "compose1" {
  docker {
    compose "app" {
      directory = "/opt/app"
      wanted_by = [""]

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "wanted_by entries must be non-empty strings",
		},
		{
			name: "compose generated unit path conflicts with systemd unit",
			hcl: `
host "compose1" {
  systemd {
    unit "debianform-compose-app.service" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }

  docker {
    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`,
			want: "docker compose systemd unit path",
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
