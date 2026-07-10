package graph

import (
	"strings"
	"testing"
)

func TestCompileVariableDefaultsResourceGraph(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/variable-defaults.dbf.hcl")

	file := nodeFor(resourceGraph, `host.vars1.files.file["/etc/debianform/message.txt"]`)
	if file == nil {
		t.Fatal("variable-backed file node missing")
	}
	if file.Desired["content"] != "hello from variable default" {
		t.Fatalf("file desired = %#v", file.Desired)
	}
	profileFile := nodeFor(resourceGraph, `host.vars1.files.file["/etc/debianform/profile-message.txt"]`)
	if profileFile == nil {
		t.Fatal("profile variable-backed file node missing")
	}
	if profileFile.Desired["content"] != "hello from variable default" {
		t.Fatalf("profile file desired = %#v", profileFile.Desired)
	}
	unit := nodeFor(resourceGraph, `host.vars1.components.message_unit.systemd.unit["message.service"]`)
	if unit == nil {
		t.Fatal("variable-backed component unit node missing")
	}
	content, _ := unit.Desired["content"].(string)
	if !strings.Contains(content, "Variable backed service") || !strings.Contains(content, "hello from variable default") {
		t.Fatalf("unit content = %q", content)
	}
}
