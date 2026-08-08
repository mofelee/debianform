package engine

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestNativeProviderSensitiveComponentDownloadRedactsFailure(t *testing.T) {
	secretURL := "https://not-a-real-variable-secret@example.invalid/private-tool"
	node := graph.Node{
		Address: "host.server1.components.private.artifact.download[\"amd64\"]",
		Host:    "server1",
		Kind:    "component_download",
		Desired: map[string]any{
			"path":      "/var/cache/debianform/components/private/redacted/source",
			"url":       secretURL,
			"sha256":    "5555555555555555555555555555555555555555555555555555555555555555",
			"owner":     "root",
			"group":     "root",
			"mode":      "0644",
			"ensure":    "present",
			"sensitive": true,
		},
	}
	runner := &recordingRunner{errors: []error{errors.New("remote echoed " + secretURL)}}
	provider := NewNativeProvider(runner)

	_, err := provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionCreate})
	if err == nil {
		t.Fatal("sensitive component download succeeded, want injected failure")
	}
	if strings.Contains(err.Error(), secretURL) || err.Error() != "redacted payload command failed: <redacted>" {
		t.Fatalf("sensitive component download error = %q", err)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], secretURL) {
		t.Fatalf("provider did not receive sensitive URL in memory: %#v", runner.scripts)
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
			"cache_path":   "/var/cache/debianform/components/hello/source",
			"build_path":   "/var/cache/debianform/components/hello/build",
			"output_path":  "/var/cache/debianform/components/hello/build/out/hash/hello",
			"staging_root": "/var/tmp/debianform-source-staging",
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
	assertComponentWorkspaceScript(t, applied, "/var/tmp/debianform-source-staging")
	for _, want := range []string{
		"cp -- '/var/cache/debianform/components/hello/source' \"$src/hello.c\"",
		"rm -rf -- \"$build_root/work\"",
		"cd \"$src\"",
		"set -- 'cc' '-O2' '-o' 'hello' 'hello.c'\n\"$@\"",
		"install -o 'root' -g 'root' -m '0644' \"$built\" '/var/cache/debianform/components/hello/build/out/hash/hello'",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component build script missing %q:\n%s", want, applied)
		}
	}
	for _, unwanted := range []string{
		"mv \"$src\" \"$build_root/work\"",
		"cd \"$build_root/work\"",
	} {
		if strings.Contains(applied, unwanted) {
			t.Fatalf("component build script retained persistent work path %q:\n%s", unwanted, applied)
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
			"staging_root":     "/var/tmp/debianform component staging",
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
	assertComponentWorkspaceScript(t, applied, "/var/tmp/debianform component staging")
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
			"staging_root":     "/var/tmp/debianform-tar-staging",
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
	assertComponentWorkspaceScript(t, applied, "/var/tmp/debianform-tar-staging")
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
			"staging_root":   "/var/tmp/debianform-gzip-staging",
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
	assertComponentWorkspaceScript(t, applied, "/var/tmp/debianform-gzip-staging")
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
			"staging_root":     "/opt/.debianform-myapp-staging",
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
	assertComponentWorkspaceScript(t, applied, "/opt/.debianform-myapp-staging")
	for _, want := range []string{
		"tar --no-same-owner -xzf '/var/cache/debianform/components/myapp/source'",
		"--strip-components '1'",
		"chown -R 'myapp:myapp'",
		"mv -- \"$tmp\" \"$archive_destination\"",
		"component_workspace_rollback() {",
		"archive_old_moved=1",
		"archive_committed=1",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("component archive apply script missing %q:\n%s", want, applied)
		}
	}
}

func TestComponentWorkspaceSetupKeepsAutomaticMktempCompatibility(t *testing.T) {
	script := strings.Join(componentWorkspaceSetup(graph.Node{}, ""), "\n")
	for _, want := range []string{
		"staging_root=${TMPDIR:-/tmp}",
		"work=$(mktemp -d)",
		"staging_root=$(dirname \"$work\")",
		"df -Pk \"$staging_root\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("automatic component workspace script missing %q:\n%s", want, script)
		}
	}
}

func TestNativeProviderComponentWorkspaceFailureReportsSpaceAndCleans(t *testing.T) {
	if _, err := exec.LookPath("gzip"); err != nil {
		t.Skip("gzip is required for component workspace failure test")
	}

	root := t.TempDir()
	stagingRoot := filepath.Join(root, "alternate staging root with spaces")
	cachePath := filepath.Join(root, "broken.gz")
	installPath := filepath.Join(root, "bin", "tool")
	if err := os.WriteFile(cachePath, []byte("not a gzip stream\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner, group := currentUserAndGroup(t)
	node := graph.Node{
		Address: "host.local.components.tool.artifact.install[\"" + installPath + "\"]",
		Host:    "local",
		Kind:    "component_binary",
		Desired: map[string]any{
			"path":           installPath,
			"cache_path":     cachePath,
			"staging_root":   stagingRoot,
			"extract_format": "gz",
			"owner":          owner,
			"group":          group,
			"mode":           "0755",
			"ensure":         "present",
		},
	}
	runner := &countingLocalRunner{}
	provider := NewNativeProvider(runner)

	_, err := provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionCreate})
	if err == nil {
		t.Fatal("broken compressed component succeeded, want staging failure")
	}
	for _, want := range []string{stagingRoot, "work_path=", "available_kib="} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("component workspace failure = %v, want %q", err, want)
		}
	}
	entries, readErr := os.ReadDir(stagingRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("component workspace residue = %#v, want empty staging root", entries)
	}
	info, statErr := os.Stat(stagingRoot)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("created staging root mode = %04o, want 0700", got)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("component workspace runner scripts = %d, want 1", len(runner.scripts))
	}
	assertComponentWorkspaceScript(t, runner.scripts[0], stagingRoot)
}

