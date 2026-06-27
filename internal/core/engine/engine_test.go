package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/merge"
	"github.com/mofelee/debianform/internal/core/parser"
	coreplan "github.com/mofelee/debianform/internal/core/plan"
	corestate "github.com/mofelee/debianform/internal/core/state"
	"github.com/mofelee/debianform/internal/core/termstyle"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestCompareActionMatrix(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.packages.install[\"curl\"]",
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{"name": "curl", "ensure": "present"},
	}
	digest := corestate.DesiredDigest(node.Desired)
	prior := &corestate.Resource{DesiredDigest: digest, Ownership: "managed"}

	tests := []struct {
		name     string
		prior    *corestate.Resource
		observed Observed
		want     string
	}{
		{name: "missing remote creates", observed: Observed{Exists: false}, want: ActionCreate},
		{name: "matching managed is no-op", prior: prior, observed: Observed{Exists: true, DesiredDigest: digest}, want: ActionNoOp},
		{name: "matching unmanaged adopts", observed: Observed{Exists: true, DesiredDigest: digest}, want: ActionAdopt},
		{name: "different remote updates", prior: prior, observed: Observed{Exists: true, DesiredDigest: "old"}, want: ActionUpdate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compare(node, tt.prior, tt.observed)
			if got.Action != tt.want {
				t.Fatalf("action = %q, want %q", got.Action, tt.want)
			}
		})
	}

	absent := node
	absent.Desired = map[string]any{"name": "curl", "ensure": "absent"}
	if got := Compare(absent, prior, Observed{Exists: true, DesiredDigest: digest}); got.Action != ActionDelete {
		t.Fatalf("absent desired action = %q, want delete", got.Action)
	}
}

func TestApplyWithMemoryProviderIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/foundation.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Create == 0 {
		t.Fatalf("initial apply summary = %#v, want creates", plan.Summary)
	}
	if len(provider.Applied) == 0 {
		t.Fatalf("provider did not apply any resources")
	}
	if len(provider.Operations) != 1 || provider.Operations[0] != "host.foundation1.systemd.daemon_reload" {
		t.Fatalf("operations = %#v, want daemon_reload", provider.Operations)
	}

	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 || next.Summary.Create != 0 || next.Summary.Update != 0 || next.Summary.Delete != 0 {
		t.Fatalf("second plan should be no-op, got steps=%#v summary=%#v", next.Steps, next.Summary)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "foundation apply state", string(data))
	for address, resource := range st.Resources {
		if resource.ProviderAddress == "" {
			t.Fatalf("state resource %s has no provider address", address)
		}
	}
	secretAddress := `host.foundation1.secrets.file["/etc/myapp/token"]`
	secret, ok := st.Resources[secretAddress]
	if !ok {
		t.Fatalf("state missing compatibility secret address %s", secretAddress)
	}
	if secret.Kind != "secret" || secret.ProviderType != "file" {
		t.Fatalf("state secret kind/provider = %s/%s, want secret/file", secret.Kind, secret.ProviderType)
	}
}

func TestApplyWritesProgress(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	var progress bytes.Buffer
	engine := Engine{Backend: NewMemoryBackend(), Provider: NewMemoryProvider()}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Progress: &progress}); err != nil {
		t.Fatal(err)
	}

	output := progress.String()
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("progress output contains ANSI:\n%q", output)
	}
	for _, want := range []string{
		"dbf: server1: start lock state",
		`dbf: server1: start create host.server1.files.file["/tmp/a"] - create file /tmp/a`,
		`dbf: server1: done create host.server1.files.file["/tmp/a"] - create file /tmp/a`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("progress output missing %q:\n%s", want, output)
		}
	}
}

func TestProgressTaskLogsHeartbeat(t *testing.T) {
	var output bytes.Buffer
	progress := newProgressLogger(&output)
	progress.interval = 10 * time.Millisecond

	task := progress.Start("server1", "apply", `host.server1.files.file["/tmp/slow"]`, "write file /tmp/slow")
	time.Sleep(25 * time.Millisecond)
	task.Done(nil)

	text := output.String()
	if !strings.Contains(text, `dbf: server1: still apply host.server1.files.file["/tmp/slow"] - write file /tmp/slow`) {
		t.Fatalf("progress output missing heartbeat:\n%s", text)
	}
}

