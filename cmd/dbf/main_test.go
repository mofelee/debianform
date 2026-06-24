package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	coreir "github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/termstyle"
	"github.com/mofelee/debianform/internal/core/testassert"
)

func TestConfigFilesLoadsAllDBFHCLInCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, name := range []string{"20-app.dbf.hcl", "10-base.dbf.hcl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := configFiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10-base.dbf.hcl", "20-app.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("configFiles() = %#v, want %#v", files, want)
	}
}

func TestConfigFilesWithExplicitFile(t *testing.T) {
	files, err := configFiles([]string{"custom.dbf.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"custom.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("configFiles() = %#v, want %#v", files, want)
	}
}

func TestConfigFilesWithExplicitFiles(t *testing.T) {
	input := []string{"base.dbf.hcl", "app.dbf.hcl"}
	files, err := configFiles(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = "mutated.dbf.hcl"

	want := []string{"base.dbf.hcl", "app.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("configFiles() = %#v, want %#v", files, want)
	}
}

func TestFileFlagsPreserveSetOrder(t *testing.T) {
	var files fileFlags
	if err := files.Set("base.dbf.hcl"); err != nil {
		t.Fatal(err)
	}
	if err := files.Set("app.dbf.hcl"); err != nil {
		t.Fatal(err)
	}
	want := fileFlags{"base.dbf.hcl", "app.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("fileFlags = %#v, want %#v", files, want)
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

func TestHelpDocumentsImplementedFlags(t *testing.T) {
	for _, command := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		command := command
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			output := captureStdout(t, func() {
				if err := run(command); err != nil {
					t.Fatal(err)
				}
			})
			for _, want := range []string{
				"dbf validate [-f file ...] [-var name=value] [-var-file path] [--host name]",
				"dbf plan     [-f file ...] [-var name=value] [-var-file path] [--host name] [--format text|json] [--html file] [--debug] [--color auto|always|never] [--offline]",
				"dbf apply    [-f file ...] [-var name=value] [-var-file path] [--host name] [--color auto|always|never] [--parallel n] [--lock-timeout duration] [--auto-approve]",
				"dbf check    [-f file ...] [-var name=value] [-var-file path] [--host name] [--color auto|always|never] [--lock-timeout duration]",
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("help output does not contain %q:\n%s", want, output)
				}
			}
		})
	}
}

func TestValidateAcceptsRepeatedConfigFiles(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.dbf.hcl")
	app := filepath.Join(dir, "app.dbf.hcl")
	writeTestFile(t, base, `
profile "base" {
  packages {
    install = ["curl"]
  }
}
`)
	writeTestFile(t, app, `
host "server1" {
  imports = [profile.base]
}
`)

	output := captureStdout(t, func() {
		if err := run([]string{"validate", "-f", base, "-f", app}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "configuration is valid: 1 host(s)") {
		t.Fatalf("validate output = %q", output)
	}
}

func TestPlanRepeatedConfigFilesCommandMetadata(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.dbf.hcl")
	app := filepath.Join(dir, "app.dbf.hcl")
	writeTestFile(t, base, `
profile "base" {
  packages {
    install = ["curl"]
  }
}
`)
	writeTestFile(t, app, `
host "server1" {
  imports = [profile.base]
}
`)

	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", base, "-f", app, "--offline", "--format", "json"}); err != nil {
			t.Fatal(err)
		}
	})
	var doc struct {
		Command struct {
			File string `json:"file"`
		} `json:"command"`
	}
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("plan JSON did not parse: %v\n%s", err, output)
	}
	want := base + "," + app
	if doc.Command.File != want {
		t.Fatalf("command.file = %q, want %q", doc.Command.File, want)
	}
}

