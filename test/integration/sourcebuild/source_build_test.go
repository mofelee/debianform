package sourcebuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"

	coreengine "github.com/mofelee/debianform/internal/core/engine"
	coregraph "github.com/mofelee/debianform/internal/core/graph"
)

type localRunner struct{}

func (localRunner) Run(ctx context.Context, host, script string) (coreengine.Result, error) {
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Stdin = bytes.NewBufferString(script)
	return localRunner{}.run(cmd)
}

func (localRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (coreengine.Result, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", remoteCommand)
	cmd.Stdin = input
	return localRunner{}.run(cmd)
}

func (localRunner) run(cmd *exec.Cmd) (coreengine.Result, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := coreengine.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return result, fmt.Errorf("local script failed: %w: %s", err, stderr.String())
	}
	return result, nil
}

func (r localRunner) RunCommand(ctx context.Context, host, remoteCommand string) (coreengine.Result, error) {
	return r.Run(ctx, host, remoteCommand+"\n")
}

func TestSourceBuildDownloadCompileInstall(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc is required for source-build integration test")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is required for source-build integration test")
	}

	ctx := context.Background()
	root := t.TempDir()
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	currentGroup, err := user.LookupGroupId(currentUser.Gid)
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(root, "cache", "source")
	buildPath := filepath.Join(root, "build")
	buildOutputPath := filepath.Join(root, "build-output", "hello-from-source")
	installPath := filepath.Join(root, "bin", "hello-from-source")

	source := []byte(`#include <stdio.h>

int main(void) {
  puts("hello from debianform source build");
  return 0;
}
`)
	sum := sha256.Sum256(source)
	sourceSHA := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello.c" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/x-c")
		_, _ = w.Write(source)
	}))
	defer server.Close()

	provider := coreengine.NewNativeProvider(localRunner{})
	download := coregraph.Node{
		Address: "host.local.components.hello_from_source.artifact.download[\"default\"]",
		Host:    "local",
		Kind:    "component_download",
		Desired: map[string]any{
			"path":   cachePath,
			"url":    server.URL + "/hello.c",
			"sha256": sourceSHA,
			"owner":  currentUser.Username,
			"group":  currentGroup.Name,
			"mode":   "0644",
			"ensure": "present",
		},
	}
	build := coregraph.Node{
		Address: "host.local.components.hello_from_source.artifact.build[\"" + buildOutputPath + "\"]",
		Host:    "local",
		Kind:    "component_build",
		Desired: map[string]any{
			"cache_path":  cachePath,
			"build_path":  buildPath,
			"output_path": buildOutputPath,
			"commands": [][]string{
				{"cc", "-O2", "-Wall", "-o", "hello-from-source", "hello.c"},
			},
			"output":      "hello-from-source",
			"source_name": "hello.c",
			"owner":       currentUser.Username,
			"group":       currentGroup.Name,
			"mode":        "0755",
			"ensure":      "present",
		},
	}
	install := coregraph.Node{
		Address: "host.local.components.hello_from_source.artifact.install[\"" + installPath + "\"]",
		Host:    "local",
		Kind:    "component_binary",
		Desired: map[string]any{
			"path":       installPath,
			"cache_path": buildOutputPath,
			"owner":      currentUser.Username,
			"group":      currentGroup.Name,
			"mode":       "0755",
			"ensure":     "present",
		},
	}

	for _, node := range []coregraph.Node{download, build, install} {
		plan, err := provider.Plan(ctx, node, nil)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Action != coreengine.ActionCreate {
			t.Fatalf("%s action = %q, want create", node.Address, plan.Action)
		}
		if _, err := provider.Apply(ctx, coreengine.Step{Address: node.Address, Host: node.Host, Action: coreengine.ActionCreate, Node: node}); err != nil {
			t.Fatalf("%s apply failed: %v", node.Address, err)
		}
	}

	out, err := exec.CommandContext(ctx, installPath).Output()
	if err != nil {
		t.Fatalf("installed binary failed: %v", err)
	}
	if got := string(bytes.TrimSpace(out)); got != "hello from debianform source build" {
		t.Fatalf("installed binary output = %q", got)
	}

	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("download cache missing: %v", err)
	}
	if _, err := os.Stat(buildOutputPath); err != nil {
		t.Fatalf("build output missing: %v", err)
	}
}