func TestNativeProviderComponentWorkspaceCleanupFailureFailsApply(t *testing.T) {
	realRM, err := exec.LookPath("rm")
	if err != nil {
		t.Skip("rm is required for component workspace cleanup failure test")
	}
	if _, err := exec.LookPath("gzip"); err != nil {
		t.Skip("gzip is required for component workspace cleanup failure test")
	}

	root := t.TempDir()
	stagingRoot := filepath.Join(root, "alternate staging root")
	cachePath := filepath.Join(root, "tool.gz")
	installPath := filepath.Join(root, "bin", "tool")
	writeComponentGzipFixture(t, cachePath, []byte("installed\n"))

	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	rmWrapper := "#!/bin/sh\ncase \"$*\" in\n  *.debianform-component.*) exit 43 ;;\nesac\nexec " + shellQuote(realRM) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "rm"), []byte(rmWrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+":"+os.Getenv("PATH"))

	owner, group := currentUserAndGroup(t)
	node := graph.Node{
		Address: "host.local.components.tool.artifact.install[\"" + installPath + "\"]",
		Host:    "local",
		Kind:    "component_binary",
		Desired: map[string]any{
			"path":           installPath,
			"cache_path":     cachePath,
			"staging_root":   stagingRoot,
			"extract_format": "gz",
			"owner":          owner,
			"group":          group,
			"mode":           "0755",
			"ensure":         "present",
		},
	}
	runner := &countingLocalRunner{}
	provider := NewNativeProvider(runner)

	_, err = provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionCreate})
	if err == nil {
		t.Fatal("component cleanup failure succeeded, want apply failure")
	}
	for _, want := range []string{"exit status 43", stagingRoot, "work_path=", "available_kib="} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("component cleanup failure = %v, want %q", err, want)
		}
	}
	if got, readErr := os.ReadFile(installPath); readErr != nil || string(got) != "installed\n" {
		t.Fatalf("component operation did not finish before cleanup failure: content=%q err=%v", got, readErr)
	}
	entries, readErr := os.ReadDir(stagingRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), ".debianform-component.") {
		t.Fatalf("injected cleanup residue = %#v, want one workspace", entries)
	}
}

func TestNativeProviderComponentArchiveFailureRollsBackAndCleans(t *testing.T) {
	realMV, err := exec.LookPath("mv")
	if err != nil {
		t.Skip("mv is required for component archive rollback test")
	}
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is required for component archive rollback test")
	}

	root := t.TempDir()
	parent := filepath.Join(root, "archive destination with spaces")
	destination := filepath.Join(parent, "app")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "old.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(root, "app.tar.gz")
	writeComponentTarGzipFixture(t, cachePath, "payload/new.txt", []byte("new\n"))

	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	mvWrapper := "#!/bin/sh\nif [ \"$1\" = -- ]; then shift; fi\ncase \"$1\" in\n  *.dbf-new) exit 42 ;;\nesac\nexec " + shellQuote(realMV) + " -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "mv"), []byte(mvWrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+":"+os.Getenv("PATH"))

	owner, group := currentUserAndGroup(t)
	node := graph.Node{
		Address: "host.local.components.app.artifact.install[\"" + destination + "\"]",
		Host:    "local",
		Kind:    "component_archive",
		Desired: map[string]any{
			"path":             destination,
			"cache_path":       cachePath,
			"extract_format":   "tar.gz",
			"strip_components": 1,
			"owner":            owner,
			"group":            group,
			"mode":             "0755",
			"ensure":           "present",
		},
	}
	runner := &countingLocalRunner{}
	provider := NewNativeProvider(runner)

	_, err = provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionCreate})
	if err == nil {
		t.Fatal("archive commit succeeded, want injected mv failure")
	}
	for _, want := range []string{"exit status 42", parent, "work_path=", "available_kib="} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("archive workspace failure = %v, want %q", err, want)
		}
	}
	if got, readErr := os.ReadFile(filepath.Join(destination, "old.txt")); readErr != nil || string(got) != "old\n" {
		t.Fatalf("archive rollback original = %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(destination, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("failed archive content survived rollback: %v", statErr)
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".debianform-component.") || entry.Name() == ".app.dbf-new" || entry.Name() == ".app.dbf-old" {
			t.Fatalf("archive rollback left residue %q", entry.Name())
		}
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("archive workspace runner scripts = %d, want 1", len(runner.scripts))
	}
	assertComponentWorkspaceScript(t, runner.scripts[0], parent)
}

func assertComponentWorkspaceScript(t *testing.T, script, stagingRoot string) {
	t.Helper()
	for _, want := range []string{
		"staging_root=" + shellQuote(stagingRoot),
		"component_previous_umask=$(umask)",
		"umask 077",
		"trap component_workspace_cleanup EXIT",
		"mkdir -p -- \"$staging_root\"",
		"work=$(mktemp -d \"${staging_root%/}/.debianform-component.XXXXXX\")",
		"chmod 0700 \"$work\"",
		"umask \"$component_previous_umask\"",
		"df -Pk \"$staging_root\"",
		"available_kib=%s",
		"if rm -rf -- \"$work\"; then",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("component workspace script missing %q:\n%s", want, script)
		}
	}
	for _, line := range strings.Split(script, "\n") {
		if strings.TrimSpace(line) == "work=$(mktemp -d)" {
			t.Fatalf("explicit component workspace used bare mktemp:\n%s", script)
		}
	}
}

func writeComponentGzipFixture(t *testing.T, destination string, content []byte) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	if _, err := gzipWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeComponentTarGzipFixture(t *testing.T, destination, name string, content []byte) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
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
