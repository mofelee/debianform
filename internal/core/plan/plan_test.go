package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/merge"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestBBRPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/bbr.dbf.hcl", Options{
		CommandFile: "../../../examples/bbr.dbf.hcl",
		Host:        "bbr1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/bbr.golden.json", got)

	if doc.FormatVersion != FormatVersion {
		t.Fatalf("format version = %q, want %q", doc.FormatVersion, FormatVersion)
	}
	if doc.Summary.Create != 3 {
		t.Fatalf("create count = %d, want 3", doc.Summary.Create)
	}
	for _, change := range doc.Changes {
		if change.ProviderAddress != "" {
			t.Fatalf("default plan leaked provider address %q", change.ProviderAddress)
		}
	}
}

func TestFoundationPlanJSONGoldenDoesNotLeakSecrets(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/foundation.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/foundation.dbf.hcl",
		Host:        "foundation1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/foundation.golden.json", got)

	testassert.NoSecretLeak(t, "foundation plan JSON", got)
	if doc.Summary.Operations != 1 {
		t.Fatalf("operations = %d, want 1", doc.Summary.Operations)
	}
}

func TestAPTRepositoryPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/apt-repository.dbf.hcl", Options{
		CommandFile: "../../../examples/apt-repository.dbf.hcl",
		Host:        "apt1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/apt-repository.golden.json", got)

	if doc.Summary.Create != 4 {
		t.Fatalf("create count = %d, want 4", doc.Summary.Create)
	}
	if doc.Summary.Operations != 1 {
		t.Fatalf("operations = %d, want 1", doc.Summary.Operations)
	}
}

func TestDockerMinimalPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-minimal.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-minimal.dbf.hcl",
		Host:        "docker1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/docker-minimal.golden.json", got)

	if doc.Summary.Create != 9 {
		t.Fatalf("create count = %d, want 9", doc.Summary.Create)
	}
	if doc.Summary.Operations != 1 {
		t.Fatalf("operations = %d, want 1", doc.Summary.Operations)
	}
	for _, want := range []string{
		`host.docker1.docker.apt.signing_key["docker-official"]`,
		`host.docker1.docker.apt.repository["docker-official"]`,
		`host.docker1.docker.package_conflicts`,
		`host.docker1.docker.package["docker-ce"]`,
		`host.docker1.docker.service["docker"]`,
	} {
		if !hasChange(doc, want) {
			t.Fatalf("docker plan missing change %q", want)
		}
	}
	if !hasOperation(doc, "host.docker1.apt.cache_refresh") {
		t.Fatalf("docker apt cache refresh operation missing: %#v", doc.Operations)
	}
}

func TestDockerPackageSourcesPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-package-sources.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-package-sources.dbf.hcl",
		Host:        "docker-sources1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/docker-package-sources.golden.json", got)

	if doc.Summary.Create != 3 {
		t.Fatalf("create count = %d, want 3", doc.Summary.Create)
	}
	if doc.Summary.Operations != 0 {
		t.Fatalf("operations = %d, want 0", doc.Summary.Operations)
	}
	for _, want := range []string{
		`host.docker-sources1.docker.package["docker.io"]`,
		`host.docker-sources1.docker.package["docker-compose-plugin"]`,
		`host.docker-sources1.docker.service["docker"]`,
	} {
		if !hasChange(doc, want) {
			t.Fatalf("docker package sources plan missing change %q", want)
		}
	}
	if hasChange(doc, `host.docker-sources1.docker.apt.repository["docker-official"]`) {
		t.Fatalf("debian source plan generated docker official repository")
	}
}

func TestDockerPackageSourcesPlanTextGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-package-sources.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-package-sources.dbf.hcl",
		Host:        "docker-sources1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/docker-package-sources.golden.txt", text.String())
	for _, want := range []string{
		`host.docker-sources1.docker.package["docker.io"]`,
		`host.docker-sources1.docker.package["docker-compose-plugin"]`,
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("docker package sources text plan missing %q:\n%s", want, text.String())
		}
	}
	if strings.Contains(text.String(), "docker-official") {
		t.Fatalf("debian source text plan mentioned docker official repository:\n%s", text.String())
	}
}

func TestDockerMinimalPlanTextGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-minimal.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-minimal.dbf.hcl",
		Host:        "docker1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/docker-minimal.golden.txt", text.String())
	if !strings.Contains(text.String(), `host.docker1.docker.package["docker-ce"]`) {
		t.Fatalf("docker text plan missing high-level address:\n%s", text.String())
	}
	if strings.Contains(text.String(), "provider:") {
		t.Fatalf("docker text plan leaked provider addresses:\n%s", text.String())
	}
}

func TestDockerDaemonPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-daemon.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-daemon.dbf.hcl",
		Host:        "docker-daemon1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/docker-daemon.golden.json", got)

	if doc.Summary.Create != 10 {
		t.Fatalf("create count = %d, want 10", doc.Summary.Create)
	}
	if doc.Summary.Operations != 2 {
		t.Fatalf("operations = %d, want 2", doc.Summary.Operations)
	}
	if !hasChange(doc, `host.docker-daemon1.docker.daemon.file["/etc/docker/daemon.json"]`) {
		t.Fatalf("docker daemon file change missing")
	}
	if !hasOperation(doc, "host.docker-daemon1.docker.daemon.restart") {
		t.Fatalf("docker daemon restart operation missing: %#v", doc.Operations)
	}
}

func TestDockerDaemonPlanTextGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-daemon.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-daemon.dbf.hcl",
		Host:        "docker-daemon1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/docker-daemon.golden.txt", text.String())
	for _, want := range []string{
		`host.docker-daemon1.docker.daemon.file["/etc/docker/daemon.json"]`,
		`+   "log-driver": "json-file",`,
		`host.docker-daemon1.docker.daemon.restart`,
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("docker daemon text plan missing %q:\n%s", want, text.String())
		}
	}
}

func TestDockerComposePlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-compose.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-compose.dbf.hcl",
		Host:        "compose1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/docker-compose.golden.json", got)

	if doc.Summary.Create != 15 {
		t.Fatalf("create count = %d, want 15", doc.Summary.Create)
	}
	if doc.Summary.Operations != 3 {
		t.Fatalf("operations = %d, want 3", doc.Summary.Operations)
	}
	if !hasChange(doc, `host.compose1.docker.compose["app"].directory`) {
		t.Fatalf("compose directory change missing")
	}
	if !hasChange(doc, `host.compose1.docker.compose["app"].file`) {
		t.Fatalf("compose file change missing")
	}
	if !hasChange(doc, `host.compose1.docker.compose["app"].env_file["app"]`) {
		t.Fatalf("compose env file change missing")
	}
	if !hasChange(doc, `host.compose1.docker.compose["app"].project`) {
		t.Fatalf("compose project change missing")
	}
	if !hasChange(doc, `host.compose1.docker.compose["app"].systemd_unit`) {
		t.Fatalf("compose systemd unit change missing")
	}
	if !hasChange(doc, `host.compose1.docker.compose["app"].service`) {
		t.Fatalf("compose service change missing")
	}
	if !hasOperation(doc, `host.compose1.docker.compose["app"].validate`) {
		t.Fatalf("compose validate operation missing: %#v", doc.Operations)
	}
	if !hasOperation(doc, `host.compose1.docker.compose["app"].daemon_reload`) {
		t.Fatalf("compose daemon-reload operation missing: %#v", doc.Operations)
	}
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "docker compose plan JSON", string(data))
	if strings.Contains(string(data), "not-a-real-preview-secret") {
		t.Fatalf("docker compose plan JSON leaked env file content:\n%s", data)
	}
}

func TestDockerComposePlanTextGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-compose.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-compose.dbf.hcl",
		Host:        "compose1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/docker-compose.golden.txt", text.String())
	for _, want := range []string{
		`host.compose1.docker.compose["app"].file`,
		`host.compose1.docker.compose["app"].project`,
		`host.compose1.docker.compose["app"].systemd_unit`,
		`ExecStart=/usr/bin/docker compose -p app -f /opt/app/compose.yaml up -d`,
		`+     image: nginx:1.27-alpine`,
		`host.compose1.docker.compose["app"].validate`,
		`docker compose -p app -f /opt/app/compose.yaml config`,
		`<sensitive sha256=`,
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("docker compose text plan missing %q:\n%s", want, text.String())
		}
	}
	if strings.Contains(text.String(), "not-a-real-preview-secret") {
		t.Fatalf("docker compose text plan leaked env file content:\n%s", text.String())
	}
}

func TestDockerUsersPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-users.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-users.dbf.hcl",
		Host:        "docker-users1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/docker-users.golden.json", got)

	if doc.Summary.Create != 13 {
		t.Fatalf("create count = %d, want 13", doc.Summary.Create)
	}
	if !hasChange(doc, `host.docker-users1.docker.user_group_membership["deploy:docker"]`) {
		t.Fatalf("docker users membership change missing")
	}
}

func TestDockerUsersPlanTextGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/docker-users.dbf.hcl", Options{
		CommandFile: "../../../examples/docker-users.dbf.hcl",
		Host:        "docker-users1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/docker-users.golden.txt", text.String())
	for _, want := range []string{
		`host.docker-users1.docker.group["docker"]`,
		`host.docker-users1.docker.user_group_membership["deploy:docker"]`,
		`log out and back in`,
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("docker users text plan missing %q:\n%s", want, text.String())
		}
	}
}

func TestNftablesPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/nftables.dbf.hcl", Options{
		CommandFile: "../../../examples/nftables.dbf.hcl",
		Host:        "edge1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/nftables.golden.json", got)

	if doc.Summary.Create != 6 {
		t.Fatalf("create count = %d, want 6", doc.Summary.Create)
	}
	if doc.Summary.Operations != 2 {
		t.Fatalf("operations = %d, want 2", doc.Summary.Operations)
	}
	if !hasOperation(doc, "host.edge1.nftables.validate") || !hasOperation(doc, "host.edge1.nftables.activate") {
		t.Fatalf("nftables operations missing: %#v", doc.Operations)
	}
}

func TestBIRD2PlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/bird2.dbf.hcl", Options{
		CommandFile: "../../../examples/bird2.dbf.hcl",
		Host:        "router1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/bird2.golden.json", got)

	if doc.Summary.Create != 4 {
		t.Fatalf("create count = %d, want 4", doc.Summary.Create)
	}
	if doc.Summary.Operations != 1 {
		t.Fatalf("operations = %d, want 1", doc.Summary.Operations)
	}
}

func TestComponentBinaryPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/component-binary.dbf.hcl", Options{
		CommandFile: "../../../examples/component-binary.dbf.hcl",
		Host:        "tool1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/component-binary.golden.json", got)

	if doc.Summary.Create != 2 {
		t.Fatalf("create count = %d, want 2", doc.Summary.Create)
	}
}

func TestComponentSourceBuildPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/component-source-build.dbf.hcl", Options{
		CommandFile: "../../../examples/component-source-build.dbf.hcl",
		Host:        "build1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/component-source-build.golden.json", got)

	if doc.Summary.Create != 4 {
		t.Fatalf("create count = %d, want 4", doc.Summary.Create)
	}
}

func TestComponentInputsPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/component-inputs.dbf.hcl", Options{
		CommandFile: "../../../examples/component-inputs.dbf.hcl",
		Host:        "input1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/component-inputs.golden.json", got)

	if doc.Summary.Create != 2 {
		t.Fatalf("create count = %d, want 2", doc.Summary.Create)
	}
	testassert.NoSecretLeak(t, "component input plan JSON", got)

	var text bytes.Buffer
	PrintText(&text, doc)
	testassert.NoSecretLeak(t, "component input plan text", text.String())
}

func TestComponentScriptOnChangePlanGoldens(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/component-script-on-change.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/component-script-on-change.dbf.hcl",
		Host:        "app1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/component-script-on-change.golden.json", got)

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/component-script-on-change.golden.txt", text.String())

	if doc.Summary.Create != 1 {
		t.Fatalf("create count = %d, want 1", doc.Summary.Create)
	}
	if doc.Summary.Operations != 1 {
		t.Fatalf("operations = %d, want 1", doc.Summary.Operations)
	}
	if !hasOperation(doc, `host.app1.components.app.script["reload"]`) {
		t.Fatalf("script operation missing: %#v", doc.Operations)
	}
}