func TestProgressTaskCanUseStyledStatusBadges(t *testing.T) {
	var output bytes.Buffer
	progress := newProgressLoggerWithStyle(&output, termstyle.Options{Color: true, Unicode: true, Background: true})
	progress.interval = time.Hour

	task := progress.Start("server1", "create", `host.server1.files.file["/tmp/a"]`, "create file /tmp/a")
	task.Done(nil)

	text := output.String()
	for _, want := range []string{
		"\x1b[1m\x1b[97m\x1b[44m ▶ START \x1b[0m",
		"\x1b[1m\x1b[30m\x1b[42m ✓ DONE \x1b[0m",
		"\x1b[1m\x1b[36mserver1:\x1b[0m",
		"\x1b[32mcreate\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("styled progress output missing %q:\n%q", want, text)
		}
	}
}

func TestApplyStateDoesNotLeakCurrentSensitiveBaseline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
		host    string
	}{
		{name: "secrets file", fixture: "../testdata/fixtures/foundation.dbf.hcl", host: "foundation1"},
		{name: "sensitive file content", fixture: "../../../examples/files-plan-preview.dbf.hcl", host: "preview1"},
		{name: "sensitive component input", fixture: "../../../examples/component-inputs.dbf.hcl", host: "input1"},
		{name: "sensitive service environment", fixture: "../testdata/fixtures/sensitive-service-environment.dbf.hcl", host: "server1"},
		{name: "ephemeral variable content", fixture: "../testdata/fixtures/ephemeral-variable-content.dbf.hcl", host: "ephemeral1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			program, resourceGraph := fixtureProgramAndGraph(t, tt.fixture)
			backend := NewMemoryBackend()
			engine := Engine{
				Backend:  backend,
				Provider: NewMemoryProvider(),
				Now: func() time.Time {
					return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
				},
			}
			if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: tt.host}); err != nil {
				t.Fatal(err)
			}
			var host ir.HostSpec
			for _, candidate := range program.Hosts {
				if candidate.Name == tt.host {
					host = candidate
					break
				}
			}
			if host.Name == "" {
				t.Fatalf("host %q not found", tt.host)
			}
			st, err := backend.Read(context.Background(), host)
			if err != nil {
				t.Fatal(err)
			}
			data, err := corestate.Encode(st)
			if err != nil {
				t.Fatal(err)
			}
			testassert.NoSecretLeak(t, tt.name+" apply state", string(data))
		})
	}
}

func TestApplyWriteOnlyFilePersistsVersionAndPassesPayload(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/ephemeral-variable-content.dbf.hcl")
	backend := NewMemoryBackend()
	provider := &recordingPayloadProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "ephemeral1"}); err != nil {
		t.Fatal(err)
	}
	address := `host.ephemeral1.files.file["/etc/debianform/runtime-token.txt"]`
	payload := provider.Payloads[address]
	if payload["content"] != testassert.EphemeralVariableValue {
		t.Fatalf("provider payload content = %#v, want ephemeral value", payload["content"])
	}
	host := program.Hosts[0]
	st, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	resource := st.Resources[address]
	if resource.Desired["content_version"] != "v1" {
		t.Fatalf("state content_version = %#v, want v1", resource.Desired["content_version"])
	}
	for _, key := range []string{"content", "content_sha256", "content_bytes", "summary"} {
		if _, ok := resource.Desired[key]; ok {
			t.Fatalf("state desired contains %s: %#v", key, resource.Desired)
		}
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "write-only apply state", string(data))
}

