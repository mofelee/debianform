package parser

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/function"
)

type EvalContext struct {
	ModuleDir string
	Locals    map[string]Value
	Variables map[string]cty.Value
}

func evalValue(expr hcl.Expression, ctx EvalContext, source ir.SourceRef) (Value, error) {
	if call, ok := expr.(*hclsyntax.FunctionCallExpr); ok {
		switch call.Name {
		case string(ModifierUnset):
			if len(call.Args) != 0 {
				return Value{}, fmt.Errorf("%s:%d: unset() takes no arguments", source.File, source.Line)
			}
			value := NullValue(source)
			value.Modifier = ModifierUnset
			return value, nil
		case string(ModifierForce), string(ModifierBefore), string(ModifierAfter):
			if len(call.Args) != 1 {
				return Value{}, fmt.Errorf("%s:%d: %s() takes exactly one argument", source.File, source.Line, call.Name)
			}
			value, err := evalValue(call.Args[0], ctx, source)
			if err != nil {
				return Value{}, err
			}
			value.Modifier = Modifier(call.Name)
			value.Source = source
			return value, nil
		}
	}

	if items, diags := hcl.ExprList(expr); !diags.HasErrors() {
		values := make([]Value, 0, len(items))
		for i, item := range items {
			itemSource := source
			itemSource.Line = item.Range().Start.Line
			itemSource.Path = fmt.Sprintf("%s[%d]", source.Path, i)
			value, err := evalValue(item, ctx, itemSource)
			if err != nil {
				return Value{}, err
			}
			values = append(values, value)
		}
		return Value{Kind: KindList, List: values, Source: source}, nil
	}

	if pairs, diags := hcl.ExprMap(expr); !diags.HasErrors() {
		evalCtx, err := hclEvalContext(ctx)
		if err != nil {
			return Value{}, err
		}
		values := make(map[string]Value, len(pairs))
		for _, pair := range pairs {
			keyValue, diags := pair.Key.Value(evalCtx)
			if diags.HasErrors() {
				return Value{}, fmt.Errorf("%s:%d: map key: %s", source.File, pair.Key.Range().Start.Line, diags.Error())
			}
			keyValue, err = convert.Convert(keyValue, cty.String)
			if err != nil {
				return Value{}, fmt.Errorf("%s:%d: map key must be a string: %w", source.File, pair.Key.Range().Start.Line, err)
			}
			key := keyValue.AsString()
			itemSource := source
			itemSource.Line = pair.Value.Range().Start.Line
			itemSource.Path = fmt.Sprintf("%s[%q]", source.Path, key)
			value, err := evalValue(pair.Value, ctx, itemSource)
			if err != nil {
				return Value{}, err
			}
			values[key] = value
		}
		return MapValue(values, source), nil
	}

	value, err := evalCty(expr, ctx)
	if err != nil {
		return Value{}, err
	}
	return ctyToValue(value, source)
}

func evalCty(expr hcl.Expression, ctx EvalContext) (cty.Value, error) {
	evalCtx, err := hclEvalContext(ctx)
	if err != nil {
		return cty.NilVal, err
	}
	value, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		return cty.NilVal, fmt.Errorf("%s", diags.Error())
	}
	return value, nil
}

func hclEvalContext(ctx EvalContext) (*hcl.EvalContext, error) {
	vars := map[string]cty.Value{
		"path": cty.ObjectVal(map[string]cty.Value{
			"module": cty.StringVal(ctx.ModuleDir),
		}),
		"local": cty.EmptyObjectVal,
	}

	if len(ctx.Locals) > 0 {
		localValues := make(map[string]cty.Value, len(ctx.Locals))
		for name, value := range ctx.Locals {
			converted, err := value.ToCty()
			if err != nil {
				return nil, fmt.Errorf("local.%s: %w", name, err)
			}
			localValues[name] = converted
		}
		vars["local"] = cty.ObjectVal(localValues)
	}
	for name, value := range ctx.Variables {
		vars[name] = value
	}

	return &hcl.EvalContext{
		Variables: vars,
		Functions: map[string]function.Function{
			"file":         fileFunction(ctx.ModuleDir),
			"templatefile": templatefileFunction(ctx.ModuleDir),
			"toset":        tosetFunction(),
		},
	}, nil
}

func ctyToValue(value cty.Value, source ir.SourceRef) (Value, error) {
	value, _ = value.UnmarkDeep()
	if !value.IsKnown() {
		return Value{}, fmt.Errorf("%s:%d: unknown values are not supported", source.File, source.Line)
	}
	if value.IsNull() {
		return NullValue(source), nil
	}

	ty := value.Type()
	switch {
	case ty.Equals(cty.String):
		return Value{Kind: KindString, String: value.AsString(), Source: source}, nil
	case ty.Equals(cty.Bool):
		return Value{Kind: KindBool, Bool: value.True(), Source: source}, nil
	case ty.Equals(cty.Number):
		return Value{Kind: KindNumber, Number: value.AsBigFloat().Text('g', -1), Source: source}, nil
	case ty.IsTupleType() || ty.IsListType() || ty.IsSetType():
		out := []Value{}
		it := value.ElementIterator()
		i := 0
		for it.Next() {
			_, item := it.Element()
			itemSource := source
			itemSource.Path = fmt.Sprintf("%s[%d]", source.Path, i)
			converted, err := ctyToValue(item, itemSource)
			if err != nil {
				return Value{}, err
			}
			out = append(out, converted)
			i++
		}
		return Value{Kind: KindList, List: out, Source: source}, nil
	case ty.IsObjectType() || ty.IsMapType():
		out := map[string]Value{}
		it := value.ElementIterator()
		for it.Next() {
			key, item := it.Element()
			itemSource := source
			itemSource.Path = fmt.Sprintf("%s[%q]", source.Path, key.AsString())
			converted, err := ctyToValue(item, itemSource)
			if err != nil {
				return Value{}, err
			}
			out[key.AsString()] = converted
		}
		return MapValue(out, source), nil
	default:
		return Value{}, fmt.Errorf("%s:%d: unsupported value type %s", source.File, source.Line, ty.FriendlyName())
	}
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
