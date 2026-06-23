package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mofelee/debianform/internal/v2/graph"
	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/merge"
	"github.com/mofelee/debianform/internal/v2/parser"
	v2state "github.com/mofelee/debianform/internal/v2/state"
	"github.com/mofelee/debianform/internal/v2/testassert"
)

func TestCompareActionMatrix(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.packages.install[\"curl\"]",
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{"name": "curl", "ensure": "present"},
	}
	digest := v2state.DesiredDigest(node.Desired)
	prior := &v2state.Resource{DesiredDigest: digest, Ownership: "managed"}

	tests := []struct {
		name     string
		prior    *v2state.Resource
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
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/v2-foundation.dbf.hcl")
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
	data, err := v2state.Encode(st)
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

func TestApplyStateDoesNotLeakCurrentSensitiveBaseline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
		host    string
	}{
		{name: "secrets file", fixture: "../testdata/fixtures/v2-foundation.dbf.hcl", host: "foundation1"},
		{name: "sensitive file content", fixture: "../../../examples/v2-files-plan-preview.dbf.hcl", host: "preview1"},
		{name: "sensitive component input", fixture: "../../../examples/v2-component-inputs.dbf.hcl", host: "input1"},
		{name: "sensitive service environment", fixture: "../testdata/fixtures/v2-sensitive-service-environment.dbf.hcl", host: "server1"},
		{name: "ephemeral variable content", fixture: "../testdata/fixtures/v2-ephemeral-variable-content.dbf.hcl", host: "ephemeral1"},
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
			data, err := v2state.Encode(st)
			if err != nil {
				t.Fatal(err)
			}
			testassert.NoSecretLeak(t, tt.name+" apply state", string(data))
		})
	}
}

func TestApplyWriteOnlyFilePersistsVersionAndPassesPayload(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../testdata/fixtures/v2-ephemeral-variable-content.dbf.hcl")
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
	data, err := v2state.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "write-only apply state", string(data))
}

func TestApplyPersistsOnlySuccessfulStepsOnFailure(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
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
	program, _ := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.packages.install["curl"]`
	if err := backend.Write(context.Background(), program.Hosts[0], v2state.State{
		Version: v2state.Version,
		Host:    "bbr1",
		Resources: map[string]v2state.Resource{
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

func TestApplyForgetsAdoptedOrphanWithoutDestroy(t *testing.T) {
	program, _ := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.packages.install["curl"]`
	if err := backend.Write(context.Background(), program.Hosts[0], v2state.State{
		Version: v2state.Version,
		Host:    "bbr1",
		Resources: map[string]v2state.Resource{
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
	program, _ := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	orphanAddress := `host.bbr1.apt.source_file["main"]`
	if err := backend.Write(context.Background(), program.Hosts[0], v2state.State{
		Version: v2state.Version,
		Host:    "bbr1",
		Resources: map[string]v2state.Resource{
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
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
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
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-docker-minimal.dbf.hcl")
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
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-docker-compose.dbf.hcl")
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
	if len(provider.Operations) != 2 || !containsString(provider.Operations, `host.compose1.docker.compose["app"].validate`) {
		t.Fatalf("compose operations = %#v, want apt refresh and compose validate", provider.Operations)
	}

	st, err := backend.Read(context.Background(), program.Hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := v2state.Encode(st)
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
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-docker-compose.dbf.hcl")
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

func TestDockerComposeMemoryCheckDetectsStoppedProjectDrift(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-docker-compose.dbf.hcl")
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

func TestDockerEngineMemoryCheckDetectsPackageAndServiceDrift(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-docker-minimal.dbf.hcl")
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
	program, resourceGraph := fixtureProgramAndGraph(t, "../../../examples/v2-bbr.dbf.hcl")
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

func hasStepAction(plan Plan, address string, action string) bool {
	for _, step := range plan.Steps {
		if step.Address == address && step.Action == action {
			return true
		}
	}
	return false
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

func (p planErrorProvider) Plan(ctx context.Context, node graph.Node, prior *v2state.Resource) (ProviderPlan, error) {
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