func TestPlanColorFlagControlsTextOutput(t *testing.T) {
	fixture := "../../examples/bbr.dbf.hcl"

	colored := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", fixture, "--offline", "--color", "always"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"\x1b[1m\x1b[30m\x1b[42m CREATE \x1b[0m",
		"\x1b[1m\x1b[36mhost.bbr1.kernel.module[\"tcp_bbr\"]\x1b[0m",
		"\x1b[1m\x1b[30m\x1b[42m 3 create \x1b[0m",
	} {
		if !strings.Contains(colored, want) {
			t.Fatalf("--color always output missing %q:\n%q", want, colored)
		}
	}

	plain := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", fixture, "--offline", "--color", "never"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("--color never output contains ANSI:\n%q", plain)
	}

	t.Setenv("NO_COLOR", "1")
	autoNoColor := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", fixture, "--offline"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(autoNoColor, "\x1b[") {
		t.Fatalf("NO_COLOR auto output contains ANSI:\n%q", autoNoColor)
	}

	jsonOut := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", fixture, "--offline", "--format", "json", "--color", "always"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(jsonOut, "\x1b[") {
		t.Fatalf("JSON output contains ANSI despite --color always:\n%q", jsonOut)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("colored JSON plan did not parse: %v\n%s", err, jsonOut)
	}
}

func TestPlanRejectsUnsupportedColorMode(t *testing.T) {
	err := run([]string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--offline", "--color", "sometimes"})
	if err == nil || !strings.Contains(err.Error(), "unsupported --color value") {
		t.Fatalf("plan --color error = %v, want unsupported color mode", err)
	}
}

func TestValidateRejectsColorFlag(t *testing.T) {
	err := run([]string{"validate", "-f", "../../examples/bbr.dbf.hcl", "--color", "never"})
	if err == nil || !strings.Contains(err.Error(), "--color is only supported for plan, apply, and check") {
		t.Fatalf("validate --color error = %v, want unsupported color flag", err)
	}
}

func TestPrintWarningsWithStyleKeepsWarningTextAndAddsBadge(t *testing.T) {
	_, stderr := captureOutput(t, func() {
		printWarningsWithStyle([]coreir.Warning{{Message: "deprecated input"}}, termstyle.Options{Color: true, Background: true})
	})
	for _, want := range []string{
		"\x1b[1m\x1b[30m\x1b[43m WARNING \x1b[0m",
		"warning: deprecated input",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("styled warning output missing %q:\n%q", want, stderr)
		}
	}
}

func TestValidateRunnableExamples(t *testing.T) {
	for _, example := range runnableExamples() {
		t.Run(filepath.Base(example), func(t *testing.T) {
			output := captureStdout(t, func() {
				if err := run([]string{"validate", "-f", "../../" + example}); err != nil {
					t.Fatal(err)
				}
			})

			if !strings.Contains(output, "configuration is valid: 1 host(s)") {
				t.Fatalf("validate output = %q", output)
			}
		})
	}
}

func TestValidateStillRejectsComponentInputErrors(t *testing.T) {
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
		if err := run([]string{"validate", "-f", "../../internal/core/testdata/fixtures/variable-declarations.dbf.hcl"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "configuration is valid: 1 host(s)") {
		t.Fatalf("validate output = %q", output)
	}
}

func TestValidateAndPlanAcceptVariableDefaults(t *testing.T) {
	validateOutput := captureStdout(t, func() {
		if err := run([]string{"validate", "-f", "../../internal/core/testdata/fixtures/variable-defaults.dbf.hcl"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(validateOutput, "configuration is valid: 1 host(s)") {
		t.Fatalf("validate output = %q", validateOutput)
	}

	planOutput := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../internal/core/testdata/fixtures/variable-defaults.dbf.hcl", "--offline"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(planOutput, "hello from variable default") {
		t.Fatalf("plan output = %q", planOutput)
	}
}

func TestValidateAndPlanAcceptCLIVariableValues(t *testing.T) {
	fixture := "../../internal/core/testdata/fixtures/variable-cli.dbf.hcl"
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
	if !strings.Contains(validateOutput, "configuration is valid: 1 host(s)") {
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
			"plan", "-f", "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl", "--offline",
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
			"plan", "-f", "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl", "--offline",
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
			"plan", "-f", "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl", "--offline",
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
			"plan", "-f", "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl", "--offline",
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
			"plan", "-f", "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl", "--offline",
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
		"validate", "-f", "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl",
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
		"validate", "-f", "../../internal/core/testdata/fixtures/sensitive-variable-files.dbf.hcl",
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
	fixture := "../../internal/core/testdata/fixtures/variable-cli.dbf.hcl"

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
	if doc.FormatVersion != "debianform.plan.alpha1" {
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

func TestComponentInspectAcceptsRepeatedConfigFiles(t *testing.T) {
	dir := t.TempDir()
	components := filepath.Join(dir, "components.dbf.hcl")
	hosts := filepath.Join(dir, "hosts.dbf.hcl")
	writeTestFile(t, components, `
component "proxy" {
  input "port" {
    type    = number
    default = 8080
  }
}
`)
	writeTestFile(t, hosts, `
host "server1" {}
`)

	output := captureStdout(t, func() {
		if err := run([]string{"component", "inspect", "-f", components, "-f", hosts, "proxy"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, `"name": "proxy"`) || !strings.Contains(output, `"name": "port"`) {
		t.Fatalf("component inspect output = %q", output)
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

func TestPlanBBRText(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--offline"}); err != nil {
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

func TestPlanNftablesText(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/nftables.dbf.hcl", "--offline"}); err != nil {
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

func TestPlanBBRJSON(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--format", "json", "--offline"}); err != nil {
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
	if doc.FormatVersion != "debianform.plan.alpha1" {
		t.Fatalf("format_version = %q", doc.FormatVersion)
	}
	if doc.Summary.Create != 3 {
		t.Fatalf("summary.create = %d, want 3", doc.Summary.Create)
	}
}

func TestPlanBBRJSONDebug(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--format", "json", "--debug", "--offline"}); err != nil {
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
	err := run([]string{"plan", "-f", "../../examples/bird2.dbf.hcl", "--offline"})
	if err == nil || !strings.Contains(err.Error(), "offline plan cannot resolve runtime facts") {
		t.Fatalf("plan --offline error = %v, want runtime facts explanation", err)
	}
}

func TestOfflinePlanExplainsDockerRuntimeFactsDependency(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "docker.dbf.hcl")
	if err := os.WriteFile(file, []byte(`
host "docker1" {
  docker {
    enable = true
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"plan", "-f", file, "--offline"})
	if err == nil || !strings.Contains(err.Error(), "offline plan cannot resolve runtime facts") {
		t.Fatalf("docker plan --offline error = %v, want runtime facts explanation", err)
	}
	if !strings.Contains(err.Error(), "must declare system.architecture") {
		t.Fatalf("docker plan --offline error = %v, want missing architecture detail", err)
	}
}

func TestCheckDockerServiceDriftReturnsError(t *testing.T) {
	fakeBin := t.TempDir()
	sshPath := filepath.Join(fakeBin, "ssh")
	script := `#!/bin/sh
input="$(cat)"
case "$input" in
  *"printf 'hostname=%s\n'"*)
    printf 'hostname=docker1\narchitecture=amd64\ncodename=trixie\n'
    exit 0
    ;;
  *"/var/lib/debianform/state/docker1.json"*)
    exit 0
    ;;
  *"/etc/apt/keyrings/docker.asc"*)
    printf 'file\nroot\nroot\n644\n1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570\n'
    exit 0
    ;;
  *"/etc/apt/sources.list.d/docker_official.sources"*)
    printf 'file\nroot\nroot\n644\n61246a32aa079ddf3d8a57d922a7fc311d82f23379bfe0bec65f7c685c141e97\n'
    exit 0
    ;;
  *"dpkg-query"*"docker.io"*"podman-docker"*"runc"*)
    exit 0
    ;;
  *"dpkg-query"*"docker-ce"*|*"dpkg-query"*"docker-ce-cli"*|*"dpkg-query"*"containerd.io"*|*"dpkg-query"*"docker-buildx-plugin"*|*"dpkg-query"*"docker-compose-plugin"*)
    printf 'install ok installed\t1.0\n'
    exit 0
    ;;
  *"systemctl is-enabled 'docker.service'"*)
    printf 'enabled=disabled\nactive=inactive\n'
    exit 0
    ;;
esac
printf 'unexpected fake ssh input:\n%s\n' "$input" >&2
exit 1
`
	if err := os.WriteFile(sshPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	output := captureStdout(t, func() {
		err := run([]string{"check", "-f", "../../examples/docker-minimal.dbf.hcl"})
		if err == nil || !strings.Contains(err.Error(), "remote state does not match configuration") {
			t.Fatalf("docker check error = %v, want drift failure", err)
		}
	})
	if !strings.Contains(output, `host.docker1.docker.service["docker"]`) ||
		!strings.Contains(output, "enable start service docker.service") {
		t.Fatalf("docker check output missing service drift:\n%s", output)
	}
}

func TestCheckDockerComposeProjectStoppedDriftReturnsError(t *testing.T) {
	fakeBin := t.TempDir()
	sshPath := filepath.Join(fakeBin, "ssh")
	script := `#!/bin/sh
input="$(cat)"
case "$input" in
  *"printf 'hostname=%s\n'"*)
    printf 'hostname=compose1\narchitecture=amd64\ncodename=trixie\n'
    exit 0
    ;;
  *"/var/lib/debianform/state/compose1.json"*)
    exit 0
    ;;
  *"/etc/apt/keyrings/docker.asc"*)
    printf 'file\nroot\nroot\n644\n1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570\n'
    exit 0
    ;;
  *"/etc/apt/sources.list.d/docker_official.sources"*)
    printf 'file\nroot\nroot\n644\n61246a32aa079ddf3d8a57d922a7fc311d82f23379bfe0bec65f7c685c141e97\n'
    exit 0
    ;;
  *"dpkg-query"*"docker.io"*"podman-docker"*"runc"*)
    exit 0
    ;;
  *"dpkg-query"*"docker-ce"*|*"dpkg-query"*"docker-ce-cli"*|*"dpkg-query"*"containerd.io"*|*"dpkg-query"*"docker-buildx-plugin"*|*"dpkg-query"*"docker-compose-plugin"*)
    printf 'install ok installed\t1.0\n'
    exit 0
    ;;
  *"systemctl is-enabled 'docker.service'"*)
    printf 'enabled=enabled\nactive=active\n'
    exit 0
    ;;
  *"systemctl is-enabled 'debianform-compose-app.service'"*)
    printf 'enabled=enabled\nactive=active\n'
    exit 0
    ;;
  *"/etc/systemd/system/debianform-compose-app.service"*)
    printf 'file\nroot\nroot\n644\n6cd6cbb3f51b6cd295517fa585d8573a31c5d55879472513038c5329b490072a\n'
    exit 0
    ;;
  *"'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'config' '--services'"*)
    printf 'web\n'
    exit 0
    ;;
  *"'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'ps' '--all' '--format' 'json'"*)
    printf '[{"Name":"web","State":"exited"}]\n'
    exit 0
    ;;
  *"/opt/app/compose.yaml"*)
    printf 'file\nroot\nroot\n644\nd1f744f61ab84b59402583765d65b4d6b984fd44c128388ac3ea0a07ef123d0f\n'
    exit 0
    ;;
  *"/opt/app/.env"*)
    printf 'file\nroot\nroot\n600\n13960174065f5092768d3f5b11c1acad09272142febb757484b9709779b52588\n'
    exit 0
    ;;
  *"/opt/app"*)
    printf 'dir\nroot\nroot\n755\n\n'
    exit 0
    ;;
