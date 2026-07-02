package parser

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

func validateVariableValues(cfg *Config) error {
	for _, name := range sortedVariableNames(cfg.Variables) {
		variable := cfg.Variables[name]
		value, ok := cfg.VariableValues[name]
		if !ok {
			continue
		}
		if err := EvaluateVariableValidations(variable, value); err != nil {
			return err
		}
	}
	return nil
}

func EvaluateVariableValidations(variable Variable, value Value) error {
	if len(variable.Validations) == 0 {
		return nil
	}
	converted, err := value.ToCty()
	if err != nil {
		return fmt.Errorf("%s:%d:%s: variable %q: %w", value.Source.File, value.Source.Line, value.Source.Path, variable.Name, err)
	}
	converted, _ = converted.UnmarkDeep()
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.ObjectVal(map[string]cty.Value{
				variable.Name: converted,
			}),
		},
		Functions: validationFunctions(),
	}
	for i, validation := range variable.Validations {
		if err := validateVariableValidationReferences(variable, validation); err != nil {
			return err
		}
		result, diags := validation.Condition.Value(ctx)
		if diags.HasErrors() {
			return fmt.Errorf("%s:%d:%s: variable validation condition: %s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, diags.Error())
		}
		result, _ = result.UnmarkDeep()
		if !result.IsKnown() || result.IsNull() || !result.Type().Equals(cty.Bool) {
			return fmt.Errorf("%s:%d:%s: variable validation condition must evaluate to a boolean", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path)
		}
		if !result.True() {
			return fmt.Errorf("%s:%d:%s: validation failed for variable %q: %s", validation.Source.File, validation.Source.Line, fmt.Sprintf("%s.validation[%d]", variable.Source.Path, i), variable.Name, validation.Message)
		}
	}
	return nil
}

func validateVariableValidationReferences(variable Variable, validation VariableValidation) error {
	for _, traversal := range validation.Condition.Variables() {
		if len(traversal) == 0 {
			continue
		}
		root, ok := traversal[0].(hcl.TraverseRoot)
		if !ok {
			continue
		}
		if root.Name != "var" {
			return fmt.Errorf("%s:%d:%s: variable validation can only read var.%s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, variable.Name)
		}
		if len(traversal) < 2 {
			return fmt.Errorf("%s:%d:%s: variable validation can only read var.%s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, variable.Name)
		}
		attr, ok := traversal[1].(hcl.TraverseAttr)
		if !ok || attr.Name != variable.Name {
			return fmt.Errorf("%s:%d:%s: variable validation can only read var.%s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, variable.Name)
		}
	}
	return nil
}

func validationFunctions() map[string]function.Function {
	return map[string]function.Function{
		"length":     stdlib.LengthFunc,
		"contains":   validationContainsFunction(),
		"startswith": validationStartsWithFunction(),
		"endswith":   validationEndsWithFunction(),
		"alltrue":    validationAllTrueFunction(),
		"anytrue":    validationAnyTrueFunction(),
		"distinct":   stdlib.DistinctFunc,
		"sort":       stdlib.SortFunc,
		"keys":       stdlib.KeysFunc,
		"values":     stdlib.ValuesFunc,
		"flatten":    stdlib.FlattenFunc,
		"toset":      validationToSetFunction(),
		"tonumber":   stdlib.MakeToFunc(cty.Number),
		"tostring":   stdlib.MakeToFunc(cty.String),
		"tobool":     stdlib.MakeToFunc(cty.Bool),
		"regex":      stdlib.RegexFunc,
		"can":        tryfunc.CanFunc,
	}
}

func validationStartsWithFunction() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "str", Type: cty.String},
			{Name: "prefix", Type: cty.String},
		},
		Type: function.StaticReturnType(cty.Bool),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			return cty.BoolVal(strings.HasPrefix(args[0].AsString(), args[1].AsString())), nil
		},
	})
}

func validationEndsWithFunction() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "str", Type: cty.String},
			{Name: "suffix", Type: cty.String},
		},
		Type: function.StaticReturnType(cty.Bool),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			return cty.BoolVal(strings.HasSuffix(args[0].AsString(), args[1].AsString())), nil
		},
	})
}

func validationAllTrueFunction() function.Function {
	return validationBoolCollectionFunction("alltrue", true)
}

func validationAnyTrueFunction() function.Function {
	return validationBoolCollectionFunction("anytrue", false)
}

func validationBoolCollectionFunction(name string, all bool) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "collection", Type: cty.DynamicPseudoType},
		},
		Type: function.StaticReturnType(cty.Bool),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			collection, _ := args[0].UnmarkDeep()
			if !collection.IsKnown() || collection.IsNull() {
				return cty.NilVal, fmt.Errorf("%s() collection must be known and non-null", name)
			}
			ty := collection.Type()
			if !ty.IsTupleType() && !ty.IsListType() && !ty.IsSetType() {
				return cty.NilVal, fmt.Errorf("%s() collection must be a list, tuple, or set", name)
			}
			for it := collection.ElementIterator(); it.Next(); {
				_, item := it.Element()
				item, _ = item.UnmarkDeep()
				if !item.IsKnown() || item.IsNull() || !item.Type().Equals(cty.Bool) {
					return cty.NilVal, fmt.Errorf("%s() entries must be booleans", name)
				}
				if all && item.False() {
					return cty.BoolVal(false), nil
				}
				if !all && item.True() {
					return cty.BoolVal(true), nil
				}
			}
			return cty.BoolVal(all), nil
		},
	})
}

func validationContainsFunction() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "collection", Type: cty.DynamicPseudoType},
			{Name: "value", Type: cty.DynamicPseudoType},
		},
		Type: function.StaticReturnType(cty.Bool),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			collection, _ := args[0].UnmarkDeep()
			needle, _ := args[1].UnmarkDeep()
			if !collection.IsKnown() || collection.IsNull() {
				return cty.NilVal, fmt.Errorf("contains() collection must be known and non-null")
			}
			ty := collection.Type()
			if !ty.IsTupleType() && !ty.IsListType() && !ty.IsSetType() {
				return cty.NilVal, fmt.Errorf("contains() collection must be a list, tuple, or set")
			}
			for it := collection.ElementIterator(); it.Next(); {
				_, item := it.Element()
				item, _ = item.UnmarkDeep()
				if item.RawEquals(needle) {
					return cty.BoolVal(true), nil
				}
			}
			return cty.BoolVal(false), nil
		},
	})
}

func validationToSetFunction() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "value", Type: cty.DynamicPseudoType},
		},
		Type: function.StaticReturnType(cty.Set(cty.String)),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			value, _ := args[0].UnmarkDeep()
			if !value.IsKnown() || value.IsNull() {
				return cty.NilVal, fmt.Errorf("toset() argument must be known and non-null")
			}
			ty := value.Type()
			if !ty.IsTupleType() && !ty.IsListType() && !ty.IsSetType() {
				return cty.NilVal, fmt.Errorf("toset() argument must be a list, tuple, or set")
			}
			seen := map[string]struct{}{}
			out := []cty.Value{}
			for it := value.ElementIterator(); it.Next(); {
				_, item := it.Element()
				item, _ = item.UnmarkDeep()
				if !item.IsKnown() || item.IsNull() {
					return cty.NilVal, fmt.Errorf("toset() entries must be known and non-null")
				}
				converted, err := convert.Convert(item, cty.String)
				if err != nil {
					return cty.NilVal, fmt.Errorf("toset() entries must be convertible to string: %w", err)
				}
				key := converted.AsString()
				if _, ok := seen[key]; ok {
					continue
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
