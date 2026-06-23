package merge

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/parser"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

func evaluateComponentInputValidations(input parser.ComponentInput, value parser.Value) error {
	if len(input.Validations) == 0 {
		return nil
	}
	converted, err := value.ToCty()
	if err != nil {
		return fmt.Errorf("%s:%d:%s: component input %q: %w", value.Source.File, value.Source.Line, value.Source.Path, input.Name, err)
	}
	converted, _ = converted.UnmarkDeep()
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"input": cty.ObjectVal(map[string]cty.Value{
				input.Name: converted,
			}),
		},
		Functions: componentInputValidationFunctions(),
	}
	for i, validation := range input.Validations {
		if err := validateComponentInputValidationReferences(input, validation); err != nil {
			return err
		}
		result, diags := validation.Condition.Value(ctx)
		if diags.HasErrors() {
			return fmt.Errorf("%s:%d:%s: input validation condition: %s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, diags.Error())
		}
		result, _ = result.UnmarkDeep()
		if !result.IsKnown() || result.IsNull() || !result.Type().Equals(cty.Bool) {
			return fmt.Errorf("%s:%d:%s: input validation condition must evaluate to a boolean", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path)
		}
		if !result.True() {
			return fmt.Errorf("%s:%d:%s: validation failed for input %q: %s", validation.Source.File, validation.Source.Line, fmt.Sprintf("%s.validation[%d]", input.Source.Path, i), input.Name, validation.Message)
		}
	}
	return nil
}

func validateComponentInputValidationReferences(input parser.ComponentInput, validation parser.ComponentInputValidation) error {
	for _, traversal := range validation.Condition.Variables() {
		if len(traversal) == 0 {
			continue
		}
		root, ok := traversal[0].(hcl.TraverseRoot)
		if !ok {
			continue
		}
		if root.Name != "input" {
			return fmt.Errorf("%s:%d:%s: input validation can only read input.%s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, input.Name)
		}
		if len(traversal) < 2 {
			return fmt.Errorf("%s:%d:%s: input validation can only read input.%s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, input.Name)
		}
		attr, ok := traversal[1].(hcl.TraverseAttr)
		if !ok || attr.Name != input.Name {
			return fmt.Errorf("%s:%d:%s: input validation can only read input.%s", validation.ConditionSource.File, validation.ConditionSource.Line, validation.ConditionSource.Path, input.Name)
		}
	}
	return nil
}

func componentInputValidationSpecs(validations []parser.ComponentInputValidation) []ir.ComponentInputValidationSpec {
	if len(validations) == 0 {
		return nil
	}
	out := make([]ir.ComponentInputValidationSpec, 0, len(validations))
	for _, validation := range validations {
		out = append(out, ir.ComponentInputValidationSpec{
			ConditionSource: validation.ConditionSource,
			Message:         validation.Message,
			MessageSource:   validation.MessageSource,
		})
	}
	return out
}

func variableValidationSpecs(validations []parser.VariableValidation) []ir.ComponentInputValidationSpec {
	if len(validations) == 0 {
		return nil
	}
	out := make([]ir.ComponentInputValidationSpec, 0, len(validations))
	for _, validation := range validations {
		out = append(out, ir.ComponentInputValidationSpec{
			ConditionSource: validation.ConditionSource,
			Message:         validation.Message,
			MessageSource:   validation.MessageSource,
		})
	}
	return out
}

func componentInputValidationFunctions() map[string]function.Function {
	return map[string]function.Function{
		"length":     stdlib.LengthFunc,
		"contains":   containsFunction(),
		"startswith": startsWithFunction(),
		"endswith":   endsWithFunction(),
		"alltrue":    allTrueFunction(),
		"anytrue":    anyTrueFunction(),
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

func startsWithFunction() function.Function {
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

func endsWithFunction() function.Function {
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

func allTrueFunction() function.Function {
	return boolCollectionFunction("alltrue", true)
}

func anyTrueFunction() function.Function {
	return boolCollectionFunction("anytrue", false)
}

func boolCollectionFunction(name string, all bool) function.Function {
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
			if all {
				for it := collection.ElementIterator(); it.Next(); {
					_, item := it.Element()
					item, _ = item.UnmarkDeep()
					if !item.IsKnown() || item.IsNull() || !item.Type().Equals(cty.Bool) {
						return cty.NilVal, fmt.Errorf("%s() entries must be booleans", name)
					}
					if item.False() {
						return cty.BoolVal(false), nil
					}
				}
				return cty.BoolVal(true), nil
			}
			for it := collection.ElementIterator(); it.Next(); {
				_, item := it.Element()
				item, _ = item.UnmarkDeep()
				if !item.IsKnown() || item.IsNull() || !item.Type().Equals(cty.Bool) {
					return cty.NilVal, fmt.Errorf("%s() entries must be booleans", name)
				}
				if item.True() {
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

func regexMatches(pattern string, value string) bool {
	return regexp.MustCompile(pattern).MatchString(value)
}
