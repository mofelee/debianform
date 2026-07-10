package merge

import (
	"fmt"
	"sort"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func moduleSpecs(kernel parser.Value) ([]ir.KernelModuleSpec, error) {
	modules, ok, err := listField(kernel, "modules")
	if err != nil || !ok {
		return nil, err
	}
	out := []ir.KernelModuleSpec{}
	seen := map[string]struct{}{}
	for _, item := range modules.List {
		name, ok := item.StringValue()
		if !ok || name == "" {
			return nil, fmt.Errorf("%s:%d:%s: kernel module entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate kernel module %q", item.Source.File, item.Source.Line, item.Source.Path, name)
		}
		seen[name] = struct{}{}
		out = append(out, ir.KernelModuleSpec{
			Name:    name,
			Persist: true,
			Ensure:  "present",
			Source:  item.Source,
		})
	}
	return out, nil
}

func sysctlSpecs(kernel parser.Value) (map[string]ir.SysctlSpec, error) {
	value, ok := kernel.Map["sysctl"]
	if !ok {
		return map[string]ir.SysctlSpec{}, nil
	}
	if !value.IsMap() {
		return nil, fmt.Errorf("%s:%d: %s must be a map", value.Source.File, value.Source.Line, value.Source.Path)
	}
	out := make(map[string]ir.SysctlSpec, len(value.Map))
	keys := make([]string, 0, len(value.Map))
	for key := range value.Map {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "" {
			item := value.Map[key]
			return nil, fmt.Errorf("%s:%d:%s: sysctl key must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		item := value.Map[key]
		str, ok := item.StringValue()
		if !ok {
			return nil, fmt.Errorf("%s:%d:%s: sysctl value must be a string", item.Source.File, item.Source.Line, item.Source.Path)
		}
		out[key] = ir.SysctlSpec{
			Key:          key,
			Value:        str,
			Persist:      true,
			ApplyRuntime: true,
			Source:       item.Source,
		}
	}
	return out, nil
}
