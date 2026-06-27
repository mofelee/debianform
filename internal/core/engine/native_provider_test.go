package engine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

type recordingRunner struct {
	outputs []Result
	errors  []error
	scripts []string
	inputs  []string
}

func (r *recordingRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.scripts = append(r.scripts, script)
	return r.next()
}

func (r *recordingRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return Result{}, err
	}
	r.scripts = append(r.scripts, remoteCommand)
	r.inputs = append(r.inputs, string(data))
	return r.next()
}

func (r *recordingRunner) next() (Result, error) {
	if len(r.outputs) == 0 {
		if len(r.errors) == 0 {
			return Result{}, nil
		}
		err := r.errors[0]
		r.errors = r.errors[1:]
		return Result{}, err
	}
	out := r.outputs[0]
	r.outputs = r.outputs[1:]
	if len(r.errors) == 0 {
		return out, nil
	}
	err := r.errors[0]
	r.errors = r.errors[1:]
	return out, err
}

func (r *recordingRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

func TestNativeProviderAbsentMissingDoesNotAdopt(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.files.file[\"/tmp/dbf-absent\"]",
		Host:    "server1",
		Kind:    "file",
		Desired: map[string]any{"path": "/tmp/dbf-absent", "ensure": "absent"},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}, {Stdout: "missing\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("missing absent file action = %q, want no-op", got.Action)
	}

	prior := &corestate.Resource{Ownership: "managed"}
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionDelete {
		t.Fatalf("missing absent file with prior action = %q, want delete to clear state", got.Action)
	}
}

func TestNativeProviderWriteOnlyFileUsesVersionForPlan(t *testing.T) {
	node := writeOnlyFileNode("v1")
	prior := &corestate.Resource{
		DesiredDigest: corestate.DesiredDigest(node.Desired),
		Ownership:     "managed",
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n600\nremote-sha\n"}}}
	provider := NewNativeProvider(runner)
	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("matching write-only version action = %q, want no-op", got.Action)
	}
	if strings.Contains(anyMapText(got.Observed), "not-a-real-ephemeral-token") {
		t.Fatalf("provider plan observed leaked write-only content: %#v", got.Observed)
	}
	if _, ok := got.Observed["sha256"]; ok {
		t.Fatalf("provider plan observed retained sha256 for write-only file: %#v", got.Observed)
	}

	next := writeOnlyFileNode("current")
	runner = &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n600\nremote-sha\n"}}}
	provider = NewNativeProvider(runner)
	got, err = provider.Plan(context.Background(), next, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate {
		t.Fatalf("changed write-only version action = %q, want update", got.Action)
	}
}

func TestNativeProviderObservedModeUsesFourDigitDisplay(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.files.file[\"/etc/example.conf\"]",
		Host:    "server1",
		Kind:    "file",
		Desired: map[string]any{
			"path":      "/etc/example.conf",
			"content":   "hello\n",
			"owner":     "root",
			"group":     "root",
			"mode":      "0640",
			"ensure":    "present",
			"sensitive": false,
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n640\nold-sha\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Observed["mode"] != "0640" {
		t.Fatalf("observed mode = %#v, want 0640", got.Observed["mode"])
	}
}

func TestNativeProviderWriteOnlyFileApplyUsesProviderPayload(t *testing.T) {
	node := writeOnlyFileNode("v1")
	runner := &recordingRunner{outputs: []Result{{Stdout: "not-a-real-ephemeral-token", Stderr: "not-a-real-ephemeral-token"}}}
	provider := NewNativeProvider(runner)
	observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(anyMapText(observed), "not-a-real-ephemeral-token") {
		t.Fatalf("observed leaked write-only content: %#v", observed)
	}
	if len(runner.inputs) == 0 || runner.inputs[0] != "not-a-real-ephemeral-token" {
		t.Fatalf("apply stdin = %#v, want write-only payload", runner.inputs)
	}
	if strings.Contains(strings.Join(runner.scripts, "\n---\n"), "not-a-real-ephemeral-token") ||
		strings.Contains(strings.Join(runner.scripts, "\n---\n"), base64.StdEncoding.EncodeToString([]byte("not-a-real-ephemeral-token"))) {
		t.Fatalf("apply script leaked write-only payload:\n%s", strings.Join(runner.scripts, "\n---\n"))
	}
	if !strings.Contains(runner.scripts[0], "mktemp") || !strings.Contains(runner.scripts[0], "trap 'rm -f --") {
		t.Fatalf("apply script missing controlled temp file cleanup:\n%s", runner.scripts[0])
	}
}

func TestNativeProviderWriteOnlyFileErrorRedactsPayload(t *testing.T) {
	node := writeOnlyFileNode("v1")
	runner := &recordingRunner{errors: []error{errors.New("remote stderr not-a-real-ephemeral-token")}}
	provider := NewNativeProvider(runner)
	_, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
	if err == nil {
		t.Fatal("apply succeeded, want injected runner failure")
	}
	if strings.Contains(err.Error(), "not-a-real-ephemeral-token") {
		t.Fatalf("error leaked payload: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("error = %v, want redacted marker", err)
	}
}

func TestNativeProviderSysctlAbsentRemovesManagedPersistence(t *testing.T) {
	key := "net.ipv4.ip_forward"
	value := "1"
	persistedHash := sha256Hex([]byte(key + " = " + value + "\n"))
	node := graph.Node{
		Address: "host.server1.kernel.sysctl[\"net.ipv4.ip_forward\"]",
		Host:    "server1",
		Kind:    "sysctl",
		Desired: map[string]any{"key": key, "value": value, "ensure": "absent"},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: value + "\n" + persistedHash + "\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionDelete {
		t.Fatalf("persisted absent sysctl action = %q, want delete", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionDelete}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	if strings.Contains(applied, "sysctl -w") {
		t.Fatalf("absent sysctl should not write runtime value:\n%s", applied)
	}
	if !strings.Contains(applied, "rm -f") {
		t.Fatalf("absent sysctl should remove persisted file:\n%s", applied)
	}
}

func TestNativeProviderAPTSigningKeyURL(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.apt.signing_key[\"tools\"]",
		Host:    "server1",
		Kind:    "apt_signing_key",
		Desired: map[string]any{
			"path":   "/etc/apt/keyrings/tools.asc",
			"url":    "https://repo.example/key.asc",
			"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
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
		t.Fatalf("missing apt signing key action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"curl -fsSL 'https://repo.example/key.asc'",
		"sha256sum --check --status",
		"install -o 'root' -g 'root' -m '0644'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("apt signing key apply script missing %q:\n%s", want, applied)
		}
	}
}

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

func TestNativeProviderAPTSourceFilePreservesOriginalAndRestores(t *testing.T) {
	oldContent := "deb http://deb.debian.org/debian trixie main\n"
	newContent := "deb https://mirrors.aliyun.com/debian/ trixie main\n"
	oldOutput := "file\nroot\nroot\n644\n" + sha256Hex([]byte(oldContent)) + "\n" + base64.StdEncoding.EncodeToString([]byte(oldContent)) + "\n"
	node := graph.Node{
		Address: "host.server1.apt.source_file[\"main\"]",
		Host:    "server1",
		Kind:    "apt_source_file",
		Desired: map[string]any{
			"label":      "main",
			"path":       "/etc/apt/sources.list",
			"content":    newContent,
			"owner":      "root",
			"group":      "root",
			"mode":       "0644",
			"ensure":     "present",
			"on_destroy": "restore",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: oldOutput}, {Stdout: oldOutput}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate {
		t.Fatalf("existing different apt source file action = %q, want update", got.Action)
	}
	if _, ok := got.Observed["original_content"]; ok {
		t.Fatalf("plan observed should not expose original content: %#v", got.Observed)
	}

	observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate})
	if err != nil {
		t.Fatal(err)
	}
	if observed["original_content"] != oldContent || observed["original_mode"] != "644" {
		t.Fatalf("apply observed original = %#v", observed)
	}
	if len(runner.inputs) == 0 || runner.inputs[len(runner.inputs)-1] != newContent {
		t.Fatalf("apply stdin = %#v, want new content", runner.inputs)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	if strings.Contains(applied, newContent) || strings.Contains(applied, base64.StdEncoding.EncodeToString([]byte(newContent))) {
		t.Fatalf("apply script leaked new content:\n%s", applied)
	}

	prior := &corestate.Resource{
		Host: "server1",
		Kind: "apt_source_file",
		Desired: map[string]any{
			"path":       "/etc/apt/sources.list",
			"on_destroy": "restore",
		},
		Observed: map[string]any{
			"original_exists":  true,
			"original_content": oldContent,
			"original_owner":   "root",
			"original_group":   "root",
			"original_mode":    "0644",
		},
	}
	if err := provider.Destroy(context.Background(), Step{Address: node.Address, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) < 2 {
		t.Fatalf("restore should write original content and refresh apt cache, scripts = %#v", runner.scripts)
	}
	restored := runner.scripts[len(runner.scripts)-2]
	if len(runner.inputs) == 0 || runner.inputs[len(runner.inputs)-1] != oldContent {
		t.Fatalf("restore stdin = %#v, want original content", runner.inputs)
	}
	if strings.Contains(restored, oldContent) || strings.Contains(restored, base64.StdEncoding.EncodeToString([]byte(oldContent))) {
		t.Fatalf("destroy restore script leaked original content:\n%s", restored)
	}
	if !strings.Contains(runner.scripts[len(runner.scripts)-1], "apt-get update") {
		t.Fatalf("destroy restore should refresh apt cache:\n%s", runner.scripts[len(runner.scripts)-1])
	}
}

func TestNativeProviderAPTSourceFileKeepForgetsWithoutRemoteChange(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.apt.source_file[\"main\"]",
		Host:    "server1",
		Kind:    "apt_source_file",
		Desired: map[string]any{
			"label":      "main",
			"path":       "/etc/apt/sources.list",
			"ensure":     "absent",
			"on_destroy": "keep",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n644\nabc\n\n"}}}
	provider := NewNativeProvider(runner)
	prior := &corestate.Resource{Ownership: "managed", Observed: map[string]any{"original_exists": true}}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionForget {
		t.Fatalf("absent keep action = %q, want forget", got.Action)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("plan should only read remote state, scripts = %#v", runner.scripts)
	}
}

func TestNativeProviderPackageInstallRefreshesMissingAPTCache(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.packages.install[\"nftables\"]",
		Host:    "server1",
		Kind:    "package",
		Desired: map[string]any{
			"name":   "nftables",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"apt-cache policy 'nftables'",
		"apt-get update",
		"apt-get install -y 'nftables'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("package install script missing %q:\n%s", want, applied)
		}
	}
	if strings.Index(applied, "apt-get update") > strings.Index(applied, "apt-get install -y 'nftables'") {
		t.Fatalf("package install should refresh apt cache before install:\n%s", applied)
	}
}

func TestNativeProviderDockerPackageApplyScript(t *testing.T) {
	node := graph.Node{
		Address: `host.docker1.docker.package["docker-ce"]`,
		Host:    "docker1",
		Kind:    "package",
		Desired: map[string]any{
			"name":         "docker-ce",
			"ensure":       "present",
			"repositories": []string{"docker-official"},
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: ""}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing docker package action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"apt-cache policy 'docker-ce'",
		"apt-get update",
		"apt-get install -y 'docker-ce'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("docker package script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderDockerPackageConflictsPlanNoopWhenAbsent(t *testing.T) {
	node := dockerPackageConflictsNode("auto")
	runner := &recordingRunner{outputs: []Result{{Stdout: ""}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("conflict plan action = %q, want no-op; observed=%#v", got.Action, got.Observed)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "dpkg-query") {
		t.Fatalf("conflict detection script = %#v, want dpkg-query", runner.scripts)
	}
}

func TestNativeProviderDockerPackageConflictsPlanAutoAndTrueDelete(t *testing.T) {
	for _, tt := range []struct {
		removeConflicts string
		wantSummary     string
	}{
		{removeConflicts: "auto", wantSummary: "replace docker conflict packages docker.io, runc"},
		{removeConflicts: "true", wantSummary: "remove docker conflict packages docker.io, runc"},
	} {
		t.Run(tt.removeConflicts, func(t *testing.T) {
			node := dockerPackageConflictsNode(tt.removeConflicts)
			runner := &recordingRunner{outputs: []Result{{Stdout: "docker.io\tinstall ok installed\nrunc\tinstall ok installed\n"}}}
			provider := NewNativeProvider(runner)

			got, err := provider.Plan(context.Background(), node, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != ActionDelete || got.Summary != tt.wantSummary {
				t.Fatalf("conflict plan = %#v, want delete summary %q", got, tt.wantSummary)
			}
			installed := stringListMapValue(got.Observed, "installed")
			if strings.Join(installed, ",") != "docker.io,runc" {
				t.Fatalf("installed conflicts = %#v, want docker.io,runc", installed)
			}
		})
	}
}

func TestNativeProviderDockerPackageConflictsApplyRemovesInstalledPackages(t *testing.T) {
	node := dockerPackageConflictsNode("auto")
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)

	observed, err := provider.Apply(context.Background(), Step{
		Node:     node,
		Action:   ActionDelete,
		Observed: map[string]any{"installed": []string{"docker.io", "runc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stringListMapValue(observed, "removed"), ","); got != "docker.io,runc" {
		t.Fatalf("removed conflicts = %q, want docker.io,runc", got)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("scripts = %#v, want one removal script", runner.scripts)
	}
	for _, want := range []string{
		"export DEBIAN_FRONTEND=noninteractive",
		"'apt-get' 'remove' '-y' 'docker.io' 'runc'",
	} {
		if !strings.Contains(runner.scripts[0], want) {
			t.Fatalf("conflict removal script missing %q:\n%s", want, runner.scripts[0])
		}
	}
}

func TestNativeProviderDockerPackageConflictsPlanFalseErrors(t *testing.T) {
	node := dockerPackageConflictsNode("false")
	runner := &recordingRunner{outputs: []Result{{Stdout: "docker.io\tinstall ok installed\n"}}}
	provider := NewNativeProvider(runner)

	_, err := provider.Plan(context.Background(), node, nil)
	if err == nil || !strings.Contains(err.Error(), "docker conflict packages are installed: docker.io") || !strings.Contains(err.Error(), "remove_conflicts") {
		t.Fatalf("conflict false error = %v", err)
	}

	_, err = provider.Apply(context.Background(), Step{
		Node:     node,
		Action:   ActionDelete,
		Observed: map[string]any{"installed": []string{"docker.io"}},
	})
	if err == nil || !strings.Contains(err.Error(), "docker conflict packages are installed: docker.io") || !strings.Contains(err.Error(), "remove_conflicts") {
		t.Fatalf("conflict false apply error = %v", err)
	}
}

func TestNativeProviderComponentDownloadURL(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.components.rclone.artifact.download[\"amd64\"]",
		Host:    "server1",
		Kind:    "component_download",
		Desired: map[string]any{
			"path":   "/var/cache/debianform/components/rclone/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/source",
			"url":    "https://downloads.example/rclone.zip",
			"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
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
		t.Fatalf("missing component download action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"source_url='https://downloads.example/rclone.zip'",
		`curl -fsSL "$source_url"`,
		"sha256sum --check --status",
		"install -o 'root' -g 'root' -m '0644'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component download apply script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderComponentDownloadFileURL(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.components.hello.artifact.download[\"default\"]",
		Host:    "server1",
		Kind:    "component_download",
		Desired: map[string]any{
			"path":   "/var/cache/debianform/components/hello/source",
			"url":    "file:///var/lib/debianform-integration/hello.c",
			"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"owner":  "root",
			"group":  "root",
			"mode":   "0644",
			"ensure": "present",
		},
	}
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		`source_url='file:///var/lib/debianform-integration/hello.c'`,
		`cp -- "${source_url#file://}"`,
		"sha256sum --check --status",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("file URL download script missing %q:\n%s", want, applied)
		}
	}
	if !strings.Contains(applied, "file://*) ;;") {
		t.Fatalf("file URL download should skip curl install at runtime:\n%s", applied)
	}
}

func TestNativeProviderComponentBuildSingleSourceFile(t *testing.T) {
	builtSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	node := graph.Node{
		Address: "host.server1.components.hello.artifact.build[\"/var/cache/debianform/components/hello/build/out/hash/hello\"]",
		Host:    "server1",
		Kind:    "component_build",
		Desired: map[string]any{
			"cache_path":  "/var/cache/debianform/components/hello/source",
			"build_path":  "/var/cache/debianform/components/hello/build",
			"output_path": "/var/cache/debianform/components/hello/build/out/hash/hello",
			"commands": [][]string{
				{"cc", "-O2", "-o", "hello", "hello.c"},
			},
			"output":      "hello",
			"source_name": "hello.c",
			"owner":       "root",
			"group":       "root",
			"mode":        "0644",
			"ensure":      "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "missing\n"},
		{},
		{Stdout: "file\nroot\nroot\n644\n" + builtSHA + "\n"},
	}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing component build action = %q, want create", got.Action)
	}
	observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if observed["sha256"] != builtSHA {
		t.Fatalf("observed sha256 = %#v, want %s", observed["sha256"], builtSHA)
	}
	applied := runner.scripts[len(runner.scripts)-2]
	for _, want := range []string{
		"cp -- '/var/cache/debianform/components/hello/source' \"$src/hello.c\"",
		"set -- 'cc' '-O2' '-o' 'hello' 'hello.c'\n\"$@\"",
		"install -o 'root' -g 'root' -m '0644' \"$built\" '/var/cache/debianform/components/hello/build/out/hash/hello'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component build script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderComponentBinaryZipInstall(t *testing.T) {
	installedSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	node := graph.Node{
		Address: "host.server1.components.rclone.artifact.install[\"/usr/local/bin/rclone\"]",
		Host:    "server1",
		Kind:    "component_binary",
		Desired: map[string]any{
			"path":             "/usr/local/bin/rclone",
			"cache_path":       "/var/cache/debianform/components/rclone/source",
			"extract_format":   "zip",
			"strip_components": 1,
			"include":          "rclone",
			"owner":            "root",
			"group":            "root",
			"mode":             "0755",
			"ensure":           "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "missing\n"},
		{},
		{Stdout: "file\nroot\nroot\n755\n" + installedSHA + "\n"},
	}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing component binary action = %q, want create", got.Action)
	}
	observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if observed["sha256"] != installedSHA {
		t.Fatalf("observed sha256 = %#v, want %s", observed["sha256"], installedSHA)
	}
	applied := runner.scripts[len(runner.scripts)-2]
	for _, want := range []string{
		"unzip -q '/var/cache/debianform/components/rclone/source'",
		"include='rclone'",
		"strip_components='1'",
		"install -o 'root' -g 'root' -m '0755'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component binary apply script missing %q:\n%s", want, applied)
		}
	}

	prior := &corestate.Resource{
		DesiredDigest: corestate.DesiredDigest(node.Desired),
		Ownership:     "managed",
		Observed:      map[string]any{"sha256": installedSHA},
	}
	runner = &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n755\n" + installedSHA + "\n"}}}
	provider = NewNativeProvider(runner)
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("managed matching component binary action = %q, want no-op", got.Action)
	}
}

func TestNativeProviderComponentBinaryTarXZInstall(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.components.tool.artifact.install[\"/usr/local/bin/tool\"]",
		Host:    "server1",
		Kind:    "component_binary",
		Desired: map[string]any{
			"path":             "/usr/local/bin/tool",
			"cache_path":       "/var/cache/debianform/components/tool/source",
			"extract_format":   "tar.xz",
			"strip_components": 1,
			"include":          "tool",
			"owner":            "root",
			"group":            "root",
			"mode":             "0755",
			"ensure":           "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "missing\n"},
		{},
		{Stdout: "file\nroot\nroot\n755\naaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"},
	}}
	provider := NewNativeProvider(runner)

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-2]
	for _, want := range []string{
		"apt-get install -y tar xz-utils",
		"tar --no-same-owner -xJf '/var/cache/debianform/components/tool/source'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component binary tar.xz script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderComponentBinaryGzipInstall(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.components.tool.artifact.install[\"/usr/local/bin/tool\"]",
		Host:    "server1",
		Kind:    "component_binary",
		Desired: map[string]any{
			"path":           "/usr/local/bin/tool",
			"cache_path":     "/var/cache/debianform/components/tool/source",
			"extract_format": "gz",
			"owner":          "root",
			"group":          "root",
			"mode":           "0755",
			"ensure":         "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "missing\n"},
		{},
		{Stdout: "file\nroot\nroot\n755\naaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"},
	}}
	provider := NewNativeProvider(runner)

	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-2]
	for _, want := range []string{
		"apt-get install -y gzip",
		"gzip -dc '/var/cache/debianform/components/tool/source' > \"$work/binary\"",
		"install -o 'root' -g 'root' -m '0755' \"$work/binary\" '/usr/local/bin/tool'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component binary gzip script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderComponentFileInstall(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.components.config.artifact.install[\"/etc/myapp/config.yaml\"]",
		Host:    "server1",
		Kind:    "component_file",
		Desired: map[string]any{
			"path":          "/etc/myapp/config.yaml",
			"cache_path":    "/var/cache/debianform/components/config/source",
			"source_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"owner":         "root",
			"group":         "root",
			"mode":          "0644",
			"ensure":        "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "missing\n"},
		{},
		{Stdout: "file\nroot\nroot\n644\n0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"},
	}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing component file action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-2]
	if !strings.Contains(applied, "install -o 'root' -g 'root' -m '0644' '/var/cache/debianform/components/config/source' '/etc/myapp/config.yaml'") {
		t.Fatalf("component file apply script did not install from cache:\n%s", applied)
	}
}

func TestNativeProviderComponentArchiveInstall(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.components.myapp.artifact.install[\"/opt/myapp\"]",
		Host:    "server1",
		Kind:    "component_archive",
		Desired: map[string]any{
			"path":             "/opt/myapp",
			"cache_path":       "/var/cache/debianform/components/myapp/source",
			"extract_format":   "tar.gz",
			"strip_components": 1,
			"owner":            "myapp",
			"group":            "myapp",
			"mode":             "0755",
			"ensure":           "present",
		},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "missing\n"},
		{},
		{Stdout: "dir\nmyapp\nmyapp\n755\n\n"},
	}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing component archive action = %q, want create", got.Action)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-2]
	for _, want := range []string{
		"tar --no-same-owner -xzf '/var/cache/debianform/components/myapp/source'",
		"--strip-components '1'",
		"chown -R 'myapp:myapp'",
		"mv \"$tmp\" '/opt/myapp'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component archive apply script missing %q:\n%s", want, applied)
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
		Address:        `host.compose1.docker.compose["app"].validate`,
		Action:         "run",
		CommandPreview: "docker compose -p app -f /opt/app/compose.yaml config",
	}

	if err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || runner.scripts[0] != operation.CommandPreview {
		t.Fatalf("compose validate command = %#v, want %q", runner.scripts, operation.CommandPreview)
	}
}

func TestNativeProviderComponentScriptRunOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Address: `host.app1.components.app.script["reload"]`,
		Action:  "run",
		ScriptPayload: &graph.ScriptPayload{
			Name:        "reload",
			Mode:        "once",
			Kind:        "run",
			Interpreter: []string{"/bin/bash", "-e"},
			Run:         "systemctl reload app.service",
		},
	}

	if err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || runner.scripts[0] != "'/bin/bash' '-e'" {
		t.Fatalf("script interpreter command = %#v, want bash -e", runner.scripts)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != "systemctl reload app.service\n" {
		t.Fatalf("script input = %#v, want run body with newline", runner.inputs)
	}
}

func TestNativeProviderComponentScriptContentOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Address: `host.app1.components.app.script["reload"]`,
		Action:  "run",
		ScriptPayload: &graph.ScriptPayload{
			Name:        "reload",
			Mode:        "once",
			Kind:        "content",
			Interpreter: []string{"/bin/sh", "-eu"},
			Content:     "printf '%s\\n' ready\n",
		},
	}

	if err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || runner.scripts[0] != "'/bin/sh' '-eu'" {
		t.Fatalf("script interpreter command = %#v, want sh -eu", runner.scripts)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != "printf '%s\\n' ready\n" {
		t.Fatalf("script input = %#v, want content body unchanged", runner.inputs)
	}
}

func TestNativeProviderComponentScriptCommandsOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Address: `host.app1.components.app.script["reload"]`,
		Action:  "run",
		ScriptPayload: &graph.ScriptPayload{
			Name:        "reload",
			Mode:        "once",
			Kind:        "commands",
			Interpreter: []string{"/bin/sh", "-eu"},
			Commands: [][]string{
				{"systemctl", "reload", "app.service"},
				{"printf", "owner's value"},
			},
		},
	}

	if err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	want := "'systemctl' 'reload' 'app.service'\n'printf' 'owner'\"'\"'s value'\n"
	if len(runner.inputs) != 1 || runner.inputs[0] != want {
		t.Fatalf("script commands input = %#v, want %q", runner.inputs, want)
	}
}

func TestNativeProviderComponentScriptOperationFailure(t *testing.T) {
	runner := &recordingRunner{errors: []error{errors.New("script failed")}}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Address: `host.app1.components.app.script["reload"]`,
		Action:  "run",
		ScriptPayload: &graph.ScriptPayload{
			Name:        "reload",
			Mode:        "once",
			Kind:        "run",
			Interpreter: []string{"/bin/sh", "-eu"},
			Run:         "exit 1",
		},
	}

	err := provider.RunOperation(context.Background(), operation)
	if err == nil {
		t.Fatal("script operation succeeded, want injected runner failure")
	}
	if !strings.Contains(err.Error(), "script failed") {
		t.Fatalf("script operation error = %v, want runner error", err)
	}
}

func TestNativeProviderDockerComposeDaemonReloadOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Address:        `host.compose1.docker.compose["app"].daemon_reload`,
		Action:         "run",
		CommandPreview: "systemctl daemon-reload",
	}

	if err := provider.RunOperation(context.Background(), operation); err != nil {
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

	if err := provider.Destroy(context.Background(), Step{Address: node.Address, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'down'") {
		t.Fatalf("destroy compose command = %#v, want docker compose down", runner.scripts)
	}
}

func TestNativeProviderUserGroupMembershipPlanAndApply(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "deploy:x:1000:1000::/home/deploy:/bin/bash\ndeploy\ndeploy\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || !strings.Contains(got.Summary, "log out and back in") {
		t.Fatalf("membership plan = %#v, want update with relogin hint", got)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"getent passwd 'deploy'",
		"getent group 'docker'",
		"usermod -aG 'docker' 'deploy'",
		"must log out and back in",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("membership apply script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderUserGroupMembershipPlanNoopWhenAlreadyPresent(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "deploy:x:1000:1000::/home/deploy:/bin/bash\ndeploy\ndeploy docker\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")
	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(node.Desired)}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("membership plan action = %q, want no-op; observed=%#v", got.Action, got.Observed)
	}
}

func TestNativeProviderUserGroupMembershipPlanErrorsWhenUserMissing(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "__ABSENT__\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")

	_, err := provider.Plan(context.Background(), node, nil)
	if err == nil || !strings.Contains(err.Error(), `user "deploy" does not exist`) || !strings.Contains(err.Error(), `users.user["deploy"]`) {
		t.Fatalf("membership missing user error = %v", err)
	}
}

func TestNativeProviderUserGroupMembershipPlansCreateWhenManagedUserDependencyIsMissing(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "__ABSENT__\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")
	node.DependsOn = []string{`host.server1.users.user["deploy"]`, `host.server1.docker.group["docker"]`}

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("membership missing managed user action = %q, want create", got.Action)
	}
}

func TestNativeProviderUserGroupMembershipDestroyRemovesSupplementaryOnly(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")
	prior := &corestate.Resource{
		Host:    node.Host,
		Kind:    node.Kind,
		Desired: cloneMap(node.Desired),
	}

	if err := provider.Destroy(context.Background(), Step{Address: node.Address, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("scripts = %#v, want destroy command", runner.scripts)
	}
	for _, want := range []string{
		"gpasswd -d 'deploy' 'docker'",
		`[ "$(id -gn 'deploy')" != 'docker' ]`,
	} {
		if !strings.Contains(runner.scripts[0], want) {
			t.Fatalf("membership destroy script missing %q:\n%s", want, runner.scripts[0])
		}
	}
}

func TestSSHRunnerExpandsHomeIdentityFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	runner := NewSSHRunner(map[string]Host{
		"server1": {
			Address:      "192.0.2.10",
			IdentityFile: "~/.ssh/id_ed25519",
		},
	})

	args := runner.SSHArgs("server1")
	want := filepath.Join(home, ".ssh", "id_ed25519")
	for _, arg := range args {
		if arg == want {
			return
		}
	}
	t.Fatalf("ssh args %q do not contain expanded identity file %q", strings.Join(args, " "), want)
}

func TestSSHRunnerUsesConfiguredSSHConfig(t *testing.T) {
	t.Setenv("DBF_SSH_CONFIG", "/tmp/debianform-ssh-config")
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})

	args := runner.SSHArgs("server1")
	if len(args) < 2 || args[0] != "-F" || args[1] != "/tmp/debianform-ssh-config" {
		t.Fatalf("ssh args = %#v, want -F DBF_SSH_CONFIG prefix", args)
	}
}

