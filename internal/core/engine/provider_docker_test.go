package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestNativeProviderDockerSigningKeyApplyScript(t *testing.T) {
	node := graph.Node{
		Address: `host.docker1.docker.apt.signing_key["docker-official"]`,
		Host:    "docker1",
		Kind:    "apt_signing_key",
		Desired: map[string]any{
			"path":   "/etc/apt/keyrings/docker.asc",
			"url":    "https://download.docker.com/linux/debian/gpg",
			"sha256": "1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570",
			"owner":  "root",
			"group":  "root",
			"mode":   "0644",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing docker signing key action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"curl -fsSL 'https://download.docker.com/linux/debian/gpg' -o '/etc/apt/keyrings/docker.asc.dbf-tmp'",
		"printf '%s  %s\\n' '1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570' '/etc/apt/keyrings/docker.asc.dbf-tmp' | sha256sum --check --status",
		"install -o 'root' -g 'root' -m '0644' '/etc/apt/keyrings/docker.asc.dbf-tmp' '/etc/apt/keyrings/docker.asc'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("docker signing key script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerRepositoryApplyScript(t *testing.T) {
	content := "Types: deb\nURIs: https://download.docker.com/linux/debian\nSuites: trixie\nComponents: stable\nArchitectures: amd64\nSigned-By: /etc/apt/keyrings/docker.asc\n"
	node := graph.Node{
		Address: `host.docker1.docker.apt.repository["docker-official"]`,
		Host:    "docker1",
		Kind:    "file",
		Desired: map[string]any{
			"path":    "/etc/apt/sources.list.d/docker_official.sources",
			"content": content,
			"owner":   "root",
			"group":   "root",
			"mode":    "0644",
			"ensure":  "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing docker repository action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) == 0 || runner.inputs[0] != content {
		t.Fatalf("docker repository apply input = %#v, want repository content", runner.inputs)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"install -o 'root' -g 'root' -m '0644' \"$tmp\" \"$dest\"",
		"dest='/etc/apt/sources.list.d/docker_official.sources'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("docker repository script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerDaemonFileApplyScript(t *testing.T) {
	content := "{\n  \"log-driver\": \"json-file\",\n  \"log-opts\": {\n    \"max-file\": \"3\",\n    \"max-size\": \"100m\"\n  }\n}\n"
	node := graph.Node{
		Address: `host.docker-daemon1.docker.daemon.file["/etc/docker/daemon.json"]`,
		Host:    "docker-daemon1",
		Kind:    "file",
		Desired: map[string]any{
			"path":    "/etc/docker/daemon.json",
			"content": content,
			"owner":   "root",
			"group":   "root",
			"mode":    "0644",
			"ensure":  "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing docker daemon file action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) == 0 || runner.inputs[0] != content {
		t.Fatalf("docker daemon apply input = %#v, want daemon JSON", runner.inputs)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"dest='/etc/docker/daemon.json'",
		"install -o 'root' -g 'root' -m '0644' \"$tmp\" \"$dest\"",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("docker daemon file script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerServiceApplyScript(t *testing.T) {
	node := graph.Node{
		Address: `host.docker1.docker.service["docker"]`,
		Host:    "docker1",
		Kind:    "service",
		Desired: map[string]any{
			"name":    "docker",
			"unit":    "docker.service",
			"enabled": true,
			"state":   "running",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "enabled=disabled\nactive=inactive\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || !strings.Contains(got.Summary, "enable start service docker.service") {
		t.Fatalf("docker service drift plan = %#v, want enable/start update", got)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"systemctl enable 'docker.service'",
		"systemctl start 'docker.service'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("docker service script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerComposeValidateOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Host:           "compose.example.com",
		Address:        `host.compose1.docker.compose["app"].validate`,
		Action:         "run",
		CommandPreview: "docker compose -p app -f /opt/app/compose.yaml config",
	}

	if _, err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || runner.scripts[0] != operation.CommandPreview {
		t.Fatalf("compose validate command = %#v, want %q", runner.scripts, operation.CommandPreview)
	}
	if len(runner.hosts) != 1 || runner.hosts[0] != operation.Host {
		t.Fatalf("compose validate hosts = %#v, want explicit host %q", runner.hosts, operation.Host)
	}
}

func TestNativeProviderDockerComposeDaemonReloadOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Host:           "compose1",
		Address:        `host.compose1.docker.compose["app"].daemon_reload`,
		Action:         "run",
		CommandPreview: "systemctl daemon-reload",
	}

	if _, err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || runner.scripts[0] != operation.CommandPreview {
		t.Fatalf("compose daemon-reload command = %#v, want %q", runner.scripts, operation.CommandPreview)
	}
}

func TestNativeProviderDockerComposeProjectPlanStates(t *testing.T) {
	tests := []struct {
		name   string
		state  string
		stdout string
		prior  *corestate.Resource
		want   string
	}{
		{name: "running no-op", state: "running", stdout: `[{"Name":"web","State":"running"}]`, prior: &corestate.Resource{Ownership: "managed"}, want: ActionNoOp},
		{name: "stopped drift", state: "running", stdout: `[{"Name":"web","State":"exited"}]`, prior: &corestate.Resource{Ownership: "managed"}, want: ActionUpdate},
		{name: "absent no-op", state: "absent", stdout: `[]`, want: ActionNoOp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingRunner{outputs: []Result{{Stdout: "web\n"}, {Stdout: tt.stdout}}}
			provider := NewNativeProvider(runner)
			node := dockerComposeProjectNode(tt.state)

			got, err := provider.Plan(context.Background(), node, tt.prior)
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != tt.want {
				t.Fatalf("plan action = %q, want %q; observed=%#v", got.Action, tt.want, got.Observed)
			}
		})
	}
}

func TestNativeProviderDockerComposeProjectReadsAllContainers(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "web\n"}, {Stdout: `[{"Name":"web","State":"exited"}]`}}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")

	_, err := provider.Plan(context.Background(), node, &corestate.Resource{Ownership: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) < 2 {
		t.Fatalf("scripts = %#v, want services and ps reads", runner.scripts)
	}
	if !strings.Contains(runner.scripts[1], "'ps' '--all' '--format' 'json'") {
		t.Fatalf("compose ps command = %q, want --all json output", runner.scripts[1])
	}
}

func TestNativeProviderDockerComposeProjectMixedContainerStatesAreDegraded(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "redis\nsearxng\nrabbitmq\nnuq-postgres\nplaywright-service\napi\n"},
		{Stdout: `[
			{"Name":"api-1","Service":"api","State":"running"},
			{"Name":"nuq-postgres-1","Service":"nuq-postgres","State":"running"},
			{"Name":"playwright-service-1","Service":"playwright-service","State":"running"},
			{"Name":"rabbitmq-1","Service":"rabbitmq","State":"running"},
			{"Name":"redis-1","Service":"redis","State":"running"},
			{"Name":"searxng-1","Service":"searxng","State":"exited"}
		]`},
	}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")

	got, err := provider.Plan(context.Background(), node, &corestate.Resource{Ownership: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || !strings.Contains(got.Summary, "from degraded to running") {
		t.Fatalf("mixed compose plan = %#v, want degraded update", got)
	}
	if got.Observed["state"] != "degraded" || got.Observed["orphan_count"] != 0 {
		t.Fatalf("mixed compose observed = %#v, want degraded without orphans", got.Observed)
	}
	services, ok := got.Observed["services"].(map[string]any)
	if !ok {
		t.Fatalf("services observed = %#v", got.Observed["services"])
	}
	wantServices := []string{"api", "nuq-postgres", "playwright-service", "rabbitmq", "redis", "searxng"}
	if !reflect.DeepEqual(services["actual"], wantServices) || !reflect.DeepEqual(services["expected"], wantServices) {
		t.Fatalf("service lists = actual %#v expected %#v, want sorted %#v", services["actual"], services["expected"], wantServices)
	}
}

func TestNativeProviderDockerComposeProjectPlanOrphans(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "web\n"},
		{Stdout: `[{"Name":"web-1","Service":"web","State":"running"},{"Name":"worker-1","Service":"worker","State":"running"}]`},
	}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")

	got, err := provider.Plan(context.Background(), node, &corestate.Resource{Ownership: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || !strings.Contains(got.Summary, "orphan service") || !strings.Contains(got.Summary, "remove_orphans = true") {
		t.Fatalf("orphan plan = %#v, want update with orphan hint", got)
	}
	if got.Observed["orphan_count"] != 1 {
		t.Fatalf("orphan observed = %#v, want count 1", got.Observed)
	}

	node.Desired["remove_orphans"] = true
	runner = &recordingRunner{outputs: []Result{
		{Stdout: "web\n"},
		{Stdout: `[{"Name":"web-1","Service":"web","State":"running"},{"Name":"worker-1","Service":"worker","State":"running"}]`},
	}}
	provider = NewNativeProvider(runner)
	got, err = provider.Plan(context.Background(), node, &corestate.Resource{Ownership: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || !strings.Contains(got.Summary, "remove 1 orphan service") {
		t.Fatalf("remove orphan plan = %#v, want cleanup update", got)
	}
}

func TestNativeProviderDockerComposeProjectPlanDesiredChangeUpdates(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "web\n"}, {Stdout: `[{"Name":"web","Service":"web","State":"running"}]`}}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")
	priorDesired := cloneMap(node.Desired)
	priorDesired["pull"] = "never"
	node.Desired["pull"] = "always"
	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(priorDesired)}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate {
		t.Fatalf("plan action = %q, want update", got.Action)
	}
}

func TestNativeProviderDockerComposeProjectPlanProjectRename(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "web\n"}, {Stdout: `[{"Name":"web","Service":"web","State":"running"}]`}}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")
	node.Desired["project"] = "newapp"
	priorDesired := cloneMap(node.Desired)
	priorDesired["project"] = "app"
	prior := &corestate.Resource{Ownership: "managed", Desired: priorDesired, DesiredDigest: corestate.DesiredDigest(priorDesired)}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || got.Summary != "replace docker compose project app with newapp" {
		t.Fatalf("project rename plan = %#v, want replace summary", got)
	}
}

func TestNativeProviderDockerComposeProjectApplyRunningCommand(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "web\n"}, {Stdout: `[{"Name":"web","Service":"web","State":"running"}]`}}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) < 3 {
		t.Fatalf("scripts = %#v, want apply and read", runner.scripts)
	}
	applied := runner.scripts[0]
	for _, want := range []string{
		"'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'up' '-d'",
		"'--pull' 'missing'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("running compose command missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerComposeProjectApplyProjectRenameRunsDownFirst(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "web\n"}, {Stdout: `[{"Name":"web","Service":"web","State":"running"}]`}}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")
	node.Desired["project"] = "newapp"
	priorDesired := cloneMap(node.Desired)
	priorDesired["project"] = "app"
	prior := &corestate.Resource{Desired: priorDesired}

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) < 4 {
		t.Fatalf("scripts = %#v, want old down, new up, services, ps", runner.scripts)
	}
	if !strings.Contains(runner.scripts[0], "'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'down'") {
		t.Fatalf("first script = %q, want old project down", runner.scripts[0])
	}
	if !strings.Contains(runner.scripts[1], "'docker' 'compose' '-p' 'newapp' '-f' '/opt/app/compose.yaml' 'up' '-d'") {
		t.Fatalf("second script = %q, want new project up", runner.scripts[1])
	}
}

