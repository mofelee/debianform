package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/merge"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestCompileBBRResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/bbr.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/bbr.golden.json", got)

	dependsOn := dependsOnFor(resourceGraph, `host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`)
	want := []string{`host.bbr1.kernel.module["tcp_bbr"]`}
	if strings.Join(dependsOn, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tcp_congestion_control depends_on = %#v, want %#v", dependsOn, want)
	}
}

func TestCompileFoundationResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/foundation.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/foundation.golden.json", got)

	userDeps := dependsOnFor(resourceGraph, `host.foundation1.users.user["deploy"]`)
	if !containsString(userDeps, `host.foundation1.groups.group["deploy"]`) {
		t.Fatalf("user deps = %#v, want deploy group dependency", userDeps)
	}
	directoryDeps := dependsOnFor(resourceGraph, `host.foundation1.directories.directory["/etc/myapp"]`)
	if len(directoryDeps) != 0 {
		t.Fatalf("root-owned directory deps = %#v, want none", directoryDeps)
	}
	serviceDeps := dependsOnFor(resourceGraph, `host.foundation1.services.service["myapp"]`)
	for _, want := range []string{
		`host.foundation1.packages.install["curl"]`,
		`host.foundation1.systemd.unit["myapp.service"]`,
		`host.foundation1.systemd.daemon_reload`,
	} {
		if !containsString(serviceDeps, want) {
			t.Fatalf("service deps = %#v, want %q", serviceDeps, want)
		}
	}
}

func TestSecretFileCompilesAsFileProviderWithStableAddress(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/foundation.dbf.hcl")
	address := `host.foundation1.secrets.file["/etc/myapp/token"]`
	node := nodeFor(resourceGraph, address)
	if node == nil {
		t.Fatalf("secret file node %s was not found", address)
	}
	if node.Kind != "secret" {
		t.Fatalf("kind = %q, want secret", node.Kind)
	}
	if node.Address != address {
		t.Fatalf("address = %q, want %q", node.Address, address)
	}
	if node.ProviderType != "file" || node.ProviderAddress != "file.foundation1__etc_myapp_token" {
		t.Fatalf("provider = %s %s, want file provider for preserved secret address", node.ProviderType, node.ProviderAddress)
	}
	if node.Desired["sensitive"] != true {
		t.Fatalf("desired sensitive = %#v, want true", node.Desired["sensitive"])
	}
	if node.Desired["source_path"] == "" || node.ProviderPayload["source_path"] != node.Desired["source_path"] {
		t.Fatalf("source_path desired/payload mismatch: desired=%#v payload=%#v", node.Desired, node.ProviderPayload)
	}
	if _, ok := node.Desired["content"]; ok {
		t.Fatalf("secret desired unexpectedly contains content: %#v", node.Desired)
	}
}

func TestCompileAPTRepositoryResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/apt-repository.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/apt-repository.golden.json", got)

	packageDeps := dependsOnFor(resourceGraph, `host.apt1.packages.install["example-tool"]`)
	for _, want := range []string{
		`host.apt1.apt.repository["example_tools"]`,
		`host.apt1.apt.cache_refresh`,
	} {
		if !containsString(packageDeps, want) {
			t.Fatalf("example-tool deps = %#v, want %q", packageDeps, want)
		}
	}
}

func TestCompileDockerMinimalResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/docker-minimal.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/docker-minimal.golden.json", got)

	keyAddress := `host.docker1.docker.apt.signing_key["docker-official"]`
	repositoryAddress := `host.docker1.docker.apt.repository["docker-official"]`
	refreshAddress := `host.docker1.apt.cache_refresh`
	conflictAddress := `host.docker1.docker.package_conflicts`
	serviceAddress := `host.docker1.docker.service["docker"]`
	packageAddresses := []string{
		`host.docker1.docker.package["docker-ce"]`,
		`host.docker1.docker.package["docker-ce-cli"]`,
		`host.docker1.docker.package["containerd.io"]`,
		`host.docker1.docker.package["docker-buildx-plugin"]`,
		`host.docker1.docker.package["docker-compose-plugin"]`,
	}

	repository := nodeFor(resourceGraph, repositoryAddress)
	if repository == nil {
		t.Fatalf("docker repository node missing")
	}
	if !containsString(repository.DependsOn, keyAddress) {
		t.Fatalf("docker repository deps = %#v, want signing key", repository.DependsOn)
	}
	content, _ := repository.Desired["content"].(string)
	for _, want := range []string{
		"URIs: https://download.docker.com/linux/debian",
		"Suites: trixie",
		"Components: stable",
		"Architectures: amd64",
		"Signed-By: /etc/apt/keyrings/docker.asc",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("docker repository content missing %q:\n%s", want, content)
		}
	}

	refresh := operationFor(resourceGraph, refreshAddress)
	if refresh == nil {
		t.Fatalf("apt cache refresh operation missing")
	}
	for _, want := range []string{keyAddress, repositoryAddress} {
		if !containsString(refresh.DependsOn, want) || !containsString(refresh.TriggeredBy, want) {
			t.Fatalf("refresh deps=%#v triggered_by=%#v, want %q", refresh.DependsOn, refresh.TriggeredBy, want)
		}
	}
	for _, packageAddress := range packageAddresses {
		deps := dependsOnFor(resourceGraph, packageAddress)
		for _, want := range []string{repositoryAddress, refreshAddress, conflictAddress} {
			if !containsString(deps, want) {
				t.Fatalf("%s deps = %#v, want %q", packageAddress, deps, want)
			}
		}
	}
	conflicts := nodeFor(resourceGraph, conflictAddress)
	if conflicts == nil || conflicts.Kind != "docker_package_conflicts" {
		t.Fatalf("docker conflict node = %#v", conflicts)
	}
	if conflicts.Desired["remove_conflicts"] != "auto" {
		t.Fatalf("docker conflict desired = %#v, want remove_conflicts auto", conflicts.Desired)
	}
	serviceDeps := dependsOnFor(resourceGraph, serviceAddress)
	for _, want := range packageAddresses {
		if !containsString(serviceDeps, want) {
			t.Fatalf("docker service deps = %#v, want %q", serviceDeps, want)
		}
	}

	order := topologicalOrder(t, resourceGraph)
	assertBefore(t, order, keyAddress, repositoryAddress)
	assertBefore(t, order, repositoryAddress, refreshAddress)
	for _, packageAddress := range packageAddresses {
		assertBefore(t, order, refreshAddress, packageAddress)
		assertBefore(t, order, conflictAddress, packageAddress)
		assertBefore(t, order, packageAddress, serviceAddress)
	}
}

func TestCompileDockerOfficialMirrorResourceGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "docker1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    package {
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
      gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
