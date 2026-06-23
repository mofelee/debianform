package engine

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/graph"
	v2state "github.com/mofelee/debianform/internal/v2/state"
)

type recordingRunner struct {
	outputs []Result
	scripts []string
}

func (r *recordingRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.scripts = append(r.scripts, script)
	if len(r.outputs) == 0 {
		return Result{}, nil
	}
	out := r.outputs[0]
	r.outputs = r.outputs[1:]
	return out, nil
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

	prior := &v2state.Resource{Ownership: "managed"}
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionDelete {
		t.Fatalf("missing absent file with prior action = %q, want delete to clear state", got.Action)
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
	applied := runner.scripts[len(runner.scripts)-1]
	if !strings.Contains(applied, base64.StdEncoding.EncodeToString([]byte(newContent))) {
		t.Fatalf("apply script should write new content:\n%s", applied)
	}

	prior := &v2state.Resource{
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
	if !strings.Contains(restored, base64.StdEncoding.EncodeToString([]byte(oldContent))) {
		t.Fatalf("destroy restore script should write original content:\n%s", restored)
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
	prior := &v2state.Resource{Ownership: "managed", Observed: map[string]any{"original_exists": true}}

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

	prior := &v2state.Resource{
		DesiredDigest: v2state.DesiredDigest(node.Desired),
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
