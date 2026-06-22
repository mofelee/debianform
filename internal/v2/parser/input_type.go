package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

type ComponentInputTypeKind string

const (
	ComponentInputTypeString ComponentInputTypeKind = "string"
	ComponentInputTypeNumber ComponentInputTypeKind = "number"
	ComponentInputTypeBool   ComponentInputTypeKind = "bool"
	ComponentInputTypeAny    ComponentInputTypeKind = "any"
	ComponentInputTypeList   ComponentInputTypeKind = "list"
	ComponentInputTypeSet    ComponentInputTypeKind = "set"
	ComponentInputTypeMap    ComponentInputTypeKind = "map"
	ComponentInputTypeObject ComponentInputTypeKind = "object"
	ComponentInputTypeTuple  ComponentInputTypeKind = "tuple"
)

type ComponentInputTypeSpec struct {
	Kind       ComponentInputTypeKind             `json:"kind"`
	Element    *ComponentInputTypeSpec            `json:"element,omitempty"`
	Attributes map[string]ComponentObjectAttrSpec `json:"attributes,omitempty"`
	Tuple      []ComponentInputTypeSpec           `json:"tuple,omitempty"`
}

type ComponentObjectAttrSpec struct {
	Type     ComponentInputTypeSpec `json:"type"`
	Optional bool                   `json:"optional,omitempty"`
	Default  *Value                 `json:"default,omitempty"`
}

func parseComponentInputType(expr hcl.Expression, ctx EvalContext, sourcePath string) (ComponentInputTypeSpec, string, error) {
	spec, err := parseComponentInputTypeExpr(expr, ctx, sourcePath, false)
	if err != nil {
		return ComponentInputTypeSpec{}, "", err
	}
	return spec, spec.String(), nil
}

func parseComponentInputTypeExpr(expr hcl.Expression, ctx EvalContext, sourcePath string, allowOptional bool) (ComponentInputTypeSpec, error) {
	if traversal, diags := hcl.AbsTraversalForExpr(expr); !diags.HasErrors() && len(traversal) == 1 {
		if root, ok := traversal[0].(hcl.TraverseRoot); ok {
			switch root.Name {
			case "string":
				return ComponentInputTypeSpec{Kind: ComponentInputTypeString}, nil
			case "number":
				return ComponentInputTypeSpec{Kind: ComponentInputTypeNumber}, nil
			case "bool":
				return ComponentInputTypeSpec{Kind: ComponentInputTypeBool}, nil
			case "any":
				return ComponentInputTypeSpec{Kind: ComponentInputTypeAny}, nil
			case "list", "map", "set", "object", "tuple":
				return ComponentInputTypeSpec{}, fmt.Errorf("%s requires an element type, for example %s(string)", root.Name, root.Name)
			}
		}
	}

	call, ok := expr.(*hclsyntax.FunctionCallExpr)
	if !ok {
		return ComponentInputTypeSpec{}, fmt.Errorf("supported types are string, number, bool, any, list(T), set(T), map(T), object({...}), and tuple([...])")
	}

	switch call.Name {
	case "array":
		return ComponentInputTypeSpec{}, fmt.Errorf("array(T) is not supported; use list(T)")
	case "list", "set", "map":
		if len(call.Args) != 1 {
			return ComponentInputTypeSpec{}, fmt.Errorf("%s() requires exactly one type argument", call.Name)
		}
		element, err := parseComponentInputTypeExpr(call.Args[0], ctx, sourcePath, false)
		if err != nil {
			return ComponentInputTypeSpec{}, fmt.Errorf("%s() element: %w", call.Name, err)
		}
		kind := ComponentInputTypeKind(call.Name)
		return ComponentInputTypeSpec{Kind: kind, Element: &element}, nil
	case "object":
		if len(call.Args) != 1 {
			return ComponentInputTypeSpec{}, fmt.Errorf("object() requires exactly one schema argument")
		}
		return parseObjectInputType(call.Args[0], ctx, sourcePath)
	case "tuple":
		if len(call.Args) != 1 {
			return ComponentInputTypeSpec{}, fmt.Errorf("tuple() requires exactly one schema argument")
		}
		return parseTupleInputType(call.Args[0], ctx, sourcePath)
	case "optional":
		if !allowOptional {
			return ComponentInputTypeSpec{}, fmt.Errorf("optional() is only allowed inside object attribute type declarations")
		}
		if len(call.Args) < 1 || len(call.Args) > 2 {
			return ComponentInputTypeSpec{}, fmt.Errorf("optional() requires one type argument and optional default")
		}
		return parseComponentInputTypeExpr(call.Args[0], ctx, sourcePath, false)
	default:
		return ComponentInputTypeSpec{}, fmt.Errorf("unsupported type constructor %s()", call.Name)
	}
}

