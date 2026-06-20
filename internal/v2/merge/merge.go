package merge

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func Compile(cfg *parser.Config) (*ir.Program, error) {
	compiler := &compiler{
		cfg:          cfg,
		profileCache: map[string]parser.Value{},
		visiting:     map[string]bool{},
	}

	names := make([]string, 0, len(cfg.Hosts))
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	program := &ir.Program{Hosts: make([]ir.HostSpec, 0, len(names))}
	for _, name := range names {
		host := cfg.Hosts[name]
		raw := parser.MapValue(nil, host.Source)
		for _, importName := range host.Imports {
			profile, err := compiler.resolveProfile(importName, nil)
			if err != nil {
				return nil, err
			}
			merged, err := Merge(raw, profile)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: host %q import profile.%s: %w", host.Source.File, host.Source.Line, host.Name, importName, err)
			}
			raw = merged
		}
		merged, err := Merge(raw, host.Body)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: host %q: %w", host.Source.File, host.Source.Line, host.Name, err)
		}
		spec, err := buildHostSpec(host, merged)
		if err != nil {
			return nil, err
		}
		program.Hosts = append(program.Hosts, spec)
	}
	return program, nil
}

type compiler struct {
	cfg          *parser.Config
	profileCache map[string]parser.Value
	visiting     map[string]bool
}

func (c *compiler) resolveProfile(name string, stack []string) (parser.Value, error) {
	if cached, ok := c.profileCache[name]; ok {
		return cached, nil
	}
	profile, ok := c.cfg.Profiles[name]
	if !ok {
		return parser.Value{}, fmt.Errorf("unknown profile.%s", name)
	}
	if c.visiting[name] {
		cycle := append(append([]string(nil), stack...), name)
		return parser.Value{}, fmt.Errorf("profile import cycle: %s", cycleString(cycle))
	}

	c.visiting[name] = true
	nextStack := append(append([]string(nil), stack...), name)
	raw := parser.MapValue(nil, profile.Source)
	for _, importName := range profile.Imports {
		imported, err := c.resolveProfile(importName, nextStack)
		if err != nil {
			return parser.Value{}, err
		}
		merged, err := Merge(raw, imported)
		if err != nil {
			return parser.Value{}, fmt.Errorf("%s:%d: profile.%s import profile.%s: %w", profile.Source.File, profile.Source.Line, name, importName, err)
		}
		raw = merged
	}
	merged, err := Merge(raw, profile.Body)
	if err != nil {
		return parser.Value{}, fmt.Errorf("%s:%d: profile.%s: %w", profile.Source.File, profile.Source.Line, name, err)
	}
	delete(c.visiting, name)
	c.profileCache[name] = merged
	return merged, nil
}

func cycleString(names []string) string {
	out := ""
	for i, name := range names {
		if i > 0 {
			out += " -> "
		}
		out += "profile." + name
	}
	return out
}

func Merge(base, overlay parser.Value) (parser.Value, error) {
	merged, keep, err := mergeValue(base, overlay)
	if err != nil {
		return parser.Value{}, err
	}
	if !keep {
		return parser.MapValue(nil, overlay.Source), nil
	}
	return merged, nil
}

