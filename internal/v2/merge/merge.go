package merge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/v2/ir"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func Compile(cfg *parser.Config) (*ir.Program, error) {
	compiler := &compiler{
		cfg:          cfg,
		profileCache: map[string]resolvedProfile{},
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
		asserts := []parser.Assert{}
		for _, importName := range host.Imports {
			profile, err := compiler.resolveProfile(importName, nil)
			if err != nil {
				return nil, err
			}
			merged, err := Merge(raw, profile.Body)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: host %q import profile.%s: %w", host.Source.File, host.Source.Line, host.Name, importName, err)
			}
			raw = merged
			asserts = append(asserts, profile.Asserts...)
		}
		merged, err := Merge(raw, host.Body)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: host %q: %w", host.Source.File, host.Source.Line, host.Name, err)
		}
		asserts = append(asserts, host.Asserts...)
		spec, err := buildHostSpec(host, merged)
		if err != nil {
			return nil, err
		}
		if err := evaluateAssertions(asserts, spec); err != nil {
			return nil, err
		}
		program.Hosts = append(program.Hosts, spec)
	}
	return program, nil
}

type compiler struct {
	cfg          *parser.Config
	profileCache map[string]resolvedProfile
	visiting     map[string]bool
}

type resolvedProfile struct {
	Body    parser.Value
	Asserts []parser.Assert
}