func TestSensitiveServiceEnvironmentPlanDoesNotLeak(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/sensitive-service-environment.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/sensitive-service-environment.dbf.hcl",
		Host:        "server1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "sensitive service environment plan JSON", string(data))

	var text bytes.Buffer
	PrintText(&text, doc)
	rendered := text.String()
	testassert.NoSecretLeak(t, "sensitive service environment plan text", rendered)
	if !strings.Contains(rendered, "<sensitive sha256=") {
		t.Fatalf("plan text does not show sensitive summary:\n%s", rendered)
	}
}

func TestSensitiveVariablePlanDoesNotLeak(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/sensitive-variable-files.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/sensitive-variable-files.dbf.hcl",
		Host:        "varsecret1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "sensitive variable plan JSON", string(data))

	var text bytes.Buffer
	PrintText(&text, doc)
	rendered := text.String()
	testassert.NoSecretLeak(t, "sensitive variable plan text", rendered)
	if !strings.Contains(rendered, "<sensitive sha256=") {
		t.Fatalf("plan text does not show sensitive summary:\n%s", rendered)
	}
	if !strings.Contains(rendered, "+ prod") {
		t.Fatalf("plan text does not show non-sensitive variable content:\n%s", rendered)
	}
}

func TestEphemeralVariablePlanDoesNotLeak(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/ephemeral-variable-content.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/ephemeral-variable-content.dbf.hcl",
		Host:        "ephemeral1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "ephemeral variable plan JSON", string(data))

	var text bytes.Buffer
	PrintText(&text, doc)
	rendered := text.String()
	testassert.NoSecretLeak(t, "ephemeral variable plan text", rendered)
	if !strings.Contains(rendered, `+ content_version: "v1"`) || !strings.Contains(rendered, "+ content: <sensitive changed>") {
		t.Fatalf("plan text does not show write-only redaction and version:\n%s", rendered)
	}
}

func TestVariableDefaultsPlanOffline(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/variable-defaults.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/variable-defaults.dbf.hcl",
		Host:        "vars1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	if !hasChange(doc, `host.vars1.files.file["/etc/debianform/message.txt"]`) {
		t.Fatalf("plan missing variable-backed file change: %#v", doc.Changes)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello from variable default") {
		t.Fatalf("plan JSON missing variable default content:\n%s", data)
	}
}

func TestProfileMergePlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/profile-merge.dbf.hcl", Options{
		CommandFile: "../../../examples/profile-merge.dbf.hcl",
		Host:        "merge1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/profile-merge.golden.json", got)

	for _, want := range []string{
		`host.merge1.packages.install["curl"]`,
		`host.merge1.packages.install["git"]`,
		`host.merge1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`,
	} {
		if !hasChange(doc, want) {
			t.Fatalf("profile merge plan missing change %q", want)
		}
	}
}

func TestSystemdServicePlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/systemd-service.dbf.hcl", Options{
		CommandFile: "../../../examples/systemd-service.dbf.hcl",
		Host:        "service1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/systemd-service.golden.json", got)

	if !hasOperation(doc, "host.service1.systemd.daemon_reload") {
		t.Fatalf("systemd daemon reload operation missing: %#v", doc.Operations)
	}
	if !hasChange(doc, `host.service1.services.service["myapp"]`) {
		t.Fatalf("myapp service change missing: %#v", doc.Changes)
	}
}