esac
printf 'unexpected fake ssh input:\n%s\n' "$input" >&2
exit 1
`
	if err := os.WriteFile(sshPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	output := captureStdout(t, func() {
		err := run([]string{"check", "-f", "../../examples/docker-compose.dbf.hcl"})
		if err == nil || !strings.Contains(err.Error(), "remote state does not match configuration") {
			t.Fatalf("docker compose check error = %v, want drift failure", err)
		}
	})
	if !strings.Contains(output, `host.compose1.docker.compose["app"].project`) ||
		!strings.Contains(output, "converge docker compose project app from stopped to running") {
		t.Fatalf("docker compose check output missing project drift:\n%s", output)
	}
	if strings.Contains(output, "not-a-real-preview-secret") {
		t.Fatalf("docker compose check output leaked env file content:\n%s", output)
	}
}

func TestCheckDockerComposeProjectOrphanDriftReturnsError(t *testing.T) {
	fakeBin := t.TempDir()
	sshPath := filepath.Join(fakeBin, "ssh")
	script := `#!/bin/sh
input="$(cat)"
case "$input" in
  *"printf 'hostname=%s\n'"*)
    printf 'hostname=compose1\narchitecture=amd64\ncodename=trixie\n'
    exit 0
    ;;
  *"/var/lib/debianform/state/compose1.json"*)
    exit 0
    ;;
  *"/etc/apt/keyrings/docker.asc"*)
    printf 'file\nroot\nroot\n644\n1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570\n'
    exit 0
    ;;
  *"/etc/apt/sources.list.d/docker_official.sources"*)
    printf 'file\nroot\nroot\n644\n61246a32aa079ddf3d8a57d922a7fc311d82f23379bfe0bec65f7c685c141e97\n'
    exit 0
    ;;
  *"dpkg-query"*"docker.io"*"podman-docker"*"runc"*)
    exit 0
    ;;
  *"dpkg-query"*"docker-ce"*|*"dpkg-query"*"docker-ce-cli"*|*"dpkg-query"*"containerd.io"*|*"dpkg-query"*"docker-buildx-plugin"*|*"dpkg-query"*"docker-compose-plugin"*)
    printf 'install ok installed\t1.0\n'
    exit 0
    ;;
  *"systemctl is-enabled 'docker.service'"*)
    printf 'enabled=enabled\nactive=active\n'
    exit 0
    ;;
  *"systemctl is-enabled 'debianform-compose-app.service'"*)
    printf 'enabled=enabled\nactive=active\n'
    exit 0
    ;;
  *"/etc/systemd/system/debianform-compose-app.service"*)
    printf 'file\nroot\nroot\n644\n6cd6cbb3f51b6cd295517fa585d8573a31c5d55879472513038c5329b490072a\n'
    exit 0
    ;;
  *"'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'config' '--services'"*)
    printf 'web\n'
    exit 0
    ;;
  *"'docker' 'compose' '-p' 'app' '-f' '/opt/app/compose.yaml' 'ps' '--all' '--format' 'json'"*)
    printf '[{"Name":"web","Service":"web","State":"running"},{"Name":"worker","Service":"worker","State":"running"}]\n'
    exit 0
    ;;
  *"/opt/app/compose.yaml"*)
    printf 'file\nroot\nroot\n644\nd1f744f61ab84b59402583765d65b4d6b984fd44c128388ac3ea0a07ef123d0f\n'
    exit 0
    ;;
  *"/opt/app/.env"*)
    printf 'file\nroot\nroot\n600\n13960174065f5092768d3f5b11c1acad09272142febb757484b9709779b52588\n'
    exit 0
    ;;
  *"/opt/app"*)
    printf 'dir\nroot\nroot\n755\n\n'
    exit 0
    ;;