func parseObjectInputType(expr hcl.Expression, ctx EvalContext, sourcePath string) (ComponentInputTypeSpec, error) {
	pairs, diags := hcl.ExprMap(expr)
	if diags.HasErrors() {
		return ComponentInputTypeSpec{}, fmt.Errorf("object() argument must be an object schema")
	}
	attrs := make(map[string]ComponentObjectAttrSpec, len(pairs))
	for _, pair := range pairs {
		name, err := objectTypeAttributeName(pair.Key, ctx)
		if err != nil {
			return ComponentInputTypeSpec{}, err
		}
		if _, exists := attrs[name]; exists {
			return ComponentInputTypeSpec{}, fmt.Errorf("duplicate object attribute %q", name)
		}
		attrSourcePath := sourcePath + "." + name
		attr, err := parseObjectAttributeType(name, pair.Value, ctx, attrSourcePath)
		if err != nil {
			return ComponentInputTypeSpec{}, err
		}
		attrs[name] = attr
	}
	return ComponentInputTypeSpec{Kind: ComponentInputTypeObject, Attributes: attrs}, nil
}

func objectTypeAttributeName(expr hcl.Expression, ctx EvalContext) (string, error) {
	if traversal, diags := hcl.AbsTraversalForExpr(expr); !diags.HasErrors() && len(traversal) == 1 {
		if root, ok := traversal[0].(hcl.TraverseRoot); ok {
			return root.Name, nil
		}
	}
	evalCtx, err := hclEvalContext(ctx)
	if err != nil {
		return "", err
	}
	value, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		return "", fmt.Errorf("object attribute name: %s", diags.Error())
	}
	if value.IsNull() || !value.IsKnown() {
		return "", fmt.Errorf("object attribute name must be a known non-null string")
	}
	value, err = convert.Convert(value, cty.String)
	if err != nil {
		return "", fmt.Errorf("object attribute name must be a string: %w", err)
	}
	return value.AsString(), nil
}

func parseObjectAttributeType(name string, expr hcl.Expression, ctx EvalContext, sourcePath string) (ComponentObjectAttrSpec, error) {
	if call, ok := expr.(*hclsyntax.FunctionCallExpr); ok && call.Name == "optional" {
		if len(call.Args) < 1 || len(call.Args) > 2 {
			return ComponentObjectAttrSpec{}, fmt.Errorf("object attribute %q optional() requires one type argument and optional default", name)
		}
		spec, err := parseComponentInputTypeExpr(call.Args[0], ctx, sourcePath, false)
		if err != nil {
			return ComponentObjectAttrSpec{}, fmt.Errorf("object attribute %q: %w", name, err)
		}
		attr := ComponentObjectAttrSpec{Type: spec, Optional: true}
		if len(call.Args) == 2 {
			defaultSource := defaultSourceForExpr(call.Args[1], sourcePath+".default")
			value, err := evalValue(call.Args[1], ctx, defaultSource)
			if err != nil {
				return ComponentObjectAttrSpec{}, fmt.Errorf("object attribute %q optional default: %w", name, err)
			}
			attr.Default = &value
		}
		return attr, nil
	}
	spec, err := parseComponentInputTypeExpr(expr, ctx, sourcePath, false)
	if err != nil {
		return ComponentObjectAttrSpec{}, fmt.Errorf("object attribute %q: %w", name, err)
	}
	return ComponentObjectAttrSpec{Type: spec}, nil
}

func parseTupleInputType(expr hcl.Expression, ctx EvalContext, sourcePath string) (ComponentInputTypeSpec, error) {
	items, diags := hcl.ExprList(expr)
	if diags.HasErrors() {
		return ComponentInputTypeSpec{}, fmt.Errorf("tuple() argument must be a list of types")
	}
	out := make([]ComponentInputTypeSpec, 0, len(items))
	for i, item := range items {
		spec, err := parseComponentInputTypeExpr(item, ctx, fmt.Sprintf("%s[%d]", sourcePath, i), false)
		if err != nil {
			return ComponentInputTypeSpec{}, fmt.Errorf("tuple element %d: %w", i, err)
		}
		out = append(out, spec)
	}
	return ComponentInputTypeSpec{Kind: ComponentInputTypeTuple, Tuple: out}, nil
}

func defaultSourceForExpr(expr hcl.Expression, path string) ir.SourceRef {
	r := expr.Range()
	return ir.SourceRef{File: r.Filename, Line: r.Start.Line, Path: path}
}

func (t ComponentInputTypeSpec) String() string {
	switch t.Kind {
	case ComponentInputTypeString, ComponentInputTypeNumber, ComponentInputTypeBool, ComponentInputTypeAny:
		return string(t.Kind)
	case ComponentInputTypeList, ComponentInputTypeSet, ComponentInputTypeMap:
		if t.Element == nil {
			return string(t.Kind) + "(any)"
		}
		return fmt.Sprintf("%s(%s)", t.Kind, t.Element.String())
	case ComponentInputTypeObject:
		names := make([]string, 0, len(t.Attributes))
		for name := range t.Attributes {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			attr := t.Attributes[name]
			typeExpr := attr.Type.String()
			if attr.Optional {
				if attr.Default != nil {
					typeExpr = fmt.Sprintf("optional(%s,%s)", typeExpr, attr.Default.CanonicalString())
				} else {
					typeExpr = fmt.Sprintf("optional(%s)", typeExpr)
				}
			}
			parts = append(parts, fmt.Sprintf("%s=%s", name, typeExpr))
		}
		return "object({" + strings.Join(parts, ",") + "})"
	case ComponentInputTypeTuple:
		parts := make([]string, 0, len(t.Tuple))
		for _, item := range t.Tuple {
			parts = append(parts, item.String())
		}
		return "tuple([" + strings.Join(parts, ",") + "])"
	default:
		return string(t.Kind)
	}
}
