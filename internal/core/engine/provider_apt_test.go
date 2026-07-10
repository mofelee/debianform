package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
	"github.com/mofelee/debianform/internal/core/testassert"
)

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

func TestNativeProviderSensitiveAPTSigningKeyContentUsesProviderPayload(t *testing.T) {
	content := testassert.SensitiveVariableDefault
	node := graph.Node{
		Address: "host.server1.apt.signing_key[\"private\"]",
		Host:    "server1",
		Kind:    "apt_signing_key",
		Desired: map[string]any{
			"path":           "/etc/apt/keyrings/private.asc",
			"content_sha256": sha256Hex([]byte(content)),
			"content_bytes":  len(content),
			"owner":          "root",
			"group":          "root",
			"mode":           "0644",
			"ensure":         "present",
			"sensitive":      true,
		},
	}
	node.ProviderPayload = cloneMap(node.Desired)
	node.ProviderPayload["content"] = content
	runner := &recordingRunner{outputs: []Result{{Stdout: "missing\n"}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("missing sensitive apt signing key action = %q, want create", got.Action)
	}
	observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != content {
		t.Fatalf("apt signing key provider input = %#v, want sensitive payload", runner.inputs)
	}
	data, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	output := strings.Join(runner.scripts, "\n") + string(data)
	testassert.NoSecretLeak(t, "sensitive apt signing key provider output", output)
	if strings.Contains(output, base64.StdEncoding.EncodeToString([]byte(content))) {
		t.Fatalf("sensitive apt signing key provider output contains encoded payload: %s", output)
	}
}

func TestNativeProviderAPTSigningKeyURLWithoutSHA(t *testing.T) {
	node := graph.Node{
		Address: `host.docker1.docker.apt.signing_key["docker-official"]`,
		Host:    "docker1",
		Kind:    "apt_signing_key",
		Desired: map[string]any{
			"path":   "/etc/apt/keyrings/docker.asc",
			"url":    "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg",
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
	if !strings.Contains(applied, "curl -fsSL 'https://mirrors.aliyun.com/docker-ce/linux/debian/gpg'") {
		t.Fatalf("apt signing key apply script missing mirror curl:\n%s", applied)
	}
	if strings.Contains(applied, "sha256sum --check --status") {
		t.Fatalf("apt signing key apply script should not run sha256 check without desired sha:\n%s", applied)
	}
	if !strings.Contains(applied, "install -o 'root' -g 'root' -m '0644'") {
		t.Fatalf("apt signing key apply script missing install:\n%s", applied)
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
		Host: "foreign-server",
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
	destroyCallStart := len(runner.hosts)
	if err := provider.Destroy(context.Background(), Step{Address: node.Address, Host: node.Host, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	for _, host := range runner.hosts[destroyCallStart:] {
		if host != node.Host {
			t.Fatalf("destroy host = %q, want authoritative step host %q", host, node.Host)
		}
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

func TestNativeProviderSensitiveAPTSourceFileDoesNotPreserveOriginal(t *testing.T) {
	content := testassert.SensitiveVariableDefault
	current := "file\nroot\nroot\n644\n" + sha256Hex([]byte(content)) + "\n" + base64.StdEncoding.EncodeToString([]byte(content)) + "\n"
	node := graph.Node{
		Address: "host.server1.apt.source_file[\"private\"]",
		Host:    "server1",
		Kind:    "apt_source_file",
		Desired: map[string]any{
			"label":          "private",
			"path":           "/etc/apt/sources.list.d/private.list",
			"content_sha256": sha256Hex([]byte(content)),
			"content_bytes":  len(content),
			"owner":          "root",
			"group":          "root",
			"mode":           "0644",
			"ensure":         "present",
			"on_destroy":     "keep",
			"sensitive":      true,
		},
	}
	node.ProviderPayload = cloneMap(node.Desired)
	node.ProviderPayload["content"] = content
	runner := &recordingRunner{outputs: []Result{{Stdout: current}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionAdopt {
		t.Fatalf("matching sensitive apt source action = %q, want adopt", got.Action)
	}
	if _, ok := got.Observed["original_content"]; ok {
		t.Fatalf("sensitive plan observed contains original content: %#v", got.Observed)
	}
	data, err := json.Marshal(got.Observed)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "sensitive apt source online plan", string(data))

	observed, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := observed["original_content"]; ok {
		t.Fatalf("sensitive apply observed contains original content: %#v", observed)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != content {
		t.Fatalf("apt source provider input = %#v, want sensitive payload", runner.inputs)
	}
}

func TestNativeProviderDestroyUsesAuthoritativeStepHost(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	prior := &corestate.Resource{
		Host:    "foreign-server",
		Kind:    "file",
		Desired: map[string]any{"path": "/tmp/example"},
	}

	err := provider.Destroy(context.Background(), Step{
		Address: `host.server1.files.file["/tmp/example"]`,
		Host:    "server1",
		Prior:   prior,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.hosts) != 1 || runner.hosts[0] != "server1" {
		t.Fatalf("destroy hosts = %#v, want authoritative step host server1", runner.hosts)
	}
}

func TestNativeProviderSensitiveAPTSourceFileRefreshesLeakedLegacyState(t *testing.T) {
	content := testassert.SensitiveVariableDefault
	current := "file\nroot\nroot\n644\n" + sha256Hex([]byte(content)) + "\n" + base64.StdEncoding.EncodeToString([]byte(content)) + "\n"
	node := graph.Node{
		Address: "host.server1.apt.source_file[\"private\"]",
		Host:    "server1",
		Kind:    "apt_source_file",
		Desired: map[string]any{
			"path":           "/etc/apt/sources.list.d/private.list",
			"content_sha256": sha256Hex([]byte(content)),
			"content_bytes":  len(content),
			"owner":          "root",
			"group":          "root",
			"mode":           "0644",
			"ensure":         "present",
			"on_destroy":     "keep",
			"sensitive":      true,
		},
	}
	prior := &corestate.Resource{
		DesiredDigest: "legacy-digest",
		Ownership:     "managed",
		Observed: map[string]any{
			"original_exists":  true,
			"original_content": content,
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: current}}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionAdopt {
		t.Fatalf("legacy sensitive apt source action = %q, want state-refresh adopt", got.Action)
	}
	if _, ok := got.Observed["original_content"]; ok {
		t.Fatalf("state-refresh observed contains legacy content: %#v", got.Observed)
	}
	resource := resourceStateForStep(Step{Node: node, Ownership: got.Ownership}, got.Observed, "2026-07-10T00:00:00Z")
	data, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	testassert.NoSecretLeak(t, "refreshed sensitive apt source state", string(data))
}

func TestNativeProviderAPTSourceFileRestoreRequiresBaseline(t *testing.T) {
	node := graph.Node{
		Address: "host.server1.apt.source_file[\"private\"]",
		Host:    "server1",
		Kind:    "apt_source_file",
		Desired: map[string]any{
			"path":       "/etc/apt/sources.list.d/private.list",
			"ensure":     "absent",
			"on_destroy": "restore",
		},
	}
	prior := &corestate.Resource{Ownership: "managed", Observed: map[string]any{"exists": true}}
	runner := &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n644\nabc\n\n"}}}
	provider := NewNativeProvider(runner)

	_, err := provider.Plan(context.Background(), node, prior)
	if err == nil || !strings.Contains(err.Error(), "original content baseline is unavailable") {
		t.Fatalf("error = %v, want missing restore baseline rejection", err)
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