func TestApplyPersistsOnlySuccessfulStepsOnFailure(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	provider.FailApplyAt = `host.bbr1.kernel.sysctl["net.core.default_qdisc"]`
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err == nil {
		t.Fatal("apply succeeded, want injected failure")
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[`host.bbr1.kernel.module["tcp_bbr"]`]; !ok {
		t.Fatalf("successful dependency was not persisted: %#v", st.Resources)
	}
	if _, ok := st.Resources[`host.bbr1.kernel.sysctl["net.core.default_qdisc"]`]; ok {
		t.Fatalf("failed step was persisted: %#v", st.Resources)
	}
}

func TestApplyDestroysOrphanStateResource(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.packages.install["curl"]`
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "bbr1",
		Resources: map[string]corestate.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "package",
				Ownership:     "managed",
				DesiredDigest: "old",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Destroyed) != 1 || provider.Destroyed[0] != orphanAddress {
		t.Fatalf("destroyed = %#v, want %s", provider.Destroyed, orphanAddress)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("orphan still in state: %#v", st.Resources)
	}
}

func TestApplyForgetsSharedDirectoryOrphanWithoutDestroy(t *testing.T) {
	program := &ir.Program{
		Hosts: []ir.HostSpec{{
			Name: "server1",
		}},
	}
	activeAddress := `host.server1.components.wg_backup.directories.directory["/etc/wireguard"]`
	orphanAddress := `host.server1.components.wg_prod.directories.directory["/etc/wireguard"]`
	desired := map[string]any{
		"path":   "/etc/wireguard",
		"owner":  "root",
		"group":  "systemd-network",
		"mode":   "0750",
		"ensure": "present",
	}
	resourceGraph := &graph.ResourceGraph{
		Nodes: []graph.Node{{
			Address:         activeAddress,
			Host:            "server1",
			Kind:            "directory",
			Summary:         "create directory /etc/wireguard",
			Desired:         cloneMap(desired),
			ProviderType:    "directory",
			ProviderAddress: "directory.server1_wg_backup_etc_wireguard",
			ProviderPayload: cloneMap(desired),
		}},
	}
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "server1",
		Resources: map[string]corestate.Resource{
			activeAddress: {
				Host:            "server1",
				Kind:            "directory",
				ProviderType:    "directory",
				ProviderAddress: "directory.server1_wg_backup_etc_wireguard",
				Ownership:       "managed",
				Desired:         cloneMap(desired),
				DesiredDigest:   corestate.DesiredDigest(desired),
				Observed:        map[string]any{"exists": true},
			},
			orphanAddress: {
				Host:            "server1",
				Kind:            "directory",
				ProviderType:    "directory",
				ProviderAddress: "directory.server1_wg_prod_etc_wireguard",
				Ownership:       "managed",
				Desired:         cloneMap(desired),
				DesiredDigest:   corestate.DesiredDigest(desired),
				Observed:        map[string]any{"exists": true},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget shared directory orphan", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("shared directory orphan still in state: %#v", st.Resources)
	}
	if _, ok := st.Resources[activeAddress]; !ok {
		t.Fatalf("active shared directory missing from state: %#v", st.Resources)
	}
}

func TestApplyForgetsAdoptedOrphanWithoutDestroy(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.packages.install["curl"]`
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "bbr1",
		Resources: map[string]corestate.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "package",
				Ownership:     "adopted",
				DesiredDigest: "old",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[orphanAddress]; ok {
		t.Fatalf("adopted orphan still in state: %#v", st.Resources)
	}
}

func TestApplyForgetsAPTSourceFileOrphanWhenDestroyKeeps(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.apt.source_file["main"]`
	if err := backend.Write(context.Background(), program.Hosts[0], corestate.State{
		Version: corestate.Version,
		Host:    "bbr1",
		Resources: map[string]corestate.Resource{
			orphanAddress: {
				Host:          "bbr1",
				Kind:          "apt_source_file",
				Ownership:     "managed",
				DesiredDigest: "old",
				Desired:       map[string]any{"path": "/etc/apt/sources.list", "on_destroy": "keep"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, &graph.ResourceGraph{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Action != ActionForget {
		t.Fatalf("plan steps = %#v, want forget", plan.Steps)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no remote destroy", provider.Destroyed)
	}
}

func TestBBRMemoryApplyIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	engine := Engine{Backend: NewMemoryBackend(), Provider: NewMemoryProvider()}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 {
		t.Fatalf("second BBR plan steps = %#v, want no-op", next.Steps)
	}
}

func TestDockerEngineMemoryApplyIsIdempotentAndPersistsHighLevelState(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-minimal.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Create != 8 || plan.Summary.Operations != 1 {
		t.Fatalf("docker apply summary = %#v, want 8 creates and 1 operation", plan.Summary)
	}
	if len(provider.Operations) != 1 || provider.Operations[0] != "host.docker1.apt.cache_refresh" {
		t.Fatalf("docker operations = %#v, want apt cache refresh", provider.Operations)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		address         string
		kind            string
		providerType    string
		providerAddress string
	}{
		{
			address:         `host.docker1.docker.apt.signing_key["docker-official"]`,
			kind:            "apt_signing_key",
			providerType:    "apt_signing_key",
			providerAddress: "apt_signing_key.docker1_docker_official",
		},
		{
			address:         `host.docker1.docker.apt.repository["docker-official"]`,
			kind:            "file",
			providerType:    "file",
			providerAddress: "file.docker1__etc_apt_sources_list_d_docker_official_sources",
		},
		{
			address:         `host.docker1.docker.package["docker-ce"]`,
			kind:            "package",
			providerType:    "package",
			providerAddress: "package.docker1_docker_docker_ce",
		},
		{
			address:         `host.docker1.docker.service["docker"]`,
			kind:            "service",
			providerType:    "service",
			providerAddress: "service.docker1_docker",
		},
	} {
		resource, ok := st.Resources[want.address]
		if !ok {
			t.Fatalf("docker state missing %s; resources=%#v", want.address, st.Resources)
		}
		if resource.Kind != want.kind || resource.ProviderType != want.providerType || resource.ProviderAddress != want.providerAddress {
			t.Fatalf("state resource %s kind/provider = %s/%s/%s, want %s/%s/%s", want.address, resource.Kind, resource.ProviderType, resource.ProviderAddress, want.kind, want.providerType, want.providerAddress)
		}
		if resource.Ownership != "managed" {
			t.Fatalf("state resource %s ownership = %q, want managed", want.address, resource.Ownership)
		}
	}

	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 || next.Summary.Create != 0 || next.Summary.Update != 0 || next.Summary.Delete != 0 || next.Summary.Operations != 0 {
		t.Fatalf("second docker plan should be no-op, got steps=%#v summary=%#v", next.Steps, next.Summary)
	}
}

func TestDockerDaemonDesiredChangePlansRestartOperation(t *testing.T) {
	initial := `
host "server1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "100m"
          "max-file" = "3"
        }
      }
    }
  }
}
`
	updated := strings.Replace(initial, `"100m"`, `"200m"`, 1)
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, initial))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}

	nextProgram, nextGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, updated))
	plan, err := engine.Plan(context.Background(), nextProgram, nextGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.server1.docker.daemon.file["/etc/docker/daemon.json"]`, ActionUpdate) {
		t.Fatalf("daemon desired change plan missing daemon file update: %#v", plan.Steps)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Operation.Address != "host.server1.docker.daemon.restart" {
		t.Fatalf("daemon desired change operations = %#v, want docker daemon restart", plan.Operations)
	}
}

