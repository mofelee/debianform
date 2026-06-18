package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type EvalContext struct {
	PathModule string
	Locals     map[string]any
	EachKey    string
	EachValue  any
	HasEach    bool
}

func Eval(expr Expr, ctx EvalContext) (any, error) {
	switch v := expr.(type) {
	case StringLit:
		return interpolate(string(v), ctx), nil
	case HeredocLit:
		return interpolate(string(v), ctx), nil
	case Ref:
		return evalRef(string(v), ctx)
	case Number:
		return v, nil
	case bool:
		return v, nil
	case List:
		out := make([]any, 0, len(v))
		for _, item := range v {
			evaluated, err := Eval(item, ctx)
			if err != nil {
				return nil, err
			}
			out = append(out, evaluated)
		}
		return out, nil
	case Map:
		out := make(map[string]any, len(v))
		for key, item := range v {
			evaluated, err := Eval(item, ctx)
			if err != nil {
				return nil, err
			}
			out[key] = evaluated
		}
		return out, nil
	case FuncCall:
		return evalFunc(v, ctx)
	case ConditionalExpr:
		return evalConditional(v, ctx)
	case BinaryExpr:
		return evalBinary(v, ctx)
	default:
		return nil, fmt.Errorf("unsupported expression %T", expr)
	}
}

func evalConditional(expr ConditionalExpr, ctx EvalContext) (any, error) {
	if err := validateConditionalTypes(expr.True, expr.False); err != nil {
		return nil, err
	}

	condition, err := Eval(expr.Condition, ctx)
	if err != nil {
		return nil, err
	}
	selected, ok := condition.(bool)
	if !ok {
		return nil, fmt.Errorf("conditional expression condition must be a boolean, got %s", valueType(condition))
	}
	if selected {
		return Eval(expr.True, ctx)
	}
	return Eval(expr.False, ctx)
}

func evalBinary(expr BinaryExpr, ctx EvalContext) (any, error) {
	left, err := Eval(expr.Left, ctx)
	if err != nil {
		return nil, err
	}
	right, err := Eval(expr.Right, ctx)
	if err != nil {
		return nil, err
	}

	equal := equalValues(left, right)
	switch expr.Op {
	case "==":
		return equal, nil
	case "!=":
		return !equal, nil
	default:
		return nil, fmt.Errorf("unsupported binary operator %q", expr.Op)
	}
}

func equalValues(left, right any) bool {
	leftNumber, leftIsNumber := left.(Number)
	rightNumber, rightIsNumber := right.(Number)
	if leftIsNumber || rightIsNumber {
		if !leftIsNumber || !rightIsNumber {
			return false
		}
		leftFloat, leftErr := strconv.ParseFloat(string(leftNumber), 64)
		rightFloat, rightErr := strconv.ParseFloat(string(rightNumber), 64)
		return leftErr == nil && rightErr == nil && leftFloat == rightFloat
	}
	return reflect.DeepEqual(left, right)
}

func validateConditionalTypes(trueExpr, falseExpr Expr) error {
	trueType := staticExprType(trueExpr)
	falseType := staticExprType(falseExpr)
	if trueType != "dynamic" && falseType != "dynamic" && trueType != falseType {
		return fmt.Errorf("conditional expression branches must have compatible types, got %s and %s", trueType, falseType)
	}
	return nil
}

func staticExprType(expr Expr) string {
	switch v := expr.(type) {
	case StringLit, HeredocLit:
		return "string"
	case Number:
		return "number"
	case bool, BinaryExpr:
		return "bool"
	case List:
		return "list"
	case Map:
		return "map"
	case FuncCall:
		switch v.Name {
		case "file":
			return "string"
		case "toset":
			return "list"
		default:
			return "dynamic"
		}
	case ConditionalExpr:
		trueType := staticExprType(v.True)
		falseType := staticExprType(v.False)
		if trueType == falseType {
			return trueType
		}
		return "dynamic"
	default:
		return "dynamic"
	}
}

func valueType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case Number:
		return "number"
	case bool:
		return "bool"
	case []any:
		return "list"
	case map[string]any:
		return "map"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func evalRef(ref string, ctx EvalContext) (any, error) {
	switch ref {
	case "path.module":
		return ctx.PathModule, nil
	case "each.key":
		if !ctx.HasEach {
			return nil, fmt.Errorf("each.key used outside for_each")
		}
		return ctx.EachKey, nil
	case "each.value":
		if !ctx.HasEach {
			return nil, fmt.Errorf("each.value used outside for_each")
		}
		return ctx.EachValue, nil
	default:
		if strings.HasPrefix(ref, "local.") {
			name := strings.TrimPrefix(ref, "local.")
			if ctx.Locals == nil {
				return nil, fmt.Errorf("local.%s is not defined", name)
			}
			value, ok := ctx.Locals[name]
			if !ok {
				return nil, fmt.Errorf("local.%s is not defined", name)
			}
			return value, nil
		}
		return ref, nil
	}
}

func evalFunc(call FuncCall, ctx EvalContext) (any, error) {
	switch call.Name {
	case "file":
		if len(call.Args) != 1 {
			return nil, fmt.Errorf("file() expects one argument")
		}
		value, err := Eval(call.Args[0], ctx)
		if err != nil {
			return nil, err
		}
		path, ok := value.(string)
		if !ok || path == "" {
			return nil, fmt.Errorf("file() argument must be a string")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(ctx.PathModule, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	case "toset":
		if len(call.Args) != 1 {
			return nil, fmt.Errorf("toset() expects one argument")
		}
		value, err := Eval(call.Args[0], ctx)
		if err != nil {
			return nil, err
		}
		list, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("toset() argument must be a list")
		}
		seen := map[string]struct{}{}
		for _, item := range list {
			key, ok := item.(string)
			if !ok || key == "" {
				return nil, fmt.Errorf("toset() list entries must be non-empty strings")
			}
			if _, exists := seen[key]; exists {
				return nil, fmt.Errorf("toset() duplicate entry %q", key)
			}
			seen[key] = struct{}{}
		}
		return list, nil
	default:
		return nil, fmt.Errorf("unsupported function %s()", call.Name)
	}
}

func interpolate(input string, ctx EvalContext) string {
	replacer := strings.NewReplacer(
		"${path.module}", ctx.PathModule,
		"${each.key}", ctx.EachKey,
		"${each.value}", fmt.Sprint(ctx.EachValue),
	)
	return replacer.Replace(input)
}
