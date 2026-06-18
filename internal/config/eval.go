package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/function"
)

type EvalContext struct {
	PathModule string
	Locals     map[string]any
	EachKey    string
	EachValue  any
	HasEach    bool
}

func Eval(expr Expr, ctx EvalContext) (any, error) {
	evalCtx, err := newHCLEvalContext(ctx)
	if err != nil {
		return nil, err
	}

	value, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	return ctyToGo(value)
}

func newHCLEvalContext(ctx EvalContext) (*hcl.EvalContext, error) {
	vars := map[string]cty.Value{
		"path": cty.ObjectVal(map[string]cty.Value{
			"module": cty.StringVal(ctx.PathModule),
		}),
		"local": cty.EmptyObjectVal,
	}

	if len(ctx.Locals) > 0 {
		localValues := make(map[string]cty.Value, len(ctx.Locals))
		for name, value := range ctx.Locals {
			converted, err := goToCty(value)
			if err != nil {
				return nil, fmt.Errorf("local.%s: %w", name, err)
			}
			localValues[name] = converted
		}
		vars["local"] = cty.ObjectVal(localValues)
	}

	if ctx.HasEach {
		eachValue, err := goToCty(ctx.EachValue)
		if err != nil {
			return nil, fmt.Errorf("each.value: %w", err)
		}
		vars["each"] = cty.ObjectVal(map[string]cty.Value{
			"key":   cty.StringVal(ctx.EachKey),
			"value": eachValue,
		})
	}

	return &hcl.EvalContext{
		Variables: vars,
		Functions: map[string]function.Function{
			"file":         fileFunction(ctx.PathModule),
			"templatefile": templatefileFunction(ctx.PathModule),
			"toset":        tosetFunction(),
		},
	}, nil
}

func fileFunction(moduleDir string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "path", Type: cty.String},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			path := args[0].AsString()
			if path == "" {
				return cty.NilVal, fmt.Errorf("file() argument must be a non-empty string")
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(moduleDir, path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return cty.NilVal, err
			}
			return cty.StringVal(string(data)), nil
		},
	})
}

// templatefileFunction renders a file as an HCL template, mirroring
// Terraform's templatefile(path, vars). The file may use ${...} interpolation
// and %{ for }/%{ if } directives, with the supplied vars as the only available
// variables. templatefile itself is not exposed inside the template, so a
// template cannot recurse into another templatefile call.
func templatefileFunction(moduleDir string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "path", Type: cty.String},
			{Name: "vars", Type: cty.DynamicPseudoType},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			path := args[0].AsString()
			if path == "" {
				return cty.NilVal, fmt.Errorf("templatefile() path argument must be a non-empty string")
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(moduleDir, path)
			}

			varsArg := args[1]
			if varsArg.IsNull() {
				return cty.NilVal, fmt.Errorf("templatefile() vars argument must not be null")
			}
			vt := varsArg.Type()
			if !vt.IsObjectType() && !vt.IsMapType() {
				return cty.NilVal, fmt.Errorf("templatefile() vars argument must be an object or map")
			}
			variables := map[string]cty.Value{}
			it := varsArg.ElementIterator()
			for it.Next() {
				key, value := it.Element()
				variables[key.AsString()] = value
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return cty.NilVal, err
			}

			expr, diags := hclsyntax.ParseTemplate(data, path, hcl.Pos{Line: 1, Column: 1})
			if diags.HasErrors() {
				return cty.NilVal, fmt.Errorf("templatefile() parse %s: %s", path, diags.Error())
			}
			rendered, diags := expr.Value(&hcl.EvalContext{Variables: variables})
			if diags.HasErrors() {
				return cty.NilVal, fmt.Errorf("templatefile() render %s: %s", path, diags.Error())
			}
			rendered, err = convert.Convert(rendered, cty.String)
			if err != nil {
				return cty.NilVal, fmt.Errorf("templatefile() %s: %w", path, err)
			}
			return rendered, nil
		},
	})
}

