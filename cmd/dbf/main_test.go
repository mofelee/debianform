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

func TestValidateV2BBR(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"validate", "-f", "../../examples/v2-bbr.dbf.hcl"}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "v2 configuration is valid: 1 host(s)") {
		t.Fatalf("validate output = %q", output)
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
