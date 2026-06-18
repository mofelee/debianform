package config

import (
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

func mustExpr(t *testing.T, src string) Expr {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.dbf.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	return expr
}