func TestUserGroupPlanJSONGolden(t *testing.T) {
	doc := planFixture(t, "../../../examples/user-group.dbf.hcl", Options{
		CommandFile: "../../../examples/user-group.dbf.hcl",
		Host:        "users1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/plan/user-group.golden.json", got)

	for _, want := range []string{
		`host.users1.groups.group["deploy"]`,
		`host.users1.users.user["deploy"]`,
	} {
		if !hasChange(doc, want) {
			t.Fatalf("user/group plan missing change %q", want)
		}
	}
}

func TestPlanHTMLDoesNotLeakSecrets(t *testing.T) {
	doc := planFixture(t, "../testdata/fixtures/foundation.dbf.hcl", Options{
		CommandFile: "../testdata/fixtures/foundation.dbf.hcl",
		Host:        "foundation1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	var out bytes.Buffer
	if err := PrintHTML(&out, doc); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		"DebianForm Plan",
		`host.foundation1.files.file[&#34;/etc/myapp/config.yaml&#34;]`,
		"host.foundation1.systemd.daemon_reload",
		"debianform.plan.alpha1",
		`id="action-filter"`,
		`id="host-filter"`,
		`id="search-filter"`,
		`value="foundation1"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML output does not contain %q:\n%s", want, html)
		}
	}
	testassert.NoSecretLeak(t, "plan HTML", html)
}

func TestFilesPlanPreviewHasTextAndSensitiveDiffs(t *testing.T) {
	doc := planFixture(t, "../../../examples/files-plan-preview.dbf.hcl", Options{
		CommandFile: "../../../examples/files-plan-preview.dbf.hcl",
		Host:        "preview1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	if len(doc.Changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(doc.Changes))
	}

	var textDiff, sensitiveDiff *DiffNode
	for i := range doc.Changes {
		for j := range doc.Changes[i].Diff.Children {
			child := &doc.Changes[i].Diff.Children[j]
			if child.Kind == "text" {
				textDiff = child
			}
			if child.Kind == "sensitive" {
				sensitiveDiff = child
			}
		}
	}
	if textDiff == nil || len(textDiff.Hunks) != 1 || textDiff.Hunks[0].NewLines != 2 {
		t.Fatalf("text diff = %#v", textDiff)
	}
	if sensitiveDiff == nil || sensitiveDiff.AfterSummary["bytes"] != float64(len("not-a-real-preview-secret")) {
		t.Fatalf("sensitive diff = %#v", sensitiveDiff)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "files preview plan JSON", string(data))

	var text bytes.Buffer
	PrintText(&text, doc)
	rendered := text.String()
	for _, want := range []string{
		`source: ../../../examples/files-plan-preview.dbf.hcl`,
		`+ content`,
		`@@ -1,0 +1,2 @@`,
		`+ listen = "127.0.0.1:8080"`,
		`<sensitive sha256=`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("text plan does not contain %q:\n%s", want, rendered)
		}
	}
	testassert.NoSecretLeak(t, "files preview plan text", rendered)
}

func TestNftablesPlanPreviewDoesNotLeakSecret(t *testing.T) {
	doc := planFixture(t, "../../../examples/plan-preview.dbf.hcl", Options{
		CommandFile: "../../../examples/plan-preview.dbf.hcl",
		Host:        "preview1",
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	})
	if doc.Summary.Create != 4 || doc.Summary.Operations != 2 {
		t.Fatalf("summary = %#v, want 4 creates and 2 operations", doc.Summary)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "nftables preview plan JSON", string(data))
	if !hasChange(doc, `host.preview1.nftables.file["20-services"]`) {
		t.Fatalf("nftables snippet change missing: %#v", doc.Changes)
	}
	if !hasChange(doc, `host.preview1.secrets.file["/etc/app/token"]`) {
		t.Fatalf("secret change missing: %#v", doc.Changes)
	}
}

func TestNftablesPortChangeTextDiffGolden(t *testing.T) {
	source := ir.SourceRef{
		File: "examples/plan-preview.dbf.hcl",
		Line: 29,
		Path: `host.preview1.nftables.file["20-services"]`,
	}
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/plan-preview.dbf.hcl", Host: "preview1"},
		Summary:       Summary{Update: 1, Operations: 2},
		Changes: []Change{
			{
				Address: `host.preview1.nftables.file["20-services"]`,
				Action:  "update",
				Summary: "update nftables snippet 20-services",
				Source:  source,
				Diff: BuildDiff("update",
					map[string]any{"content": "add rule inet filter input tcp dport { 22, 80 } accept\n"},
					map[string]any{"content": "add rule inet filter input tcp dport { 22, 80, 443 } accept\n"},
				),
			},
		},
		Operations: []OperationNode{
			{
				Address:        "host.preview1.nftables.validate",
				Action:         "run",
				Summary:        "validate nftables ruleset",
				TriggeredBy:    []string{`host.preview1.nftables.file["20-services"]`},
				CommandPreview: "nft -c -f /etc/nftables.conf",
				Source:         source,
			},
			{
				Address:        "host.preview1.nftables.activate",
				Action:         "run",
				Summary:        "activate nftables ruleset",
				DependsOn:      []string{"host.preview1.nftables.validate"},
				TriggeredBy:    []string{`host.preview1.nftables.file["20-services"]`},
				CommandPreview: "nft -f /etc/nftables.conf",
				Source:         source,
			},
		},
		Diagnostics: []Diagnostic{},
	}
	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/nftables-port-change.golden.txt", text.String())
}

func TestPlanDebugIncludesProviderAddress(t *testing.T) {
	doc := planFixture(t, "../../../examples/bbr.dbf.hcl", Options{
		CommandFile: "../../../examples/bbr.dbf.hcl",
		Host:        "bbr1",
		Debug:       true,
	})
	if len(doc.Changes) == 0 {
		t.Fatal("debug plan has no changes")
	}
	for _, change := range doc.Changes {
		if change.ProviderAddress == "" {
			t.Fatalf("debug change %s has no provider address", change.Address)
		}
	}

	var text bytes.Buffer
	PrintText(&text, doc)
	if !strings.Contains(text.String(), "provider: kernel_module.bbr1_tcp_bbr") {
		t.Fatalf("debug text plan does not show provider address:\n%s", text.String())
	}
}

func TestActionMatrixJSONAndTerminalGolden(t *testing.T) {
	source := ir.SourceRef{
		File: "examples/action-matrix.dbf.hcl",
		Line: 12,
		Path: `host.server1.files.file["/tmp/example"]`,
	}
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/action-matrix.dbf.hcl", Host: "server1"},
		Summary:       Summary{Create: 1, Update: 1, Delete: 1, NoOp: 1, Operations: 1},
		Changes: []Change{
			{
				Address: `host.server1.files.file["/tmp/create"]`,
				Action:  "create",
				Summary: "create file /tmp/create",
				Source:  source,
				Diff:    BuildDiff("create", nil, map[string]any{"path": "/tmp/create", "mode": "0644"}),
			},
			{
				Address: `host.server1.files.file["/tmp/update"]`,
				Action:  "update",
				Summary: "update file /tmp/update",
				Source:  source,
				Diff: BuildDiff("update",
					map[string]any{"content": "old\n", "mode": "0644"},
					map[string]any{"content": "new\n", "mode": "0600"},
				),
			},
			{
				Address: `host.server1.files.file["/tmp/delete"]`,
				Action:  "delete",
				Summary: "delete file /tmp/delete",
				Source:  source,
				Diff:    BuildDiff("delete", map[string]any{"path": "/tmp/delete"}, nil),
			},
			{
				Address: `host.server1.files.file["/tmp/no-op"]`,
				Action:  "no-op",
				Summary: "no changes for file /tmp/no-op",
				Source:  source,
				Diff:    BuildDiff("no-op", map[string]any{"mode": "0644"}, map[string]any{"mode": "0644"}),
			},
		},
		Operations: []OperationNode{
			{
				Address:        "host.server1.systemd.daemon_reload",
				Action:         "run",
				Summary:        "reload systemd manager configuration",
				TriggeredBy:    []string{`host.server1.files.file["/tmp/update"]`},
				CommandPreview: "systemctl daemon-reload",
				Source:         source,
			},
		},
		Diagnostics: []Diagnostic{},
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "../testdata/plan/action-matrix.golden.json", string(data)+"\n")

	var text bytes.Buffer
	PrintText(&text, doc)
	assertGolden(t, "../testdata/plan/action-matrix.golden.txt", text.String())
}

func TestPrintTextWithColorOption(t *testing.T) {
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/action-matrix.dbf.hcl", Host: "server1"},
		Summary:       Summary{Create: 1, Update: 1, Delete: 1, Operations: 1},
		Changes: []Change{
			{
				Address: `host.server1.files.file["/tmp/create"]`,
				Action:  "create",
				Summary: "create file /tmp/create",
				Diff:    BuildDiff("create", nil, map[string]any{"path": "/tmp/create"}),
			},
			{
				Address: `host.server1.files.file["/tmp/update"]`,
				Action:  "update",
				Summary: "update file /tmp/update",
				Diff: BuildDiff("update",
					map[string]any{"content": "old\n"},
					map[string]any{"content": "new\n"},
				),
			},
			{
				Address: `host.server1.files.file["/tmp/delete"]`,
				Action:  "delete",
				Summary: "delete file /tmp/delete",
				Diff:    BuildDiff("delete", map[string]any{"path": "/tmp/delete"}, nil),
			},
		},
		Operations: []OperationNode{
			{
				Address: "host.server1.systemd.daemon_reload",
				Action:  "run",
				Summary: "reload systemd manager configuration",
			},
		},
		Diagnostics: []Diagnostic{},
	}

	var plain bytes.Buffer
	PrintText(&plain, doc)
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("plain text output contains ANSI:\n%q", plain.String())
	}

	var colored bytes.Buffer
	PrintTextWithOptions(&colored, doc, TextOptions{Color: true})
	rendered := colored.String()
	for _, want := range []string{
		"\x1b[32m+\x1b[0m",
		"\x1b[33m~\x1b[0m",
		"\x1b[31m-\x1b[0m",
		"\x1b[34m!\x1b[0m",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("colored text output missing %q:\n%q", want, rendered)
		}
	}
	if !strings.Contains(rendered, `host.server1.files.file["/tmp/delete"]`) {
		t.Fatalf("colored text output lost address:\n%s", rendered)
	}

	var rich bytes.Buffer
	PrintTextWithOptions(&rich, doc, TextOptions{Color: true, Background: true})
	richRendered := rich.String()
	for _, want := range []string{
		"\x1b[1m\x1b[30m\x1b[42m CREATE \x1b[0m",
		"\x1b[1m\x1b[30m\x1b[43m UPDATE \x1b[0m",
		"\x1b[1m\x1b[97m\x1b[41m DELETE \x1b[0m",
		"\x1b[1m\x1b[97m\x1b[44m RUN \x1b[0m",
		"\x1b[1m\x1b[36mhost.server1.files.file[\"/tmp/delete\"]\x1b[0m",
	} {
		if !strings.Contains(richRendered, want) {
			t.Fatalf("rich colored text output missing %q:\n%q", want, richRendered)
		}
	}
}

func TestPrintTextShowsDeleteBehaviorDiagnostics(t *testing.T) {
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/delete.dbf.hcl", Host: "server1"},
		Summary:       Summary{Delete: 1},
		Changes: []Change{
			{
				Address:        `host.server1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`,
				Action:         "destroy",
				Summary:        "destroy sysctl host.server1.kernel.sysctl[\"net.ipv4.tcp_congestion_control\"]",
				DeleteBehavior: "remove-managed-artifact",
				DeleteNotes: []string{
					"removes /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf",
					"runtime sysctl value is not restored",
				},
				DeleteRisk: "medium",
				Diff:       BuildDiff("destroy", map[string]any{"key": "net.ipv4.tcp_congestion_control"}, nil),
			},
		},
		Diagnostics: []Diagnostic{},
	}

	var text bytes.Buffer
	PrintText(&text, doc)
	rendered := text.String()
	for _, want := range []string{
		"delete behavior: remove-managed-artifact (risk: medium)",
		"meaning: removes DebianForm-managed persistent artifacts; it does not guarantee runtime state restoration.",
		"note: runtime sysctl value is not restored",
		"will not: restore runtime values or guess system defaults.",
		"Delete behavior legend:",
		"  - remove-managed-artifact = removes DebianForm-managed persistent artifacts; it does not guarantee runtime state restoration.",
		"  - unknown = the provider has not declared precise delete behavior; review conservatively.",
		"docs/delete-behavior-diagnostics-plan.zh.md",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("delete behavior text missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Delete behavior legend: forget") {
		t.Fatalf("delete behavior legend should be multiline:\n%s", rendered)
	}

	var noDelete bytes.Buffer
	PrintText(&noDelete, Document{FormatVersion: FormatVersion, Summary: Summary{Create: 1}, Changes: []Change{{
		Address: `host.server1.files.file["/tmp/create"]`,
		Action:  "create",
		Summary: "create file /tmp/create",
		Diff:    BuildDiff("create", nil, map[string]any{"path": "/tmp/create"}),
	}}})
	if strings.Contains(noDelete.String(), "Delete behavior legend:") {
		t.Fatalf("non-delete text output showed delete behavior legend:\n%s", noDelete.String())
	}
}

func TestPrintTextWithBackgroundColorShowsDeleteBehaviorMeaning(t *testing.T) {
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/delete.dbf.hcl", Host: "server1"},
		Summary:       Summary{Delete: 1},
		Changes: []Change{
			{
				Address:        `host.server1.apt.source_file["docker"]`,
				Action:         "forget",
				Summary:        "forget apt source file /etc/apt/sources.list.d/docker.list",
				DeleteBehavior: "forget",
				DeleteNotes:    []string{"keeps the remote apt source file and removes only DebianForm state", "path: /etc/apt/sources.list.d/docker.list"},
				DeleteRisk:     "low",
				Diff:           BuildDiff("forget", map[string]any{"path": "/etc/apt/sources.list.d/docker.list"}, nil),
			},
		},
		Diagnostics: []Diagnostic{},
	}

	var out bytes.Buffer
	PrintTextWithOptions(&out, doc, TextOptions{Color: true, Background: true})
	rendered := out.String()
	for _, want := range []string{
		"\x1b[1m\x1b[97m\x1b[100m FORGET \x1b[0m",
		"\x1b[1m\x1b[97m\x1b[100m FORGET \x1b[0m \x1b[1m\x1b[36mrisk:\x1b[0m \x1b[1m\x1b[97m\x1b[100m LOW \x1b[0m",
		"meaning:",
		"removes only DebianForm state; it does not modify the remote server resource.",
		"will do:",
		"will not:",
		"modify the remote server resource.",
		" = removes only DebianForm state; it does not modify the remote server resource.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rich delete behavior output missing %q:\n%s", want, rendered)
		}
	}
}

func TestPrintHTMLShowsDeleteBehaviorDiagnostics(t *testing.T) {
	doc := Document{
		FormatVersion: FormatVersion,
		GeneratedAt:   "2026-06-20T12:00:00Z",
		Command:       Command{File: "examples/delete.dbf.hcl", Host: "server1"},
		Summary:       Summary{Delete: 1},
		Changes: []Change{
			{
				Address:        `host.server1.users.user["oldapp"]`,
				Action:         "destroy",
				Summary:        "destroy user oldapp",
				DeleteBehavior: "destructive",
				DeleteNotes:    []string{"deletes user and does not restore previous account state"},
				DeleteRisk:     "high",
				Diff:           BuildDiff("destroy", map[string]any{"name": "oldapp"}, nil),
			},
		},
		Diagnostics: []Diagnostic{},
	}
	var out bytes.Buffer
	if err := PrintHTML(&out, doc); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		"Delete behavior",
		"delete-behavior-destructive",
		"deletes user and does not restore previous account state",
		"Delete behavior legend:",
		"docs/delete-behavior-diagnostics-plan.zh.md",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML output missing %q:\n%s", want, html)
		}
	}
}

func planFixture(t *testing.T, fixture string, opts Options) Document {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.CompileWithOptions(cfg, merge.CompileOptions{HostFacts: testHostFacts()})
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := graph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return New(resourceGraph, opts)
}

func testHostFacts() map[string]ir.HostFacts {
	out := map[string]ir.HostFacts{}
	for _, name := range []string{
		"apt1",
		"bbr1",
		"compose1",
		"docker-daemon1",
		"docker1",
		"docker-sources1",
		"docker-users1",
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

func hasOperation(doc Document, address string) bool {
	for _, operation := range doc.Operations {
		if operation.Address == address {
			return true
		}
	}
	return false
}

func hasChange(doc Document, address string) bool {
	for _, change := range doc.Changes {
		if change.Address == address {
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