func tosetFunction() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "value", Type: cty.DynamicPseudoType},
		},
		Type: function.StaticReturnType(cty.Set(cty.String)),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			value, _ := args[0].UnmarkDeep()
			if !value.IsKnown() {
				return cty.NilVal, fmt.Errorf("toset() argument must be known")
			}
			if value.IsNull() {
				return cty.NilVal, fmt.Errorf("toset() argument must not be null")
			}

			ty := value.Type()
			if !ty.IsTupleType() && !ty.IsListType() && !ty.IsSetType() {
				return cty.NilVal, fmt.Errorf("toset() argument must be a list, tuple, or set of strings")
			}

			seen := map[string]struct{}{}
			out := []cty.Value{}
			it := value.ElementIterator()
			for it.Next() {
				_, item := it.Element()
				item, _ = item.UnmarkDeep()
				if !item.IsKnown() {
					return cty.NilVal, fmt.Errorf("toset() entries must be known")
				}
				if item.IsNull() || !item.Type().Equals(cty.String) {
					return cty.NilVal, fmt.Errorf("toset() entries must be non-empty strings")
				}
				key := item.AsString()
				if key == "" {
					return cty.NilVal, fmt.Errorf("toset() entries must be non-empty strings")
				}
				if _, exists := seen[key]; exists {
					return cty.NilVal, fmt.Errorf("toset() duplicate entry %q", key)
				}
				seen[key] = struct{}{}
				out = append(out, cty.StringVal(key))
			}

			if len(out) == 0 {
				return cty.SetValEmpty(cty.String), nil
			}
			return cty.SetVal(out), nil
		},
	})
}

func ctyToGo(value cty.Value) (any, error) {
	value, _ = value.UnmarkDeep()
	if !value.IsKnown() {
		return nil, fmt.Errorf("unknown values are not supported")
	}
	if value.IsNull() {
		return nil, nil
	}

	ty := value.Type()
	switch {
	case ty.Equals(cty.String):
		return value.AsString(), nil
	case ty.Equals(cty.Bool):
		return value.True(), nil
	case ty.Equals(cty.Number):
		return Number(value.AsBigFloat().Text('g', -1)), nil
	case ty.IsTupleType() || ty.IsListType() || ty.IsSetType():
		out := []any{}
		it := value.ElementIterator()
		for it.Next() {
			_, item := it.Element()
			converted, err := ctyToGo(item)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	case ty.IsObjectType() || ty.IsMapType():
		out := map[string]any{}
		it := value.ElementIterator()
		for it.Next() {
			key, item := it.Element()
			converted, err := ctyToGo(item)
			if err != nil {
				return nil, err
			}
			out[key.AsString()] = converted
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported value type %s", ty.FriendlyName())
	}
}

func goToCty(value any) (cty.Value, error) {
	switch v := value.(type) {
	case nil:
		return cty.NullVal(cty.DynamicPseudoType), nil
	case string:
		return cty.StringVal(v), nil
	case Number:
		return cty.ParseNumberVal(string(v))
	case bool:
		return cty.BoolVal(v), nil
	case []any:
		if len(v) == 0 {
			return cty.EmptyTupleVal, nil
		}
		values := make([]cty.Value, 0, len(v))
		for _, item := range v {
			converted, err := goToCty(item)
			if err != nil {
				return cty.NilVal, err
			}
			values = append(values, converted)
		}
		return cty.TupleVal(values), nil
	case map[string]any:
		if len(v) == 0 {
			return cty.EmptyObjectVal, nil
		}
		values := make(map[string]cty.Value, len(v))
		for key, item := range v {
			converted, err := goToCty(item)
			if err != nil {
				return cty.NilVal, err
			}
			values[key] = converted
		}
		return cty.ObjectVal(values), nil
	default:
		return cty.NilVal, fmt.Errorf("unsupported value %T", value)
	}
}