`)

	key := nodeFor(resourceGraph, `host.docker1.docker.apt.signing_key["docker-official"]`)
	if key == nil {
		t.Fatalf("docker mirror signing key node missing")
	}
	if key.Desired["url"] != "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg" {
		t.Fatalf("docker mirror key desired = %#v, want custom gpg_url", key.Desired)
	}
	if _, exists := key.Desired["sha256"]; exists {
		t.Fatalf("docker mirror key desired = %#v, want no official sha256 for custom gpg_url", key.Desired)
	}

	repository := nodeFor(resourceGraph, `host.docker1.docker.apt.repository["docker-official"]`)
	if repository == nil {
		t.Fatalf("docker mirror repository node missing")
	}
	content, _ := repository.Desired["content"].(string)
	if !strings.Contains(content, "URIs: https://mirrors.aliyun.com/docker-ce/linux/debian") {
		t.Fatalf("docker mirror repository content missing custom URL:\n%s", content)
	}
}

func TestCompileDockerOfficialMirrorResourceGraphWithGPGSHA256(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "docker1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    package {
      gpg_url    = "https://mirror.example/docker/gpg"
      gpg_sha256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
    }
  }
}
`)

	key := nodeFor(resourceGraph, `host.docker1.docker.apt.signing_key["docker-official"]`)
	if key == nil {
		t.Fatalf("docker mirror signing key node missing")
	}
	if key.Desired["url"] != "https://mirror.example/docker/gpg" {
		t.Fatalf("docker mirror key desired = %#v, want custom gpg_url", key.Desired)
	}
	if key.Desired["sha256"] != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("docker mirror key desired = %#v, want custom gpg_sha256", key.Desired)
	}
}

func TestCompileDockerPackageSourcesResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/docker-package-sources.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/docker-package-sources.golden.json", got)

	for _, unexpected := range []string{
		`host.docker-sources1.docker.apt.signing_key["docker-official"]`,
		`host.docker-sources1.docker.apt.repository["docker-official"]`,
		`host.docker-sources1.docker.package_conflicts`,
		`host.docker-sources1.apt.cache_refresh`,
	} {
		if nodeFor(resourceGraph, unexpected) != nil || operationFor(resourceGraph, unexpected) != nil {
			t.Fatalf("package source debian generated %s", unexpected)
		}
	}
	for _, want := range []string{
		`host.docker-sources1.docker.package["docker.io"]`,
		`host.docker-sources1.docker.package["docker-compose-plugin"]`,
	} {
		if nodeFor(resourceGraph, want) == nil {
			t.Fatalf("package source debian missing %s", want)
		}
		if !containsString(dependsOnFor(resourceGraph, `host.docker-sources1.docker.service["docker"]`), want) {
			t.Fatalf("docker service deps = %#v, want %q", dependsOnFor(resourceGraph, `host.docker-sources1.docker.service["docker"]`), want)
		}
	}
}

func TestCompileDockerPackageSourceNoneResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/docker-package-source-none.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/docker-package-source-none.golden.json", got)

	for _, unexpected := range []string{
		`host.docker-none1.docker.apt.repository["docker-official"]`,
		`host.docker-none1.docker.package["docker-ce"]`,
		`host.docker-none1.docker.package_conflicts`,
	} {
		if nodeFor(resourceGraph, unexpected) != nil {
			t.Fatalf("package source none generated %s", unexpected)
		}
	}
	if nodeFor(resourceGraph, `host.docker-none1.docker.daemon.file["/etc/docker/daemon.json"]`) == nil {
		t.Fatalf("package source none did not generate daemon file")
	}
	if nodeFor(resourceGraph, `host.docker-none1.docker.service["docker"]`) == nil {
		t.Fatalf("package source none did not generate docker service")
	}
}

func TestCompileDockerPackageSourceCustomResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/docker-package-source-custom.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/docker-package-source-custom.golden.json", got)

	for _, unexpected := range []string{
		`host.docker-custom1.docker.apt.repository["docker-official"]`,
		`host.docker-custom1.docker.package["docker-ce"]`,
		`host.docker-custom1.docker.package_conflicts`,
	} {
		if nodeFor(resourceGraph, unexpected) != nil {
			t.Fatalf("package source custom generated %s", unexpected)
		}
	}
	if nodeFor(resourceGraph, `host.docker-custom1.docker.service["docker"]`) == nil {
		t.Fatalf("package source custom did not generate docker service")
	}
	if nodeFor(resourceGraph, `host.docker-custom1.docker.compose["app"].project`) == nil {
		t.Fatalf("package source custom did not generate compose project")
	}
}

func TestCompileDockerDaemonResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/docker-daemon.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/docker-daemon.golden.json", got)

	daemonAddress := `host.docker-daemon1.docker.daemon.file["/etc/docker/daemon.json"]`
	serviceAddress := `host.docker-daemon1.docker.service["docker"]`
	restartAddress := `host.docker-daemon1.docker.daemon.restart`
	packageAddress := `host.docker-daemon1.docker.package["docker-ce"]`

	daemon := nodeFor(resourceGraph, daemonAddress)
	if daemon == nil {
		t.Fatalf("docker daemon file node missing")
	}
	if daemon.ProviderType != "file" || daemon.ProviderAddress != "file.docker_daemon1__etc_docker_daemon_json" {
		t.Fatalf("daemon provider = %s %s, want file provider", daemon.ProviderType, daemon.ProviderAddress)
	}
	content, _ := daemon.Desired["content"].(string)
	for _, want := range []string{
		`"log-driver": "json-file"`,
		`"max-file": "3"`,
		`"registry-mirrors": [`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("daemon content missing %q:\n%s", want, content)
		}
	}
	if !containsString(daemon.DependsOn, packageAddress) {
		t.Fatalf("daemon deps = %#v, want docker package dependency", daemon.DependsOn)
	}

	serviceDeps := dependsOnFor(resourceGraph, serviceAddress)
	if !containsString(serviceDeps, daemonAddress) {
		t.Fatalf("docker service deps = %#v, want daemon file dependency", serviceDeps)
	}
	restart := operationFor(resourceGraph, restartAddress)
	if restart == nil {
		t.Fatalf("docker daemon restart operation missing")
	}
	if restart.CommandPreview != "systemctl restart docker.service" {
		t.Fatalf("restart command = %q", restart.CommandPreview)
	}
	for _, want := range []string{daemonAddress, serviceAddress} {
		if !containsString(restart.DependsOn, want) {
			t.Fatalf("restart deps = %#v, want %q", restart.DependsOn, want)
		}
	}
	if !containsString(restart.TriggeredBy, daemonAddress) {
		t.Fatalf("restart triggered_by = %#v, want daemon file", restart.TriggeredBy)
	}
}

func TestCompileDockerComposeResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/docker-compose.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/docker-compose.golden.json", got)

	directoryAddress := `host.compose1.docker.compose["app"].directory`
	composeFileAddress := `host.compose1.docker.compose["app"].file`
	envFileAddress := `host.compose1.docker.compose["app"].env_file["app"]`
	validateAddress := `host.compose1.docker.compose["app"].validate`
	unitAddress := `host.compose1.docker.compose["app"].systemd_unit`
	daemonReloadAddress := `host.compose1.docker.compose["app"].daemon_reload`
	composeServiceAddress := `host.compose1.docker.compose["app"].service`
	projectAddress := `host.compose1.docker.compose["app"].project`
	serviceAddress := `host.compose1.docker.service["docker"]`
	packageAddress := `host.compose1.docker.package["docker-compose-plugin"]`

	directory := nodeFor(resourceGraph, directoryAddress)
	if directory == nil {
		t.Fatal("compose directory node missing")
	}
	if directory.Kind != "directory" || directory.Desired["mode"] != "0755" {
		t.Fatalf("compose directory = %#v", directory)
	}
	composeFile := nodeFor(resourceGraph, composeFileAddress)
	if composeFile == nil {
		t.Fatal("compose file node missing")
	}
	for _, want := range []string{directoryAddress, serviceAddress, packageAddress} {
		if !containsString(composeFile.DependsOn, want) {
			t.Fatalf("compose file deps = %#v, want %q", composeFile.DependsOn, want)
		}
	}
	envFile := nodeFor(resourceGraph, envFileAddress)
	if envFile == nil {
		t.Fatal("compose env file node missing")
	}
	if envFile.Desired["sensitive"] != true {
		t.Fatalf("env file desired should be sensitive: %#v", envFile.Desired)
	}
	if _, ok := envFile.Desired["content"]; ok {
		t.Fatalf("env file desired leaked content: %#v", envFile.Desired)
	}
	if envFile.ProviderPayload["content"] != "TOKEN=not-a-real-preview-secret\n" {
		t.Fatalf("env file provider payload missing content: %#v", envFile.ProviderPayload)
	}

	validate := operationFor(resourceGraph, validateAddress)
	if validate == nil {
		t.Fatal("compose validate operation missing")
	}
	if validate.CommandPreview != "docker compose -p app -f /opt/app/compose.yaml config" {
		t.Fatalf("validate command = %q", validate.CommandPreview)
	}
	for _, want := range []string{composeFileAddress, envFileAddress} {
		if !containsString(validate.DependsOn, want) || !containsString(validate.TriggeredBy, want) {
			t.Fatalf("validate deps=%#v triggered_by=%#v, want %q", validate.DependsOn, validate.TriggeredBy, want)
		}
	}
	unit := nodeFor(resourceGraph, unitAddress)
	if unit == nil {
		t.Fatal("compose systemd unit node missing")
	}
	content, _ := unit.Desired["content"].(string)
	for _, want := range []string{
		"Requires=docker.service",
		"After=docker.service network-online.target",
		"WorkingDirectory=/opt/app",
		"ExecStart=/usr/bin/docker compose -p app -f /opt/app/compose.yaml up -d",
		"ExecStop=/usr/bin/docker compose -p app -f /opt/app/compose.yaml stop",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("compose systemd unit content missing %q:\n%s", want, content)
		}
	}
	if unit.Desired["name"] != "debianform-compose-app.service" || unit.Desired["path"] != "/etc/systemd/system/debianform-compose-app.service" {
		t.Fatalf("compose systemd unit desired = %#v", unit.Desired)
	}
	if !containsString(unit.DependsOn, validateAddress) {
		t.Fatalf("compose systemd unit deps = %#v, want validate", unit.DependsOn)
	}
	reload := operationFor(resourceGraph, daemonReloadAddress)
	if reload == nil {
		t.Fatal("compose daemon-reload operation missing")
	}
	if !containsString(reload.DependsOn, unitAddress) || !containsString(reload.TriggeredBy, unitAddress) {
		t.Fatalf("compose daemon-reload deps=%#v triggered_by=%#v, want unit", reload.DependsOn, reload.TriggeredBy)
	}
	service := nodeFor(resourceGraph, composeServiceAddress)
	if service == nil {
		t.Fatal("compose service node missing")
	}
	if service.Kind != "service" || service.Desired["unit"] != "debianform-compose-app.service" || service.Desired["enabled"] != true || service.Desired["state"] != "running" {
		t.Fatalf("compose service node = %#v", service)
	}
	for _, want := range []string{unitAddress, daemonReloadAddress, serviceAddress} {
		if !containsString(service.DependsOn, want) {
			t.Fatalf("compose service deps = %#v, want %q", service.DependsOn, want)
		}
	}
	project := nodeFor(resourceGraph, projectAddress)
	if project == nil {
		t.Fatal("compose project node missing")
	}
	if project.Kind != "docker_compose_project" || project.Desired["state"] != "running" || project.Desired["pull"] != "missing" {
		t.Fatalf("compose project node = %#v", project)
	}
	for _, want := range []string{validateAddress, composeServiceAddress} {
		if !containsString(project.DependsOn, want) {
			t.Fatalf("project deps = %#v, want %q", project.DependsOn, want)
		}
	}
}

func TestCompileDockerUsersResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/docker-users.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/docker-users.golden.json", got)

	groupAddress := `host.docker-users1.docker.group["docker"]`
	userAddress := `host.docker-users1.users.user["deploy"]`
	membershipAddress := `host.docker-users1.docker.user_group_membership["deploy:docker"]`

	group := nodeFor(resourceGraph, groupAddress)
	if group == nil || group.Kind != "group" || group.Desired["name"] != "docker" {
		t.Fatalf("docker group node = %#v", group)
	}
	membership := nodeFor(resourceGraph, membershipAddress)
	if membership == nil || membership.Kind != "user_group_membership" {
		t.Fatalf("docker membership node = %#v", membership)
	}
	if membership.Desired["user"] != "deploy" || membership.Desired["group"] != "docker" {
		t.Fatalf("docker membership desired = %#v", membership.Desired)
	}
	for _, want := range []string{groupAddress, userAddress} {
		if !containsString(membership.DependsOn, want) {
			t.Fatalf("docker membership deps = %#v, want %q", membership.DependsOn, want)
		}
	}
}

func TestCompileDockerUsersReusesDeclaredDockerGroup(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "docker-users1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  groups {
    group "docker" {}
  }

  docker {
    enable = true
    users  = ["deploy"]
  }
}
`)
	if nodeFor(resourceGraph, `host.docker-users1.docker.group["docker"]`) != nil {
		t.Fatalf("docker group node should reuse declared host group")
	}
	membershipDeps := dependsOnFor(resourceGraph, `host.docker-users1.docker.user_group_membership["deploy:docker"]`)
	if !containsString(membershipDeps, `host.docker-users1.groups.group["docker"]`) {
		t.Fatalf("membership deps = %#v, want declared docker group", membershipDeps)
	}
}

func TestHostOwnedPathResourcesDependOnManagedUserAndGroup(t *testing.T) {
	resourceGraph := compileGraphInlineWithFiles(t, `
