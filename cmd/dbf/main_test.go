package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/testassert"
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

func TestPlanSensitiveCLIVariableDoesNotLeak(t *testing.T) {
	output, stderr := captureOutput(t, func() {
		if err := run([]string{
			"plan", "-f", "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl", "--offline",
			"-var", "api_token=" + testassert.SensitiveVariableCLIValue,
		}); err != nil {
			t.Fatal(err)
		}
	})
	testassert.NoSecretLeak(t, "sensitive CLI variable plan stdout", output)
	testassert.NoSecretLeak(t, "sensitive CLI variable plan stderr", stderr)
	if !strings.Contains(output, "<sensitive sha256=") {
		t.Fatalf("plan output does not show sensitive summary:\n%s", output)
	}
	if !strings.Contains(output, "+ prod") {
		t.Fatalf("plan output does not show non-sensitive variable content:\n%s", output)
	}
}

func TestPlanCLIVariableRuntimeSources(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "api-token.txt")
	if err := os.WriteFile(secretPath, []byte(testassert.SensitiveVariableCLIValue), 0600); err != nil {
		t.Fatal(err)
	}
	output, stderr := captureOutput(t, func() {
		if err := run([]string{
			"plan", "-f", "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl", "--offline",
			"-var", "api_token=@" + secretPath,
		}); err != nil {
			t.Fatal(err)
		}
	})
	testassert.NoSecretLeak(t, "@path plan stdout", output)
	testassert.NoSecretLeak(t, "@path plan stderr", stderr)
	if strings.Contains(output, secretPath) || strings.Contains(stderr, secretPath) {
		t.Fatalf("sensitive @path leaked source path\nstdout=%q\nstderr=%q", output, stderr)
	}
	if !strings.Contains(output, "<sensitive sha256=") {
		t.Fatalf("@path plan output missing sensitive summary:\n%s", output)
	}

	t.Setenv("DBF_RUNTIME_TOKEN", testassert.SensitiveVariableCLIValue)
	output, stderr = captureOutput(t, func() {
		if err := run([]string{
			"plan", "-f", "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl", "--offline",
			"-var", "api_token=env:DBF_RUNTIME_TOKEN",
		}); err != nil {
			t.Fatal(err)
		}
	})
	testassert.NoSecretLeak(t, "env: plan stdout", output)
	testassert.NoSecretLeak(t, "env: plan stderr", stderr)
	if strings.Contains(output, "DBF_RUNTIME_TOKEN") || strings.Contains(stderr, "DBF_RUNTIME_TOKEN") {
		t.Fatalf("sensitive env source leaked name\nstdout=%q\nstderr=%q", output, stderr)
	}

	t.Setenv("DBF_EMPTY_ENVIRONMENT", "")
	output = captureStdout(t, func() {
		if err := run([]string{
			"plan", "-f", "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl", "--offline",
			"-var", "environment=env:DBF_EMPTY_ENVIRONMENT",
		}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, `summary.sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"`) {
		t.Fatalf("empty env value did not reach non-sensitive file summary:\n%s", output)
	}
}

func TestPlanCLIVariableStdinSource(t *testing.T) {
	oldStdin := os.Stdin
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = read
	defer func() {
		os.Stdin = oldStdin
	}()
	if _, err := write.WriteString(testassert.SensitiveVariableCLIValue); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}

	output, stderr := captureOutput(t, func() {
		if err := run([]string{
			"plan", "-f", "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl", "--offline",
			"-var", "api_token=@-",
		}); err != nil {
			t.Fatal(err)
		}
	})
	testassert.NoSecretLeak(t, "@- plan stdout", output)
	testassert.NoSecretLeak(t, "@- plan stderr", stderr)
	if !strings.Contains(output, "<sensitive sha256=") {
		t.Fatalf("@- plan output missing sensitive summary:\n%s", output)
	}
}

