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
	"github.com/zclconf/go-cty/cty"
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
		components, err := compiler.instantiateComponents(host.Components, spec)
		if err != nil {
			return nil, err
		}
		spec.Components = components
		if err := validateHostSpec(spec); err != nil {
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

func (c *compiler) instantiateComponents(instances []parser.ComponentInstance, target ir.HostSpec) ([]ir.ComponentInstanceSpec, error) {
	if len(instances) == 0 {
		return nil, nil
	}
	out := make([]ir.ComponentInstanceSpec, 0, len(instances))
	seen := map[string]ir.SourceRef{}
	targetValue := hostSpecToCty(target)
	for _, instance := range instances {
		if instance.Name == "" {
			return nil, fmt.Errorf("%s:%d:%s: component instance name must be non-empty", instance.Source.File, instance.Source.Line, instance.Source.Path)
		}
		if previous, exists := seen[instance.Name]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate component instance %q; first declared at %s:%d:%s", instance.Source.File, instance.Source.Line, instance.Source.Path, instance.Name, previous.File, previous.Line, previous.Path)
		}
		seen[instance.Name] = instance.Source

		template, ok := c.cfg.Components[instance.Template]
		if !ok {
			return nil, fmt.Errorf("%s:%d:%s: unknown component.%s", instance.Source.File, instance.Source.Line, instance.Source.Path, instance.Template)
		}
		inputValues, inputJSON, err := componentInputValues(template, instance)
		if err != nil {
			return nil, err
		}
		inputVars := make(map[string]cty.Value, len(inputValues))
		for name, value := range inputValues {
			converted, err := value.ToCty()
			if err != nil {
				return nil, fmt.Errorf("%s:%d:%s.inputs[%q]: %w", instance.Source.File, instance.Source.Line, instance.Source.Path, name, err)
			}
			inputVars[name] = converted
		}
		raw, err := parser.ParseComponentBody(template, parser.EvalContext{
			ModuleDir: template.ModuleDir,
			Locals:    c.cfg.Locals,
			Variables: map[string]cty.Value{
				"target": targetValue,
				"input":  objectOrEmpty(inputVars),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("%s:%d:%s mounted at %s:%d:%s: %w", template.Source.File, template.Source.Line, template.Source.Path, instance.Source.File, instance.Source.Line, instance.Source.Path, err)
		}
		component, err := buildComponentSpec(instance, raw)
		if err != nil {
			return nil, err
		}
		if err := applyComponentArtifact(&component, template, target); err != nil {
			return nil, err
		}
		component.Template = template.Name
		component.InputValues = inputJSON
		out = append(out, component)
	}
	return out, nil
}

func componentInputValues(template parser.Component, instance parser.ComponentInstance) (map[string]parser.Value, map[string]any, error) {
	values := map[string]parser.Value{}
	jsonValues := map[string]any{}
	for name, value := range instance.Inputs {
		if _, ok := template.Inputs[name]; !ok {
			return nil, nil, fmt.Errorf("%s:%d:%s.inputs[%q]: unknown input for component.%s", value.Source.File, value.Source.Line, value.Source.Path, name, template.Name)
		}
		values[name] = value
	}
	for _, name := range sortedKeys(template.Inputs) {
		input := template.Inputs[name]
		value, ok := values[name]
		if !ok {
			if input.Default == nil {
				return nil, nil, fmt.Errorf("%s:%d:%s: component.%s input %q is required", instance.Source.File, instance.Source.Line, instance.Source.Path, template.Name, name)
			}
			value = *input.Default
			values[name] = value
		}
		if err := validateComponentInputType(input, value); err != nil {
			return nil, nil, err
		}
		if input.Sensitive {
			jsonValues[name] = "<sensitive>"
		} else {
			jsonValues[name] = parserValueToAny(value)
		}
	}
	return values, jsonValues, nil
}

func validateComponentInputType(input parser.ComponentInput, value parser.Value) error {
	switch input.Type {
	case "string":
		if value.Kind != parser.KindString {
			return fmt.Errorf("%s:%d:%s: component input %q must be a string", value.Source.File, value.Source.Line, value.Source.Path, input.Name)
		}
	case "number":
		if value.Kind != parser.KindNumber {
			return fmt.Errorf("%s:%d:%s: component input %q must be a number", value.Source.File, value.Source.Line, value.Source.Path, input.Name)
		}
	case "bool":
		if value.Kind != parser.KindBool {
			return fmt.Errorf("%s:%d:%s: component input %q must be a bool", value.Source.File, value.Source.Line, value.Source.Path, input.Name)
		}
	case "list(string)":
		if !value.IsList() {
			return fmt.Errorf("%s:%d:%s: component input %q must be a list(string)", value.Source.File, value.Source.Line, value.Source.Path, input.Name)
		}
		for _, item := range value.List {
			if item.Kind != parser.KindString {
				return fmt.Errorf("%s:%d:%s: component input %q entries must be strings", item.Source.File, item.Source.Line, item.Source.Path, input.Name)
			}
		}
	case "map(string)":
		if !value.IsMap() {
			return fmt.Errorf("%s:%d:%s: component input %q must be a map(string)", value.Source.File, value.Source.Line, value.Source.Path, input.Name)
		}
		for _, key := range sortedKeys(value.Map) {
			item := value.Map[key]
			if item.Kind != parser.KindString {
				return fmt.Errorf("%s:%d:%s: component input %q values must be strings", item.Source.File, item.Source.Line, item.Source.Path, input.Name)
			}
		}
	default:
		return fmt.Errorf("%s:%d:%s: unsupported component input type %q", input.Source.File, input.Source.Line, input.Source.Path, input.Type)
	}
	return nil
}

func applyComponentArtifact(spec *ir.ComponentInstanceSpec, template parser.Component, target ir.HostSpec) error {
	if template.Type == "" && template.Version == "" && len(template.Sources) == 0 && template.Extract == nil && template.Install == nil {
		return nil
	}
	if template.Type == "" {
		return fmt.Errorf("%s:%d:%s.type: component artifact type is required when source, extract, install, or version is declared", template.Source.File, template.Source.Line, template.Source.Path)
	}
	if template.Type != "binary" {
		return fmt.Errorf("%s:%d:%s.type: component artifact type %q is not supported yet", template.Source.File, template.Source.Line, template.Source.Path, template.Type)
	}
	source, err := selectComponentArtifactSource(template, target)
	if err != nil {
		return err
	}
	spec.ArtifactType = template.Type
	spec.Version = template.Version
	spec.SelectedSource = &ir.ComponentArtifactSourceSpec{
		Architecture: source.Architecture,
		URL:          source.URL,
		SHA256:       strings.ToLower(source.SHA256),
		Source:       source.Source,
	}
	if template.Extract != nil {
		extract, err := componentArtifactExtractSpec(*template.Extract, source)
		if err != nil {
			return err
		}
		spec.Extract = extract
	}
	install, err := componentArtifactInstallSpec(template)
	if err != nil {
		return err
	}
	spec.Install = install
	return nil
}

func selectComponentArtifactSource(template parser.Component, target ir.HostSpec) (parser.ComponentArtifactSource, error) {
	if len(template.Sources) == 0 {
		return parser.ComponentArtifactSource{}, fmt.Errorf("%s:%d:%s.source: binary component requires at least one source block", template.Source.File, template.Source.Line, template.Source.Path)
	}
	independent, hasIndependent := template.Sources[""]
	if hasIndependent {
		if len(template.Sources) != 1 {
			return parser.ComponentArtifactSource{}, fmt.Errorf("%s:%d:%s.source: cannot mix unlabeled and architecture-labeled source blocks", independent.Source.File, independent.Source.Line, independent.Source.Path)
		}
		if err := validateComponentArtifactSource(independent); err != nil {
			return parser.ComponentArtifactSource{}, err
		}
		return independent, nil
	}
	architecture := target.System.Architecture
	if architecture == "" {
		return parser.ComponentArtifactSource{}, fmt.Errorf("%s:%d:%s: host %q must declare system.architecture to select a component source", target.Source.File, target.Source.Line, target.Source.Path, target.Name)
	}
	source, ok := template.Sources[architecture]
	if !ok {
		return parser.ComponentArtifactSource{}, fmt.Errorf("%s:%d:%s: component.%s has no source for host %q architecture %q", template.Source.File, template.Source.Line, template.Source.Path, template.Name, target.Name, architecture)
	}
	if err := validateComponentArtifactSource(source); err != nil {
		return parser.ComponentArtifactSource{}, err
	}
	return source, nil
}

func validateComponentArtifactSource(source parser.ComponentArtifactSource) error {
	if source.URL == "" {
		return fmt.Errorf("%s:%d:%s.url: source url must be non-empty", source.Source.File, source.Source.Line, source.Source.Path)
	}
	if source.SHA256 == "" {
		return fmt.Errorf("%s:%d:%s.sha256: source url requires sha256", source.Source.File, source.Source.Line, source.Source.Path)
	}
	if !sha256Pattern.MatchString(source.SHA256) {
		return fmt.Errorf("%s:%d:%s.sha256: sha256 must be a 64 character hex string", source.Source.File, source.Source.Line, source.Source.Path)
	}
	return nil
}

func componentArtifactExtractSpec(extract parser.ComponentArtifactExtract, source parser.ComponentArtifactSource) (*ir.ComponentArtifactExtractSpec, error) {
	format := extract.Format
	if format == "" {
		format = inferArtifactFormat(source.URL)
	}
	if format == "" {
		return nil, fmt.Errorf("%s:%d:%s.format: extract format is required when it cannot be inferred from source url", extract.Source.File, extract.Source.Line, extract.Source.Path)
	}
	if format != "zip" {
		return nil, fmt.Errorf("%s:%d:%s.format: only zip extract is supported for binary components", extract.Source.File, extract.Source.Line, extract.Source.Path)
	}
	if extract.StripComponents < 0 {
		return nil, fmt.Errorf("%s:%d:%s.strip_components: strip_components must be greater than or equal to 0", extract.Source.File, extract.Source.Line, extract.Source.Path)
	}
	if extract.Include == "" {
		return nil, fmt.Errorf("%s:%d:%s.include: binary extract requires include", extract.Source.File, extract.Source.Line, extract.Source.Path)
	}
	return &ir.ComponentArtifactExtractSpec{
		Format:          format,
		StripComponents: extract.StripComponents,
		Include:         extract.Include,
		Source:          extract.Source,
	}, nil
}

func componentArtifactInstallSpec(template parser.Component) (*ir.ComponentArtifactInstallSpec, error) {
	if template.Install == nil {
		return nil, fmt.Errorf("%s:%d:%s.install: binary component requires an install block", template.Source.File, template.Source.Line, template.Source.Path)
	}
	install := *template.Install
	if install.Path == "" {
		return nil, fmt.Errorf("%s:%d:%s.path: install path must be non-empty", install.Source.File, install.Source.Line, install.Source.Path)
	}
	if !filepath.IsAbs(install.Path) {
		return nil, fmt.Errorf("%s:%d:%s.path: install path must be absolute", install.Source.File, install.Source.Line, install.Source.Path)
	}
	owner := install.Owner
	if owner == "" {
		owner = "root"
	}
	group := install.Group
	if group == "" {
		group = "root"
	}
	mode := install.Mode
	if mode == "" {
		mode = "0755"
	}
	if !modePattern.MatchString(mode) {
		return nil, fmt.Errorf("%s:%d:%s.mode: mode must be a four digit octal string", install.Source.File, install.Source.Line, install.Source.Path)
	}
	return &ir.ComponentArtifactInstallSpec{
		Path:   install.Path,
		Owner:  owner,
		Group:  group,
		Mode:   mode,
		Source: install.Source,
	}, nil
}

func inferArtifactFormat(sourceURL string) string {
	base := sourceURL
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	switch {
	case strings.HasSuffix(strings.ToLower(base), ".zip"):
		return "zip"
	default:
		return ""
	}
}

func parserValueToAny(value parser.Value) any {
	switch value.Kind {
	case parser.KindNull:
		return nil
	case parser.KindString:
		return value.String
	case parser.KindBool:
		return value.Bool
	case parser.KindNumber:
		return value.Number
	case parser.KindList:
		out := make([]any, 0, len(value.List))
		for _, item := range value.List {
			out = append(out, parserValueToAny(item))
		}
		return out
	case parser.KindMap:
		out := make(map[string]any, len(value.Map))
		for _, key := range sortedKeys(value.Map) {
			out[key] = parserValueToAny(value.Map[key])
		}
		return out
	default:
		return nil
	}
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
			Path:     "/var/lib/debianform/state/" + host.Name + ".json",
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
		APT: ir.APTSpec{
			Repositories: map[string]ir.APTRepositorySpec{},
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

	if apt, ok, err := mapField(raw, "apt"); err != nil {
		return spec, err
	} else if ok {
		spec.APT.Source = apt.Source
		repositories, err := aptRepositorySpecs(apt)
		if err != nil {
			return spec, err
		}
		spec.APT.Repositories = repositories
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

	return spec, nil
}

func buildComponentSpec(instance parser.ComponentInstance, raw parser.Value) (ir.ComponentInstanceSpec, error) {
	spec := ir.ComponentInstanceSpec{
		Name:     instance.Name,
		Template: instance.Template,
		Source:   instance.Source,
		APT: ir.APTSpec{
			Repositories: map[string]ir.APTRepositorySpec{},
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

	if apt, ok, err := mapField(raw, "apt"); err != nil {
		return spec, err
	} else if ok {
		spec.APT.Source = apt.Source
		repositories, err := aptRepositorySpecs(apt)
		if err != nil {
			return spec, err
		}
		spec.APT.Repositories = repositories
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
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		seen[name] = item.Source
		out = append(out, ir.PackageItem{Name: name, Repositories: repositories, Lifecycle: lifecycle, Source: item.Source})
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

func requiredStringListField(root parser.Value, name string) ([]string, error) {
	value, ok := root.Map[name]
	if !ok {
		return nil, fmt.Errorf("%s:%d:%s.%s: required non-empty string list", root.Source.File, root.Source.Line, root.Source.Path, name)
	}
	values, err := stringListField(root, name)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s:%d:%s: required non-empty string list", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return values, nil
}

func aptRepositorySpecs(apt parser.Value) (map[string]ir.APTRepositorySpec, error) {
	objects, ok, err := objectCollection(apt, "repository")
	if err != nil || !ok {
		return map[string]ir.APTRepositorySpec{}, err
	}
	out := make(map[string]ir.APTRepositorySpec, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: apt repository label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		var uris, suites, components []string
		if ensure == "present" {
			uris, err = requiredStringListField(item, "uris")
			if err != nil {
				return nil, err
			}
			suites, err = requiredStringListField(item, "suites")
			if err != nil {
				return nil, err
			}
			components, err = requiredStringListField(item, "components")
			if err != nil {
				return nil, err
			}
		} else {
			uris, err = stringListField(item, "uris")
			if err != nil {
				return nil, err
			}
			suites, err = stringListField(item, "suites")
			if err != nil {
				return nil, err
			}
			components, err = stringListField(item, "components")
			if err != nil {
				return nil, err
			}
		}
		architectures, err := stringListField(item, "architectures")
		if err != nil {
			return nil, err
		}
		signingKey, err := aptSigningKeySpec(name, item, ensure)
		if err != nil {
			return nil, err
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[name] = ir.APTRepositorySpec{
			Name:          name,
			URIs:          uris,
			Suites:        suites,
			Components:    components,
			Architectures: architectures,
			SigningKey:    signingKey,
			Ensure:        ensure,
			Lifecycle:     lifecycle,
			Source:        item.Source,
		}
	}
	return out, nil
}

func aptSigningKeySpec(repoName string, repo parser.Value, ensure string) (*ir.APTSigningKeySpec, error) {
	signingKey, ok, err := mapField(repo, "signing_key")
	if err != nil || !ok {
		return nil, err
	}
	url, hasURL, err := stringField(signingKey, "url")
	if err != nil {
		return nil, err
	}
	content, hasContent, err := stringField(signingKey, "content")
	if err != nil {
		return nil, err
	}
	sha, hasSHA, err := stringField(signingKey, "sha256")
	if err != nil {
		return nil, err
	}
	path, hasPath, err := stringField(signingKey, "path")
	if err != nil {
		return nil, err
	}
	if hasURL && url == "" {
		return nil, fmt.Errorf("%s:%d:%s.url: signing key url must be non-empty", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasContent && content == "" {
		return nil, fmt.Errorf("%s:%d:%s.content: signing key content must be non-empty", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasURL && hasContent {
		return nil, fmt.Errorf("%s:%d:%s: signing_key requires exactly one of url or content", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if ensure == "present" && !hasURL && !hasContent {
		return nil, fmt.Errorf("%s:%d:%s: signing_key requires exactly one of url or content when repository ensure is present", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasURL && (!hasSHA || sha == "") {
		return nil, fmt.Errorf("%s:%d:%s.sha256: signing key url requires sha256", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	if hasSHA && sha != "" {
		if !sha256Pattern.MatchString(sha) {
			return nil, fmt.Errorf("%s:%d:%s.sha256: sha256 must be a 64 character hex string", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
		}
		sha = strings.ToLower(sha)
		if hasContent && sha != contentSummary([]byte(content)).SHA256 {
			return nil, fmt.Errorf("%s:%d:%s.sha256: sha256 does not match signing key content", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
		}
	}
	if !hasPath || path == "" {
		path = defaultAPTSigningKeyPath(repoName)
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s:%d:%s.path: signing key path must be absolute", signingKey.Source.File, signingKey.Source.Line, signingKey.Source.Path)
	}
	return &ir.APTSigningKeySpec{
		URL:     url,
		Content: content,
		SHA256:  sha,
		Path:    path,
		Source:  signingKey.Source,
	}, nil
}

func defaultAPTSigningKeyPath(name string) string {
	return "/etc/apt/keyrings/" + safeAPTName(name) + ".asc"
}

func safeAPTName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "repository"
	}
	return strings.ToLower(out)
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
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
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
			Lifecycle:  lifecycle,
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
		lifecycle, err := lifecycleSpec(item)
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
			Lifecycle:  lifecycle,
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
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[path] = ir.ManagedDirectory{Path: path, Owner: owner, Group: group, Mode: mode, Ensure: ensure, Lifecycle: lifecycle, Source: item.Source}
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
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[name] = ir.ManagedGroup{Name: name, GID: gid, System: system, Ensure: ensure, Lifecycle: lifecycle, Source: item.Source}
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
		lifecycle, err := lifecycleSpec(item)
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
			Lifecycle:         lifecycle,
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
		lifecycle, err := lifecycleSpec(item)
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
			Lifecycle:  lifecycle,
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
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[name] = ir.ManagedService{Name: name, Unit: serviceUnitName(name), Package: pkg, Enabled: enabled, State: state, Lifecycle: lifecycle, Source: item.Source}
	}
	return out, nil
}

func validateHostSpec(spec ir.HostSpec) error {
	files := map[string]ir.SourceRef{}
	secrets := map[string]ir.SourceRef{}
	repositories := map[string]ir.APTRepositorySpec{}
	packages := map[string]ir.PackageItem{}
	directories := map[string]ir.ManagedDirectory{}
	groups := map[string]ir.ManagedGroup{}
	users := map[string]ir.ManagedUser{}
	units := map[string]ir.SystemdUnit{}
	services := map[string]ir.ManagedService{}

	for path, file := range spec.Files.Files {
		files[path] = file.Source
	}
	for path, secret := range spec.Secrets.Files {
		secrets[path] = secret.Source
		if fileSource, exists := files[path]; exists {
			return fmt.Errorf("%s:%d:%s: file path %q conflicts with secret declared at %s:%d:%s", fileSource.File, fileSource.Line, fileSource.Path, path, secret.Source.File, secret.Source.Line, secret.Source.Path)
		}
	}
	for name, repository := range spec.APT.Repositories {
		repositories[name] = repository
	}
	for _, pkg := range spec.Packages.Install {
		packages[pkg.Name] = pkg
	}
	for path, directory := range spec.Directories.Directories {
		directories[path] = directory
	}
	for name, group := range spec.Groups.Groups {
		groups[name] = group
	}
	for name, user := range spec.Users.Users {
		users[name] = user
	}
	for name, unit := range spec.Systemd.Units {
		units[name] = unit
	}
	for name, service := range spec.Services.Services {
		services[name] = service
	}

	for _, component := range spec.Components {
		if component.Install != nil {
			path := component.Install.Path
			if previous, exists := files[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q binary path %q conflicts with file declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q binary path %q conflicts with secret declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := directories[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q binary path %q conflicts with directory declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			files[path] = component.Install.Source
		}
		for path, file := range component.Files.Files {
			if previous, exists := files[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q file path %q conflicts with file declared at %s:%d:%s", file.Source.File, file.Source.Line, file.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q file path %q conflicts with secret declared at %s:%d:%s", file.Source.File, file.Source.Line, file.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			files[path] = file.Source
		}
		for path, secret := range component.Secrets.Files {
			if previous, exists := files[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q secret path %q conflicts with file declared at %s:%d:%s", secret.Source.File, secret.Source.Line, secret.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q secret path %q conflicts with secret declared at %s:%d:%s", secret.Source.File, secret.Source.Line, secret.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			secrets[path] = secret.Source
		}
		for name, repository := range component.APT.Repositories {
			if previous, exists := repositories[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q apt.repository %q conflicts with repository declared at %s:%d:%s", repository.Source.File, repository.Source.Line, repository.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			repositories[name] = repository
		}
		for _, pkg := range component.Packages.Install {
			if previous, exists := packages[pkg.Name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q package %q conflicts with package declared at %s:%d:%s", pkg.Source.File, pkg.Source.Line, pkg.Source.Path, component.Name, pkg.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			packages[pkg.Name] = pkg
		}
		for path, directory := range component.Directories.Directories {
			if previous, exists := directories[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q directory %q conflicts with directory declared at %s:%d:%s", directory.Source.File, directory.Source.Line, directory.Source.Path, component.Name, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			directories[path] = directory
		}
		for name, group := range component.Groups.Groups {
			if previous, exists := groups[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q group %q conflicts with group declared at %s:%d:%s", group.Source.File, group.Source.Line, group.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			groups[name] = group
		}
		for name, user := range component.Users.Users {
			if previous, exists := users[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q user %q conflicts with user declared at %s:%d:%s", user.Source.File, user.Source.Line, user.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			users[name] = user
		}
		for name, unit := range component.Systemd.Units {
			if previous, exists := units[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd unit %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			units[name] = unit
		}
		for name, service := range component.Services.Services {
			if previous, exists := services[name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q service %q conflicts with service declared at %s:%d:%s", service.Source.File, service.Source.Line, service.Source.Path, component.Name, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			services[name] = service
		}
	}

	for _, user := range users {
		if user.PrimaryGroup == "" {
			continue
		}
		if _, ok := groups[user.PrimaryGroup]; !ok && user.PrimaryGroup != user.Name {
			return fmt.Errorf("%s:%d:%s: user %q references missing primary group %q", user.Source.File, user.Source.Line, user.Source.Path, user.Name, user.PrimaryGroup)
		}
	}
	for _, pkg := range packages {
		for _, repo := range pkg.Repositories {
			repository, ok := repositories[repo]
			if !ok {
				return fmt.Errorf("%s:%d:%s: package %q references missing apt.repository %q", pkg.Source.File, pkg.Source.Line, pkg.Source.Path, pkg.Name, repo)
			}
			if repository.Ensure == "absent" {
				return fmt.Errorf("%s:%d:%s: package %q references absent apt.repository %q", pkg.Source.File, pkg.Source.Line, pkg.Source.Path, pkg.Name, repo)
			}
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

func lifecycleSpec(root parser.Value) (*ir.LifecycleSpec, error) {
	lifecycle, ok, err := mapField(root, "lifecycle")
	if err != nil || !ok {
		return nil, err
	}
	preventDestroy, ok, err := boolField(lifecycle, "prevent_destroy")
	if err != nil {
		return nil, err
	}
	if !ok || !preventDestroy {
		return nil, nil
	}
	return &ir.LifecycleSpec{PreventDestroy: preventDestroy, Source: lifecycle.Source}, nil
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

var (
	modePattern   = regexp.MustCompile(`^0[0-7]{3}$`)
	sha256Pattern = regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)
)

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