host "server1" {
  groups {
    group "app" {
      system = true
    }
  }

  users {
    user "app" {
      system = true
      group  = "app"
      home   = "/var/lib/app"
      shell  = "/usr/sbin/nologin"
    }
  }

  directories {
    directory "/var/lib/app" {
      owner = "app"
      group = "app"
      mode  = "0750"
    }
  }

  files {
    file "/var/lib/app/config.env" {
      owner   = "app"
      group   = "app"
      mode    = "0640"
      content = "APP_ENV=prod\n"
    }
  }

  secrets {
    file "/var/lib/app/token" {
      owner  = "app"
      group  = "app"
      mode   = "0600"
      source = "token.txt"
    }
  }

  systemd {
    unit "app.service" {
      owner   = "app"
      group   = "app"
      mode    = "0644"
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }
}
`, map[string]string{
		"token.txt": "not-a-real-token\n",
	})
	groupAddress := `host.server1.groups.group["app"]`
	userAddress := `host.server1.users.user["app"]`
	for _, address := range []string{
		`host.server1.directories.directory["/var/lib/app"]`,
		`host.server1.files.file["/var/lib/app/config.env"]`,
		`host.server1.secrets.file["/var/lib/app/token"]`,
		`host.server1.systemd.unit["app.service"]`,
	} {
		deps := dependsOnFor(resourceGraph, address)
		for _, want := range []string{groupAddress, userAddress} {
			if !containsString(deps, want) {
				t.Fatalf("%s deps = %#v, want %q", address, deps, want)
			}
		}
	}
}

func TestCompileDockerComposePackageSourceNoneSkipsPackageDependencies(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  docker {
    enable = true

    package {
      source = "none"
    }

    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`)

	composeFile := nodeFor(resourceGraph, `host.server1.docker.compose["app"].file`)
	if composeFile == nil {
		t.Fatal("compose file node missing")
	}
	for _, dep := range composeFile.DependsOn {
		if strings.Contains(dep, `.docker.package[`) {
			t.Fatalf("source none compose file should not depend on packages: %#v", composeFile.DependsOn)
		}
	}
	if !containsString(composeFile.DependsOn, `host.server1.docker.service["docker"]`) {
		t.Fatalf("source none compose file deps = %#v, want docker service", composeFile.DependsOn)
	}
	project := nodeFor(resourceGraph, `host.server1.docker.compose["app"].project`)
	if project == nil {
		t.Fatal("compose project node missing")
	}
	if containsString(project.DependsOn, `host.server1.docker.service["docker"]`) {
		t.Fatalf("source none compose project deps = %#v, want no docker service dependency", project.DependsOn)
	}
	if !containsString(project.DependsOn, `host.server1.docker.compose["app"].validate`) {
		t.Fatalf("source none compose project deps = %#v, want validate dependency", project.DependsOn)
	}
	if !containsString(project.DependsOn, `host.server1.docker.compose["app"].service`) {
		t.Fatalf("source none compose project deps = %#v, want compose service dependency", project.DependsOn)
	}
	service := nodeFor(resourceGraph, `host.server1.docker.compose["app"].service`)
	if service == nil {
		t.Fatal("compose service node missing")
	}
	if containsString(service.DependsOn, `host.server1.docker.service["docker"]`) {
		t.Fatalf("source none compose service deps = %#v, want no docker service dependency", service.DependsOn)
	}
}

func TestCompileDockerComposeServiceNameAndStateMapping(t *testing.T) {
	for _, tt := range []struct {
		name      string
		state     string
		service   string
		wantUnit  string
		wantState string
		enabled   bool
	}{
		{name: "running default", state: "running", wantUnit: "debianform-compose-app.service", wantState: "running", enabled: true},
		{name: "stopped custom", state: "stopped", service: "custom-compose-app", wantUnit: "custom-compose-app.service", wantState: "stopped", enabled: true},
		{name: "absent", state: "absent", wantUnit: "debianform-compose-app.service", wantState: "stopped", enabled: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceBlock := ""
			if tt.service != "" {
				serviceBlock = `
      service {
        name = "` + tt.service + `"
      }
`
			}
			resourceGraph := compileGraphInline(t, `
host "server1" {
  docker {
    enable = true

    package {
      source = "none"
    }

    compose "app" {
      state     = "`+tt.state+`"
      directory = "/opt/app"
`+serviceBlock+`
      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`)

			unit := nodeFor(resourceGraph, `host.server1.docker.compose["app"].systemd_unit`)
			if unit == nil || unit.Desired["name"] != tt.wantUnit {
				t.Fatalf("compose unit = %#v, want unit %q", unit, tt.wantUnit)
			}
			service := nodeFor(resourceGraph, `host.server1.docker.compose["app"].service`)
			if service == nil {
				t.Fatal("compose service node missing")
			}
			if service.Desired["unit"] != tt.wantUnit || service.Desired["state"] != tt.wantState || service.Desired["enabled"] != tt.enabled {
				t.Fatalf("compose service desired = %#v, want unit=%q state=%q enabled=%v", service.Desired, tt.wantUnit, tt.wantState, tt.enabled)
			}
		})
	}
}

func TestCompileDockerDisabledGeneratesNoDockerNodes(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  docker {
    enable = false
  }
}
`)

	for _, node := range resourceGraph.Nodes {
		if strings.Contains(node.Address, ".docker.") {
			t.Fatalf("docker disabled generated node %s", node.Address)
		}
	}
	for _, operation := range resourceGraph.Operations {
		if strings.Contains(operation.Address, ".docker.") {
			t.Fatalf("docker disabled generated operation %s", operation.Address)
		}
	}
}

func TestCompileDockerPackageSourceDebianUsesDebianPackages(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    package {
      source = "debian"
    }
  }
}
`)

	for _, unexpected := range []string{
		`host.server1.docker.apt.signing_key["docker-official"]`,
		`host.server1.docker.apt.repository["docker-official"]`,
		`host.server1.docker.package_conflicts`,
		`host.server1.apt.cache_refresh`,
	} {
		if nodeFor(resourceGraph, unexpected) != nil || operationFor(resourceGraph, unexpected) != nil {
			t.Fatalf("package source debian generated %s", unexpected)
		}
	}
	for _, want := range []string{
		`host.server1.docker.package["docker.io"]`,
		`host.server1.docker.package["docker-compose-plugin"]`,
	} {
		if nodeFor(resourceGraph, want) == nil {
			t.Fatalf("package source debian missing %s", want)
		}
		if !containsString(dependsOnFor(resourceGraph, `host.server1.docker.service["docker"]`), want) {
			t.Fatalf("docker service deps = %#v, want %q", dependsOnFor(resourceGraph, `host.server1.docker.service["docker"]`), want)
		}
	}
}

func TestCompileDockerPackageSourceNoneAllowsDaemonAndService(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  docker {
    enable = true

    package {
      source = "none"
    }

    daemon {
      settings = {
        "log-driver" = "json-file"
      }
    }
  }
}
`)

	for _, unexpected := range []string{
		`host.server1.docker.apt.repository["docker-official"]`,
		`host.server1.docker.package["docker-ce"]`,
		`host.server1.apt.cache_refresh`,
	} {
		if nodeFor(resourceGraph, unexpected) != nil || operationFor(resourceGraph, unexpected) != nil {
			t.Fatalf("package source none generated %s", unexpected)
		}
	}
	if nodeFor(resourceGraph, `host.server1.docker.daemon.file["/etc/docker/daemon.json"]`) == nil {
		t.Fatalf("package source none did not generate daemon file")
	}
	if nodeFor(resourceGraph, `host.server1.docker.service["docker"]`) == nil {
		t.Fatalf("package source none did not generate docker service")
	}
}

func TestCompileDockerPackageSourceCustomSkipsManagedPackages(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  docker {
    enable = true

    package {
      source = "custom"
    }

    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
`)

	for _, unexpected := range []string{
		`host.server1.docker.apt.signing_key["docker-official"]`,
		`host.server1.docker.apt.repository["docker-official"]`,
		`host.server1.docker.package_conflicts`,
		`host.server1.docker.package["docker-ce"]`,
		`host.server1.docker.package["docker.io"]`,
		`host.server1.apt.cache_refresh`,
	} {
		if nodeFor(resourceGraph, unexpected) != nil || operationFor(resourceGraph, unexpected) != nil {
			t.Fatalf("package source custom generated %s", unexpected)
		}
	}
	service := nodeFor(resourceGraph, `host.server1.docker.service["docker"]`)
	if service == nil {
		t.Fatal("package source custom did not generate docker service")
	}
	if deps := service.DependsOn; len(deps) != 0 {
		t.Fatalf("package source custom docker service deps = %#v, want no managed package deps", deps)
	}
	projectDeps := dependsOnFor(resourceGraph, `host.server1.docker.compose["app"].project`)
	if containsString(projectDeps, `host.server1.docker.service["docker"]`) {
		t.Fatalf("source custom compose project deps = %#v, want no docker service dependency", projectDeps)
	}
}

func TestCompileProfileMergeResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/profile-merge.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/profile-merge.golden.json", got)

	for _, want := range []string{
		`host.merge1.packages.install["curl"]`,
		`host.merge1.packages.install["vim"]`,
		`host.merge1.packages.install["htop"]`,
		`host.merge1.packages.install["git"]`,
		`host.merge1.kernel.module["tcp_bbr"]`,
	} {
		if nodeFor(resourceGraph, want) == nil {
			t.Fatalf("resource graph missing %q", want)
		}
	}
}

func TestCompileSystemdServiceResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/systemd-service.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/systemd-service.golden.json", got)

	serviceDeps := dependsOnFor(resourceGraph, `host.service1.services.service["myapp"]`)
	for _, want := range []string{
		`host.service1.systemd.unit["myapp.service"]`,
		`host.service1.systemd.daemon_reload`,
	} {
		if !containsString(serviceDeps, want) {
			t.Fatalf("myapp service deps = %#v, want %q", serviceDeps, want)
		}
	}
}

func TestCompileUserGroupResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/user-group.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/user-group.golden.json", got)

	userDeps := dependsOnFor(resourceGraph, `host.users1.users.user["deploy"]`)
	if !containsString(userDeps, `host.users1.groups.group["deploy"]`) {
		t.Fatalf("deploy user deps = %#v, want deploy group dependency", userDeps)
	}
}

func TestCompileNftablesResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/nftables.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/nftables.golden.json", got)

	validate := operationFor(resourceGraph, "host.edge1.nftables.validate")
	if validate == nil {
		t.Fatal("nftables validate operation missing")
	}
	activate := operationFor(resourceGraph, "host.edge1.nftables.activate")
	if activate == nil {
		t.Fatal("nftables activate operation missing")
	}
	if !containsString(activate.DependsOn, "host.edge1.nftables.validate") {
		t.Fatalf("activate deps = %#v, want validate", activate.DependsOn)
	}
	for _, want := range []string{
		`host.edge1.nftables.file["main"]`,
		`host.edge1.nftables.file["10-base"]`,
		`host.edge1.nftables.file["20-services"]`,
		`host.edge1.nftables.file["30-wireguard"]`,
	} {
		if !containsString(validate.TriggeredBy, want) {
			t.Fatalf("validate triggered_by = %#v, want %q", validate.TriggeredBy, want)
		}
	}
	deps := dependsOnFor(resourceGraph, `host.edge1.nftables.file["20-services"]`)
	if !containsString(deps, `host.edge1.packages.install["nftables"]`) {
		t.Fatalf("nftables file deps = %#v, want package dependency", deps)
	}
	enableDeps := dependsOnFor(resourceGraph, "host.edge1.nftables.enable")
	if !containsString(enableDeps, "host.edge1.nftables.activate") {
		t.Fatalf("nftables enable deps = %#v, want activate dependency", enableDeps)
	}
}

func TestCompileBIRD2ResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/bird2.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/bird2.golden.json", got)

	serviceDeps := dependsOnFor(resourceGraph, `host.router1.components.bird2.services.service["bird"]`)
	if !containsString(serviceDeps, `host.router1.components.bird2.packages.install["bird2"]`) {
		t.Fatalf("bird service deps = %#v, want bird2 package", serviceDeps)
	}
	packageDeps := dependsOnFor(resourceGraph, `host.router1.components.bird2.packages.install["bird2"]`)
	for _, want := range []string{
		`host.router1.components.bird2.apt.repository["cznic_bird2"]`,
		`host.router1.apt.cache_refresh`,
	} {
		if !containsString(packageDeps, want) {
			t.Fatalf("bird2 package deps = %#v, want %q", packageDeps, want)
		}
	}
}

func TestCompileComponentBinaryResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/component-binary.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-binary.golden.json", got)

	installDeps := dependsOnFor(resourceGraph, `host.tool1.components.rclone.artifact.install["/usr/local/bin/rclone"]`)
	if !containsString(installDeps, `host.tool1.components.rclone.artifact.download["amd64"]`) {
		t.Fatalf("rclone install deps = %#v, want artifact download", installDeps)
	}
}

func TestCompileComponentSourceBuildResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/component-source-build.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-source-build.golden.json", got)

	installDeps := dependsOnFor(resourceGraph, `host.build1.components.hello_from_source.artifact.install["/usr/local/bin/hello-from-source"]`)
	foundBuild := false
	for _, dep := range installDeps {
		if strings.HasPrefix(dep, `host.build1.components.hello_from_source.artifact.build[`) {
			foundBuild = true
			break
		}
	}
	if !foundBuild {
		t.Fatalf("source install deps = %#v, want artifact build", installDeps)
	}
}

func TestCompileComponentInputsResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/component-inputs.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-inputs.golden.json", got)

	if node := nodeFor(resourceGraph, `host.input1.components.proxy.files.file["/etc/reverse-proxy/listeners.json"]`); node == nil {
		t.Fatalf("component input generated file node was not found")
	}
}

func TestCompileComponentScriptOnChangeResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/component-script-on-change.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/component-script-on-change.golden.json", got)

	fileAddress := `host.app1.components.app.files.file["/etc/managed-app/config.env"]`
	outputAddress := `host.app1.components.app.script["reload"].outputs["/etc/managed-app/rendered.env"]`
	scriptAddress := `host.app1.components.app.script["reload"]`
	if node := nodeFor(resourceGraph, fileAddress); node == nil {
		t.Fatalf("component file node %s was not found", fileAddress)
	}
	if node := nodeFor(resourceGraph, outputAddress); node == nil {
		t.Fatalf("component script output node %s was not found", outputAddress)
	} else if node.Desired["script_digest"] == "" {
		t.Fatalf("component script output missing script digest: %#v", node.Desired)
	}
	operation := operationFor(resourceGraph, scriptAddress)
	if operation == nil {
		t.Fatalf("script operation %s was not found", scriptAddress)
	}
	if operation.CommandPreview != "script reload (once)" {
		t.Fatalf("command preview = %q", operation.CommandPreview)
	}
	for _, want := range []string{fileAddress, outputAddress} {
		if !containsString(operation.TriggeredBy, want) || !containsString(operation.DependsOn, want) {
			t.Fatalf("script operation deps=%#v triggered_by=%#v, want %q", operation.DependsOn, operation.TriggeredBy, want)
		}
	}
}

func TestCompileComponentScriptWithoutOnChangeDoesNotGenerateOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "app" {
  script "unused" {
    run = "systemctl reload app.service"
  }

  files {
    file "/etc/app.conf" {
      content = "managed"
    }
  }
}