func TestNativeProviderDockerComposeProjectApplyStoppedAndAbsentCommands(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{state: "stopped", want: "'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'stop'"},
		{state: "absent", want: "'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'down'"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			runner := &recordingRunner{outputs: []Result{{Stdout: ""}, {Stdout: "[]"}}}
			provider := NewNativeProvider(runner)
			node := dockerComposeProjectNode(tt.state)

			if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate}); err != nil {
				t.Fatal(err)
			}
			if len(runner.scripts) < 1 || !strings.Contains(runner.scripts[0], tt.want) {
				t.Fatalf("%s compose command = %#v, want %q", tt.state, runner.scripts, tt.want)
			}
		})
	}
}

func TestNativeProviderDockerComposeProjectApplyFlags(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "web\n"}, {Stdout: `[{"Name":"web","Service":"web","State":"running"}]`}}}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")
	node.Desired["pull"] = "always"
	node.Desired["recreate"] = "always"
	node.Desired["remove_orphans"] = true

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[0]
	for _, want := range []string{"'--pull' 'always'", "'--force-recreate'", "'--remove-orphans'"} {
		if !strings.Contains(applied, want) {
			t.Fatalf("compose flags command missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerComposeProjectDestroyRunsDown(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	node := dockerComposeProjectNode("running")
	prior := &corestate.Resource{
		Host:    node.Host,
		Kind:    node.Kind,
		Desired: cloneMap(node.Desired),
	}

	if err := provider.Destroy(context.Background(), Step{Address: node.Address, Host: node.Host, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'down'") {
		t.Fatalf("destroy compose command = %#v, want docker compose down", runner.scripts)
	}
}
