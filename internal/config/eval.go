package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	default:
		return nil, fmt.Errorf("unsupported expression %T", expr)
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