func TestDockerComposeMemoryApplyDoesNotLeakEnvFileContent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{
		Backend:  backend,
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
		},
	}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.compose1.docker.compose["app"].env_file["app"]`, ActionCreate) {
		t.Fatalf("compose apply plan missing env file create: %#v", plan.Steps)
	}
	if len(provider.Operations) != 3 ||
		!containsString(provider.Operations, `host.compose1.docker.compose["app"].validate`) ||
		!containsString(provider.Operations, `host.compose1.docker.compose["app"].daemon_reload`) {
		t.Fatalf("compose operations = %#v, want apt refresh, compose daemon_reload, and compose validate", provider.Operations)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "compose apply state", string(data))
	env, ok := st.Resources[`host.compose1.docker.compose["app"].env_file["app"]`]
	if !ok {
		t.Fatalf("state missing compose env file: %#v", st.Resources)
	}
	if _, ok := env.Desired["content"]; ok {
		t.Fatalf("state env desired leaked content: %#v", env.Desired)
	}
	if env.Desired["content_sha256"] == "" || env.Desired["content_bytes"] == nil {
		t.Fatalf("state env desired missing content summary: %#v", env.Desired)
	}
}

func TestDockerComposeMemoryApplyIsIdempotent(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "compose1"}); err != nil {
		t.Fatal(err)
	}
	next, err := engine.Plan(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Steps) != 0 || next.Summary.Create != 0 || next.Summary.Update != 0 || next.Summary.Delete != 0 || next.Summary.Operations != 0 {
		t.Fatalf("second compose plan should be no-op, got steps=%#v summary=%#v", next.Steps, next.Summary)
	}
}

func TestDockerComposeMemoryApplyIncludesSystemdUnitAndService(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	backend := NewMemoryBackend()
	provider := &recordingPayloadProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Host: "compose1"}); err != nil {
		t.Fatal(err)
	}
	if !containsString(provider.Operations, `host.compose1.docker.compose["app"].daemon_reload`) {
		t.Fatalf("compose operations = %#v, want daemon_reload", provider.Operations)
	}
	unitPayload := provider.Payloads[`host.compose1.docker.compose["app"].systemd_unit`]
	content, _ := unitPayload["content"].(string)
	if !strings.Contains(content, "ExecStart=/usr/bin/docker compose -p app -f /opt/app/compose.yaml up -d") {
		t.Fatalf("compose unit payload missing ExecStart:\n%s", content)
	}
	servicePayload := provider.Payloads[`host.compose1.docker.compose["app"].service`]
	if servicePayload["unit"] != "debianform-compose-app.service" || servicePayload["enabled"] != true || servicePayload["state"] != "running" {
		t.Fatalf("compose service payload = %#v", servicePayload)
	}
}

func TestComponentScriptOnChangeOperationRunsAfterFileChange(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/component-script-on-change.dbf.hcl")
	provider := NewMemoryProvider()
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	fileAddress := `host.app1.components.app.files.file["/etc/managed-app/config.env"]`
	scriptAddress := `host.app1.components.app.script["reload"]`
	if !hasStepAction(plan, fileAddress, ActionCreate) {
		t.Fatalf("apply plan missing component file create: %#v", plan.Steps)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Operation.Address != scriptAddress {
		t.Fatalf("apply operations = %#v, want script reload", plan.Operations)
	}
	if !containsString(provider.Operations, scriptAddress) {
		t.Fatalf("provider operations = %#v, want script reload", provider.Operations)
	}
}

func TestDockerComposeMemoryCheckDetectsStoppedProjectDrift(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	provider := NewMemoryProvider()
	provider.Observed[`host.compose1.docker.compose["app"].project`] = Observed{Exists: true, DesiredDigest: "drifted", Values: map[string]any{
		"exists": true,
		"state":  "stopped",
		"services": map[string]any{
			"total":   1,
			"running": 0,
			"stopped": 1,
		},
	}}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.compose1.docker.compose["app"].project`, ActionUpdate) {
		t.Fatalf("compose stopped drift plan missing project update: %#v", plan.Steps)
	}
}

