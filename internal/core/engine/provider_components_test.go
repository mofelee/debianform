package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

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

func TestNativeProviderComponentScriptRunOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Host:    "app.example.com",
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

	if _, err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || !strings.HasSuffix(runner.scripts[0], "'/bin/bash' '-e'") {
		t.Fatalf("script interpreter command = %#v, want bash -e", runner.scripts)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != "systemctl reload app.service\n" {
		t.Fatalf("script input = %#v, want run body with newline", runner.inputs)
	}
	if len(runner.hosts) != 1 || runner.hosts[0] != operation.Host {
		t.Fatalf("script hosts = %#v, want explicit host %q", runner.hosts, operation.Host)
	}
}

func TestNativeProviderComponentScriptContentOperation(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Host:    "app1",
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

	if _, err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || !strings.HasSuffix(runner.scripts[0], "'/bin/sh' '-eu'") {
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
		Host:    "app1",
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

	if _, err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	want := "'systemctl' 'reload' 'app.service'\n'printf' 'owner'\"'\"'s value'\n"
	if len(runner.inputs) != 1 || runner.inputs[0] != want {
		t.Fatalf("script commands input = %#v, want %q", runner.inputs, want)
	}
}

func TestNativeProviderComponentScriptOperationEnvironment(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Host:    "app1",
		Address: `host.app1.components.app.script["reload"]`,
		Action:  "run",
		ScriptPayload: &graph.ScriptPayload{
			Name:          "reload",
			ComponentName: "app",
			Mode:          "once",
			Kind:          "run",
			Interpreter:   []string{"/bin/sh", "-eu"},
			Run:           "systemctl reload app.service",
			TriggerAddresses: []string{
				`host.app1.components.app.files.file["/etc/app.conf"]`,
				`host.app1.components.app.files.file["/etc/app.d/extra.conf"]`,
			},
			TriggerPaths: []string{"/etc/app.conf", "/etc/app.d/extra.conf"},
		},
	}

	if _, err := provider.RunOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("scripts = %#v, want one command", runner.scripts)
	}
	command := runner.scripts[0]
	for _, want := range []string{
		"DBF_SCRIPT_NAME='reload'",
		"DBF_COMPONENT_NAME='app'",
		"DBF_TRIGGER_ADDRESS='host.app1.components.app.files.file[\"/etc/app.conf\"]'",
		"DBF_TRIGGER_PATH='/etc/app.conf'",
		"DBF_TRIGGER_ADDRESSES='host.app1.components.app.files.file[\"/etc/app.conf\"]\nhost.app1.components.app.files.file[\"/etc/app.d/extra.conf\"]'",
		"DBF_TRIGGER_PATHS='/etc/app.conf\n/etc/app.d/extra.conf'",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("script environment command missing %q:\n%s", want, command)
		}
	}
}

func TestNativeProviderComponentScriptOperationRecordsOutputs(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{
		{},
		{Stdout: "file\nroot\nroot\n644\nrendered-sha\n"},
	}}
	provider := NewNativeProvider(runner)
	outputAddress := `host.app1.components.app.script["render"].outputs["/tmp/rendered.conf"]`
	operation := graph.Operation{
		Host:    "app1",
		Address: `host.app1.components.app.script["render"]`,
		Action:  "run",
		ScriptPayload: &graph.ScriptPayload{
			Name:          "render",
			ComponentName: "app",
			Mode:          "once",
			Kind:          "run",
			Interpreter:   []string{"/bin/sh", "-eu"},
			Run:           "cp /tmp/source.conf /tmp/rendered.conf",
			Outputs: []graph.ScriptOutputPayload{{
				Address: outputAddress,
				Path:    "/tmp/rendered.conf",
			}},
		},
	}

	result, err := provider.RunOperation(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	output := result.Outputs[outputAddress]
	if output["sha256"] != "rendered-sha" || output["path"] != "/tmp/rendered.conf" || output["exists"] != true {
		t.Fatalf("operation outputs = %#v", result.Outputs)
	}
	if len(runner.scripts) != 2 || len(runner.inputs) != 1 {
		t.Fatalf("runner calls scripts=%#v inputs=%#v", runner.scripts, runner.inputs)
	}
}

func TestNativeProviderComponentScriptOutputPlanDetectsDrift(t *testing.T) {
	node := graph.Node{
		Address: `host.app1.components.app.script["render"].outputs["/tmp/rendered.conf"]`,
		Host:    "app1",
		Kind:    "component_script_output",
		Desired: map[string]any{
			"path":      "/tmp/rendered.conf",
			"component": "app",
			"script":    "render",
		},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "file\nroot\nroot\n644\ndrifted-sha\n"}}}
	provider := NewNativeProvider(runner)
	prior := &corestate.Resource{
		Ownership: "managed",
		Observed:  map[string]any{"sha256": "old-sha"},
	}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || !strings.Contains(got.Summary, "repair script output drift") {
		t.Fatalf("script output plan = %#v, want update drift", got)
	}
}

func TestNativeProviderComponentScriptOperationFailure(t *testing.T) {
	runner := &recordingRunner{errors: []error{errors.New("script failed")}}
	provider := NewNativeProvider(runner)
	operation := graph.Operation{
		Host:    "app1",
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

	_, err := provider.RunOperation(context.Background(), operation)
	if err == nil {
		t.Fatal("script operation succeeded, want injected runner failure")
	}
	if !strings.Contains(err.Error(), "script failed") {
		t.Fatalf("script operation error = %v, want runner error", err)
	}
}
