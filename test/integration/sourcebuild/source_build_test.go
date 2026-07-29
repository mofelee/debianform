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
	"strings"
	"testing"

	coreengine "github.com/mofelee/debianform/internal/core/engine"
	coregraph "github.com/mofelee/debianform/internal/core/graph"
	coreir "github.com/mofelee/debianform/internal/core/ir"
	coremerge "github.com/mofelee/debianform/internal/core/merge"
	coreparser "github.com/mofelee/debianform/internal/core/parser"
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

func TestParsedComponentArtifactInputsDownloadPerInstance(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is required for component artifact input integration test")
	}

	payloads := map[string][]byte{
		"binary":  []byte("binary artifact input payload\n"),
		"archive": []byte("archive artifact input payload\n"),
		"source":  []byte("source artifact input payload\n"),
	}
	requested := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) != 2 || (parts[0] != "mirror-a" && parts[0] != "mirror-b") {
			http.NotFound(w, r)
			return
		}
		payload, ok := payloads[parts[1]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		requested[r.URL.Path]++
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	shaValues := map[string]string{}
	for name, payload := range payloads {
		sum := sha256.Sum256(payload)
		shaValues[name] = hex.EncodeToString(sum[:])
	}
	variableSource := coreir.SourceRef{File: "<integration>", Line: 1, Path: "test.variable"}
	cfg, err := coreparser.ParseFilesWithOptions(
		[]string{"testdata/component_artifact_inputs.dbf.hcl"},
		coreparser.ParseOptions{VariableValues: []coreparser.ExternalVariableValue{
			{Name: "base_url", Value: server.URL, Source: variableSource},
			{Name: "binary_sha256", Value: shaValues["binary"], Source: variableSource},
			{Name: "archive_sha256", Value: shaValues["archive"], Source: variableSource},
			{Name: "source_sha256", Value: shaValues["source"], Source: variableSource},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	program, err := coremerge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := coregraph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}

	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	currentGroup, err := user.LookupGroupId(currentUser.Gid)
	if err != nil {
		t.Fatal(err)
	}
	provider := coreengine.NewNativeProvider(localRunner{})
	root := t.TempDir()
	downloads := 0
	for _, original := range resourceGraph.Nodes {
		if original.Kind != "component_download" {
			continue
		}
		downloads++
		node := original
		node.Desired = cloneMap(original.Desired)
		node.ProviderPayload = cloneMap(original.ProviderPayload)
		component, _ := node.Desired["component"].(string)
		mirror := strings.ReplaceAll(node.Host, "_", "-")
		wantURL := server.URL + "/" + mirror + "/" + component
		if node.Desired["url"] != wantURL || node.Desired["sha256"] != shaValues[component] {
			t.Fatalf("%s resolved source = %#v, want %s / %s", node.Address, node.Desired, wantURL, shaValues[component])
		}
		downloadPath := filepath.Join(root, node.Host, component)
		for _, values := range []map[string]any{node.Desired, node.ProviderPayload} {
			values["path"] = downloadPath
			values["owner"] = currentUser.Username
			values["group"] = currentGroup.Name
		}

		plan, err := provider.Plan(context.Background(), node, nil)
		if err != nil {
			t.Fatalf("%s plan failed: %v", node.Address, err)
		}
		if plan.Action != coreengine.ActionCreate {
			t.Fatalf("%s action = %q, want create", node.Address, plan.Action)
		}
		if _, err := provider.Apply(context.Background(), coreengine.Step{Address: node.Address, Host: node.Host, Action: coreengine.ActionCreate, Node: node}); err != nil {
			t.Fatalf("%s apply failed: %v", node.Address, err)
		}
		got, err := os.ReadFile(downloadPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payloads[component]) {
			t.Fatalf("%s payload = %q, want %q", node.Address, got, payloads[component])
		}
	}
	if downloads != 6 {
		t.Fatalf("component download count = %d, want 6", downloads)
	}
	for _, mirror := range []string{"mirror-a", "mirror-b"} {
		for component := range payloads {
			path := "/" + mirror + "/" + component
			if requested[path] != 1 {
				t.Fatalf("request count for %s = %d, want 1", path, requested[path])
			}
		}
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
