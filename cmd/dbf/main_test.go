package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigFilesLoadsAllDBFHCLInCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, name := range []string{"20-app.dbf.hcl", "10-base.dbf.hcl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := configFiles("")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10-base.dbf.hcl", "20-app.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("configFiles() = %#v, want %#v", files, want)
	}
}

func TestConfigFilesWithExplicitFile(t *testing.T) {
	files, err := configFiles("custom.dbf.hcl")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"custom.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("configFiles() = %#v, want %#v", files, want)
	}
}

func TestVersionCommand(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"version"}); err != nil {
			t.Fatal(err)
		}
	})

	for _, field := range []string{"dbf ", "commit: ", "built: ", "go: ", "platform: "} {
		if !strings.Contains(output, field) {
			t.Fatalf("version output %q does not contain %q", output, field)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"--version"}); err != nil {
			t.Fatal(err)
		}
	})

	if lines := strings.Count(strings.TrimSpace(output), "\n") + 1; lines != 1 {
		t.Fatalf("--version output has %d lines, want 1: %q", lines, output)
	}
}

func TestValidateRunnableV2Examples(t *testing.T) {
	for _, example := range runnableV2Examples() {
		t.Run(filepath.Base(example), func(t *testing.T) {
			output := captureStdout(t, func() {
				if err := run([]string{"validate", "-f", "../../" + example}); err != nil {
					t.Fatal(err)
				}
			})

			if !strings.Contains(output, "v2 configuration is valid: 1 host(s)") {
				t.Fatalf("validate output = %q", output)
			}
		})
	}
}

func TestValidateV2StillRejectsComponentInputErrors(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
component "tools" {
  input "repo_uri" {
    type = string
  }
}

host "server1" {
  components = [component.tools]
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"validate", "-f", config})
	if err == nil || !strings.Contains(err.Error(), `input "repo_uri" is required`) {
		t.Fatalf("validate error = %v, want missing input", err)
	}
}

func TestValidateAcceptsUnreferencedVariableDeclarations(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"validate", "-f", "../../internal/v2/testdata/fixtures/v2-variable-declarations.dbf.hcl"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "v2 configuration is valid: 1 host(s)") {
		t.Fatalf("validate output = %q", output)
	}
}