func mergeValue(base, overlay parser.Value) (parser.Value, bool, error) {
	if overlay.Modifier == parser.ModifierUnset {
		return parser.Value{}, false, nil
	}
	if overlay.Modifier == parser.ModifierForce {
		return overlay.WithoutModifier(), true, nil
	}

	if base.Kind == "" {
		if overlay.Modifier == parser.ModifierBefore || overlay.Modifier == parser.ModifierAfter {
			if !overlay.IsList() {
				return parser.Value{}, true, fmt.Errorf("%s:%d: %s() can only be used with lists", overlay.Source.File, overlay.Source.Line, overlay.Modifier)
			}
			return overlay.WithoutModifier(), true, nil
		}
		return overlay.WithoutModifier(), true, nil
	}

	if base.IsMap() && overlay.IsMap() {
		result := parser.MapValue(copyMap(base.Map), overlay.Source)
		keys := make([]string, 0, len(overlay.Map))
		for key := range overlay.Map {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			merged, keep, err := mergeValue(result.Map[key], overlay.Map[key])
			if err != nil {
				return parser.Value{}, true, err
			}
			if !keep {
				delete(result.Map, key)
				continue
			}
			result.Map[key] = merged
		}
		return result, true, nil
	}

	listModifier := overlay.Modifier
	if listModifier == parser.ModifierBefore || listModifier == parser.ModifierAfter {
		if !overlay.IsList() {
			return parser.Value{}, true, fmt.Errorf("%s:%d: %s() can only be used with lists", overlay.Source.File, overlay.Source.Line, overlay.Modifier)
		}
		if !base.IsList() {
			return overlay.WithoutModifier(), true, nil
		}
	}

	if base.IsList() && overlay.IsList() {
		overlay = overlay.WithoutModifier()
		if listModifier == parser.ModifierBefore {
			return listValue(appendDedup(overlay.List, base.List), overlay.Source), true, nil
		}
		return listValue(appendDedup(base.List, overlay.List), overlay.Source), true, nil
	}

	return overlay.WithoutModifier(), true, nil
}

