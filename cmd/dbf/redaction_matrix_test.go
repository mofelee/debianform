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

	v2engine "github.com/mofelee/debianform/internal/v2/engine"
	v2graph "github.com/mofelee/debianform/internal/v2/graph"
	v2ir "github.com/mofelee/debianform/internal/v2/ir"
	v2merge "github.com/mofelee/debianform/internal/v2/merge"
	v2parser "github.com/mofelee/debianform/internal/v2/parser"
	v2state "github.com/mofelee/debianform/internal/v2/state"
	"github.com/mofelee/debianform/internal/v2/testassert"
)

func TestSecretRedactionRegressionMatrix(t *testing.T) {
	fixture := "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl"
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
				program := compileRedactionFixture(t, fixture, []v2parser.ExternalVariableValue{{
					Name:   "api_token",
					Value:  testassert.SensitiveVariableCLIValue,
					Source: v2ir.SourceRef{File: "<test>", Line: 1, Path: `variable["api_token"]`},
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
				program := compileRedactionFixture(t, fixture, []v2parser.ExternalVariableValue{{
					Name:   "api_token",
					Value:  testassert.SensitiveVariableCLIValue,
					Source: v2ir.SourceRef{File: "<test>", Line: 1, Path: `variable["api_token"]`},
				}})
				resourceGraph, err := v2graph.Compile(program)
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
				return applyFixtureStateJSON(t, "../../internal/v2/testdata/fixtures/v2-ephemeral-variable-content.dbf.hcl", "ephemeral1")
			},
		},
		{
			name: "native provider command preview and error",
			run: func(t *testing.T) string {
				runner := &redactionMatrixRunner{
					err: errors.New("remote failed with " + testassert.EphemeralVariableValue),
				}
				provider := v2engine.NewNativeProvider(runner)
				node := redactionMatrixWriteOnlyNode()
				_, err := provider.Apply(context.Background(), v2engine.Step{Address: node.Address, Node: node, Action: v2engine.ActionCreate})
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
					result: v2engine.Result{
						Stdout: testassert.EphemeralVariableValue,
						Stderr: testassert.EphemeralVariableValue,
					},
				}
				provider := v2engine.NewNativeProvider(runner)
				node := redactionMatrixWriteOnlyNode()
				observed, err := provider.Apply(context.Background(), v2engine.Step{Address: node.Address, Node: node, Action: v2engine.ActionCreate})
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

func compileRedactionFixture(t *testing.T, fixture string, values []v2parser.ExternalVariableValue) *v2ir.Program {
	t.Helper()

	cfg, err := v2parser.ParseFilesWithOptions([]string{fixture}, v2parser.ParseOptions{VariableValues: values})
	if err != nil {
		t.Fatal(err)
	}
	program, err := v2merge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func applyFixtureStateJSON(t *testing.T, fixture, hostName string) string {
	t.Helper()

	cfg, err := v2parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := v2merge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := v2graph.Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	backend := v2engine.NewMemoryBackend()
	engine := v2engine.Engine{Backend: backend, Provider: v2engine.NewMemoryProvider()}
	if _, err := engine.Apply(context.Background(), program, resourceGraph, v2engine.Options{Host: hostName}); err != nil {
		t.Fatal(err)
	}
	var host v2ir.HostSpec
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
	data, err := v2state.Encode(st)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type redactionMatrixRunner struct {
	scripts []string
	inputs  []string
	result  v2engine.Result
	err     error
}

func (r *redactionMatrixRunner) Run(ctx context.Context, host, script string) (v2engine.Result, error) {
	r.scripts = append(r.scripts, script)
	return r.result, r.err
}

func (r *redactionMatrixRunner) RunInput(ctx context.Context, host, remoteCommand string, inputReader io.Reader) (v2engine.Result, error) {
	data, err := io.ReadAll(inputReader)
	if err != nil {
		return v2engine.Result{}, err
	}
	r.scripts = append(r.scripts, remoteCommand)
	r.inputs = append(r.inputs, string(data))
	return r.result, r.err
}

func (r *redactionMatrixRunner) RunCommand(ctx context.Context, host, remoteCommand string) (v2engine.Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

func redactionMatrixWriteOnlyNode() v2graph.Node {
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
	return v2graph.Node{
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
