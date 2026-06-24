package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreengine "github.com/mofelee/debianform/internal/core/engine"
	coregraph "github.com/mofelee/debianform/internal/core/graph"
	coreir "github.com/mofelee/debianform/internal/core/ir"
	coremerge "github.com/mofelee/debianform/internal/core/merge"
	coreparser "github.com/mofelee/debianform/internal/core/parser"
	corestate "github.com/mofelee/debianform/internal/core/state"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestSecretRedactionRegressionMatrix(t *testing.T) {
	fixture := "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl"
	htmlPath := filepath.Join(t.TempDir(), "plan.html")

	matrix := []struct {
		name string
		run  func(t *testing.T) string
	}{
		{
			name: "plan text stdout",
			run: func(t *testing.T) string {
				stdout, stderr := captureOutput(t, func() {
					if err := run([]string{"plan", "-f", fixture, "--offline", "-var", "api_token=" + testassert.SensitiveVariableCLIValue}); err != nil {
						t.Fatal(err)
					}
				})
				return stdout + stderr
			},
		},
		{
			name: "plan json stdout",
			run: func(t *testing.T) string {
				stdout, stderr := captureOutput(t, func() {
					if err := run([]string{"plan", "-f", fixture, "--offline", "--format", "json", "-var", "api_token=" + testassert.SensitiveVariableCLIValue}); err != nil {
						t.Fatal(err)
					}
				})
				var decoded map[string]any
				if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
					t.Fatalf("plan JSON did not parse: %v\n%s", err, stdout)
				}
				return stdout + stderr
			},
		},
		{
			name: "plan html artifact and stdout",
			run: func(t *testing.T) string {
				stdout, stderr := captureOutput(t, func() {
					if err := run([]string{"plan", "-f", fixture, "--offline", "--html", htmlPath, "-var", "api_token=" + testassert.SensitiveVariableCLIValue}); err != nil {
						t.Fatal(err)
					}
				})
				data, err := os.ReadFile(htmlPath)
				if err != nil {
					t.Fatal(err)
				}
				return stdout + stderr + string(data)
			},
		},
		{
			name: "hostspec json",
			run: func(t *testing.T) string {
				program := compileRedactionFixture(t, fixture, []coreparser.ExternalVariableValue{{
					Name:   "api_token",
					Value:  testassert.SensitiveVariableCLIValue,
					Source: coreir.SourceRef{File: "<test>", Line: 1, Path: `variable["api_token"]`},
				}})
				data, err := json.Marshal(program)
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			},
		},
		{
			name: "resource graph desired json",
			run: func(t *testing.T) string {
				program := compileRedactionFixture(t, fixture, []coreparser.ExternalVariableValue{{
					Name:   "api_token",
					Value:  testassert.SensitiveVariableCLIValue,
					Source: coreir.SourceRef{File: "<test>", Line: 1, Path: `variable["api_token"]`},
				}})
				resourceGraph, err := coregraph.Compile(program)
				if err != nil {
					t.Fatal(err)
				}
				desired := make(map[string]map[string]any, len(resourceGraph.Nodes))
				for _, node := range resourceGraph.Nodes {
					desired[node.Address] = node.Desired
				}
				data, err := json.Marshal(desired)
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			},
		},
		{
			name: "state json",
			run: func(t *testing.T) string {
				return applyFixtureStateJSON(t, "../../internal/core/testdata/fixtures/ephemeral-variable-content.dbf.hcl", "ephemeral1")
			},
		},
		{
			name: "native provider command preview and error",
			run: func(t *testing.T) string {
				runner := &redactionMatrixRunner{
					err: errors.New("remote failed with " + testassert.EphemeralVariableValue),
				}
				provider := coreengine.NewNativeProvider(runner)
				node := redactionMatrixWriteOnlyNode()
				_, err := provider.Apply(context.Background(), coreengine.Step{Address: node.Address, Node: node, Action: coreengine.ActionCreate})
				if err == nil {
					t.Fatal("apply succeeded, want injected failure")
				}
				if len(runner.inputs) != 1 || runner.inputs[0] != testassert.EphemeralVariableValue {
					t.Fatalf("stdin payload = %#v, want write-only value", runner.inputs)
				}
				return strings.Join(runner.scripts, "\n") + "\n" + err.Error()
			},
		},
		{
			name: "native provider stdout stderr",
			run: func(t *testing.T) string {
				runner := &redactionMatrixRunner{
					result: coreengine.Result{
						Stdout: testassert.EphemeralVariableValue,
						Stderr: testassert.EphemeralVariableValue,
					},
				}
				provider := coreengine.NewNativeProvider(runner)
				node := redactionMatrixWriteOnlyNode()
				observed, err := provider.Apply(context.Background(), coreengine.Step{Address: node.Address, Node: node, Action: coreengine.ActionCreate})
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(observed)
				if err != nil {
					t.Fatal(err)
				}
				return strings.Join(runner.scripts, "\n") + "\n" + string(data)
			},
		},
	}

	for _, tt := range matrix {
		t.Run(tt.name, func(t *testing.T) {
			output := tt.run(t)
			testassert.NoSecretLeak(t, tt.name, output)
			assertRedactionMatrixNoEncodedPayload(t, output)
		})
	}
}