esac
printf 'unexpected fake ssh input:\n%s\n' "$input" >&2
exit 1
`
	if err := os.WriteFile(sshPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	output := captureStdout(t, func() {
		err := run([]string{"check", "-f", "../../examples/docker-compose.dbf.hcl"})
		if err == nil || !strings.Contains(err.Error(), "remote state does not match configuration") {
			t.Fatalf("docker compose orphan check error = %v, want drift failure", err)
		}
	})
	if !strings.Contains(output, "orphan service") ||
		!strings.Contains(output, "remove_orphans = true") {
		t.Fatalf("docker compose check output missing orphan drift:\n%s", output)
	}
}

func TestParallelFlagIsApplyOnlyAndPositive(t *testing.T) {
	err := run([]string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--parallel", "2"})
	if err == nil || !strings.Contains(err.Error(), "--parallel is only supported for apply") {
		t.Fatalf("plan --parallel error = %v", err)
	}

	err = run([]string{"check", "-f", "../../examples/bbr.dbf.hcl", "--offline"})
	if err == nil || !strings.Contains(err.Error(), "--offline is only supported for plan") {
		t.Fatalf("check --offline error = %v", err)
	}

	err = run([]string{"apply", "-f", "../../examples/bbr.dbf.hcl", "--parallel", "0", "--auto-approve"})
	if err == nil || !strings.Contains(err.Error(), "--parallel must be at least 1") {
		t.Fatalf("apply --parallel 0 error = %v", err)
	}
}

func TestPlanBBRHTML(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "plan.html")
	output := captureStdout(t, func() {
		if err := run([]string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--html", htmlPath, "--offline"}); err != nil {
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
	for _, example := range runnableExamples() {
		if !strings.Contains(readmeText, example) {
			t.Fatalf("README does not mention runnable example %s", example)
		}
	}
	if strings.Contains(readmeText, "dbf plan -f examples/bird2.dbf.hcl\n") {
		t.Fatal("README contains a copy-runnable-looking online BIRD2 plan command")
	}

	dir := t.TempDir()
	fmtFixture := filepath.Join(dir, "bbr.dbf.hcl")
	data, err := os.ReadFile("../../examples/bbr.dbf.hcl")
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
		{name: "validate-bbr", args: []string{"validate", "-f", "../../examples/bbr.dbf.hcl"}},
		{name: "validate-vars", args: []string{
			"validate", "-f", "../../internal/core/testdata/fixtures/variable-cli.dbf.hcl",
			"-var-file", "../../internal/core/testdata/fixtures/variable-prod.dbfvars",
			"-var", "environment=staging",
		}},
		{name: "validate-realistic-systemd-app", args: []string{"validate", "-f", "../../examples/realistic-systemd-app.dbf.hcl"}},
		{name: "plan-realistic-systemd-app-offline", args: []string{"plan", "-f", "../../examples/realistic-systemd-app.dbf.hcl", "--offline"}},
		{name: "plan-bbr-offline", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--offline"}},
		{name: "plan-bbr-json", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--format", "json", "--offline"}},
		{name: "plan-bbr-debug-json", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--format", "json", "--debug", "--offline"}},
		{name: "plan-files-html", args: []string{"plan", "-f", "../../examples/files-plan-preview.dbf.hcl", "--html", filepath.Join(dir, "plan.html"), "--offline"}},
		{name: "fmt-bbr-copy", args: []string{"fmt", "-f", fmtFixture}},
		{name: "validate-bird2", args: []string{"validate", "-f", "../../examples/bird2.dbf.hcl"}},
		{name: "plan-nftables-offline", args: []string{"plan", "-f", "../../examples/nftables.dbf.hcl", "--offline"}},
		{name: "plan-variable-secret-file-offline", args: []string{"plan", "-f", "../../examples/variable-secret-file.dbf.hcl", "--offline"}},
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

func TestDSLReferenceMarkedExamplesAreRunnable(t *testing.T) {
	doc, err := os.ReadFile("../../docs/dsl-reference.zh.md")
	if err != nil {
		t.Fatal(err)
	}
	examples := extractDBFDocExamples(t, string(doc))
	if len(examples) == 0 {
		t.Fatal("no dbf-test examples found in docs/dsl-reference.zh.md")
	}
	for _, example := range examples {
		t.Run(example.Name, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "site.dbf.hcl")
			writeTestFile(t, config, example.HCL)
			for _, fixture := range example.Files {
				switch fixture {
				case "token.txt":
					writeTestFile(t, filepath.Join(dir, fixture), "not-a-real-secret-token\n")
				case "local-source.txt":
					writeTestFile(t, filepath.Join(dir, fixture), "local source file\n")
				case "template.txt":
					writeTestFile(t, filepath.Join(dir, fixture), "message=${message}\n")
				default:
					t.Fatalf("unknown doc fixture %q", fixture)
				}
			}
			for _, command := range example.Commands {
				args := []string{}
				switch command {
				case "validate":
					args = []string{"validate", "-f", config}
				case "plan-offline":
					args = []string{"plan", "-f", config, "--offline"}
				case "variable-inspect":
					args = []string{"variable", "inspect", "-f", config}
				default:
					if componentName, ok := strings.CutPrefix(command, "component-inspect:"); ok && componentName != "" {
						args = []string{"component", "inspect", "-f", config, componentName}
					} else {
						t.Fatalf("unknown doc example command %q", command)
					}
				}
				captureOutput(t, func() {
					if err := run(args); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestUserDocsHCLExamplesAreRunnable(t *testing.T) {
	docs := []string{
		"quickstart.zh.md",
		"user-manual/01-first-apply.zh.md",
		"user-manual/02-files-and-drift.zh.md",
		"user-manual/03-users-and-ssh-keys.zh.md",
		"user-manual/04-apt-and-packages.zh.md",
		"user-manual/05-systemd-service.zh.md",
		"user-manual/06-kernel-and-sysctl.zh.md",
		"user-manual/07-nftables.zh.md",
		"user-manual/08-docker-engine.zh.md",
		"user-manual/09-docker-compose.zh.md",
		"user-manual/11-components.zh.md",
		"user-manual/12-operations.zh.md",
	}
	for _, docPath := range docs {
		t.Run(docPath, func(t *testing.T) {
			config := firstRunnableHCLBlock(t, "../../docs/"+docPath)
			runDocConfigLocalChecks(t, config)
		})
	}
}

func TestUserDocsVariableExamplesAreRunnable(t *testing.T) {
	blocks := hclCodeBlocksFromFile(t, "../../docs/user-manual/10-profiles-and-variables.zh.md")
	if len(blocks) < 3 {
		t.Fatalf("profiles-and-variables doc has %d hcl blocks, want at least 3", len(blocks))
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "site.dbf.hcl")
	devVars := filepath.Join(dir, "dev.dbfvars")
	prodVars := filepath.Join(dir, "prod.dbfvars")
	writeTestFile(t, config, blocks[0])
	writeTestFile(t, devVars, blocks[1])
	writeTestFile(t, prodVars, blocks[2])

	commands := [][]string{
		{"validate", "-f", config},
		{"plan", "-f", config, "--offline"},
		{"variable", "inspect", "-f", config, "-var-file", prodVars},
		{"plan", "-f", config, "--offline", "-var-file", devVars},
		{"plan", "-f", config, "--offline", "-var-file", prodVars},
		{"validate", "-f", config, "-var-file", prodVars},
		{"plan", "-f", config, "--offline", "-var-file", prodVars, "-var", "owner=release-team"},
	}
	for _, args := range commands {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			captureOutput(t, func() {
				if err := run(args); err != nil {
					t.Fatal(err)
				}
			})
		})
	}

	err := run([]string{"validate", "-f", config, "-var", "environment=qa"})
	if err == nil || !strings.Contains(err.Error(), `validation failed for variable "environment"`) {
		t.Fatalf("invalid environment validate error = %v, want validation failure", err)
	}
}

func TestReferenceDocsHCLExamplesAreRunnable(t *testing.T) {
	docs := []struct {
		path       string
		wrapDomain bool
	}{
		{path: "../../docs/state.md"},
		{path: "../../docs/operations-runbook.zh.md"},
		{path: "../../docs/systemd-service-units.md", wrapDomain: true},
	}
	for _, doc := range docs {
		blocks := hclCodeBlocksFromFile(t, doc.path)
		if len(blocks) == 0 {
			t.Fatalf("%s has no hcl blocks", doc.path)
		}
		for i, block := range blocks {
			t.Run(filepath.Base(doc.path)+"/block-"+strconv.Itoa(i), func(t *testing.T) {
				if doc.wrapDomain {
					block = "host \"doc_example\" {\n" + indentHCL(block, "  ") + "}\n"
				}
				runDocConfigLocalChecks(t, block)
			})
		}
	}
}

func TestCLIDocLocalCommandExamplesAreRunnable(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.dbf.hcl")
	app := filepath.Join(dir, "app.dbf.hcl")
	fmtFixture := filepath.Join(dir, "fmt.dbf.hcl")
	writeTestFile(t, base, `