host "app1" {
  components = [component.app]
}
`)
	if operationFor(resourceGraph, `host.app1.components.app.script["unused"]`) != nil {
		t.Fatalf("unused component script generated operation: %#v", resourceGraph.Operations)
	}
}

func TestComponentScriptPayloadStaysOutOfResourceGraphJSON(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "app" {
  script "reload" {
    run = "printf '%s\n' not-a-real-script-secret"
  }

  files {
    file "/etc/app.conf" {
      content   = "managed"
      on_change = script.reload
    }
  }
}

host "app1" {
  components = [component.app]
}
`)
	operation := operationFor(resourceGraph, `host.app1.components.app.script["reload"]`)
	if operation == nil || operation.ScriptPayload == nil {
		t.Fatalf("script operation payload missing: %#v", resourceGraph.Operations)
	}

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "not-a-real-script-secret") || strings.Contains(text, "script_payload") {
		t.Fatalf("ResourceGraph JSON exposed script payload:\n%s", text)
	}
	if !strings.Contains(text, `"command_preview": "script reload (once)"`) {
		t.Fatalf("ResourceGraph JSON missing script preview:\n%s", text)
	}
}

func TestResourceGraphDesiredDoesNotLeakCurrentSensitiveBaseline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
	}{
		{name: "secrets file", fixture: "../testdata/fixtures/foundation.dbf.hcl"},
		{name: "sensitive file content", fixture: "../../../examples/files-plan-preview.dbf.hcl"},
		{name: "sensitive component input", fixture: "../../../examples/component-inputs.dbf.hcl"},
		{name: "sensitive service environment", fixture: "../testdata/fixtures/sensitive-service-environment.dbf.hcl"},
		{name: "sensitive variable content", fixture: "../testdata/fixtures/sensitive-variable-files.dbf.hcl"},
		{name: "ephemeral variable content", fixture: "../testdata/fixtures/ephemeral-variable-content.dbf.hcl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resourceGraph := compileGraphFixture(t, tt.fixture)
			desired := make(map[string]map[string]any, len(resourceGraph.Nodes))
			for _, node := range resourceGraph.Nodes {
				desired[node.Address] = node.Desired
			}
			data, err := json.MarshalIndent(desired, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			testassert.NoSecretLeak(t, tt.name+" ResourceGraph desired", string(data))
		})
	}
}

func TestWriteOnlyFileContentStaysOutOfDesired(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/ephemeral-variable-content.dbf.hcl")
	node := nodeFor(resourceGraph, `host.ephemeral1.files.file["/etc/debianform/runtime-token.txt"]`)
	if node == nil {
		t.Fatal("write-only file node missing")
	}
	for _, key := range []string{"content", "content_sha256", "content_bytes", "summary"} {
		if _, ok := node.Desired[key]; ok {
			t.Fatalf("desired contains %s: %#v", key, node.Desired)
		}
	}
	if node.Desired["content_version"] != "v1" {
		t.Fatalf("content_version = %#v, want v1", node.Desired["content_version"])
	}
	if node.Desired["content_write_only"] != true {
		t.Fatalf("content_write_only = %#v, want true", node.Desired["content_write_only"])
	}
	if node.ProviderPayload["content"] != testassert.EphemeralVariableValue {
		t.Fatalf("provider payload content = %#v, want ephemeral value", node.ProviderPayload["content"])
	}

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	testassert.NoSecretLeak(t, "write-only ResourceGraph JSON", text)
	if strings.Contains(text, "provider_payload") {
		t.Fatalf("ResourceGraph JSON exposed provider_payload:\n%s", text)
	}
}

func TestCompileVariableDefaultsResourceGraph(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/variable-defaults.dbf.hcl")

	file := nodeFor(resourceGraph, `host.vars1.files.file["/etc/debianform/message.txt"]`)
	if file == nil {
		t.Fatal("variable-backed file node missing")
	}
	if file.Desired["content"] != "hello from variable default" {
		t.Fatalf("file desired = %#v", file.Desired)
	}
	profileFile := nodeFor(resourceGraph, `host.vars1.files.file["/etc/debianform/profile-message.txt"]`)
	if profileFile == nil {
		t.Fatal("profile variable-backed file node missing")
	}
	if profileFile.Desired["content"] != "hello from variable default" {
		t.Fatalf("profile file desired = %#v", profileFile.Desired)
	}
	unit := nodeFor(resourceGraph, `host.vars1.components.message_unit.systemd.unit["message.service"]`)
	if unit == nil {
		t.Fatal("variable-backed component unit node missing")
	}
	content, _ := unit.Desired["content"].(string)
	if !strings.Contains(content, "Variable backed service") || !strings.Contains(content, "hello from variable default") {
		t.Fatalf("unit content = %q", content)
	}
}

func TestCompileComponentArtifactKindsAndCAOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "company_ca" {
  type = "ca_certificate"

  source {
    url    = "https://downloads.example/company-ca.crt"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  install {
    path = "/usr/local/share/ca-certificates/company-ca.crt"
  }
}

component "config" {
  type = "file"

  source {
    url    = "https://downloads.example/config.yaml"
    sha256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
  }

  install {
    path = "/etc/myapp/config.yaml"
  }
}

component "myapp" {
  type = "archive"

  source "amd64" {
    url    = "https://downloads.example/myapp.tar.gz"
    sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
  }

  extract {
    format           = "tar.gz"
    strip_components = 1
  }

  install {
    path  = "/opt/myapp"
    owner = "myapp"
    group = "myapp"
  }

  groups {
    group "myapp" {
      system = true
    }
  }

  users {
    user "myapp" {
      system = true
      group  = "myapp"
    }
  }
}

host "server1" {
  components = [component.company_ca, component.config, component.myapp]

  system {
    architecture = "amd64"
  }
}
`)

	if node := nodeFor(resourceGraph, `host.server1.components.config.artifact.install["/etc/myapp/config.yaml"]`); node == nil || node.Kind != "component_file" {
		t.Fatalf("component file node = %#v", node)
	}
	caAddress := `host.server1.components.company_ca.artifact.install["/usr/local/share/ca-certificates/company-ca.crt"]`
	if node := nodeFor(resourceGraph, caAddress); node == nil || node.Kind != "component_ca_certificate" {
		t.Fatalf("ca certificate node = %#v", node)
	}
	operation := operationFor(resourceGraph, "host.server1.ca_certificates.update")
	if operation == nil {
		t.Fatalf("ca certificates update operation missing")
	}
	if !containsString(operation.TriggeredBy, caAddress) || operation.CommandPreview != "update-ca-certificates" {
		t.Fatalf("ca operation = %#v", operation)
	}
	archiveDeps := dependsOnFor(resourceGraph, `host.server1.components.myapp.artifact.install["/opt/myapp"]`)
	for _, want := range []string{
		`host.server1.components.myapp.artifact.download["amd64"]`,
		`host.server1.components.myapp.groups.group["myapp"]`,
		`host.server1.components.myapp.users.user["myapp"]`,
	} {
		if !containsString(archiveDeps, want) {
			t.Fatalf("archive deps = %#v, want %q", archiveDeps, want)
		}
	}
}

func TestCompileComponentSourceBuildGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
component "hello" {
  type = "source"

  source "amd64" {
    url    = "https://downloads.example/hello.c"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-o", "hello", "hello.c"],
    ]
    output      = "hello"
    source_name = "hello.c"
  }

  install {
    path = "/usr/local/bin/hello"
  }
}

host "server1" {
  components = [component.hello]

  system {
    architecture = "amd64"
  }
}
`)

	var build *Node
	for i := range resourceGraph.Nodes {
		if strings.HasPrefix(resourceGraph.Nodes[i].Address, `host.server1.components.hello.artifact.build[`) {
			build = &resourceGraph.Nodes[i]
			break
		}
	}
	if build == nil || build.Kind != "component_build" {
		t.Fatalf("build node = %#v", build)
	}
	if !containsString(build.DependsOn, `host.server1.components.hello.artifact.download["amd64"]`) {
		t.Fatalf("build deps = %#v, want download dependency", build.DependsOn)
	}
	if !containsString(build.DependsOn, `host.server1.components.hello.build.package["gcc"]`) {
		t.Fatalf("build deps = %#v, want gcc package dependency", build.DependsOn)
	}
	install := nodeFor(resourceGraph, `host.server1.components.hello.artifact.install["/usr/local/bin/hello"]`)
	if install == nil || install.Kind != "component_binary" {
		t.Fatalf("install node = %#v", install)
	}
	if !containsString(install.DependsOn, build.Address) {
		t.Fatalf("install deps = %#v, want build dependency %q", install.DependsOn, build.Address)
	}
	if got := install.Desired["cache_path"]; got != build.Desired["output_path"] {
		t.Fatalf("install cache_path = %#v, want build output %#v", got, build.Desired["output_path"])
	}
}

func TestCompileServiceRestartOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  systemd {
    unit "worker.service" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }

  services {
    service "worker" {
      state = "restarted"
    }
  }
}
`)

	if !hasOperation(resourceGraph, `host.server1.services.service["worker"].restart`) {
		t.Fatalf("restart operation missing: %#v", resourceGraph.Operations)
	}
}

func TestCompileServiceUnitDependency(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  systemd {
    service_unit "worker" {
      run = ["/bin/true"]
    }
  }

  services {
    service "worker" {
      enabled = true
      state   = "running"
    }
  }
}
`)

	serviceDeps := dependsOnFor(resourceGraph, `host.server1.services.service["worker"]`)
	for _, want := range []string{
		`host.server1.systemd.unit["worker.service"]`,
		`host.server1.systemd.daemon_reload`,
	} {
		if !containsString(serviceDeps, want) {
			t.Fatalf("service deps = %#v, want %q", serviceDeps, want)
		}
	}
}

func TestCompileSystemdTimerResolvedAndJournaldGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  packages {
    install = ["systemd-resolved"]
  }

  systemd {
    timer "cleanup" {
      enable = true
      state  = "running"

      timer = {
        OnCalendar = "daily"
      }
    }

    resolved {
      enable = true

      resolve = {
        DNS = ["1.1.1.1", "9.9.9.9"]
      }
    }

    journald {
      state = "reloaded"

      journal = {
        SystemMaxUse = "1G"
      }
    }
  }
}
`)

	timerAddress := `host.server1.systemd.timer["cleanup.timer"]`
	timer := nodeFor(resourceGraph, timerAddress)
	if timer == nil || timer.Kind != "systemd_unit" || timer.Desired["name"] != "cleanup.timer" {
		t.Fatalf("timer node = %#v", timer)
	}
	content, _ := timer.Desired["content"].(string)
	for _, want := range []string{"[Timer]", "OnCalendar=daily", "WantedBy=timers.target"} {
		if !strings.Contains(content, want) {
			t.Fatalf("timer content missing %q:\n%s", want, content)
		}
	}

	daemonReload := operationFor(resourceGraph, "host.server1.systemd.daemon_reload")
	if daemonReload == nil || !containsString(daemonReload.TriggeredBy, timerAddress) {
		t.Fatalf("daemon reload = %#v, want timer trigger", daemonReload)
	}
	timerServiceAddress := `host.server1.systemd.timer["cleanup.timer"].service`
	timerService := nodeFor(resourceGraph, timerServiceAddress)
	if timerService == nil || timerService.Desired["enabled"] != true || timerService.Desired["state"] != "running" {
		t.Fatalf("timer service = %#v", timerService)
	}
	for _, want := range []string{timerAddress, "host.server1.systemd.daemon_reload"} {
		if !containsString(timerService.DependsOn, want) {
			t.Fatalf("timer service deps = %#v, want %q", timerService.DependsOn, want)
		}
	}

	resolvedAddress := "host.server1.systemd.resolved"
	resolved := nodeFor(resourceGraph, resolvedAddress)
	if resolved == nil || resolved.Kind != "file" || resolved.Desired["path"] != "/etc/systemd/resolved.conf.d/debianform.conf" {
		t.Fatalf("resolved node = %#v", resolved)
	}
	resolvedContent, _ := resolved.Desired["content"].(string)
	if !strings.Contains(resolvedContent, "DNS=1.1.1.1") || !strings.Contains(resolvedContent, "DNS=9.9.9.9") {
		t.Fatalf("resolved content = %q", resolvedContent)
	}
	resolvedPackage := `host.server1.packages.install["systemd-resolved"]`
	if !containsString(resolved.DependsOn, resolvedPackage) {
		t.Fatalf("resolved deps = %#v, want %q", resolved.DependsOn, resolvedPackage)
	}
	resolvedService := nodeFor(resourceGraph, "host.server1.systemd.resolved.service")
	if resolvedService == nil || resolvedService.Desired["enabled"] != true {
		t.Fatalf("resolved service = %#v", resolvedService)
	}
	resolvedRestart := operationFor(resourceGraph, "host.server1.systemd.resolved.restart")
	if resolvedRestart == nil || resolvedRestart.CommandPreview != "systemctl restart systemd-resolved.service" {
		t.Fatalf("resolved restart = %#v", resolvedRestart)
	}
	if !containsString(resolvedRestart.TriggeredBy, resolvedAddress) || !containsString(resolvedRestart.DependsOn, resolvedService.Address) {
		t.Fatalf("resolved restart deps=%#v triggered_by=%#v", resolvedRestart.DependsOn, resolvedRestart.TriggeredBy)
	}

	journaldAddress := "host.server1.systemd.journald"
	journald := nodeFor(resourceGraph, journaldAddress)
	if journald == nil || journald.Kind != "file" || journald.Desired["path"] != "/etc/systemd/journald.conf.d/debianform.conf" {
		t.Fatalf("journald node = %#v", journald)
	}
	journaldService := nodeFor(resourceGraph, "host.server1.systemd.journald.service")
	if journaldService == nil || journaldService.Desired["state"] != "reloaded" {
		t.Fatalf("journald service = %#v", journaldService)
	}
	if operationFor(resourceGraph, "host.server1.systemd.journald.restart") != nil {
		t.Fatalf("journald state reloaded should not also generate config restart operation")
	}
}

func TestCompileSystemdNetworkdWireGuardGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = 51820
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = "off"
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address = ["10.80.0.1/30"]
        }
      }
    }
  }
}
`)

	netdev := nodeFor(resourceGraph, `host.server1.systemd.networkd.netdev["10-wg0"]`)
	if netdev == nil || netdev.Kind != "networkd_netdev" {
		t.Fatalf("networkd netdev node = %#v", netdev)
	}
	if _, ok := netdev.Desired["content"]; !ok {
		t.Fatalf("networkd netdev desired content missing: %#v", netdev.Desired)
	}
	networkDeps := dependsOnFor(resourceGraph, `host.server1.systemd.networkd.network["20-wg0"]`)
	if !containsString(networkDeps, `host.server1.systemd.networkd.netdev["10-wg0"]`) {
		t.Fatalf("network deps = %#v, want netdev dependency", networkDeps)
	}
	reload := operationFor(resourceGraph, "host.server1.systemd.networkd.restart")
	if reload == nil {
		t.Fatalf("networkd reload operation missing")
	}
	if !containsString(reload.TriggeredBy, `host.server1.systemd.networkd.netdev["10-wg0"]`) ||
		!containsString(reload.TriggeredBy, `host.server1.systemd.networkd.network["20-wg0"]`) {
		t.Fatalf("reload triggered_by = %#v", reload.TriggeredBy)
	}
	if !strings.Contains(reload.CommandPreview, "networkctl reload") {
		t.Fatalf("reload command = %q", reload.CommandPreview)
	}
	if nodeFor(resourceGraph, `host.server1.packages.install["wireguard-tools"]`) != nil {
		t.Fatalf("networkd WireGuard graph should not install wireguard-tools")
	}
}

