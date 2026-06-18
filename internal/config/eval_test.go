package config

import (
	"strings"
	"testing"
)

func TestEvalConditionalSelectsBranch(t *testing.T) {
	expr := ConditionalExpr{
		Condition: true,
		True:      StringLit("enabled"),
		False:     StringLit("disabled"),
	}

	got, err := Eval(expr, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "enabled" {
		t.Fatalf("Eval() = %#v, want %q", got, "enabled")
	}
}

func TestEvalConditionalShortCircuits(t *testing.T) {
	expr := ConditionalExpr{
		Condition: true,
		True:      StringLit("selected"),
		False:     Ref("local.missing"),
	}

	got, err := Eval(expr, EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "selected" {
		t.Fatalf("Eval() = %#v, want %q", got, "selected")
	}
}

func TestEvalConditionalRequiresBoolean(t *testing.T) {
	expr := ConditionalExpr{
		Condition: StringLit("yes"),
		True:      StringLit("enabled"),
		False:     StringLit("disabled"),
	}

	_, err := Eval(expr, EvalContext{})
	if err == nil || !strings.Contains(err.Error(), "condition must be a boolean") {
		t.Fatalf("Eval() error = %v, want boolean condition error", err)
	}
}

func TestEvalConditionalRejectsKnownIncompatibleTypes(t *testing.T) {
	expr := ConditionalExpr{
		Condition: true,
		True:      StringLit("enabled"),
		False:     List{StringLit("disabled")},
	}

	_, err := Eval(expr, EvalContext{})
	if err == nil || !strings.Contains(err.Error(), "compatible types") {
		t.Fatalf("Eval() error = %v, want incompatible branch error", err)
	}
}

func TestEvalEquality(t *testing.T) {
	tests := []struct {
		name string
		expr BinaryExpr
		want bool
	}{
		{
			name: "equal",
			expr: BinaryExpr{Left: StringLit("ci"), Op: "==", Right: StringLit("ci")},
			want: true,
		},
		{
			name: "not equal",
			expr: BinaryExpr{Left: Number("1"), Op: "!=", Right: Number("2")},
			want: true,
		},
		{
			name: "equivalent numbers",
			expr: BinaryExpr{Left: Number("1"), Op: "==", Right: Number("1.0")},
			want: true,
		},
		{
			name: "different types",
			expr: BinaryExpr{Left: Number("1"), Op: "==", Right: StringLit("1")},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(tt.expr, EvalContext{})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Eval() = %#v, want %t", got, tt.want)
			}
		})
	}
}