profile "base" {
  packages {
    install = ["curl"]
  }
}
`)
	writeTestFile(t, app, `
host "app" {
  imports = [profile.base]
}
`)
	writeTestFile(t, fmtFixture, `
host "fmt1" {
files {
file "/tmp/fmt" {
content = "fmt"
}
}
}
`)

	htmlPath := filepath.Join(dir, "plan.html")
	commands := []struct {
		name string
		args []string
	}{
		{name: "validate-explicit", args: []string{"validate", "-f", "../../examples/bbr.dbf.hcl"}},
		{name: "validate-repeated", args: []string{"validate", "-f", base, "-f", app}},
		{name: "validate-host", args: []string{"validate", "-f", "../../examples/bird2.dbf.hcl", "--host", "router1"}},
		{name: "validate-vars", args: []string{
			"validate", "-f", "../../internal/core/testdata/fixtures/variable-cli.dbf.hcl",
			"-var-file", "../../internal/core/testdata/fixtures/variable-prod.dbfvars",
			"-var", "environment=staging",
		}},
		{name: "plan-offline", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--offline"}},
		{name: "plan-json", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--format", "json", "--offline"}},
		{name: "plan-html", args: []string{"plan", "-f", "../../examples/files-plan-preview.dbf.hcl", "--html", htmlPath, "--offline"}},
		{name: "plan-debug-json", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--format", "json", "--debug", "--offline"}},
		{name: "fmt-explicit", args: []string{"fmt", "-f", fmtFixture}},
		{name: "component-inspect", args: []string{"component", "inspect", "-f", "../../examples/component-inputs.dbf.hcl", "reverse_proxy"}},
		{name: "variable-inspect", args: []string{"variable", "inspect", "-f", "../../examples/variable-secret-file.dbf.hcl"}},
		{name: "version", args: []string{"version"}},
		{name: "version-flag", args: []string{"--version"}},
		{name: "version-short-flag", args: []string{"-version"}},
		{name: "help", args: []string{"help"}},
		{name: "help-flag", args: []string{"--help"}},
		{name: "help-short-flag", args: []string{"-h"}},
	}
	for _, command := range commands {
		command := command
		t.Run(command.name, func(t *testing.T) {
			captureOutput(t, func() {
				if err := run(command.args); err != nil {
					t.Fatal(err)
				}
			})
		})
	}

	invalidCommands := []struct {
		name string
		args []string
		want string
	}{
		{name: "plan-parallel", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--parallel", "2"}, want: "--parallel is only supported for apply"},
		{name: "check-offline", args: []string{"check", "-f", "../../examples/bbr.dbf.hcl", "--offline"}, want: "--offline is only supported for plan"},
		{name: "html-json", args: []string{"plan", "-f", "../../examples/bbr.dbf.hcl", "--html", filepath.Join(dir, "bad.html"), "--format", "json"}, want: "--html cannot be combined with --format"},
	}
	for _, command := range invalidCommands {
		command := command
		t.Run(command.name, func(t *testing.T) {
			err := run(command.args)
			if err == nil || !strings.Contains(err.Error(), command.want) {
				t.Fatalf("run(%v) error = %v, want %q", command.args, err, command.want)
			}
		})
	}
}

func TestFmtIsIdempotent(t *testing.T) {
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

func TestFmtRepeatedConfigFilesFormatsOnlyExplicitFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.dbf.hcl")
	b := filepath.Join(dir, "b.dbf.hcl")
	c := filepath.Join(dir, "c.dbf.hcl")
	rawA := `host "a" {
files {
file "/tmp/a" {
content="a"
}
}
}
`
	rawB := `host "b" {
files {
file "/tmp/b" {
content="b"
}
}
}
`
	rawC := `host "c" {
files {
file "/tmp/c" {
content="c"
}
}
}
`
	writeTestFile(t, a, rawA)
	writeTestFile(t, b, rawB)
	writeTestFile(t, c, rawC)

	output := captureStdout(t, func() {
		if err := run([]string{"fmt", "-f", a, "-f", b}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "formatted 2 file(s)") {
		t.Fatalf("fmt output = %q", output)
	}
	for _, path := range []string{a, b} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `content = "`) {
			t.Fatalf("%s was not formatted:\n%s", path, data)
		}
	}
	data, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != rawC {
		t.Fatalf("implicit file was changed:\n%s", data)
	}
}