func TestSensitiveCLIVariableSourceErrorsDoNotLeak(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing-token.txt")
	err := run([]string{
		"validate", "-f", "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl",
		"-var", "api_token=@" + missingPath,
	})
	if err == nil {
		t.Fatal("validate missing sensitive @path succeeded")
	}
	if strings.Contains(err.Error(), missingPath) {
		t.Fatalf("missing sensitive @path leaked source path: %v", err)
	}
	if !strings.Contains(err.Error(), "<sensitive-source>") {
		t.Fatalf("missing sensitive @path error = %v, want redacted source", err)
	}

	err = run([]string{
		"validate", "-f", "../../internal/v2/testdata/fixtures/v2-sensitive-variable-files.dbf.hcl",
		"-var", "api_token=env:DBF_MISSING_RUNTIME_TOKEN",
	})
	if err == nil {
		t.Fatal("validate missing sensitive env source succeeded")
	}
	if strings.Contains(err.Error(), "DBF_MISSING_RUNTIME_TOKEN") {
		t.Fatalf("missing sensitive env source leaked name: %v", err)
	}
	if !strings.Contains(err.Error(), "<sensitive-source>") {
		t.Fatalf("missing sensitive env source error = %v, want redacted source", err)
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

func TestValidateRejectsVariableValidationFailure(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
variable "environment" {
  type    = string
  default = "prod"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

host "server1" {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"validate", "-f", config, "-var", "environment=qa"})
	if err == nil || !strings.Contains(err.Error(), `validation failed for variable "environment"`) {
		t.Fatalf("validate variable validation error = %v, want validation failure", err)
	}
}

func TestValidatePrintsDeprecatedVariableWarningsOnlyForExplicitValues(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
variable "environment" {
  type       = string
  default    = "prod"
  deprecated = "Use deployment_environment instead."
}

host "server1" {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	_, defaultStderr := captureOutput(t, func() {
		if err := run([]string{"validate", "-f", config}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(defaultStderr, "deprecated") {
		t.Fatalf("default-only validate stderr = %q, want no deprecated warning", defaultStderr)
	}

	_, explicitStderr := captureOutput(t, func() {
		if err := run([]string{"validate", "-f", config, "-var", "environment=staging"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(explicitStderr, `variable "environment" is deprecated`) {
		t.Fatalf("explicit validate stderr = %q, want deprecated warning", explicitStderr)
	}
}

func TestPlanAcceptsVarFilesAutoVarFilesAndEnv(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
variable "environment" {
  type    = string
  default = "default"
}

variable "ports" {
  type    = list(number)
  default = []
}

variable "labels" {
  type = object({
    tier   = string
    source = string
  })
  default = {
    tier   = "backend"
    source = "default"
  }
}

host "server1" {
  files {
    file "/etc/debianform/vars.json" {
      content = jsonencode({
        environment = var.environment
        ports       = var.ports
        labels      = var.labels
      })
    }
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "debianform.dbfvars"), []byte(`
environment = "default-file"
labels = {
  tier   = "base"
  source = "default-file"
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "10.auto.dbfvars"), []byte(`
environment = "auto-a"
ports       = [80]
labels = {
  tier   = "auto-a"
  source = "auto-a"
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20.auto.dbfvars.json"), []byte(`{
  "environment": "auto-b",
  "ports": [8080],
  "labels": {
    "tier": "auto-b",
    "source": "auto-b"
  }
}`), 0644); err != nil {
		t.Fatal(err)
	}
	explicitA := filepath.Join(dir, "explicit-a.dbfvars")
	if err := os.WriteFile(explicitA, []byte(`
environment = "file-a"
ports       = [9000]
labels = {
  tier   = "file-a"
  source = "file-a"
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	explicitB := filepath.Join(dir, "explicit-b.dbfvars.json")
	if err := os.WriteFile(explicitB, []byte(`{
  "environment": "file-b",
  "ports": [9001, 9002],
  "labels": {
    "tier": "file-b",
    "source": "file-b"
  }
}`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DBF_VAR_environment", "env")
	t.Setenv("DBF_VAR_unknown_from_env", "ignored")
	output := captureStdout(t, func() {
		if err := run([]string{
			"plan", "-f", config, "--offline",
			"-var-file", explicitA,
			"-var-file", explicitB,
			"-var", "environment=cli",
		}); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{
		`"environment":"cli"`,
		`"ports":[9001,9002]`,
		`"labels":{"source":"file-b","tier":"file-b"}`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("plan output %q does not contain %q", output, want)
		}
	}
	for _, notWant := range []string{
		`"environment":"env"`,
		`"environment":"default-file"`,
		`"environment":"auto-a"`,
		`"environment":"auto-b"`,
		`"environment":"file-a"`,
	} {
		if strings.Contains(output, notWant) {
			t.Fatalf("plan output contains lower-priority value %q: %q", notWant, output)
		}
	}
}

func TestVarFileUnknownVariableErrors(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
variable "environment" {
  type    = string
  default = "dev"
}

host "server1" {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	varFile := filepath.Join(dir, "bad.dbfvars")
	if err := os.WriteFile(varFile, []byte(`missing = "value"`), 0644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"validate", "-f", config, "-var-file", varFile})
	if err == nil || !strings.Contains(err.Error(), `unknown variable "missing"`) {
		t.Fatalf("validate unknown var-file error = %v, want unknown variable", err)
	}
}

func TestAutoVariableFilesOrdering(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"main.dbf.hcl",
		"debianform.dbfvars",
		"debianform.dbfvars.json",
		"20.auto.dbfvars",
		"10.auto.dbfvars.json",
		"notes.dbfvars",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := autoVariableFiles([]string{filepath.Join(dir, "main.dbf.hcl")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "debianform.dbfvars"),
		filepath.Join(dir, "debianform.dbfvars.json"),
		filepath.Join(dir, "10.auto.dbfvars.json"),
		filepath.Join(dir, "20.auto.dbfvars"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("autoVariableFiles() = %#v, want %#v", got, want)
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

func TestValidateAndPlanPrintSecretFileDeprecationWarnings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte("not-a-real-secret-token"), 0644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
host "server1" {
  secrets {
    file "/etc/app/token" {
      source = "token.txt"
    }
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	_, validateStderr := captureOutput(t, func() {
		if err := run([]string{"validate", "-f", config}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(validateStderr, "warning:") || !strings.Contains(validateStderr, "secrets.file is deprecated") {
		t.Fatalf("validate stderr = %q, want secrets.file deprecation warning", validateStderr)
	}

	planStdout, planStderr := captureOutput(t, func() {
		if err := run([]string{"plan", "-f", config, "--offline"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(planStderr, "warning:") || !strings.Contains(planStderr, "secrets.file is deprecated") {
		t.Fatalf("plan stderr = %q, want secrets.file deprecation warning", planStderr)
	}
	if strings.Contains(planStdout, "warning:") {
		t.Fatalf("plan stdout contains warning: %q", planStdout)
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

func TestVariableInspect(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(config, []byte(`
variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "prod"
  nullable    = false
  deprecated  = "Use deployment_environment instead."

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be valid"
  }
}

variable "labels" {
  type = object({
    tier   = string
    canary = optional(bool, false)
  })
  default = {
    tier = "backend"
  }
}

variable "token" {
  type      = string
  default   = "secret-token"
  sensitive = true
}

variable "runtime_value" {
  type      = string
  ephemeral = true
}

host "server1" {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"variable", "inspect", "-f", config}); err != nil {
			t.Fatal(err)
		}
	})
	assertGolden(t, filepath.Join("testdata", "variable-inspect.golden.json"), output)
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
		{name: "plan-variable-secret-file-offline", args: []string{"plan", "-f", "../../examples/v2-variable-secret-file.dbf.hcl", "--offline"}},
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
		"examples/v2-variable-secret-file.dbf.hcl",
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