func TestValidateAndPlanAcceptVariableDefaults(t *testing.T) {
	validateOutput := captureStdout(t, func() {
		if err := run([]string{"validate", "-f", "../../internal/v2/testdata/fixtures/v2-variable-defaults.dbf.hcl"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(validateOutput, "v2 configuration is valid: 1 host(s)") {
		t.Fatalf("validate output = %q", validateOutput)
	}

	planOutput := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../internal/v2/testdata/fixtures/v2-variable-defaults.dbf.hcl", "--offline"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(planOutput, "hello from variable default") {
		t.Fatalf("plan output = %q", planOutput)
	}
}

func TestValidateAndPlanAcceptCLIVariableValues(t *testing.T) {
	fixture := "../../internal/v2/testdata/fixtures/v2-variable-cli.dbf.hcl"
	args := []string{
		"-var", "environment=prod",
		"-var", "environment=stage",
		"-var", "replicas=3",
		"-var", "enabled=true",
		"-var", "ports=[80,443]",
		"-var", `labels={"tier":"frontend"}`,
	}

	validateArgs := append([]string{"validate", "-f", fixture}, args...)
	validateOutput := captureStdout(t, func() {
		if err := run(validateArgs); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(validateOutput, "v2 configuration is valid: 1 host(s)") {
		t.Fatalf("validate output = %q", validateOutput)
	}

	planArgs := append([]string{"plan", "-f", fixture, "--offline"}, args...)
	planOutput := captureStdout(t, func() {
		if err := run(planArgs); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		`"environment":"stage"`,
		`"replicas":3`,
		`"enabled":true`,
		`"ports":[80,443]`,
		`"labels":{"canary":false,"tier":"frontend"}`,
	} {
		if !strings.Contains(planOutput, want) {
			t.Fatalf("plan output %q does not contain %q", planOutput, want)
		}
	}
	if strings.Contains(planOutput, `"environment":"prod"`) {
		t.Fatalf("plan output kept earlier -var value: %q", planOutput)
	}
}

func TestCLIVariableErrors(t *testing.T) {
	fixture := "../../internal/v2/testdata/fixtures/v2-variable-cli.dbf.hcl"

	err := run([]string{"validate", "-f", fixture, "-var", "missing=value"})
	if err == nil || !strings.Contains(err.Error(), `unknown variable "missing"`) {
		t.Fatalf("validate unknown -var error = %v, want unknown variable", err)
	}

	secret := "not-a-real-cli-secret"
	err = run([]string{"validate", "-f", fixture, "-var", "token_seed=" + secret})
	if err == nil || !strings.Contains(err.Error(), `invalid value for sensitive variable "token_seed"`) {
		t.Fatalf("validate sensitive -var error = %v, want redacted invalid value", err)
	}
	if err != nil && strings.Contains(err.Error(), secret) {
		t.Fatalf("sensitive CLI value leaked in error: %v", err)
	}
}

func TestValidateAndPlanPrintDeprecatedInputWarnings(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
component "app" {
  input "listen_addr" {
    type       = string
    default    = "127.0.0.1:8080"
    deprecated = "Use listeners instead."
  }
}

host "server1" {
  component "app" {
    source = component.app
    inputs = {
      listen_addr = "0.0.0.0:8080"
    }
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	_, validateOutput := captureOutput(t, func() {
		if err := run([]string{"validate", "-f", config}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(validateOutput, "warning:") || !strings.Contains(validateOutput, `input "listen_addr" is deprecated`) {
		t.Fatalf("validate output = %q, want deprecated warning", validateOutput)
	}

	planStdout, planOutput := captureOutput(t, func() {
		if err := run([]string{"plan", "-f", config, "--offline"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(planOutput, "warning:") || !strings.Contains(planOutput, `input "listen_addr" is deprecated`) {
		t.Fatalf("plan output = %q, want deprecated warning", planOutput)
	}
	if strings.Contains(planStdout, "warning:") {
		t.Fatalf("plan stdout contains warning: %q", planStdout)
	}

	planJSONStdout, planJSONStderr := captureOutput(t, func() {
		if err := run([]string{"plan", "-f", config, "--offline", "--format", "json"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(planJSONStderr, "warning:") || !strings.Contains(planJSONStderr, `input "listen_addr" is deprecated`) {
		t.Fatalf("plan JSON stderr = %q, want deprecated warning", planJSONStderr)
	}
	if strings.Contains(planJSONStdout, "warning:") {
		t.Fatalf("plan JSON stdout contains warning: %q", planJSONStdout)
	}
	var doc struct {
		FormatVersion string `json:"format_version"`
	}
	if err := json.Unmarshal([]byte(planJSONStdout), &doc); err != nil {
		t.Fatalf("plan JSON stdout did not parse: %v\n%s", err, planJSONStdout)
	}
	if doc.FormatVersion != "debianform.plan.v2alpha1" {
		t.Fatalf("format_version = %q", doc.FormatVersion)
	}
}

func TestComponentInspect(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
component "proxy" {
  input "listeners" {
    type        = list(object({ name = string, port = number }))
    description = "Listeners."
    nullable    = false
    default     = []
    deprecated  = "Use endpoints instead."
  }

  input "token" {
    type      = string
    default   = "secret-token"
    sensitive = true
  }
}

host "server1" {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"component", "inspect", "-f", config, "proxy"}); err != nil {
			t.Fatal(err)
		}
	})
	assertGolden(t, filepath.Join("testdata", "component-inspect.golden.json"), output)

	err := run([]string{"component", "inspect", "-f", config, "missing"})
	if err == nil || !strings.Contains(err.Error(), "unknown component.missing") {
		t.Fatalf("inspect unknown error = %v", err)
	}
}

func TestPlanV2BBRText(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--offline"}); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{
		`host.bbr1.kernel.module["tcp_bbr"]`,
		`host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`,
		"Summary: 3 create",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("plan output %q does not contain %q", output, want)
		}
	}
}

func TestPlanV2NftablesText(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/v2-nftables.dbf.hcl", "--offline"}); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{
		`host.edge1.nftables.file["20-services"]`,
		`host.edge1.nftables.validate`,
		`host.edge1.nftables.activate`,
		"Summary: 6 create",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("plan output %q does not contain %q", output, want)
		}
	}
}

func TestPlanV2BBRJSON(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--format", "json", "--offline"}); err != nil {
			t.Fatal(err)
		}
	})

	var doc struct {
		FormatVersion string `json:"format_version"`
		Summary       struct {
			Create int `json:"create"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("plan JSON did not parse: %v\n%s", err, output)
	}
	if doc.FormatVersion != "debianform.plan.v2alpha1" {
		t.Fatalf("format_version = %q", doc.FormatVersion)
	}
	if doc.Summary.Create != 3 {
		t.Fatalf("summary.create = %d, want 3", doc.Summary.Create)
	}
}

func TestPlanV2BBRJSONDebug(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--format", "json", "--debug", "--offline"}); err != nil {
			t.Fatal(err)
		}
	})

	var doc struct {
		Changes []struct {
			ProviderAddress string `json:"provider_address"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("debug plan JSON did not parse: %v\n%s", err, output)
	}
	if len(doc.Changes) == 0 || doc.Changes[0].ProviderAddress == "" {
		t.Fatalf("debug plan does not contain provider addresses: %s", output)
	}
}

func TestOfflinePlanExplainsRuntimeFactsDependency(t *testing.T) {
	err := run([]string{"plan", "-f", "../../examples/v2-bird2.dbf.hcl", "--offline"})
	if err == nil || !strings.Contains(err.Error(), "offline plan cannot resolve runtime facts") {
		t.Fatalf("plan --offline error = %v, want runtime facts explanation", err)
	}
}

func TestParallelFlagIsApplyOnlyAndPositive(t *testing.T) {
	err := run([]string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--parallel", "2"})
	if err == nil || !strings.Contains(err.Error(), "--parallel is only supported for v2 apply") {
		t.Fatalf("plan --parallel error = %v", err)
	}

	err = run([]string{"check", "-f", "../../examples/v2-bbr.dbf.hcl", "--offline"})
	if err == nil || !strings.Contains(err.Error(), "--offline is only supported for v2 plan") {
		t.Fatalf("check --offline error = %v", err)
	}

	err = run([]string{"apply", "-f", "../../examples/v2-bbr.dbf.hcl", "--parallel", "0", "--auto-approve"})
	if err == nil || !strings.Contains(err.Error(), "--parallel must be at least 1") {
		t.Fatalf("apply --parallel 0 error = %v", err)
	}
}

func TestPlanV2BBRHTML(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "plan.html")
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--html", htmlPath, "--offline"}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "wrote HTML plan to "+htmlPath) {
		t.Fatalf("plan --html output = %q", output)
	}
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		"DebianForm Plan",
		`host.bbr1.kernel.module[&#34;tcp_bbr&#34;]`,
		"Summary",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML output does not contain %q:\n%s", want, html)
		}
	}
}

func TestREADMELocalCommandsAreCopyRunnable(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readmeText := string(readme)
	for _, example := range runnableV2Examples() {
		if !strings.Contains(readmeText, example) {
			t.Fatalf("README does not mention runnable v2 example %s", example)
		}
	}
	if strings.Contains(readmeText, "dbf plan -f examples/v2-bird2.dbf.hcl\n") {
		t.Fatal("README contains a copy-runnable-looking online BIRD2 plan command")
	}

	dir := t.TempDir()
	fmtFixture := filepath.Join(dir, "v2-bbr.dbf.hcl")
	data, err := os.ReadFile("../../examples/v2-bbr.dbf.hcl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fmtFixture, data, 0644); err != nil {
		t.Fatal(err)
	}

	commands := []struct {
		name string
		args []string
	}{
		{name: "validate-bbr", args: []string{"validate", "-f", "../../examples/v2-bbr.dbf.hcl"}},
		{name: "plan-bbr-offline", args: []string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--offline"}},
		{name: "plan-bbr-json", args: []string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--format", "json", "--offline"}},
		{name: "plan-bbr-debug-json", args: []string{"plan", "-f", "../../examples/v2-bbr.dbf.hcl", "--format", "json", "--debug", "--offline"}},
		{name: "plan-files-html", args: []string{"plan", "-f", "../../examples/v2-files-plan-preview.dbf.hcl", "--html", filepath.Join(dir, "plan.html"), "--offline"}},
		{name: "fmt-bbr-copy", args: []string{"fmt", "-f", fmtFixture}},
		{name: "validate-bird2", args: []string{"validate", "-f", "../../examples/v2-bird2.dbf.hcl"}},
		{name: "plan-nftables-offline", args: []string{"plan", "-f", "../../examples/v2-nftables.dbf.hcl", "--offline"}},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			captureStdout(t, func() {
				if err := run(command.args); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestFmtV2IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`host "web1" {
files{
file "/tmp/example" {
content="hello"
}
}
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	first := captureStdout(t, func() {
		if err := run([]string{"fmt", "-f", config}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(first, "formatted 1 file(s)") {
		t.Fatalf("first fmt output = %q", first)
	}
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`host "web1" {`,
		`file "/tmp/example" {`,
		`content = "hello"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("formatted config does not contain %q:\n%s", want, data)
		}
	}

	second := captureStdout(t, func() {
		if err := run([]string{"fmt", "-f", config}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(second, "formatted 0 file(s)") {
		t.Fatalf("second fmt output = %q", second)
	}
}

func runnableV2Examples() []string {
	return []string{
		"examples/v2-bbr.dbf.hcl",
		"examples/v2-apt-repository.dbf.hcl",
		"examples/v2-bird2.dbf.hcl",
		"examples/v2-component-binary.dbf.hcl",
		"examples/v2-files-plan-preview.dbf.hcl",
		"examples/v2-nftables.dbf.hcl",
		"examples/v2-plan-preview.dbf.hcl",
		"examples/v2-profile-merge.dbf.hcl",
		"examples/v2-systemd-service.dbf.hcl",
		"examples/v2-user-group.dbf.hcl",
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	defer func() {
		os.Stdout = old
	}()

	fn()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutWrite
	os.Stderr = stderrWrite
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()
	if err := stdoutWrite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWrite.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(stdoutRead)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrRead)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr)
}

func assertGolden(t *testing.T, golden string, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