func TestDockerComposeDriftPlanTextShowsProjectStateAndOrphans(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-compose.dbf.hcl")
	provider := NewMemoryProvider()
	provider.Observed[`host.compose1.docker.compose["app"].project`] = Observed{Exists: true, DesiredDigest: "drifted", Values: map[string]any{
		"exists": true,
		"state":  "running",
		"services": map[string]any{
			"total":    2,
			"running":  2,
			"stopped":  0,
			"expected": []string{"web"},
			"actual":   []string{"web", "worker"},
		},
		"containers":      map[string]any{"total": 2},
		"orphan_count":    1,
		"orphan_services": []string{"worker"},
	}}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{Host: "compose1"})
	if err != nil {
		t.Fatal(err)
	}
	doc := plan.Document(coreplan.Options{CommandFile: "../../../examples/docker-compose.dbf.hcl", Host: "compose1"})
	var text bytes.Buffer
	coreplan.PrintText(&text, doc)
	got := text.String()
	assertGolden(t, "../testdata/plan/docker-compose-project-drift.golden.txt", got)
	for _, want := range []string{
		"orphan_count: 1",
		`"worker"`,
		`services.actual`,
		`services.expected`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compose drift text missing %q:\n%s", want, got)
		}
	}
}

func TestDockerEngineMemoryCheckDetectsPackageAndServiceDrift(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/docker-minimal.dbf.hcl")
	provider := NewMemoryProvider()
	provider.Observed[`host.docker1.docker.package["docker-ce"]`] = Observed{Exists: false}
	provider.Observed[`host.docker1.docker.service["docker"]`] = Observed{Exists: true, DesiredDigest: "drifted", Values: map[string]any{
		"enabled": false,
		"active":  false,
	}}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	plan, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepAction(plan, `host.docker1.docker.package["docker-ce"]`, ActionCreate) {
		t.Fatalf("docker drift plan missing package create step: %#v", plan.Steps)
	}
	if !hasStepAction(plan, `host.docker1.docker.service["docker"]`, ActionUpdate) {
		t.Fatalf("docker drift plan missing service update step: %#v", plan.Steps)
	}
	if len(plan.Steps) == 0 {
		t.Fatalf("docker drift plan has no steps; check would incorrectly pass")
	}
}

