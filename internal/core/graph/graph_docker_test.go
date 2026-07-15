package graph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/ir"
)

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

func TestCompileDockerOfficialRepositoryUsesPlatformFacts(t *testing.T) {
	resourceGraph, err := Compile(&ir.Program{Hosts: []ir.HostSpec{{
		Name:   "docker1",
		Source: ir.SourceRef{File: "inline.dbf.hcl", Line: 1, Path: "host.docker1"},
		Platform: &ir.PlatformSpec{
			Architecture: "amd64",
			Codename:     "trixie",
			Source:       ir.SourceRef{File: "inline.dbf.hcl", Line: 2, Path: "host.docker1.platform"},
		},
		Docker: &ir.DockerSpec{
			Enable: true,
			Package: ir.DockerPackageSpec{
				Source:          "official",
				Channel:         "stable",
				RemoveConflicts: "auto",
				SourceRef:       ir.SourceRef{File: "inline.dbf.hcl", Line: 5, Path: "host.docker1.docker.package"},
			},
			Service: ir.DockerServiceSpec{
				Enable:    true,
				State:     "running",
				Name:      "docker",
				SourceRef: ir.SourceRef{File: "inline.dbf.hcl", Line: 5, Path: "host.docker1.docker.service"},
			},
			Source: ir.SourceRef{File: "inline.dbf.hcl", Line: 4, Path: "host.docker1.docker"},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	repository := nodeFor(resourceGraph, `host.docker1.docker.apt.repository["docker-official"]`)
	if repository == nil {
		t.Fatalf("docker repository node missing")
	}
	content, _ := repository.Desired["content"].(string)
	for _, want := range []string{"Suites: trixie", "Architectures: amd64"} {
		if !strings.Contains(content, want) {
			t.Fatalf("docker repository content missing %q:\n%s", want, content)
		}
	}
}

func TestCompileDockerOfficialRepositoryDoesNotInferUbuntuFromCodename(t *testing.T) {
	legacyProgram := &ir.Program{Hosts: []ir.HostSpec{{
		Name:   "legacy1",
		Source: ir.SourceRef{File: "inline.dbf.hcl", Line: 1, Path: "host.legacy1"},
		Platform: &ir.PlatformSpec{
			Architecture: "amd64",
			Codename:     "noble",
		},
		Docker: &ir.DockerSpec{
			Enable: true,
			Package: ir.DockerPackageSpec{
				Source:          "official",
				Channel:         "stable",
				RemoveConflicts: "auto",
			},
		},
	}}}
	legacyGraph, err := Compile(legacyProgram)
	if err != nil {
		t.Fatal(err)
	}
	legacyRepository := nodeFor(legacyGraph, `host.legacy1.docker.apt.repository["docker-official"]`)
	legacyContent := ""
	if legacyRepository != nil {
		legacyContent, _ = legacyRepository.Desired["content"].(string)
	}
	if !strings.Contains(legacyContent, "URIs: https://download.docker.com/linux/debian") {
		t.Fatalf("legacy repository = %#v, want historical Debian default", legacyRepository)
	}

	if strings.Contains(legacyContent, "/linux/ubuntu") {
		t.Fatalf("legacy repository unexpectedly inferred Ubuntu from noble codename:\n%s", legacyContent)
	}
}

func TestCompileDockerOfficialRepositoryRequiresDistributionWithVersion(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{
		Name:   "docker1",
		Source: ir.SourceRef{File: "inline.dbf.hcl", Line: 1, Path: "host.docker1"},
		Platform: &ir.PlatformSpec{
			Version:      "26.04",
			Architecture: "amd64",
			Codename:     "resolute",
		},
		Docker: &ir.DockerSpec{
			Enable: true,
			Package: ir.DockerPackageSpec{
				Source:          "official",
				Channel:         "stable",
				RemoveConflicts: "auto",
			},
		},
	}}}
	_, err := Compile(program)
	if err == nil || !strings.Contains(err.Error(), "must declare platform.distribution when platform.version is set") {
		t.Fatalf("Compile() error = %v, want missing platform.distribution error", err)
	}
}

func TestCompileDockerOfficialRepositoryUsesUbuntuPlatformFacts(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{
		Name:   "docker1",
		Source: ir.SourceRef{File: "inline.dbf.hcl", Line: 1, Path: "host.docker1"},
		Platform: &ir.PlatformSpec{
			Distribution: "ubuntu",
			Version:      "24.04",
			Architecture: "amd64",
			Codename:     "noble",
			Source:       ir.SourceRef{File: "inline.dbf.hcl", Line: 2, Path: "host.docker1.platform"},
		},
		Docker: &ir.DockerSpec{
			Enable: true,
			Package: ir.DockerPackageSpec{
				Source:          "official",
				Channel:         "stable",
				RemoveConflicts: "auto",
				SourceRef:       ir.SourceRef{File: "inline.dbf.hcl", Line: 5, Path: "host.docker1.docker.package"},
			},
		},
	}}}
	resourceGraph, err := Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	repository := nodeFor(resourceGraph, `host.docker1.docker.apt.repository["docker-official"]`)
	if repository == nil {
		t.Fatal("Ubuntu docker repository node missing")
	}
	wantContent := "Types: deb\nURIs: https://download.docker.com/linux/ubuntu\nSuites: noble\nComponents: stable\nArchitectures: amd64\nSigned-By: /etc/apt/keyrings/docker.asc\n"
	if got, _ := repository.Desired["content"].(string); got != wantContent {
		t.Fatalf("Ubuntu docker repository content:\n%s\nwant:\n%s", got, wantContent)
	}
	key := nodeFor(resourceGraph, `host.docker1.docker.apt.signing_key["docker-official"]`)
	if key == nil {
		t.Fatal("Ubuntu docker signing key node missing")
	}
	if key.Desired["url"] != ir.DockerOfficialUbuntuGPGURL || key.Desired["sha256"] != ir.DockerOfficialGPGSHA256 {
		t.Fatalf("Ubuntu docker key desired = %#v", key.Desired)
	}
}

func TestCompileDockerOfficialRepositoryRejectsUnsupportedDistribution(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{
		Name:   "docker1",
		Source: ir.SourceRef{File: "inline.dbf.hcl", Line: 1, Path: "host.docker1"},
		Platform: &ir.PlatformSpec{
			Distribution: "fedora",
			Version:      "42",
			Architecture: "amd64",
			Codename:     "rawhide",
		},
		Docker: &ir.DockerSpec{
			Enable: true,
			Package: ir.DockerPackageSpec{
				Source:          "official",
				Channel:         "stable",
				RemoveConflicts: "auto",
			},
		},
	}}}
	_, err := Compile(program)
	if err == nil || !strings.Contains(err.Error(), `docker official repository for platform.distribution "fedora" is not implemented`) {
		t.Fatalf("Compile() error = %v, want unsupported distribution error", err)
	}
}