func userGroupMembershipNode(user, group string) graph.Node {
	return graph.Node{
		Address: `host.server1.docker.user_group_membership["` + user + `:` + group + `"]`,
		Host:    "server1",
		Kind:    "user_group_membership",
		Desired: map[string]any{
			"user":   user,
			"group":  group,
			"ensure": "present",
			"note":   "user must log out and back in for docker group membership to affect existing sessions",
		},
	}
}

func dockerPackageConflictsNode(removeConflicts string) graph.Node {
	return graph.Node{
		Address: `host.docker1.docker.package_conflicts`,
		Host:    "docker1",
		Kind:    "docker_package_conflicts",
		Desired: map[string]any{
			"packages":         []string{"docker.io", "docker-doc", "docker-compose", "podman-docker", "containerd", "runc"},
			"remove_conflicts": removeConflicts,
			"ensure":           "absent",
		},
	}
}

func dockerComposeProjectNode(state string) graph.Node {
	return graph.Node{
		Address: `host.compose1.docker.compose["app"].project`,
		Host:    "compose1",
		Kind:    "docker_compose_project",
		Desired: map[string]any{
			"directory":      "/opt/app",
			"project":        "app",
			"files":          []string{"/opt/app/compose.yaml"},
			"env_files":      []string{"/opt/app/.env"},
			"state":          state,
			"pull":           "missing",
			"recreate":       "auto",
			"remove_orphans": false,
		},
	}
}

func writeOnlyFileNode(version string) graph.Node {
	desired := map[string]any{
		"path":               "/etc/app/token",
		"owner":              "root",
		"group":              "root",
		"mode":               "0600",
		"ensure":             "present",
		"sensitive":          true,
		"content_write_only": true,
		"content_version":    version,
	}
	payload := cloneMap(desired)
	payload["content"] = "not-a-real-ephemeral-token"
	return graph.Node{
		Address:         "host.server1.files.file[\"/etc/app/token\"]",
		Host:            "server1",
		Kind:            "file",
		Desired:         desired,
		ProviderPayload: payload,
	}
}

func anyMapText(values map[string]any) string {
	parts := make([]string, 0, len(values))
	for key, value := range values {
		parts = append(parts, key+"="+fmt.Sprint(value))
	}
	return strings.Join(parts, "\n")
}