func TestPlanPropagatesObservedReadFailure(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/bbr.dbf.hcl")
	engine := Engine{
		Backend:  NewMemoryBackend(),
		Provider: planErrorProvider{MemoryProvider: NewMemoryProvider()},
	}

	_, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err == nil || !strings.Contains(err.Error(), "injected observed read failure") {
		t.Fatalf("plan error = %v, want observed read failure", err)
	}
}

func TestApplyRunsHostsInParallelWithDeterministicResultOrder(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server2" {
  files {
    file "/tmp/server2" {
      content = "server2"
    }
  }
}

host "server1" {
  files {
    file "/tmp/server1" {
      content = "server1"
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2})
	if err != nil {
		t.Fatal(err)
	}
	if provider.maxActive < 2 {
		t.Fatalf("max concurrent applies = %d, want at least 2", provider.maxActive)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v, want 2", plan.Steps)
	}
	if plan.Steps[0].Host != "server1" || plan.Steps[1].Host != "server2" {
		t.Fatalf("step host order = %s, %s; want server1, server2", plan.Steps[0].Host, plan.Steps[1].Host)
	}
	for _, host := range program.Hosts {
		st, err := backend.Read(context.Background(), host)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Resources) != 1 {
			t.Fatalf("host %s state resources = %#v, want 1", host.Name, st.Resources)
		}
	}
}

func TestApplyHonorsDefaultPerHostSerialExecution(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive != 1 {
		t.Fatalf("max concurrent applies = %d, want default per-host serial execution", provider.maxActive)
	}
}

func TestApplyAllowsSafeParallelWithinHostWhenConfigured(t *testing.T) {
	program, resourceGraph := twoFileProgramAndGraph("server1")
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2, PerHostParallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive < 2 {
		t.Fatalf("max concurrent applies = %d, want safe same-host resources to run in parallel", provider.maxActive)
	}
}

func TestApplyGlobalParallelLimit(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}, {Name: "server2"}, {Name: "server3"}}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/server1", nil),
		fileNode("server2", "/tmp/server2", nil),
		fileNode("server3", "/tmp/server3", nil),
	}}
	provider := &concurrencyProvider{MemoryProvider: NewMemoryProvider()}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2}); err != nil {
		t.Fatal(err)
	}
	if provider.maxActive != 2 {
		t.Fatalf("max concurrent applies = %d, want global limit 2", provider.maxActive)
	}
}