func compileRedactionFixture(t *testing.T, fixture string, values []coreparser.ExternalVariableValue) *coreir.Program {
	t.Helper()

	cfg, err := coreparser.ParseFilesWithOptions([]string{fixture}, coreparser.ParseOptions{VariableValues: values})
	if err != nil {
		t.Fatal(err)
	}
	program, err := coremerge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func applyFixtureStateJSON(t *testing.T, fixture, hostName string) string {
	t.Helper()

	cfg, err := coreparser.ParseFiles([]string{fixture})
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
	backend := coreengine.NewMemoryBackend()
	engine := coreengine.Engine{Backend: backend, Provider: coreengine.NewMemoryProvider()}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, coreengine.Options{Host: hostName}); err != nil {
		t.Fatal(err)
	}
	var host coreir.HostSpec
	for _, candidate := range program.Hosts {
		if candidate.Name == hostName {
			host = candidate
			break
		}
	}
	if host.Name == "" {
		t.Fatalf("host %q not found", hostName)
	}
	st, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	data, err := corestate.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type redactionMatrixRunner struct {
	scripts []string
	inputs  []string
	result  coreengine.Result
	err     error
}

func (r *redactionMatrixRunner) Run(ctx context.Context, host, script string) (coreengine.Result, error) {
	r.scripts = append(r.scripts, script)
	return r.result, r.err
}

func (r *redactionMatrixRunner) RunInput(ctx context.Context, host, remoteCommand string, inputReader io.Reader) (coreengine.Result, error) {
	data, err := io.ReadAll(inputReader)
	if err != nil {
		return coreengine.Result{}, err
	}
	r.scripts = append(r.scripts, remoteCommand)
	r.inputs = append(r.inputs, string(data))
	return r.result, r.err
}

func (r *redactionMatrixRunner) RunCommand(ctx context.Context, host, remoteCommand string) (coreengine.Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

func redactionMatrixWriteOnlyNode() coregraph.Node {
	desired := map[string]any{
		"path":               "/etc/app/token",
		"owner":              "root",
		"group":              "root",
		"mode":               "0600",
		"ensure":             "present",
		"sensitive":          true,
		"content_write_only": true,
		"content_version":    "v1",
	}
	payload := map[string]any{}
	for key, value := range desired {
		payload[key] = value
	}
	payload["content"] = testassert.EphemeralVariableValue
	return coregraph.Node{
		Address:         `host.server1.files.file["/etc/app/token"]`,
		Host:            "server1",
		Kind:            "file",
		Desired:         desired,
		ProviderPayload: payload,
	}
}

func assertRedactionMatrixNoEncodedPayload(t *testing.T, text string) {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(testassert.EphemeralVariableValue))
	if strings.Contains(text, encoded) {
		t.Fatalf("encoded write-only payload leaked:\n%s", text)
	}
}