func copyMap(in map[string]parser.Value) map[string]parser.Value {
	out := make(map[string]parser.Value, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func appendDedup(first, second []parser.Value) []parser.Value {
	out := make([]parser.Value, 0, len(first)+len(second))
	seen := map[string]struct{}{}
	for _, values := range [][]parser.Value{first, second} {
		for _, value := range values {
			key := value.Key()
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func listValue(values []parser.Value, source ir.SourceRef) parser.Value {
	return parser.Value{Kind: parser.KindList, List: values, Source: source}
}

func buildHostSpec(host parser.Host, raw parser.Value) (ir.HostSpec, error) {
	spec := ir.HostSpec{
		Name:   host.Name,
		Source: host.Source,
		SSH: ir.SSHSpec{
			Host:   host.Name,
			Port:   22,
			User:   "root",
			Source: host.Source,
		},
		State: ir.StateSpec{
			Path:     "/var/lib/debianform/state/" + host.Name + ".yaml",
			LockPath: "/var/lock/debianform/state/" + host.Name + ".lock",
			Source:   host.Source,
		},
		System: ir.SystemSpec{
			Hostname: host.Name,
			Source:   host.Source,
		},
		Kernel: ir.KernelSpec{
			Sysctl: map[string]ir.SysctlSpec{},
		},
	}

	if ssh, ok, err := mapField(raw, "ssh"); err != nil {
		return spec, err
	} else if ok {
		spec.SSH.Source = ssh.Source
		if value, ok, err := stringField(ssh, "host"); err != nil {
			return spec, err
		} else if ok {
			spec.SSH.Host = value
		}
		if value, ok, err := intField(ssh, "port"); err != nil {
			return spec, err
		} else if ok {
			spec.SSH.Port = value
		}
		if value, ok, err := stringField(ssh, "user"); err != nil {
			return spec, err
		} else if ok {
			spec.SSH.User = value
		}
		if value, ok, err := stringField(ssh, "identity_file"); err != nil {
			return spec, err
		} else if ok {
			spec.SSH.IdentityFile = value
		}
	}

	if state, ok, err := mapField(raw, "state"); err != nil {
		return spec, err
	} else if ok {
		spec.State.Source = state.Source
		if value, ok, err := stringField(state, "path"); err != nil {
			return spec, err
		} else if ok {
			spec.State.Path = value
		}
		if value, ok, err := stringField(state, "lock_path"); err != nil {
			return spec, err
		} else if ok {
			spec.State.LockPath = value
		}
	}

	if system, ok, err := mapField(raw, "system"); err != nil {
		return spec, err
	} else if ok {
		spec.System.Source = system.Source
		if value, ok, err := stringField(system, "hostname"); err != nil {
			return spec, err
		} else if ok {
			spec.System.Hostname = value
		}
		if value, ok, err := stringField(system, "architecture"); err != nil {
			return spec, err
		} else if ok {
			spec.System.Architecture = value
		}
		if value, ok, err := stringField(system, "codename"); err != nil {
			return spec, err
		} else if ok {
			spec.System.Codename = value
		}
		if value, ok, err := stringField(system, "timezone"); err != nil {
			return spec, err
		} else if ok {
			spec.System.Timezone = value
		}
		if value, ok, err := stringField(system, "locale"); err != nil {
			return spec, err
		} else if ok {
			spec.System.Locale = value
		}
	}

	if kernel, ok, err := mapField(raw, "kernel"); err != nil {
		return spec, err
	} else if ok {
		spec.Kernel.Source = kernel.Source
		modules, err := moduleSpecs(kernel)
		if err != nil {
			return spec, err
		}
		spec.Kernel.Modules = modules
		sysctl, err := sysctlSpecs(kernel)
		if err != nil {
			return spec, err
		}
		spec.Kernel.Sysctl = sysctl
	}

	if packages, ok, err := mapField(raw, "packages"); err != nil {
		return spec, err
	} else if ok {
		spec.Packages.Source = packages.Source
		install, err := packageItems(packages)
		if err != nil {
			return spec, err
		}
		spec.Packages.Install = install
	}

	return spec, nil
}

func mapField(root parser.Value, name string) (parser.Value, bool, error) {
	if !root.IsMap() {
		return parser.Value{}, false, fmt.Errorf("%s:%d: expected object at %s", root.Source.File, root.Source.Line, root.Source.Path)
	}
	value, ok := root.Map[name]
	if !ok {
		return parser.Value{}, false, nil
	}
	if !value.IsMap() {
		return parser.Value{}, false, fmt.Errorf("%s:%d: %s must be an object", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return value, true, nil
}

func stringField(root parser.Value, name string) (string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return "", false, nil
	}
	str, ok := value.StringValue()
	if !ok {
		return "", false, fmt.Errorf("%s:%d: %s must be a string", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return str, true, nil
}

func intField(root parser.Value, name string) (int, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return 0, false, nil
	}
	str, ok := value.StringValue()
	if !ok {
		return 0, false, fmt.Errorf("%s:%d: %s must be a number", value.Source.File, value.Source.Line, value.Source.Path)
	}
	n, err := strconv.Atoi(str)
	if err != nil {
		return 0, false, fmt.Errorf("%s:%d: %s must be an integer", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return n, true, nil
}

func listField(root parser.Value, name string) (parser.Value, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return parser.Value{}, false, nil
	}
	if !value.IsList() {
		return parser.Value{}, false, fmt.Errorf("%s:%d: %s must be a list", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return value, true, nil
}

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
			return nil, fmt.Errorf("%s:%d: %s entries must be non-empty strings", item.Source.File, item.Source.Line, modules.Source.Path)
		}
		if _, exists := seen[name]; exists {
			continue
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
		item := value.Map[key]
		str, ok := item.StringValue()
		if !ok {
			return nil, fmt.Errorf("%s:%d: %s must contain string values", item.Source.File, item.Source.Line, value.Source.Path)
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

func packageItems(packages parser.Value) ([]ir.PackageItem, error) {
	install, ok, err := listField(packages, "install")
	if err != nil || !ok {
		return nil, err
	}
	out := []ir.PackageItem{}
	seen := map[string]struct{}{}
	for _, item := range install.List {
		name, ok := item.StringValue()
		if !ok || name == "" {
			return nil, fmt.Errorf("%s:%d: %s entries must be non-empty strings", item.Source.File, item.Source.Line, install.Source.Path)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, ir.PackageItem{Name: name, Source: item.Source})
	}
	return out, nil
}