func TestApplySkipsDependentsAfterFailure(t *testing.T) {
	program := &ir.Program{Hosts: []ir.HostSpec{{Name: "server1"}}}
	failing := `host.server1.files.file["/tmp/failing"]`
	dependent := `host.server1.files.file["/tmp/dependent"]`
	independent := `host.server1.files.file["/tmp/independent"]`
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode("server1", "/tmp/failing", nil),
		fileNode("server1", "/tmp/independent", nil),
		fileNode("server1", "/tmp/dependent", []string{failing}),
	}}
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	provider.FailApplyAt = failing
	engine := Engine{Backend: backend, Provider: provider}

	_, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 2, PerHostParallel: 2})
	if err == nil || !strings.Contains(err.Error(), failing) {
		t.Fatalf("apply error = %v, want failing resource", err)
	}
	if containsString(provider.Applied, dependent) {
		t.Fatalf("dependent resource was applied after failed dependency: %#v", provider.Applied)
	}
	if !containsString(provider.Applied, independent) {
		t.Fatalf("independent resource was not applied after unrelated failure: %#v", provider.Applied)
	}
	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Resources[independent]; !ok {
		t.Fatalf("independent resource was not persisted: %#v", st.Resources)
	}
	if _, ok := st.Resources[dependent]; ok {
		t.Fatalf("dependent resource was persisted despite skipped dependency: %#v", st.Resources)
	}
}

func TestPreventDestroyBlocksOrphanDestroy(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  files {
    file "/tmp/protected" {
      content = "managed"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`))
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, Options{}); err != nil {
		t.Fatal(err)
	}

	emptyProgram, emptyGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {}
`))
	_, err := engine.Apply(context.Background(), emptyProgram, emptyGraph, Options{})
	if err == nil {
		t.Fatal("apply succeeded, want prevent_destroy error")
	}
	if !strings.Contains(err.Error(), "lifecycle.prevent_destroy") {
		t.Fatalf("error = %v, want prevent_destroy", err)
	}
	if len(provider.Destroyed) != 0 {
		t.Fatalf("destroyed = %#v, want no destroy", provider.Destroyed)
	}
}