func runnableExamples() []string {
	return []string{
		"examples/bbr.dbf.hcl",
		"examples/apt-repository.dbf.hcl",
		"examples/bird2.dbf.hcl",
		"examples/component-binary.dbf.hcl",
		"examples/files-plan-preview.dbf.hcl",
		"examples/fleet.dbf.hcl",
		"examples/mihomo.dbf.hcl",
		"examples/nftables.dbf.hcl",
		"examples/plan-preview.dbf.hcl",
		"examples/profile-merge.dbf.hcl",
		"examples/realistic-systemd-app.dbf.hcl",
		"examples/systemd-service.dbf.hcl",
		"examples/user-group.dbf.hcl",
		"examples/variable-secret-file.dbf.hcl",
	}
}

type dbfDocExample struct {
	Name     string
	Commands []string
	Files    []string
	HCL      string
}

func extractDBFDocExamples(t *testing.T, doc string) []dbfDocExample {
	t.Helper()
	lines := strings.Split(doc, "\n")
	examples := []dbfDocExample{}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "<!-- dbf-test:") {
			continue
		}
		meta := strings.TrimSuffix(strings.TrimPrefix(line, "<!-- dbf-test:"), " -->")
		example := dbfDocExample{Name: "example"}
		for _, part := range strings.Split(meta, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				t.Fatalf("invalid dbf-test metadata %q", part)
			}
			values := splitCSV(value)
			switch key {
			case "name":
				if len(values) != 1 || values[0] == "" {
					t.Fatalf("invalid dbf-test name %q", value)
				}
				example.Name = values[0]
			case "commands":
				example.Commands = values
			case "files":
				example.Files = values
			default:
				t.Fatalf("unknown dbf-test metadata key %q", key)
			}
		}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) || strings.TrimSpace(lines[i]) != "```hcl" {
			t.Fatalf("dbf-test %q must be followed by an hcl code block", example.Name)
		}
		i++
		var hcl strings.Builder
		for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
			hcl.WriteString(lines[i])
			hcl.WriteByte('\n')
			i++
		}
		if i >= len(lines) {
			t.Fatalf("dbf-test %q hcl block is not closed", example.Name)
		}
		example.HCL = hcl.String()
		if len(example.Commands) == 0 {
			t.Fatalf("dbf-test %q has no commands", example.Name)
		}
		examples = append(examples, example)
	}
	return examples
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstRunnableHCLBlock(t *testing.T, path string) string {
	t.Helper()
	for _, block := range hclCodeBlocksFromFile(t, path) {
		if strings.Contains(block, `host "`) {
			return block
		}
	}
	t.Fatalf("%s has no hcl block containing a host", path)
	return ""
}

func hclCodeBlocksFromFile(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	blocks := []string{}
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```hcl" {
			continue
		}
		i++
		var hcl strings.Builder
		for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
			hcl.WriteString(lines[i])
			hcl.WriteByte('\n')
			i++
		}
		if i >= len(lines) {
			t.Fatalf("%s has an unclosed hcl block", path)
		}
		blocks = append(blocks, hcl.String())
	}
	return blocks
}

func runDocConfigLocalChecks(t *testing.T, hcl string) {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, "site.dbf.hcl")
	writeTestFile(t, config, hcl)
	for _, args := range [][]string{
		{"validate", "-f", config},
		{"plan", "-f", config, "--offline"},
	} {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			captureOutput(t, func() {
				if err := run(args); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func indentHCL(hcl string, prefix string) string {
	lines := strings.Split(hcl, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
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

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
