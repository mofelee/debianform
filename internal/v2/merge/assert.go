package merge

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/parser"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

func evaluateAssertions(asserts []parser.Assert, host ir.HostSpec) error {
	if len(asserts) == 0 {
		return nil
	}
	self := hostSpecToCty(host)
	for _, assert := range asserts {
		value, diags := assert.Condition.Value(&hcl.EvalContext{
			Variables: map[string]cty.Value{
				"self": self,
			},
			Functions: map[string]function.Function{
				"contains": containsFunction(),
			},
		})
		if diags.HasErrors() {
			return fmt.Errorf("%s:%d:%s: assert condition: %s", assert.ConditionSource.File, assert.ConditionSource.Line, assert.ConditionSource.Path, diags.Error())
		}
		value, _ = value.UnmarkDeep()
		if !value.IsKnown() || value.IsNull() || !value.Type().Equals(cty.Bool) {
			return fmt.Errorf("%s:%d:%s: assert condition must evaluate to a boolean", assert.ConditionSource.File, assert.ConditionSource.Line, assert.ConditionSource.Path)
		}
		if !value.True() {
			return fmt.Errorf("%s:%d:%s: assertion failed: %s", assert.Source.File, assert.Source.Line, assert.Source.Path, assert.Message)
		}
	}
	return nil
}

func hostSpecToCty(host ir.HostSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"name":     cty.StringVal(host.Name),
		"ssh":      sshSpecToCty(host.SSH),
		"state":    stateSpecToCty(host.State),
		"system":   systemSpecToCty(host.System),
		"kernel":   kernelSpecToCty(host.Kernel),
		"packages": packageSpecToCty(host.Packages),
	})
}

func sshSpecToCty(spec ir.SSHSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"host":          cty.StringVal(spec.Host),
		"port":          cty.NumberIntVal(int64(spec.Port)),
		"user":          cty.StringVal(spec.User),
		"identity_file": cty.StringVal(spec.IdentityFile),
	})
}

func stateSpecToCty(spec ir.StateSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"path":      cty.StringVal(spec.Path),
		"lock_path": cty.StringVal(spec.LockPath),
	})
}

func systemSpecToCty(spec ir.SystemSpec) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"hostname":     cty.StringVal(spec.Hostname),
		"architecture": cty.StringVal(spec.Architecture),
		"codename":     cty.StringVal(spec.Codename),
		"timezone":     cty.StringVal(spec.Timezone),
		"locale":       cty.StringVal(spec.Locale),
	})
}

func kernelSpecToCty(spec ir.KernelSpec) cty.Value {
	modules := make([]cty.Value, 0, len(spec.Modules))
	for _, module := range spec.Modules {
		modules = append(modules, cty.StringVal(module.Name))
	}

	sysctl := make(map[string]cty.Value, len(spec.Sysctl))
	keys := make([]string, 0, len(spec.Sysctl))
	for key := range spec.Sysctl {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sysctl[key] = cty.StringVal(spec.Sysctl[key].Value)
	}

	return cty.ObjectVal(map[string]cty.Value{
		"modules": stringTuple(modules),
		"sysctl":  objectOrEmpty(sysctl),
	})
}

func packageSpecToCty(spec ir.PackageSpec) cty.Value {
	install := make([]cty.Value, 0, len(spec.Install))
	for _, item := range spec.Install {
		install = append(install, cty.StringVal(item.Name))
	}
	return cty.ObjectVal(map[string]cty.Value{
		"install": stringTuple(install),
	})
}

func stringTuple(values []cty.Value) cty.Value {
	if len(values) == 0 {
		return cty.EmptyTupleVal
	}
	return cty.TupleVal(values)
}

func objectOrEmpty(values map[string]cty.Value) cty.Value {
	if len(values) == 0 {
		return cty.EmptyObjectVal
	}
	return cty.ObjectVal(values)
}

func containsFunction() function.Function {
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
			it := collection.ElementIterator()
			for it.Next() {
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