func TestPreventDestroyBlocksExplicitDelete(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  files {
    file "/tmp/protected" {
      ensure = "absent"

      lifecycle {
        prevent_destroy = true
      }
    }
  }
}
`))
	provider := NewMemoryProvider()
	address := `host.server1.files.file["/tmp/protected"]`
	provider.Observed[address] = Observed{Exists: true}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	_, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err == nil {
		t.Fatal("plan succeeded, want prevent_destroy error")
	}
	if !strings.Contains(err.Error(), "lifecycle.prevent_destroy") {
		t.Fatalf("error = %v, want prevent_destroy", err)
	}
}

func TestDeleteDiagnosticsForPlanDocument(t *testing.T) {
	tests := []struct {
		name         string
		step         Step
		wantBehavior string
		wantRisk     string
		wantNote     string
	}{
		{
			name: "sysctl remove managed artifact",
			step: Step{
				Address: "host.server1.kernel.sysctl[\"net.ipv4.tcp_congestion_control\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "sysctl",
					Desired: map[string]any{"key": "net.ipv4.tcp_congestion_control", "value": "bbr"},
				},
			},
			wantBehavior: "remove-managed-artifact",
			wantRisk:     "medium",
			wantNote:     "runtime sysctl value is not restored",
		},
		{
			name: "apt source keep forget",
			step: Step{
				Address: "host.server1.apt.source_file[\"main\"]",
				Action:  ActionForget,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "apt_source_file",
					Desired: map[string]any{"path": "/etc/apt/sources.list.d/main.sources", "on_destroy": "keep"},
				},
			},
			wantBehavior: "forget",
			wantRisk:     "low",
			wantNote:     "without modifying the remote resource",
		},
		{
			name: "apt source restore",
			step: Step{
				Address: "host.server1.apt.source_file[\"main\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "apt_source_file",
					Desired: map[string]any{"path": "/etc/apt/sources.list.d/main.sources", "on_destroy": "restore"},
				},
			},
			wantBehavior: "restore-original",
			wantRisk:     "medium",
			wantNote:     "restores the apt source file content",
		},
		{
			name: "directory destructive",
			step: Step{
				Address: "host.server1.directories.directory[\"/srv/app\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "directory",
					Desired: map[string]any{"path": "/srv/app"},
				},
			},
			wantBehavior: "destructive",
			wantRisk:     "high",
			wantNote:     "removes directory recursively",
		},
		{
			name: "systemd unit external side effect",
			step: Step{
				Address: "host.server1.systemd.unit[\"app.service\"]",
				Action:  ActionDestroy,
				Prior: &corestate.Resource{
					Host:    "server1",
					Kind:    "systemd_unit",
					Desired: map[string]any{"path": "/etc/systemd/system/app.service"},
				},
			},
			wantBehavior: "external-side-effect",
			wantRisk:     "high",
			wantNote:     "daemon-reload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Plan{Steps: []Step{tt.step}, Summary: coreplan.Summary{Delete: 1}}.Document(coreplan.Options{})
			if len(doc.Changes) != 1 {
				t.Fatalf("changes = %d, want 1", len(doc.Changes))
			}
			change := doc.Changes[0]
			if change.DeleteBehavior != tt.wantBehavior {
				t.Fatalf("delete behavior = %q, want %q; change=%#v", change.DeleteBehavior, tt.wantBehavior, change)
			}
			if change.DeleteRisk != tt.wantRisk {
				t.Fatalf("delete risk = %q, want %q; change=%#v", change.DeleteRisk, tt.wantRisk, change)
			}
			if !containsSubstring(change.DeleteNotes, tt.wantNote) {
				t.Fatalf("delete notes = %#v, want substring %q", change.DeleteNotes, tt.wantNote)
			}
		})
	}
}

func TestDeleteDiagnosticsOmittedForNonDeleteActions(t *testing.T) {
	step := Step{
		Address: "host.server1.files.file[\"/tmp/example\"]",
		Action:  ActionCreate,
		Node: graph.Node{
			Host:    "server1",
			Kind:    "file",
			Desired: map[string]any{"path": "/tmp/example"},
		},
	}
	doc := Plan{Steps: []Step{step}, Summary: coreplan.Summary{Create: 1}}.Document(coreplan.Options{})
	if len(doc.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(doc.Changes))
	}
	change := doc.Changes[0]
	if change.DeleteBehavior != "" || len(change.DeleteNotes) != 0 || change.DeleteRisk != "" {
		t.Fatalf("non-delete change has delete diagnostics: %#v", change)
	}
}

func fixtureProgramAndGraph(t *testing.T, fixture string) (*ir.Program, *graph.ResourceGraph) {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := graph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return program, resourceGraph
}

func twoFileProgramAndGraph(host string) (*ir.Program, *graph.ResourceGraph) {
	return &ir.Program{Hosts: []ir.HostSpec{{Name: host}}}, &graph.ResourceGraph{Nodes: []graph.Node{
		fileNode(host, "/tmp/a", nil),
		fileNode(host, "/tmp/b", nil),
	}}
}

func fileNode(host, path string, deps []string) graph.Node {
	return graph.Node{
		Host:      host,
		Address:   "host." + host + ".files.file[" + fmt.Sprintf("%q", path) + "]",
		Kind:      "file",
		Summary:   "create file " + path,
		DependsOn: deps,
		Desired: map[string]any{
			"path":    path,
			"content": path,
			"owner":   "root",
			"group":   "root",
			"mode":    "0644",
			"ensure":  "present",
		},
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func hasStepAction(plan Plan, address string, action string) bool {
	for _, step := range plan.Steps {
		if step.Address == address && step.Action == action {
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
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func writeEngineConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}

type planErrorProvider struct {
	*MemoryProvider
}

func (p planErrorProvider) Plan(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	return ProviderPlan{}, fmt.Errorf("injected observed read failure")
}

type recordingPayloadProvider struct {
	*MemoryProvider
	Payloads map[string]map[string]any
}

func (p *recordingPayloadProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	if p.Payloads == nil {
		p.Payloads = map[string]map[string]any{}
	}
	p.Payloads[step.Address] = cloneMap(step.Node.ProviderPayload)
	return p.MemoryProvider.Apply(ctx, step)
}

type concurrencyProvider struct {
	*MemoryProvider
	mu        sync.Mutex
	active    int
	maxActive int
}

func (p *concurrencyProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.MemoryProvider.Apply(ctx, step)
}