func TestCompileDockerOfficialUbuntuKeepsExplicitMirrorURLs(t *testing.T) {
	resourceGraph, err := Compile(&ir.Program{Hosts: []ir.HostSpec{{
		Name: "docker1",
		Platform: &ir.PlatformSpec{
			Distribution: "ubuntu",
			Version:      "24.04",
			Architecture: "amd64",
			Codename:     "noble",
		},
		Docker: &ir.DockerSpec{
			Enable: true,
			Package: ir.DockerPackageSpec{
				Source:           "official",
				Channel:          "stable",
				RepositoryURL:    "https://mirror.example/docker/ubuntu",
				GPGURL:           "https://mirror.example/docker/ubuntu/gpg",
				RemoveConflicts:  "auto",
				RepositoryURLSet: true,
				GPGURLSet:        true,
			},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	repository := nodeFor(resourceGraph, `host.docker1.docker.apt.repository["docker-official"]`)
	if repository == nil || !strings.Contains(repository.Desired["content"].(string), "URIs: https://mirror.example/docker/ubuntu") {
		t.Fatalf("Ubuntu explicit mirror repository = %#v", repository)
	}
	key := nodeFor(resourceGraph, `host.docker1.docker.apt.signing_key["docker-official"]`)
	if key == nil || key.Desired["url"] != "https://mirror.example/docker/ubuntu/gpg" {
		t.Fatalf("Ubuntu explicit mirror key = %#v", key)
	}
}

func TestCompileDockerOfficialMirrorResourceGraph(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "docker1" {
  platform {
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
  platform {
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
  platform {
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