func (c *compiler) resolveProfile(name string, stack []string) (resolvedProfile, error) {
	if cached, ok := c.profileCache[name]; ok {
		return cached, nil
	}
	profile, ok := c.cfg.Profiles[name]
	if !ok {
		return resolvedProfile{}, fmt.Errorf("unknown profile.%s", name)
	}
	if c.visiting[name] {
		cycle := append(append([]string(nil), stack...), name)
		return resolvedProfile{}, fmt.Errorf("%s:%d:%s: profile import cycle: %s", profile.Source.File, profile.Source.Line, profile.Source.Path, cycleString(cycle))
	}

	c.visiting[name] = true
	nextStack := append(append([]string(nil), stack...), name)
	raw := parser.MapValue(nil, profile.Source)
	asserts := []parser.Assert{}
	for _, importName := range profile.Imports {
		imported, err := c.resolveProfile(importName, nextStack)
		if err != nil {
			return resolvedProfile{}, err
		}
		merged, err := Merge(raw, imported.Body)
		if err != nil {
			return resolvedProfile{}, fmt.Errorf("%s:%d:%s: profile.%s import profile.%s: %w", profile.Source.File, profile.Source.Line, profile.Source.Path, name, importName, err)
		}
		raw = merged
		asserts = append(asserts, imported.Asserts...)
	}
	merged, err := Merge(raw, profile.Body)
	if err != nil {
		return resolvedProfile{}, fmt.Errorf("%s:%d:%s: profile.%s: %w", profile.Source.File, profile.Source.Line, profile.Source.Path, name, err)
	}
	asserts = append(asserts, profile.Asserts...)
	delete(c.visiting, name)
	resolved := resolvedProfile{Body: merged, Asserts: asserts}
	c.profileCache[name] = resolved
	return resolved, nil
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
		if base.IsList() || isKnownListPath(overlay.Source.Path) {
			return parser.Value{}, true, fmt.Errorf("%s:%d:%s: unset() cannot be used on lists; use force([]) to clear a list", overlay.Source.File, overlay.Source.Line, overlay.Source.Path)
		}
		return parser.Value{}, false, nil
	}
	if overlay.Modifier == parser.ModifierForce {
		return overlay.WithoutModifier(), true, nil
	}

	if base.Kind == "" {
		if overlay.Modifier == parser.ModifierBefore || overlay.Modifier == parser.ModifierAfter {
			if !overlay.IsList() {
				return parser.Value{}, true, fmt.Errorf("%s:%d:%s: %s() can only be used with lists", overlay.Source.File, overlay.Source.Line, overlay.Source.Path, overlay.Modifier)
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
			return parser.Value{}, true, fmt.Errorf("%s:%d:%s: %s() can only be used with lists", overlay.Source.File, overlay.Source.Line, overlay.Source.Path, overlay.Modifier)
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

func isKnownListPath(path string) bool {
	for _, suffix := range []string{".install", ".modules", ".repositories"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
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
		Files: ir.FileSpec{
			Files: map[string]ir.ManagedFile{},
		},
		Secrets: ir.SecretSpec{
			Files: map[string]ir.SecretFile{},
		},
		Directories: ir.DirectorySpec{
			Directories: map[string]ir.ManagedDirectory{},
		},
		Groups: ir.GroupSpec{
			Groups: map[string]ir.ManagedGroup{},
		},
		Users: ir.UserSpec{
			Users: map[string]ir.ManagedUser{},
		},
		Systemd: ir.SystemdSpec{
			Units: map[string]ir.SystemdUnit{},
		},
		Services: ir.ServiceSpec{
			Services: map[string]ir.ManagedService{},
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

	if files, ok, err := mapField(raw, "files"); err != nil {
		return spec, err
	} else if ok {
		spec.Files.Source = files.Source
		managedFiles, err := fileSpecs(files)
		if err != nil {
			return spec, err
		}
		spec.Files.Files = managedFiles
	}

	if secrets, ok, err := mapField(raw, "secrets"); err != nil {
		return spec, err
	} else if ok {
		spec.Secrets.Source = secrets.Source
		secretFiles, err := secretSpecs(secrets)
		if err != nil {
			return spec, err
		}
		spec.Secrets.Files = secretFiles
	}

	if directories, ok, err := mapField(raw, "directories"); err != nil {
		return spec, err
	} else if ok {
		spec.Directories.Source = directories.Source
		managedDirectories, err := directorySpecs(directories)
		if err != nil {
			return spec, err
		}
		spec.Directories.Directories = managedDirectories
	}

	if groups, ok, err := mapField(raw, "groups"); err != nil {
		return spec, err
	} else if ok {
		spec.Groups.Source = groups.Source
		managedGroups, err := groupSpecs(groups)
		if err != nil {
			return spec, err
		}
		spec.Groups.Groups = managedGroups
	}

	if users, ok, err := mapField(raw, "users"); err != nil {
		return spec, err
	} else if ok {
		spec.Users.Source = users.Source
		managedUsers, err := userSpecs(users)
		if err != nil {
			return spec, err
		}
		spec.Users.Users = managedUsers
	}

	if systemd, ok, err := mapField(raw, "systemd"); err != nil {
		return spec, err
	} else if ok {
		spec.Systemd.Source = systemd.Source
		units, err := systemdSpecs(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Units = units
	}

	if services, ok, err := mapField(raw, "services"); err != nil {
		return spec, err
	} else if ok {
		spec.Services.Source = services.Source
		managedServices, err := serviceSpecs(services)
		if err != nil {
			return spec, err
		}
		spec.Services.Services = managedServices
	}

	if err := validateHostSpec(spec); err != nil {
		return spec, err
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

func boolField(root parser.Value, name string) (bool, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return false, false, nil
	}
	if value.Kind != parser.KindBool {
		return false, false, fmt.Errorf("%s:%d:%s: must be a boolean", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return value.Bool, true, nil
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

func objectCollection(root parser.Value, field string) (map[string]parser.Value, bool, error) {
	collection, ok := root.Map[field]
	if !ok {
		return nil, false, nil
	}
	if !collection.IsMap() {
		return nil, false, fmt.Errorf("%s:%d:%s: must be a map", collection.Source.File, collection.Source.Line, collection.Source.Path)
	}
	return collection.Map, true, nil
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

func packageItems(packages parser.Value) ([]ir.PackageItem, error) {
	out := []ir.PackageItem{}
	seen := map[string]ir.SourceRef{}

	install, ok, err := listField(packages, "install")
	if err != nil {
		return nil, err
	}
	if ok {
		for _, item := range install.List {
			name, ok := item.StringValue()
			if !ok || name == "" {
				return nil, fmt.Errorf("%s:%d:%s: package install entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path)
			}
			if previous, exists := seen[name]; exists {
				return nil, fmt.Errorf("%s:%d:%s: duplicate package %q; first declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, name, previous.File, previous.Line, previous.Path)
			}
			seen[name] = item.Source
			out = append(out, ir.PackageItem{Name: name, Source: item.Source})
		}
	}

	packageBlocks, ok := packages.Map["package"]
	if !ok {
		return out, nil
	}
	if !packageBlocks.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: package must be a map", packageBlocks.Source.File, packageBlocks.Source.Line, packageBlocks.Source.Path)
	}
	names := make([]string, 0, len(packageBlocks.Map))
	for name := range packageBlocks.Map {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		item := packageBlocks.Map[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: package label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if previous, exists := seen[name]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate package %q; first declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, name, previous.File, previous.Line, previous.Path)
		}
		repositories, err := stringListField(item, "repositories")
		if err != nil {
			return nil, err
		}
		seen[name] = item.Source
		out = append(out, ir.PackageItem{Name: name, Repositories: repositories, Source: item.Source})
	}
	return out, nil
}

func stringListField(root parser.Value, name string) ([]string, error) {
	list, ok, err := listField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	out := make([]string, 0, len(list.List))
	seen := map[string]struct{}{}
	for _, item := range list.List {
		value, ok := item.StringValue()
		if !ok || value == "" {
			return nil, fmt.Errorf("%s:%d:%s: %s entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path, list.Source.Path)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate %s entry %q", item.Source.File, item.Source.Line, item.Source.Path, list.Source.Path, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func fileSpecs(files parser.Value) (map[string]ir.ManagedFile, error) {
	objects, ok, err := objectCollection(files, "file")
	if err != nil || !ok {
		return map[string]ir.ManagedFile{}, err
	}
	out := make(map[string]ir.ManagedFile, len(objects))
	for _, path := range sortedKeys(objects) {
		item := objects[path]
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: file path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		content, hasContent, err := stringField(item, "content")
		if err != nil {
			return nil, err
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && hasContent == hasSource {
			return nil, fmt.Errorf("%s:%d:%s: files.file requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		owner, err := stringFieldDefault(item, "owner", "root")
		if err != nil {
			return nil, err
		}
		group, err := stringFieldDefault(item, "group", "root")
		if err != nil {
			return nil, err
		}
		mode, err := modeFieldDefault(item, "mode", "0644")
		if err != nil {
			return nil, err
		}
		sensitive, ok, err := boolField(item, "sensitive")
		if err != nil {
			return nil, err
		}
		if !ok {
			sensitive = false
		}
		managed := ir.ManagedFile{
			Path:       path,
			Content:    content,
			SourcePath: resolvePath(item.Source.File, sourcePath),
			Owner:      owner,
			Group:      group,
			Mode:       mode,
			Sensitive:  sensitive,
			Ensure:     ensure,
			Source:     item.Source,
		}
		if hasContent {
			managed.Summary = contentSummary([]byte(content))
		} else if hasSource {
			summary, err := fileSummary(managed.SourcePath, item.Source)
			if err != nil {
				return nil, err
			}
			managed.Summary = summary
		}
		out[path] = managed
	}
	return out, nil
}

func secretSpecs(secrets parser.Value) (map[string]ir.SecretFile, error) {
	objects, ok, err := objectCollection(secrets, "file")
	if err != nil || !ok {
		return map[string]ir.SecretFile{}, err
	}
	out := make(map[string]ir.SecretFile, len(objects))
	for _, path := range sortedKeys(objects) {
		item := objects[path]
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: secret path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && !hasSource {
			return nil, fmt.Errorf("%s:%d:%s: secrets.file requires source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		owner, err := stringFieldDefault(item, "owner", "root")
		if err != nil {
			return nil, err
		}
		group, err := stringFieldDefault(item, "group", "root")
		if err != nil {
			return nil, err
		}
		mode, err := modeFieldDefault(item, "mode", "0600")
		if err != nil {
			return nil, err
		}
		secret := ir.SecretFile{
			Path:       path,
			SourcePath: resolvePath(item.Source.File, sourcePath),
			Owner:      owner,
			Group:      group,
			Mode:       mode,
			Ensure:     ensure,
			Source:     item.Source,
		}
		if hasSource {
			summary, err := fileSummary(secret.SourcePath, item.Source)
			if err != nil {
				return nil, err
			}
			secret.Summary = summary
		}
		out[path] = secret
	}
	return out, nil
}

func directorySpecs(directories parser.Value) (map[string]ir.ManagedDirectory, error) {
	objects, ok, err := objectCollection(directories, "directory")
	if err != nil || !ok {
		return map[string]ir.ManagedDirectory{}, err
	}
	out := make(map[string]ir.ManagedDirectory, len(objects))
	for _, path := range sortedKeys(objects) {
		item := objects[path]
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: directory path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		owner, err := stringFieldDefault(item, "owner", "root")
		if err != nil {
			return nil, err
		}
		group, err := stringFieldDefault(item, "group", "root")
		if err != nil {
			return nil, err
		}
		mode, err := modeFieldDefault(item, "mode", "0755")
		if err != nil {
			return nil, err
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		out[path] = ir.ManagedDirectory{Path: path, Owner: owner, Group: group, Mode: mode, Ensure: ensure, Source: item.Source}
	}
	return out, nil
}

func groupSpecs(groups parser.Value) (map[string]ir.ManagedGroup, error) {
	objects, ok, err := objectCollection(groups, "group")
	if err != nil || !ok {
		return map[string]ir.ManagedGroup{}, err
	}
	out := make(map[string]ir.ManagedGroup, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: group name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		system, ok, err := boolField(item, "system")
		if err != nil {
			return nil, err
		}
		if !ok {
			system = false
		}
		gid, _, err := stringField(item, "gid")
		if err != nil {
			return nil, err
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		out[name] = ir.ManagedGroup{Name: name, GID: gid, System: system, Ensure: ensure, Source: item.Source}
	}
	return out, nil
}

func userSpecs(users parser.Value) (map[string]ir.ManagedUser, error) {
	objects, ok, err := objectCollection(users, "user")
	if err != nil || !ok {
		return map[string]ir.ManagedUser{}, err
	}
	out := make(map[string]ir.ManagedUser, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: user name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		system, ok, err := boolField(item, "system")
		if err != nil {
			return nil, err
		}
		if !ok {
			system = false
		}
		uid, _, err := stringField(item, "uid")
		if err != nil {
			return nil, err
		}
		home, _, err := stringField(item, "home")
		if err != nil {
			return nil, err
		}
		shell, _, err := stringField(item, "shell")
		if err != nil {
			return nil, err
		}
		primaryGroup, _, err := stringField(item, "group")
		if err != nil {
			return nil, err
		}
		groups, err := stringListField(item, "groups")
		if err != nil {
			return nil, err
		}
		keys, err := stringListField(item, "ssh_authorized_keys")
		if err != nil {
			return nil, err
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		out[name] = ir.ManagedUser{
			Name:              name,
			UID:               uid,
			PrimaryGroup:      primaryGroup,
			Groups:            groups,
			System:            system,
			Home:              home,
			Shell:             shell,
			SSHAuthorizedKeys: keys,
			Ensure:            ensure,
			Source:            item.Source,
		}
	}
	return out, nil
}

func systemdSpecs(systemd parser.Value) (map[string]ir.SystemdUnit, error) {
	objects, ok, err := objectCollection(systemd, "unit")
	if err != nil || !ok {
		return map[string]ir.SystemdUnit{}, err
	}
	out := make(map[string]ir.SystemdUnit, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: systemd unit name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		content, hasContent, err := stringField(item, "content")
		if err != nil {
			return nil, err
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && hasContent == hasSource {
			return nil, fmt.Errorf("%s:%d:%s: systemd.unit requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		owner, err := stringFieldDefault(item, "owner", "root")
		if err != nil {
			return nil, err
		}
		group, err := stringFieldDefault(item, "group", "root")
		if err != nil {
			return nil, err
		}
		mode, err := modeFieldDefault(item, "mode", "0644")
		if err != nil {
			return nil, err
		}
		unit := ir.SystemdUnit{
			Name:       name,
			Path:       "/etc/systemd/system/" + name,
			Content:    content,
			SourcePath: resolvePath(item.Source.File, sourcePath),
			Owner:      owner,
			Group:      group,
			Mode:       mode,
			Ensure:     ensure,
			Source:     item.Source,
		}
		if hasContent {
			unit.Summary = contentSummary([]byte(content))
		} else if hasSource {
			summary, err := fileSummary(unit.SourcePath, item.Source)
			if err != nil {
				return nil, err
			}
			unit.Summary = summary
		}
		out[name] = unit
	}
	return out, nil
}

func serviceSpecs(services parser.Value) (map[string]ir.ManagedService, error) {
	objects, ok, err := objectCollection(services, "service")
	if err != nil || !ok {
		return map[string]ir.ManagedService{}, err
	}
	out := make(map[string]ir.ManagedService, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: service name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		pkg, _, err := stringField(item, "package")
		if err != nil {
			return nil, err
		}
		enabledValue, hasEnabled, err := boolField(item, "enabled")
		if err != nil {
			return nil, err
		}
		var enabled *bool
		if hasEnabled {
			enabled = &enabledValue
		}
		state, _, err := stringField(item, "state")
		if err != nil {
			return nil, err
		}
		if state != "" && !validServiceState(state) {
			return nil, fmt.Errorf("%s:%d:%s.state: service state must be running, stopped, restarted, or reloaded", item.Source.File, item.Source.Line, item.Source.Path)
		}
		out[name] = ir.ManagedService{Name: name, Unit: serviceUnitName(name), Package: pkg, Enabled: enabled, State: state, Source: item.Source}
	}
	return out, nil
}

func validateHostSpec(spec ir.HostSpec) error {
	for path, secret := range spec.Secrets.Files {
		if file, exists := spec.Files.Files[path]; exists {
			return fmt.Errorf("%s:%d:%s: file path %q conflicts with secret declared at %s:%d:%s", file.Source.File, file.Source.Line, file.Source.Path, path, secret.Source.File, secret.Source.Line, secret.Source.Path)
		}
	}

	for _, user := range spec.Users.Users {
		if user.PrimaryGroup == "" {
			continue
		}
		if _, ok := spec.Groups.Groups[user.PrimaryGroup]; !ok && user.PrimaryGroup != user.Name {
			return fmt.Errorf("%s:%d:%s: user %q references missing primary group %q", user.Source.File, user.Source.Line, user.Source.Path, user.Name, user.PrimaryGroup)
		}
	}
	return nil
}

func stringFieldDefault(root parser.Value, name string, fallback string) (string, error) {
	value, ok, err := stringField(root, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return fallback, nil
	}
	return value, nil
}

func ensureField(root parser.Value, fallback string) (string, error) {
	ensure, ok, err := stringField(root, "ensure")
	if err != nil {
		return "", err
	}
	if !ok || ensure == "" {
		return fallback, nil
	}
	if ensure != "present" && ensure != "absent" {
		return "", fmt.Errorf("%s:%d:%s.ensure: ensure must be present or absent", root.Source.File, root.Source.Line, root.Source.Path)
	}
	return ensure, nil
}

func modeFieldDefault(root parser.Value, name string, fallback string) (string, error) {
	mode, err := stringFieldDefault(root, name, fallback)
	if err != nil {
		return "", err
	}
	if !modePattern.MatchString(mode) {
		return "", fmt.Errorf("%s:%d:%s.%s: mode must be a four digit octal string", root.Source.File, root.Source.Line, root.Source.Path, name)
	}
	return mode, nil
}

var modePattern = regexp.MustCompile(`^0[0-7]{3}$`)

func validServiceState(state string) bool {
	switch state {
	case "running", "stopped", "restarted", "reloaded":
		return true
	default:
		return false
	}
}

func serviceUnitName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".service"
}

func resolvePath(sourceFile string, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(filepath.Dir(sourceFile), value)
}

func fileSummary(path string, source ir.SourceRef) (ir.ContentSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ir.ContentSummary{}, fmt.Errorf("%s:%d:%s: read source %q: %w", source.File, source.Line, source.Path, path, err)
	}
	return contentSummary(data), nil
}

func contentSummary(data []byte) ir.ContentSummary {
	sum := sha256.Sum256(data)
	return ir.ContentSummary{SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