func TestCompileAPTRepositoryDependencies(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
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
    install = ["curl"]

    package "example-tool" {
      repositories = ["tools"]
    }
  }
}
`)

	repositoryAddress := `host.server1.apt.repository["tools"]`
	keyAddress := `host.server1.apt.signing_key["tools"]`
	refreshAddress := `host.server1.apt.cache_refresh`
	packageAddress := `host.server1.packages.install["example-tool"]`

	repository := nodeFor(resourceGraph, repositoryAddress)
	if repository == nil {
		t.Fatalf("repository node missing")
	}
	if !containsString(repository.DependsOn, keyAddress) {
		t.Fatalf("repository deps = %#v, want signing key", repository.DependsOn)
	}
	content, _ := repository.Desired["content"].(string)
	if !strings.Contains(content, "Signed-By: /etc/apt/keyrings/tools.asc") {
		t.Fatalf("repository content missing Signed-By:\n%s", content)
	}
	if !strings.Contains(content, "Architectures: amd64") {
		t.Fatalf("repository content missing Architectures:\n%s", content)
	}

	operation := operationFor(resourceGraph, refreshAddress)
	if operation == nil {
		t.Fatalf("apt cache refresh operation missing")
	}
	for _, want := range []string{keyAddress, repositoryAddress} {
		if !containsString(operation.TriggeredBy, want) {
			t.Fatalf("refresh triggered_by = %#v, want %q", operation.TriggeredBy, want)
		}
	}

	packageDeps := dependsOnFor(resourceGraph, packageAddress)
	for _, want := range []string{repositoryAddress, refreshAddress} {
		if !containsString(packageDeps, want) {
			t.Fatalf("package deps = %#v, want %q", packageDeps, want)
		}
	}
	curlDeps := dependsOnFor(resourceGraph, `host.server1.packages.install["curl"]`)
	if containsString(curlDeps, refreshAddress) {
		t.Fatalf("unrelated package deps = %#v, did not want apt refresh", curlDeps)
	}
}

func TestCompileAPTSourceFileTriggersCacheRefresh(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
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

	address := `host.server1.apt.source_file["main"]`
	node := nodeFor(resourceGraph, address)
	if node == nil {
		t.Fatalf("apt source_file node missing")
	}
	if node.Kind != "apt_source_file" || node.ProviderType != "apt_source_file" {
		t.Fatalf("node kind/provider = %s/%s", node.Kind, node.ProviderType)
	}
	if node.Desired["path"] != "/etc/apt/sources.list" || node.Desired["on_destroy"] != "restore" {
		t.Fatalf("desired = %#v", node.Desired)
	}

	operation := operationFor(resourceGraph, `host.server1.apt.cache_refresh`)
	if operation == nil {
		t.Fatalf("apt cache refresh operation missing")
	}
	if !containsString(operation.TriggeredBy, address) {
		t.Fatalf("refresh triggered_by = %#v, want %q", operation.TriggeredBy, address)
	}
}

func compileGraphFixture(t *testing.T, fixture string) *ResourceGraph {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return resourceGraph
}

func testHostFacts() map[string]ir.HostFacts {
	out := map[string]ir.HostFacts{}
	for _, name := range []string{
		"apt1",
		"bbr1",
		"compose1",
		"docker-daemon1",
		"docker1",
		"edge1",
		"foundation1",
		"input1",
		"merge1",
		"preview1",
		"router1",
		"server1",
		"server2",
		"service1",
		"tool1",
		"users1",
	} {
		out[name] = ir.HostFacts{System: ir.SystemFacts{
			Hostname:     name,
			Architecture: "amd64",
			Codename:     "trixie",
		}}
	}
	return out
}

func compileGraphInline(t *testing.T, content string) *ResourceGraph {
	t.Helper()

	return compileGraphInlineWithFiles(t, content, nil)
}

func compileGraphInlineWithFiles(t *testing.T, content string, files map[string]string) *ResourceGraph {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return compileGraphFixture(t, file)
}

func compileGraphInlineError(t *testing.T, content string) error {
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
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(program)
	return err
}

func dependsOnFor(resourceGraph *ResourceGraph, address string) []string {
	for _, node := range resourceGraph.Nodes {
		if node.Address == address {
			return node.DependsOn
		}
	}
	return nil
}

func nodeFor(resourceGraph *ResourceGraph, address string) *Node {
	for i := range resourceGraph.Nodes {
		if resourceGraph.Nodes[i].Address == address {
			return &resourceGraph.Nodes[i]
		}
	}
	return nil
}

func operationFor(resourceGraph *ResourceGraph, address string) *Operation {
	for i := range resourceGraph.Operations {
		if resourceGraph.Operations[i].Address == address {
			return &resourceGraph.Operations[i]
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func topologicalOrder(t *testing.T, resourceGraph *ResourceGraph) map[string]int {
	t.Helper()

	items, err := resourceGraph.TopologicalSort()
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]int, len(items))
	for i, item := range items {
		out[item.Address] = i
	}
	return out
}

func assertBefore(t *testing.T, order map[string]int, before string, after string) {
	t.Helper()

	beforeIndex, ok := order[before]
	if !ok {
		t.Fatalf("topological order missing %q", before)
	}
	afterIndex, ok := order[after]
	if !ok {
		t.Fatalf("topological order missing %q", after)
	}
	if beforeIndex >= afterIndex {
		t.Fatalf("topological order puts %q at %d after %q at %d", before, beforeIndex, after, afterIndex)
	}
}

func hasOperation(resourceGraph *ResourceGraph, address string) bool {
	for _, operation := range resourceGraph.Operations {
		if operation.Address == address {
			return true
		}
	}
	return false
}

func assertGolden(t *testing.T, golden string, got string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
