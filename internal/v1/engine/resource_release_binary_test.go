package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v1/config"
	"github.com/mofelee/debianform/internal/v1/sshx"
)

func releaseBinaryResource() config.Resource {
	return config.Resource{
		Type:    "debian_release_binary",
		Name:    "tool",
		Address: "debian_release_binary.tool",
		Host:    "server1",
		Attrs: map[string]any{
			"path":   "/usr/local/bin/tool",
			"member": "tool",
			"sources": map[string]any{
				"amd64": map[string]any{
					"url":            "https://example.com/tool-amd64.tar.xz",
					"archive_sha256": strings.Repeat("a", 64),
					"binary_sha256":  strings.Repeat("b", 64),
				},
				"arm64": map[string]any{
					"url":            "https://example.com/tool-arm64.tar.xz",
					"archive_sha256": strings.Repeat("c", 64),
					"binary_sha256":  strings.Repeat("d", 64),
				},
			},
		},
	}
}

func TestPlanReleaseBinarySelectsDebianArchitecture(t *testing.T) {
	res := releaseBinaryResource()
	runner := &fakeRunner{reply: func(_, script string) (sshx.Result, error) {
		if strings.Contains(script, "dpkg --print-architecture") {
			return sshx.Result{Stdout: "arm64\n"}, nil
		}
		return sshx.Result{Stdout: "missing\n"}, nil
	}}

	change := planFixture(t, runner, res)
	if change.Action != "create" {
		t.Fatalf("action = %q, want create", change.Action)
	}
	if got, want := change.Desired.ReleaseSource.URL, "https://example.com/tool-arm64.tar.xz"; got != want {
		t.Fatalf("selected URL = %q, want %q", got, want)
	}
}

func TestPlanReleaseBinaryDetectsBinaryDrift(t *testing.T) {
	res := releaseBinaryResource()
	runner := &fakeRunner{reply: func(_, script string) (sshx.Result, error) {
		if strings.Contains(script, "dpkg --print-architecture") {
			return sshx.Result{Stdout: "amd64\n"}, nil
		}
		return sshx.Result{Stdout: "file\nroot\nroot\n755\n" + strings.Repeat("c", 64) + "\n"}, nil
	}}

	if got := planFixture(t, runner, res).Action; got != "update" {
		t.Fatalf("action = %q, want update", got)
	}
}

func TestApplyReleaseBinaryVerifiesArchiveAndBinary(t *testing.T) {
	res := releaseBinaryResource()
	d, err := (releaseBinaryProvider{}).Desired(res)
	if err != nil {
		t.Fatal(err)
	}
	d.ReleaseSource = d.ReleaseSources["amd64"]
	runner := &fakeRunner{}
	e := &Engine{runner: runner}
	if err := (releaseBinaryProvider{}).Apply(context.Background(), e, change(res, d, "create", "")); err != nil {
		t.Fatal(err)
	}
	script := runner.scripts[0]
	for _, want := range []string{
		"https://example.com/tool-amd64.tar.xz",
		strings.Repeat("a", 64),
		strings.Repeat("b", 64),
		"tar -xJOf",
		"apt-get install -y ca-certificates curl tar xz-utils",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("apply script missing %q:\n%s", want, script)
		}
	}
}

func TestApplySystemdUnitReloadsManager(t *testing.T) {
	res := config.Resource{
		Type:    "debian_systemd_unit",
		Name:    "tool",
		Address: "debian_systemd_unit.tool",
		Host:    "server1",
		Attrs: map[string]any{
			"name":    "tool.service",
			"content": "[Service]\nExecStart=/usr/local/bin/tool\n",
		},
	}
	d, err := (systemdUnitProvider{}).Desired(res)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	e := &Engine{runner: runner}
	if err := (systemdUnitProvider{}).Apply(context.Background(), e, change(res, d, "create", "")); err != nil {
		t.Fatal(err)
	}
	script := runner.scripts[0]
	for _, want := range []string{"systemctl daemon-reload", ".dbf-rollback", "/etc/systemd/system/tool.service"} {
		if !strings.Contains(script, want) {
			t.Fatalf("apply script missing %q:\n%s", want, script)
		}
	}
}
