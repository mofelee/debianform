package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

func TestEvalConditionalSelectsBranch(t *testing.T) {
	expr := mustExpr(t, `true ? "enabled" : "disabled"`)
	got, err := Eval(expr, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "enabled" {
		t.Fatalf("Eval() = %#v, want %q", got, "enabled")
	}
}

func TestEvalConditionalShortCircuits(t *testing.T) {
	expr := mustExpr(t, `true ? "selected" : local.missing`)
	got, err := Eval(expr, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "selected" {
		t.Fatalf("Eval() = %#v, want %q", got, "selected")
	}
}

func TestEvalConditionalRequiresBoolean(t *testing.T) {
	expr := mustExpr(t, `"yes" ? "enabled" : "disabled"`)
	_, err := Eval(expr, EvalContext{})
	if err == nil || !strings.Contains(err.Error(), "condition") {
		t.Fatalf("Eval() error = %v, want boolean condition error", err)
	}
}

func TestEvalConditionalRejectsKnownIncompatibleTypes(t *testing.T) {
	expr := mustExpr(t, `true ? "enabled" : ["disabled"]`)
	_, err := Eval(expr, EvalContext{})
	if err == nil || !strings.Contains(err.Error(), "conditional") {
		t.Fatalf("Eval() error = %v, want incompatible branch error", err)
	}
}

func TestEvalEquality(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want bool
	}{
		{
			name: "equal",
			expr: `"ci" == "ci"`,
			want: true,
		},
		{
			name: "not equal",
			expr: `1 != 2`,
			want: true,
		},
		{
			name: "equivalent numbers",
			expr: `1 == 1.0`,
			want: true,
		},
		{
			name: "different types",
			expr: `1 == "1"`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(mustExpr(t, tt.expr), EvalContext{})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Eval() = %#v, want %t", got, tt.want)
			}
		})
	}
}

func TestTemplatefileInterpolatesVars(t *testing.T) {
	dir := writeTemplate(t, "greeting.tmpl", "Hello ${name}, welcome to ${place}!\n")
	expr := mustExpr(t, `templatefile("greeting.tmpl", { name = "ada", place = "debian" })`)

	got, err := Eval(expr, EvalContext{PathModule: dir})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Hello ada, welcome to debian!\n"; got != want {
		t.Fatalf("Eval() = %#v, want %q", got, want)
	}
}

func TestTemplatefileEvaluatesForDirective(t *testing.T) {
	dir := writeTemplate(t, "hosts.tmpl", "%{ for h in hosts }127.0.0.1 ${h}\n%{ endfor }")
	expr := mustExpr(t, `templatefile("hosts.tmpl", { hosts = ["a", "b"] })`)

	got, err := Eval(expr, EvalContext{PathModule: dir})
	if err != nil {
		t.Fatal(err)
	}
	if want := "127.0.0.1 a\n127.0.0.1 b\n"; got != want {
		t.Fatalf("Eval() = %#v, want %q", got, want)
	}
}

func TestTemplatefileEvaluatesIfDirective(t *testing.T) {
	dir := writeTemplate(t, "flag.tmpl", "%{ if enabled }on%{ else }off%{ endif }")
	expr := mustExpr(t, `templatefile("flag.tmpl", { enabled = true })`)

	got, err := Eval(expr, EvalContext{PathModule: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got != "on" {
		t.Fatalf("Eval() = %#v, want %q", got, "on")
	}
}

func TestTemplatefileMissingVariableErrors(t *testing.T) {
	dir := writeTemplate(t, "greeting.tmpl", "Hello ${name}\n")
	expr := mustExpr(t, `templatefile("greeting.tmpl", { other = "x" })`)

	if _, err := Eval(expr, EvalContext{PathModule: dir}); err == nil {
		t.Fatal("Eval() error = nil, want unknown-variable error")
	}
}

func TestTemplatefileRejectsNonMapVars(t *testing.T) {
	dir := writeTemplate(t, "greeting.tmpl", "Hello ${name}\n")
	expr := mustExpr(t, `templatefile("greeting.tmpl", "not-a-map")`)

	_, err := Eval(expr, EvalContext{PathModule: dir})
	if err == nil || !strings.Contains(err.Error(), "object or map") {
		t.Fatalf("Eval() error = %v, want object-or-map error", err)
	}
}

func TestTemplatefileMissingFileErrors(t *testing.T) {
	expr := mustExpr(t, `templatefile("absent.tmpl", {})`)

	if _, err := Eval(expr, EvalContext{PathModule: t.TempDir()}); err == nil {
		t.Fatal("Eval() error = nil, want file-not-found error")
	}
}

func writeTemplate(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustExpr(t *testing.T, src string) Expr {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.dbf.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	return expr
}
