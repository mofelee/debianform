package merge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/zclconf/go-cty/cty"
)

type CompileOptions struct {
	HostFilter                           string
	HostFacts                            map[string]ir.HostFacts
	SkipComponents                       bool
	ValidateRuntimeTemplates             bool
	SuppressSecretFileDeprecationWarning bool
	Warnings                             *[]ir.Warning
}

func Compile(cfg *parser.Config) (*ir.Program, error) {
	return CompileWithOptions(cfg, CompileOptions{})
}

func CompileWithOptions(cfg *parser.Config, opts CompileOptions) (*ir.Program, error) {
	compiler := &compiler{
		cfg:                               cfg,
		profileCache:                      map[string]resolvedProfile{},
		visiting:                          map[string]bool{},
		suppressSecretFileDeprecationWarn: opts.SuppressSecretFileDeprecationWarning,
		warnings:                          opts.Warnings,
	}

	names := make([]string, 0, len(cfg.Hosts))
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	components, err := componentTemplateSpecs(cfg.Components)
	if err != nil {
		return nil, err
	}
	if err := compiler.validateVariableValues(); err != nil {
		return nil, err
	}
	variables, err := variableSpecs(cfg)
	if err != nil {
		return nil, err
	}
	program := &ir.Program{
		Hosts:      make([]ir.HostSpec, 0, len(names)),
		Variables:  variables,
		Components: components,
	}
	for _, name := range names {
		if opts.HostFilter != "" && name != opts.HostFilter {
			continue
		}
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
		spec, err := compiler.buildHostSpec(host, merged)
		if err != nil {
			return nil, err
		}
		if facts, ok := opts.HostFacts[host.Name]; ok {
			if err := applyHostFacts(&spec, facts); err != nil {
				return nil, err
			}
		}
		if opts.ValidateRuntimeTemplates {
			if err := compiler.validateRuntimeComponentTemplates(host.Components, spec); err != nil {
				return nil, err
			}
		} else if !opts.SkipComponents {
			components, err := compiler.instantiateComponents(host.Components, spec)
			if err != nil {
				return nil, err
			}
			spec.Components = components
		}
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

func variableSpecs(cfg *parser.Config) (map[string]ir.VariableSpec, error) {
	if len(cfg.Variables) == 0 {
		return nil, nil
	}
	out := make(map[string]ir.VariableSpec, len(cfg.Variables))
	for _, name := range sortedKeys(cfg.Variables) {
		variable := cfg.Variables[name]
		if err := validateVariableDefinition(variable); err != nil {
			return nil, err
		}
		var defaultValue any
		if normalized, ok := cfg.VariableValues[name]; ok {
			defaultValue = parserValueToAny(normalized)
		} else if variable.Default != nil {
			normalized, err := normalizeVariable(variable, *variable.Default)
			if err != nil {
				return nil, err
			}
			defaultValue = parserValueToAny(normalized)
		}
		out[name] = ir.VariableSpec{
			Name:        variable.Name,
			Type:        variable.Type,
			TypeExpr:    variable.TypeExpr,
			TypeSpec:    componentInputTypeSpecToIR(variable.TypeSpec),
			Description: variable.Description,
			Default:     defaultValue,
			Sensitive:   variable.Sensitive,
			Nullable:    variable.Nullable,
			Ephemeral:   variable.Ephemeral,
			Const:       variable.Const,
			Deprecated:  variable.Deprecated,
			Validations: variableValidationSpecs(variable.Validations),
			Source:      variable.Source,
		}
	}
	return out, nil
}

type compiler struct {
	cfg                               *parser.Config
	profileCache                      map[string]resolvedProfile
	visiting                          map[string]bool
	suppressSecretFileDeprecationWarn bool
	warnings                          *[]ir.Warning
}

type resolvedProfile struct {
	Body    parser.Value
	Asserts []parser.Assert
}

func componentTemplateSpecs(components map[string]parser.Component) (map[string]ir.ComponentTemplateSpec, error) {
	if len(components) == 0 {
		return nil, nil
	}
	out := make(map[string]ir.ComponentTemplateSpec, len(components))
	for _, name := range sortedKeys(components) {
		component := components[name]
		spec := ir.ComponentTemplateSpec{
			Name:         component.Name,
			ArtifactType: component.Type,
			Version:      component.Version,
			Inputs:       map[string]ir.ComponentInputSpec{},
			Scripts:      map[string]ir.ComponentScriptSpec{},
			Sources:      map[string]ir.ComponentArtifactSourceSpec{},
			Source:       component.Source,
		}
		for _, inputName := range sortedKeys(component.Inputs) {
			input := component.Inputs[inputName]
			if err := validateComponentInputDefinition(input); err != nil {
				return nil, err
			}
			var defaultValue any
			if input.Default != nil {
				normalized, err := normalizeComponentInput(input, *input.Default)
				if err != nil {
					return nil, err
				}
				defaultValue = parserValueToAny(normalized)
			}
			spec.Inputs[inputName] = ir.ComponentInputSpec{
				Name:        input.Name,
				Type:        input.Type,
				TypeExpr:    input.TypeExpr,
				TypeSpec:    componentInputTypeSpecToIR(input.TypeSpec),
				Description: input.Description,
				Default:     defaultValue,
				Sensitive:   input.Sensitive,
				Nullable:    input.Nullable,
				Deprecated:  input.Deprecated,
				Validations: componentInputValidationSpecs(input.Validations),
				Source:      input.Source,
			}
		}
		if len(spec.Inputs) == 0 {
			spec.Inputs = nil
		}
		for _, scriptName := range sortedKeys(component.Scripts) {
			script := component.Scripts[scriptName]
			if err := validateComponentScriptShape(script); err != nil {
				return nil, err
			}
			spec.Scripts[scriptName] = ir.ComponentScriptSpec{
				Name:   script.Name,
				Source: script.Source,
			}
		}
		if len(spec.Scripts) == 0 {
			spec.Scripts = nil
		}
		for _, sourceName := range sortedKeys(component.Sources) {
			source := component.Sources[sourceName]
			spec.Sources[sourceName] = ir.ComponentArtifactSourceSpec{
				Architecture: source.Architecture,
				URL:          source.URL,
				SHA256:       strings.ToLower(source.SHA256),
				Source:       source.Source,
			}
		}
		if len(spec.Sources) == 0 {
			spec.Sources = nil
		}
		if component.Extract != nil {
			spec.Extract = &ir.ComponentArtifactExtractSpec{
				Format:          component.Extract.Format,
				StripComponents: component.Extract.StripComponents,
				Include:         component.Extract.Include,
				Source:          component.Extract.Source,
			}
		}
		if component.Build != nil {
			spec.Build = &ir.ComponentArtifactBuildSpec{
				Commands:   cloneCommandMatrix(component.Build.Commands),
				Packages:   append([]string(nil), component.Build.Packages...),
				WorkingDir: component.Build.WorkingDir,
				Output:     component.Build.Output,
				SourceName: component.Build.SourceName,
				Source:     component.Build.Source,
			}
		}
		if component.Install != nil {
			spec.Install = &ir.ComponentArtifactInstallSpec{
				Path:   component.Install.Path,
				Owner:  component.Install.Owner,
				Group:  component.Install.Group,
				Mode:   component.Install.Mode,
				Source: component.Install.Source,
			}
		}
		out[name] = spec
	}
	return out, nil
}

func componentInstanceScriptSpecs(template parser.Component, ctx parser.EvalContext) (map[string]ir.ComponentScriptSpec, error) {
	if len(template.Scripts) == 0 {
		return nil, nil
	}
	out := make(map[string]ir.ComponentScriptSpec, len(template.Scripts))
	for _, name := range sortedKeys(template.Scripts) {
		script, err := parser.EvaluateComponentScript(template.Scripts[name], ctx)
		if err != nil {
			return nil, err
		}
		spec, err := componentScriptSpec(script)
		if err != nil {
			return nil, err
		}
		out[name] = spec
	}
	return out, nil
}

func componentScriptSpec(script parser.ComponentScript) (ir.ComponentScriptSpec, error) {
	if err := validateComponentScriptShape(script); err != nil {
		return ir.ComponentScriptSpec{}, err
	}
	mode := "once"
	if script.ModeSet {
		mode = script.Mode
		if !stringIn(mode, "once", "each") {
			return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s: script mode must be once or each", script.ModeSource.File, script.ModeSource.Line, script.ModeSource.Path)
		}
	}
	interpreter := []string{"/bin/sh", "-eu"}
	if script.InterpreterSet {
		if len(script.Interpreter) == 0 {
			return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s: script interpreter must be a non-empty string list", script.InterpreterSource.File, script.InterpreterSource.Line, script.InterpreterSource.Path)
		}
		for i, arg := range script.Interpreter {
			if arg == "" {
				return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s[%d]: script interpreter entries must be non-empty strings", script.InterpreterSource.File, script.InterpreterSource.Line, script.InterpreterSource.Path, i)
			}
		}
		interpreter = append([]string(nil), script.Interpreter...)
	}
	commands := cloneCommandMatrix(script.Commands)
	if script.CommandsSet {
		if len(commands) == 0 {
			return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s: script commands must contain at least one command", script.CommandsSource.File, script.CommandsSource.Line, script.CommandsSource.Path)
		}
		for i, command := range commands {
			if len(command) == 0 {
				return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s[%d]: script command must contain at least one argument", script.CommandsSource.File, script.CommandsSource.Line, script.CommandsSource.Path, i)
			}
			for j, arg := range command {
				if arg == "" {
					return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s[%d][%d]: script command arguments must be non-empty strings", script.CommandsSource.File, script.CommandsSource.Line, script.CommandsSource.Path, i, j)
				}
			}
		}
	}
	return ir.ComponentScriptSpec{
		Name:        script.Name,
		Mode:        mode,
		Body:        componentScriptBody(script),
		Interpreter: interpreter,
		Run:         script.Run,
		Content:     script.Content,
		Commands:    commands,
		Sensitive:   script.Sensitive,
		Source:      script.Source,
	}, nil
}

func componentScriptBody(script parser.ComponentScript) string {
	switch {
	case script.RunSet:
		return "run"
	case script.ContentSet:
		return "content"
	case script.CommandsSet:
		return "commands"
	default:
		return ""
	}
}

func validateComponentScriptShape(script parser.ComponentScript) error {
	bodies := 0
	for _, ok := range []bool{script.RunSet, script.ContentSet, script.CommandsSet} {
		if ok {
			bodies++
		}
	}
	if bodies != 1 {
		return fmt.Errorf("%s:%d:%s: component script requires exactly one of run, content, or commands", script.Source.File, script.Source.Line, script.Source.Path)
	}
	return nil
}

func componentInputTypeSpecToIR(spec parser.ComponentInputTypeSpec) ir.ComponentInputTypeSpec {
	out := ir.ComponentInputTypeSpec{
		Kind: string(spec.Kind),
	}
	if spec.Element != nil {
		element := componentInputTypeSpecToIR(*spec.Element)
		out.Element = &element
	}
	if len(spec.Attributes) > 0 {
		out.Attributes = make(map[string]ir.ComponentObjectAttrSpec, len(spec.Attributes))
		for _, name := range sortedKeys(spec.Attributes) {
			attr := spec.Attributes[name]
			out.Attributes[name] = ir.ComponentObjectAttrSpec{
				Type:     componentInputTypeSpecToIR(attr.Type),
				Optional: attr.Optional,
			}
			if attr.Default != nil {
				item := out.Attributes[name]
				item.Default = parserValueToAny(*attr.Default)
				out.Attributes[name] = item
			}
		}
	}
	if len(spec.Tuple) > 0 {
		out.Tuple = make([]ir.ComponentInputTypeSpec, 0, len(spec.Tuple))
		for _, item := range spec.Tuple {
			out.Tuple = append(out.Tuple, componentInputTypeSpecToIR(item))
		}
	}
	return out
}

func (c *compiler) evalVariables(extra map[string]cty.Value) (map[string]cty.Value, error) {
	varValues := make(map[string]cty.Value, len(c.cfg.VariableValues))
	for name, value := range c.cfg.VariableValues {
		converted, err := value.ToCty()
		if err != nil {
			return nil, fmt.Errorf("var.%s: %w", name, err)
		}
		varValues[name] = converted
	}
	varNamespace := cty.EmptyObjectVal
	if len(varValues) > 0 {
		varNamespace = cty.ObjectVal(varValues)
	}
	out := map[string]cty.Value{"var": varNamespace}
	for key, value := range extra {
		out[key] = value
	}
	return out, nil
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
		inputValues, inputJSON, err := componentInputValues(template, instance, c.warningSink())
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
		variables, err := c.evalVariables(map[string]cty.Value{
			"target": targetValue,
			"input":  objectOrEmpty(inputVars),
		})
		if err != nil {
			return nil, err
		}
		componentCtx := parser.EvalContext{
			ModuleDir: template.ModuleDir,
			Locals:    c.cfg.Locals,
			Variables: variables,
		}
		raw, err := parser.ParseComponentBody(template, componentCtx)
		if err != nil {
			return nil, fmt.Errorf("%s:%d:%s mounted at %s:%d:%s: %w", template.Source.File, template.Source.Line, template.Source.Path, instance.Source.File, instance.Source.Line, instance.Source.Path, err)
		}
		component, err := c.buildComponentSpec(instance, template, componentCtx, raw)
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

func (c *compiler) validateRuntimeComponentTemplates(instances []parser.ComponentInstance, target ir.HostSpec) error {
	if len(instances) == 0 {
		return nil
	}
	seen := map[string]ir.SourceRef{}
	check := target
	check.Components = append([]ir.ComponentInstanceSpec(nil), target.Components...)
	targetValue := runtimeValidationTargetValue(target)
	for _, instance := range instances {
		if instance.Name == "" {
			return fmt.Errorf("%s:%d:%s: component instance name must be non-empty", instance.Source.File, instance.Source.Line, instance.Source.Path)
		}
		if previous, exists := seen[instance.Name]; exists {
			return fmt.Errorf("%s:%d:%s: duplicate component instance %q; first declared at %s:%d:%s", instance.Source.File, instance.Source.Line, instance.Source.Path, instance.Name, previous.File, previous.Line, previous.Path)
		}
		seen[instance.Name] = instance.Source

		template, ok := c.cfg.Components[instance.Template]
		if !ok {
			return fmt.Errorf("%s:%d:%s: unknown component.%s", instance.Source.File, instance.Source.Line, instance.Source.Path, instance.Template)
		}
		inputValues, inputJSON, err := componentInputValues(template, instance, c.warningSink())
		if err != nil {
			return err
		}
		inputVars := make(map[string]cty.Value, len(inputValues))
		for name, value := range inputValues {
			converted, err := value.ToCty()
			if err != nil {
				return fmt.Errorf("%s:%d:%s.inputs[%q]: %w", instance.Source.File, instance.Source.Line, instance.Source.Path, name, err)
			}
			inputVars[name] = converted
		}
		if err := parser.ValidateComponentBodyShape(template); err != nil {
			return fmt.Errorf("%s:%d:%s mounted at %s:%d:%s: %w", template.Source.File, template.Source.Line, template.Source.Path, instance.Source.File, instance.Source.Line, instance.Source.Path, err)
		}
		install, err := validateComponentArtifactTemplate(template)
		if err != nil {
			return err
		}
		variables, err := c.evalVariables(map[string]cty.Value{
			"target": targetValue,
			"input":  objectOrEmpty(inputVars),
		})
		if err != nil {
			return err
		}
		componentCtx := parser.EvalContext{
			ModuleDir: template.ModuleDir,
			Locals:    c.cfg.Locals,
			Variables: variables,
		}
		raw, err := parser.ParseComponentBody(template, componentCtx)
		if err != nil {
			if isRuntimeUnknownError(err) {
				if install != nil {
					component := ir.ComponentInstanceSpec{
						Name:         instance.Name,
						Template:     template.Name,
						InputValues:  inputJSON,
						ArtifactType: template.Type,
						Version:      template.Version,
						Install:      install,
						Source:       instance.Source,
					}
					if err := validateComponentAgainstHost(check, component); err != nil {
						return err
					}
					check.Components = append(check.Components, component)
				}
				continue
			}
			return fmt.Errorf("%s:%d:%s mounted at %s:%d:%s: %w", template.Source.File, template.Source.Line, template.Source.Path, instance.Source.File, instance.Source.Line, instance.Source.Path, err)
		}
		component, err := c.buildComponentSpec(instance, template, componentCtx, raw)
		if err != nil {
			if isRuntimeUnknownError(err) {
				if install != nil {
					component := ir.ComponentInstanceSpec{
						Name:         instance.Name,
						Template:     template.Name,
						InputValues:  inputJSON,
						ArtifactType: template.Type,
						Version:      template.Version,
						Install:      install,
						Source:       instance.Source,
					}
					if err := validateComponentAgainstHost(check, component); err != nil {
						return err
					}
					check.Components = append(check.Components, component)
				}
				continue
			}
			return err
		}
		component.Template = template.Name
		component.InputValues = inputJSON
		if install != nil {
			component.ArtifactType = template.Type
			component.Version = template.Version
			component.Install = install
		}
		if err := validateComponentAgainstHost(check, component); err != nil {
			return err
		}
		check.Components = append(check.Components, component)
	}
	return nil
}

func validateComponentAgainstHost(target ir.HostSpec, component ir.ComponentInstanceSpec) error {
	check := target
	check.Components = append(append([]ir.ComponentInstanceSpec(nil), target.Components...), component)
	return validateHostSpec(check)
}

func runtimeValidationTargetValue(host ir.HostSpec) cty.Value {
	architecture := cty.StringVal(host.System.Architecture)
	if host.System.Architecture == "" {
		architecture = cty.UnknownVal(cty.String)
	}
	codename := cty.StringVal(host.System.Codename)
	if host.System.Codename == "" {
		codename = cty.UnknownVal(cty.String)
	}
	system := cty.ObjectVal(map[string]cty.Value{
		"hostname":     cty.StringVal(host.System.Hostname),
		"architecture": architecture,
		"codename":     codename,
		"timezone":     cty.StringVal(host.System.Timezone),
		"locale":       cty.StringVal(host.System.Locale),
	})
	return cty.ObjectVal(map[string]cty.Value{
		"name":        cty.StringVal(host.Name),
		"ssh":         sshSpecToCty(host.SSH),
		"state":       stateSpecToCty(host.State),
		"system":      system,
		"kernel":      kernelSpecToCty(host.Kernel),
		"packages":    packageSpecToCty(host.Packages),
		"apt":         aptSpecToCty(host.APT),
		"files":       fileSpecToCty(host.Files),
		"secrets":     secretSpecToCty(host.Secrets),
		"directories": directorySpecToCty(host.Directories),
		"groups":      groupSpecToCty(host.Groups),
		"users":       userSpecToCty(host.Users),
		"systemd":     systemdSpecToCty(host.Systemd),
		"services":    serviceSpecToCty(host.Services),
		"nftables":    nftablesSpecToCty(host.Nftables),
	})
}

func isRuntimeUnknownError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown values are not supported")
}

func (c *compiler) warningSink() *[]ir.Warning {
	if c == nil {
		return nil
	}
	return c.warnings
}

func (c *compiler) warnSecretFilesDeprecated(files map[string]ir.SecretFile) {
	if c == nil || c.warnings == nil || c.suppressSecretFileDeprecationWarn {
		return
	}
	for _, path := range sortedKeys(files) {
		file := files[path]
		*c.warnings = append(*c.warnings, ir.Warning{
			Source:  file.Source,
			Message: "secrets.file is deprecated; use variable + files.file content with content_version instead",
		})
	}
}

func (c *compiler) validateVariableValues() error {
	for _, name := range sortedKeys(c.cfg.Variables) {
		variable := c.cfg.Variables[name]
		value, ok := c.cfg.VariableValues[name]
		if !ok {
			continue
		}
		if err := evaluateVariableValidations(variable, value); err != nil {
			return err
		}
		if variable.Deprecated != "" && c.cfg.ExplicitVariableValues[name] && c.warnings != nil {
			*c.warnings = append(*c.warnings, ir.Warning{
				Source:  value.Source,
				Message: fmt.Sprintf("variable %q is deprecated: %s", name, variable.Deprecated),
			})
		}
	}
	return nil
}

func componentInputValues(template parser.Component, instance parser.ComponentInstance, warnings *[]ir.Warning) (map[string]parser.Value, map[string]any, error) {
	values := map[string]parser.Value{}
	jsonValues := map[string]any{}
	explicit := map[string]bool{}
	for name, value := range instance.Inputs {
		if _, ok := template.Inputs[name]; !ok {
			return nil, nil, fmt.Errorf("%s:%d:%s.inputs[%q]: unknown input for component.%s", value.Source.File, value.Source.Line, value.Source.Path, name, template.Name)
		}
		values[name] = value
		explicit[name] = true
	}
	for _, name := range sortedKeys(template.Inputs) {
		input := template.Inputs[name]
		value, ok := values[name]
		if !ok {
			if input.Default == nil {
				return nil, nil, fmt.Errorf("%s:%d:%s: component.%s input %q is required", instance.Source.File, instance.Source.Line, instance.Source.Path, template.Name, name)
			}
			value = *input.Default
		}
		normalized, err := normalizeComponentInput(input, value)
		if err != nil {
			return nil, nil, err
		}
		if err := evaluateComponentInputValidations(input, normalized); err != nil {
			return nil, nil, err
		}
		if input.Sensitive {
			normalized.Sensitive = true
		}
		values[name] = normalized
		if input.Deprecated != "" && explicit[name] && warnings != nil {
			*warnings = append(*warnings, ir.Warning{
				Source:  value.Source,
				Message: fmt.Sprintf("component.%s input %q is deprecated: %s", template.Name, name, input.Deprecated),
			})
		}
		if input.Sensitive {
			jsonValues[name] = "<sensitive>"
		} else {
			jsonValues[name] = parserValueToAny(normalized)
		}
	}
	return values, jsonValues, nil
}

func validateComponentInputDefinition(input parser.ComponentInput) error {
	if input.Default != nil {
		normalized, err := normalizeComponentInput(input, *input.Default)
		if err != nil {
			return err
		}
		if err := evaluateComponentInputValidations(input, normalized); err != nil {
			return err
		}
	}
	return validateComponentInputTypeSpecDefaults(input.Name, input.TypeSpec, "")
}

func validateVariableDefinition(variable parser.Variable) error {
	if variable.Default != nil {
		normalized, err := normalizeVariable(variable, *variable.Default)
		if err != nil {
			return err
		}
		if err := evaluateVariableValidations(variable, normalized); err != nil {
			return err
		}
	}
	return validateComponentInputTypeSpecDefaults(variable.Name, variable.TypeSpec, "")
}

func validateComponentInputTypeSpecDefaults(inputName string, spec parser.ComponentInputTypeSpec, path string) error {
	switch spec.Kind {
	case parser.ComponentInputTypeList, parser.ComponentInputTypeSet, parser.ComponentInputTypeMap:
		if spec.Element != nil {
			return validateComponentInputTypeSpecDefaults(inputName, *spec.Element, path)
		}
	case parser.ComponentInputTypeObject:
		for _, name := range sortedKeys(spec.Attributes) {
			attr := spec.Attributes[name]
			attrPath := path + "." + name
			if attr.Default != nil {
				if _, err := normalizeComponentInputValue(inputName, attr.Type, *attr.Default, attrPath); err != nil {
					return err
				}
			}
			if err := validateComponentInputTypeSpecDefaults(inputName, attr.Type, attrPath); err != nil {
				return err
			}
		}
	case parser.ComponentInputTypeTuple:
		for i, item := range spec.Tuple {
			if err := validateComponentInputTypeSpecDefaults(inputName, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeComponentInput(input parser.ComponentInput, value parser.Value) (parser.Value, error) {
	if value.Kind == parser.KindNull && !input.Nullable {
		return parser.Value{}, fmt.Errorf("%s:%d:%s: component input %q must not be null", value.Source.File, value.Source.Line, value.Source.Path, input.Name)
	}
	return normalizeComponentInputValue(input.Name, input.TypeSpec, value, "")
}

func normalizeVariable(variable parser.Variable, value parser.Value) (parser.Value, error) {
	return parser.NormalizeVariableValue(variable, value)
}

func normalizeComponentInputValue(inputName string, spec parser.ComponentInputTypeSpec, value parser.Value, path string) (parser.Value, error) {
	if value.Kind == parser.KindNull {
		return value, nil
	}
	switch spec.Kind {
	case parser.ComponentInputTypeAny:
		return value, nil
	case parser.ComponentInputTypeString:
		if value.Kind != parser.KindString {
			return parser.Value{}, componentInputTypeError(inputName, value, path, "string")
		}
		return value, nil
	case parser.ComponentInputTypeNumber:
		if value.Kind != parser.KindNumber {
			return parser.Value{}, componentInputTypeError(inputName, value, path, "number")
		}
		return value, nil
	case parser.ComponentInputTypeBool:
		if value.Kind != parser.KindBool {
			return parser.Value{}, componentInputTypeError(inputName, value, path, "bool")
		}
		return value, nil
	case parser.ComponentInputTypeList, parser.ComponentInputTypeSet:
		if !value.IsList() {
			return parser.Value{}, componentInputTypeError(inputName, value, path, string(spec.Kind)+"("+elementTypeName(spec)+")")
		}
		out := value
		out.List = make([]parser.Value, 0, len(value.List))
		for i, item := range value.List {
			normalized, err := normalizeComponentInputValue(inputName, *spec.Element, item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return parser.Value{}, err
			}
			out.List = append(out.List, normalized)
		}
		if spec.Kind == parser.ComponentInputTypeSet {
			sort.SliceStable(out.List, func(i, j int) bool {
				return out.List[i].Key() < out.List[j].Key()
			})
			deduped := out.List[:0]
			var last string
			for i, item := range out.List {
				key := item.Key()
				if i == 0 || key != last {
					deduped = append(deduped, item)
				}
				last = key
			}
			out.List = deduped
		}
		return out, nil
	case parser.ComponentInputTypeMap:
		if !value.IsMap() {
			return parser.Value{}, componentInputTypeError(inputName, value, path, "map("+elementTypeName(spec)+")")
		}
		out := value
		out.Map = make(map[string]parser.Value, len(value.Map))
		for _, key := range sortedKeys(value.Map) {
			normalized, err := normalizeComponentInputValue(inputName, *spec.Element, value.Map[key], fmt.Sprintf("%s[%q]", path, key))
			if err != nil {
				return parser.Value{}, err
			}
			out.Map[key] = normalized
		}
		return out, nil
	case parser.ComponentInputTypeObject:
		if !value.IsMap() {
			return parser.Value{}, componentInputTypeError(inputName, value, path, "object")
		}
		out := value
		out.Map = make(map[string]parser.Value, len(spec.Attributes))
		for key := range value.Map {
			if _, ok := spec.Attributes[key]; !ok {
				return parser.Value{}, fmt.Errorf("%s:%d:%s: component input %q has unsupported attribute %s%s", value.Map[key].Source.File, value.Map[key].Source.Line, value.Map[key].Source.Path, inputName, pathPrefix(path), attributePath(key))
			}
		}
		names := make([]string, 0, len(spec.Attributes))
		for name := range spec.Attributes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			attr := spec.Attributes[name]
			item, ok := value.Map[name]
			attrPath := path + "." + name
			if !ok {
				if attr.Default != nil {
					normalized, err := normalizeComponentInputValue(inputName, attr.Type, *attr.Default, attrPath)
					if err != nil {
						return parser.Value{}, err
					}
					out.Map[name] = normalized
					continue
				}
				if attr.Optional {
					out.Map[name] = parser.NullValue(missingObjectAttributeSource(value, name, attrPath))
					continue
				}
				return parser.Value{}, fmt.Errorf("%s:%d:%s: component input %q missing required attribute %s", value.Source.File, value.Source.Line, value.Source.Path, inputName, attrPath)
			}
			normalized, err := normalizeComponentInputValue(inputName, attr.Type, item, attrPath)
			if err != nil {
				return parser.Value{}, err
			}
			out.Map[name] = normalized
		}
		return out, nil
	case parser.ComponentInputTypeTuple:
		if !value.IsList() {
			return parser.Value{}, componentInputTypeError(inputName, value, path, "tuple")
		}
		if len(value.List) != len(spec.Tuple) {
			return parser.Value{}, fmt.Errorf("%s:%d:%s: component input %q%s must have %d entries, got %d", value.Source.File, value.Source.Line, value.Source.Path, inputName, path, len(spec.Tuple), len(value.List))
		}
		out := value
		out.List = make([]parser.Value, 0, len(value.List))
		for i, item := range value.List {
			normalized, err := normalizeComponentInputValue(inputName, spec.Tuple[i], item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return parser.Value{}, err
			}
			out.List = append(out.List, normalized)
		}
		return out, nil
	default:
		return parser.Value{}, fmt.Errorf("%s:%d:%s: unsupported component input type %q", value.Source.File, value.Source.Line, value.Source.Path, spec.Kind)
	}
}

func componentInputTypeError(inputName string, value parser.Value, path string, want string) error {
	return fmt.Errorf("%s:%d:%s: component input %q%s must be %s, got %s", value.Source.File, value.Source.Line, value.Source.Path, inputName, path, want, value.Kind)
}

func elementTypeName(spec parser.ComponentInputTypeSpec) string {
	if spec.Element == nil {
		return "any"
	}
	return spec.Element.String()
}

func missingObjectAttributeSource(parent parser.Value, name string, path string) ir.SourceRef {
	source := parent.Source
	source.Path = parent.Source.Path + path
	if source.Path == "" {
		source.Path = name
	}
	return source
}

func pathPrefix(path string) string {
	if path == "" {
		return ""
	}
	return path
}

func attributePath(name string) string {
	if name == "" {
		return ""
	}
	return "." + name
}

func applyComponentArtifact(spec *ir.ComponentInstanceSpec, template parser.Component, target ir.HostSpec) error {
	if template.Type == "" && template.Version == "" && len(template.Sources) == 0 && template.Extract == nil && template.Build == nil && template.Install == nil {
		return nil
	}
	if template.Type == "" {
		return fmt.Errorf("%s:%d:%s.type: component artifact type is required when source, extract, build, install, or version is declared", template.Source.File, template.Source.Line, template.Source.Path)
	}
	if !supportedComponentArtifactType(template.Type) {
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
		extract, err := componentArtifactExtractSpec(template.Type, *template.Extract, source)
		if err != nil {
			return err
		}
		spec.Extract = extract
	} else if template.Type == "archive" {
		return fmt.Errorf("%s:%d:%s.extract: archive component requires an extract block", template.Source.File, template.Source.Line, template.Source.Path)
	}
	if template.Extract != nil && (template.Type == "file" || template.Type == "ca_certificate") {
		return fmt.Errorf("%s:%d:%s.extract: %s component does not support extract", template.Extract.Source.File, template.Extract.Source.Line, template.Extract.Source.Path, template.Type)
	}
	if template.Type == "source" {
		build, err := componentArtifactBuildSpec(template)
		if err != nil {
			return err
		}
		spec.Build = build
	} else if template.Build != nil {
		return fmt.Errorf("%s:%d:%s: %s component does not support build", template.Build.Source.File, template.Build.Source.Line, template.Build.Source.Path, template.Type)
	}
	install, err := componentArtifactInstallSpec(template)
	if err != nil {
		return err
	}
	spec.Install = install
	return nil
}

func validateComponentArtifactTemplate(template parser.Component) (*ir.ComponentArtifactInstallSpec, error) {
	if template.Type == "" && template.Version == "" && len(template.Sources) == 0 && template.Extract == nil && template.Build == nil && template.Install == nil {
		return nil, nil
	}
	if template.Type == "" {
		return nil, fmt.Errorf("%s:%d:%s.type: component artifact type is required when source, extract, build, install, or version is declared", template.Source.File, template.Source.Line, template.Source.Path)
	}
	if !supportedComponentArtifactType(template.Type) {
		return nil, fmt.Errorf("%s:%d:%s.type: component artifact type %q is not supported yet", template.Source.File, template.Source.Line, template.Source.Path, template.Type)
	}
	if len(template.Sources) == 0 {
		return nil, fmt.Errorf("%s:%d:%s.source: %s component requires at least one source block", template.Source.File, template.Source.Line, template.Source.Path, template.Type)
	}
	if independent, hasIndependent := template.Sources[""]; hasIndependent && len(template.Sources) != 1 {
		return nil, fmt.Errorf("%s:%d:%s.source: cannot mix unlabeled and architecture-labeled source blocks", independent.Source.File, independent.Source.Line, independent.Source.Path)
	}
	for _, name := range sortedKeys(template.Sources) {
		source := template.Sources[name]
		if err := validateComponentArtifactSource(source); err != nil {
			return nil, err
		}
		if template.Extract != nil {
			if _, err := componentArtifactExtractSpec(template.Type, *template.Extract, source); err != nil {
				return nil, err
			}
		} else if template.Type == "archive" {
			return nil, fmt.Errorf("%s:%d:%s.extract: archive component requires an extract block", template.Source.File, template.Source.Line, template.Source.Path)
		}
	}
	if template.Type == "source" {
		if _, err := componentArtifactBuildSpec(template); err != nil {
			return nil, err
		}
	} else if template.Build != nil {
		return nil, fmt.Errorf("%s:%d:%s: %s component does not support build", template.Build.Source.File, template.Build.Source.Line, template.Build.Source.Path, template.Type)
	}
	install, err := componentArtifactInstallSpec(template)
	if err != nil {
		return nil, err
	}
	return install, nil
}

func supportedComponentArtifactType(artifactType string) bool {
	switch artifactType {
	case "binary", "archive", "file", "ca_certificate", "source":
		return true
	default:
		return false
	}
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

func componentArtifactExtractSpec(artifactType string, extract parser.ComponentArtifactExtract, source parser.ComponentArtifactSource) (*ir.ComponentArtifactExtractSpec, error) {
	format := extract.Format
	if format == "" {
		format = inferArtifactFormat(source.URL)
	}
	if format == "" {
		return nil, fmt.Errorf("%s:%d:%s.format: extract format is required when it cannot be inferred from source url", extract.Source.File, extract.Source.Line, extract.Source.Path)
	}
	switch artifactType {
	case "binary":
		if format != "zip" && format != "tar.gz" && format != "tar.xz" && format != "bz2" && format != "gz" {
			return nil, fmt.Errorf("%s:%d:%s.format: binary extract format must be zip, tar.gz, tar.xz, bz2, or gz", extract.Source.File, extract.Source.Line, extract.Source.Path)
		}
		if format == "bz2" || format == "gz" {
			if extract.Include != "" {
				return nil, fmt.Errorf("%s:%d:%s.include: %s binary extract does not support include", extract.Source.File, extract.Source.Line, extract.Source.Path, format)
			}
			if extract.StripComponents != 0 {
				return nil, fmt.Errorf("%s:%d:%s.strip_components: %s binary extract requires strip_components = 0", extract.Source.File, extract.Source.Line, extract.Source.Path, format)
			}
		} else if extract.Include == "" {
			return nil, fmt.Errorf("%s:%d:%s.include: binary extract requires include", extract.Source.File, extract.Source.Line, extract.Source.Path)
		}
	case "archive":
		if format != "tar.gz" {
			return nil, fmt.Errorf("%s:%d:%s.format: archive extract format must be tar.gz", extract.Source.File, extract.Source.Line, extract.Source.Path)
		}
		if extract.Include != "" {
			return nil, fmt.Errorf("%s:%d:%s.include: archive extract does not support include", extract.Source.File, extract.Source.Line, extract.Source.Path)
		}
	case "source":
		if format != "zip" && format != "tar.gz" && format != "tar.xz" {
			return nil, fmt.Errorf("%s:%d:%s.format: source extract format must be zip, tar.gz, or tar.xz", extract.Source.File, extract.Source.Line, extract.Source.Path)
		}
		if extract.Include != "" {
			return nil, fmt.Errorf("%s:%d:%s.include: source extract does not support include", extract.Source.File, extract.Source.Line, extract.Source.Path)
		}
	default:
		return nil, fmt.Errorf("%s:%d:%s: %s component does not support extract", extract.Source.File, extract.Source.Line, extract.Source.Path, artifactType)
	}
	if extract.StripComponents < 0 {
		return nil, fmt.Errorf("%s:%d:%s.strip_components: strip_components must be greater than or equal to 0", extract.Source.File, extract.Source.Line, extract.Source.Path)
	}
	return &ir.ComponentArtifactExtractSpec{
		Format:          format,
		StripComponents: extract.StripComponents,
		Include:         extract.Include,
		Source:          extract.Source,
	}, nil
}

func componentArtifactBuildSpec(template parser.Component) (*ir.ComponentArtifactBuildSpec, error) {
	if template.Build == nil {
		return nil, fmt.Errorf("%s:%d:%s.build: source component requires a build block", template.Source.File, template.Source.Line, template.Source.Path)
	}
	build := *template.Build
	if len(build.Commands) == 0 {
		return nil, fmt.Errorf("%s:%d:%s.commands: build commands must contain at least one command", build.Source.File, build.Source.Line, build.Source.Path)
	}
	for i, command := range build.Commands {
		if len(command) == 0 {
			return nil, fmt.Errorf("%s:%d:%s.commands[%d]: build command must contain at least one argument", build.Source.File, build.Source.Line, build.Source.Path, i)
		}
		for j, arg := range command {
			if arg == "" {
				return nil, fmt.Errorf("%s:%d:%s.commands[%d][%d]: build command arguments must be non-empty strings", build.Source.File, build.Source.Line, build.Source.Path, i, j)
			}
		}
	}
	for i, pkg := range build.Packages {
		if pkg == "" {
			return nil, fmt.Errorf("%s:%d:%s.packages[%d]: build packages must be non-empty strings", build.Source.File, build.Source.Line, build.Source.Path, i)
		}
	}
	if build.WorkingDir != "" && filepath.IsAbs(build.WorkingDir) {
		return nil, fmt.Errorf("%s:%d:%s.working_dir: build working_dir must be relative", build.Source.File, build.Source.Line, build.Source.Path)
	}
	if build.Output == "" {
		return nil, fmt.Errorf("%s:%d:%s.output: build output must be non-empty", build.Source.File, build.Source.Line, build.Source.Path)
	}
	if filepath.IsAbs(build.Output) {
		return nil, fmt.Errorf("%s:%d:%s.output: build output must be relative", build.Source.File, build.Source.Line, build.Source.Path)
	}
	if build.SourceName != "" && (filepath.IsAbs(build.SourceName) || strings.Contains(build.SourceName, "/") || strings.Contains(build.SourceName, "\\")) {
		return nil, fmt.Errorf("%s:%d:%s.source_name: build source_name must be a relative file name", build.Source.File, build.Source.Line, build.Source.Path)
	}
	return &ir.ComponentArtifactBuildSpec{
		Commands:   cloneCommandMatrix(build.Commands),
		Packages:   append([]string(nil), build.Packages...),
		WorkingDir: build.WorkingDir,
		Output:     build.Output,
		SourceName: build.SourceName,
		Source:     build.Source,
	}, nil
}

func componentArtifactInstallSpec(template parser.Component) (*ir.ComponentArtifactInstallSpec, error) {
	if template.Install == nil {
		return nil, fmt.Errorf("%s:%d:%s.install: %s component requires an install block", template.Source.File, template.Source.Line, template.Source.Path, template.Type)
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
		mode = defaultArtifactInstallMode(template.Type)
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

func defaultArtifactInstallMode(artifactType string) string {
	switch artifactType {
	case "file", "ca_certificate":
		return "0644"
	default:
		return "0755"
	}
}

func cloneCommandMatrix(in [][]string) [][]string {
	if len(in) == 0 {
		return nil
	}
	out := make([][]string, 0, len(in))
	for _, command := range in {
		out = append(out, append([]string(nil), command...))
	}
	return out
}

func inferArtifactFormat(sourceURL string) string {
	base := sourceURL
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	switch {
	case strings.HasSuffix(strings.ToLower(base), ".zip"):
		return "zip"
	case strings.HasSuffix(strings.ToLower(base), ".tar.gz"), strings.HasSuffix(strings.ToLower(base), ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(strings.ToLower(base), ".tar.xz"), strings.HasSuffix(strings.ToLower(base), ".txz"):
		return "tar.xz"
	case strings.HasSuffix(strings.ToLower(base), ".bz2"):
		return "bz2"
	case strings.HasSuffix(strings.ToLower(base), ".gz"):
		return "gz"
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

func (c *compiler) buildHostSpec(host parser.Host, raw parser.Value) (ir.HostSpec, error) {
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
			SourceFiles:  map[string]ir.APTSourceFileSpec{},
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
			Units:  map[string]ir.SystemdUnit{},
			Timers: map[string]ir.SystemdTimer{},
		},
		Services: ir.ServiceSpec{
			Services: map[string]ir.ManagedService{},
		},
		Nftables: ir.NftablesSpec{
			Files: map[string]ir.NftablesFileSpec{},
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
			if value != "" && value != "root" {
				return spec, fmt.Errorf("%s:%d:%s.user: DebianForm manages target hosts as root; ssh.user must be \"root\" or omitted", ssh.Source.File, ssh.Source.Line, ssh.Source.Path)
			}
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
		sourceFiles, err := aptSourceFileSpecs(apt)
		if err != nil {
			return spec, err
		}
		spec.APT.Repositories = repositories
		spec.APT.SourceFiles = sourceFiles
	}

	if files, ok, err := mapField(raw, "files"); err != nil {
		return spec, err
	} else if ok {
		spec.Files.Source = files.Source
		managedFiles, err := fileSpecs(files)
		if err != nil {
			return spec, err
		}
		if err := rejectHostFileOnChange(managedFiles); err != nil {
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
		c.warnSecretFilesDeprecated(secretFiles)
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
		timers, err := systemdTimerSpecs(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Timers = timers
		networkd, err := networkdSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Networkd = networkd
		resolved, err := systemdResolvedSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Resolved = resolved
		journald, err := systemdJournaldSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Journald = journald
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

	if nftables, ok, err := mapField(raw, "nftables"); err != nil {
		return spec, err
	} else if ok {
		spec.Nftables.Source = nftables.Source
		compiled, err := nftablesSpec(nftables)
		if err != nil {
			return spec, err
		}
		spec.Nftables = compiled
	}

	if docker, ok, err := mapField(raw, "docker"); err != nil {
		return spec, err
	} else if ok {
		compiled, err := dockerSpec(docker)
		if err != nil {
			return spec, err
		}
		spec.Docker = compiled
	}

	return spec, nil
}

func applyHostFacts(spec *ir.HostSpec, facts ir.HostFacts) error {
	system := facts.System
	if system.Architecture != "" {
		if spec.System.Architecture != "" && spec.System.Architecture != system.Architecture {
			return fmt.Errorf("%s:%d:%s.system.architecture: declared architecture %q does not match detected architecture %q", spec.Source.File, spec.Source.Line, spec.Source.Path, spec.System.Architecture, system.Architecture)
		}
		spec.System.Architecture = system.Architecture
	}
	if system.Codename != "" {
		if spec.System.Codename != "" && spec.System.Codename != system.Codename {
			return fmt.Errorf("%s:%d:%s.system.codename: declared codename %q does not match detected codename %q", spec.Source.File, spec.Source.Line, spec.Source.Path, spec.System.Codename, system.Codename)
		}
		spec.System.Codename = system.Codename
	}
	spec.Facts = facts
	return nil
}

func (c *compiler) buildComponentSpec(instance parser.ComponentInstance, template parser.Component, ctx parser.EvalContext, raw parser.Value) (ir.ComponentInstanceSpec, error) {
	spec := ir.ComponentInstanceSpec{
		Name:     instance.Name,
		Template: instance.Template,
		Source:   instance.Source,
		Scripts:  map[string]ir.ComponentScriptSpec{},
		APT: ir.APTSpec{
			Repositories: map[string]ir.APTRepositorySpec{},
			SourceFiles:  map[string]ir.APTSourceFileSpec{},
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
			Units:  map[string]ir.SystemdUnit{},
			Timers: map[string]ir.SystemdTimer{},
		},
		Services: ir.ServiceSpec{
			Services: map[string]ir.ManagedService{},
		},
	}

	scripts, err := componentInstanceScriptSpecs(template, ctx)
	if err != nil {
		return spec, err
	}
	spec.Scripts = scripts

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
		sourceFiles, err := aptSourceFileSpecs(apt)
		if err != nil {
			return spec, err
		}
		spec.APT.Repositories = repositories
		spec.APT.SourceFiles = sourceFiles
	}

	if files, ok, err := mapField(raw, "files"); err != nil {
		return spec, err
	} else if ok {
		spec.Files.Source = files.Source
		managedFiles, err := fileSpecs(files)
		if err != nil {
			return spec, err
		}
		if err := validateFileOnChangeRefs(managedFiles, spec.Scripts); err != nil {
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
		c.warnSecretFilesDeprecated(secretFiles)
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
		timers, err := systemdTimerSpecs(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Timers = timers
		networkd, err := networkdSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Networkd = networkd
		resolved, err := systemdResolvedSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Resolved = resolved
		journald, err := systemdJournaldSpec(systemd)
		if err != nil {
			return spec, err
		}
		spec.Systemd.Journald = journald
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
	if err := rejectEphemeralValue(value); err != nil {
		return "", false, err
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
	if err := rejectEphemeralValue(value); err != nil {
		return 0, false, err
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
	if err := rejectEphemeralValue(value); err != nil {
		return false, false, err
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
	if err := rejectEphemeralValue(value); err != nil {
		return parser.Value{}, false, err
	}
	if !value.IsList() {
		return parser.Value{}, false, fmt.Errorf("%s:%d: %s must be a list", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return value, true, nil
}

func objectPath(root parser.Value, name string, defaultPath string) (string, error) {
	path, ok, err := stringField(root, name)
	if err != nil {
		return "", err
	}
	if ok {
		return path, nil
	}
	return defaultPath, nil
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

func stringFieldAllowEphemeral(root parser.Value, name string) (string, bool, error) {
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

func rejectEphemeralValue(value parser.Value) error {
	if !value.ContainsEphemeral() {
		return nil
	}
	return fmt.Errorf("%s:%d:%s: ephemeral value is not allowed in this field", value.Source.File, value.Source.Line, value.Source.Path)
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

func aptSourceFileSpecs(apt parser.Value) (map[string]ir.APTSourceFileSpec, error) {
	objects, ok, err := objectCollection(apt, "source_file")
	if err != nil || !ok {
		return map[string]ir.APTSourceFileSpec{}, err
	}
	out := make(map[string]ir.APTSourceFileSpec, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		if label == "" {
			return nil, fmt.Errorf("%s:%d:%s: apt source_file label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		path, ok, err := stringField(item, "path")
		if err != nil {
			return nil, err
		}
		if !ok || path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s.path: apt source_file path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		content, hasContent, err := stringFieldAllowEphemeral(item, "content")
		if err != nil {
			return nil, err
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && hasContent == hasSource {
			return nil, fmt.Errorf("%s:%d:%s: apt.source_file requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
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
		onDestroy, err := stringFieldDefault(item, "on_destroy", "keep")
		if err != nil {
			return nil, err
		}
		if onDestroy != "keep" && onDestroy != "restore" {
			return nil, fmt.Errorf("%s:%d:%s.on_destroy: on_destroy must be keep or restore", item.Source.File, item.Source.Line, item.Source.Path)
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		sourceFile := ir.APTSourceFileSpec{
			Label:      label,
			Path:       path,
			Content:    content,
			SourcePath: resolvePath(item.Source.File, sourcePath),
			Owner:      owner,
			Group:      group,
			Mode:       mode,
			Ensure:     ensure,
			OnDestroy:  onDestroy,
			Lifecycle:  lifecycle,
			Source:     item.Source,
		}
		if hasContent {
			sourceFile.Summary = contentSummary([]byte(content))
		} else if hasSource {
			summary, err := fileSummary(sourceFile.SourcePath, item.Source)
			if err != nil {
				return nil, err
			}
			sourceFile.Summary = summary
		}
		out[label] = sourceFile
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
	content, hasContent, err := stringFieldAllowEphemeral(signingKey, "content")
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
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		path, err := objectPath(item, "path", label)
		if err != nil {
			return nil, err
		}
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: file path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if previous, exists := out[path]; exists {
			return nil, fmt.Errorf("%s:%d:%s: file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		content, hasContent, err := stringFieldAllowEphemeral(item, "content")
		if err != nil {
			return nil, err
		}
		contentVersion, hasContentVersion, err := stringField(item, "content_version")
		if err != nil {
			return nil, err
		}
		if hasContentVersion && item.Map["content_version"].ContainsSensitive() {
			value := item.Map["content_version"]
			return nil, fmt.Errorf("%s:%d:%s: content_version must not be sensitive", value.Source.File, value.Source.Line, value.Source.Path)
		}
		sourcePath, hasSource, err := stringField(item, "source")
		if err != nil {
			return nil, err
		}
		if ensure == "present" && hasContent == hasSource {
			return nil, fmt.Errorf("%s:%d:%s: files.file requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		contentWriteOnly := hasContent && item.Map["content"].ContainsEphemeral()
		if contentWriteOnly && (!hasContentVersion || contentVersion == "") {
			return nil, fmt.Errorf("%s:%d:%s.content_version: files.file with ephemeral content requires content_version", item.Source.File, item.Source.Line, item.Source.Path)
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
		if hasContent && contentNeedsRedaction(item.Map["content"]) {
			sensitive = true
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		onChange, hasOnChange, err := stringField(item, "on_change")
		if err != nil {
			return nil, err
		}
		managed := ir.ManagedFile{
			Path:             path,
			Content:          content,
			ContentVersion:   contentVersion,
			ContentWriteOnly: contentWriteOnly,
			SourcePath:       resolvePath(item.Source.File, sourcePath),
			Owner:            owner,
			Group:            group,
			Mode:             mode,
			Sensitive:        sensitive,
			Ensure:           ensure,
			OnChange:         onChange,
			Lifecycle:        lifecycle,
			Source:           item.Source,
		}
		if hasOnChange {
			source := item.Map["on_change"].Source
			managed.OnChangeSource = &source
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

func rejectHostFileOnChange(files map[string]ir.ManagedFile) error {
	for _, path := range sortedKeys(files) {
		file := files[path]
		if file.OnChange == "" {
			continue
		}
		source := file.Source
		if file.OnChangeSource != nil {
			source = *file.OnChangeSource
		}
		if source.Path == "" {
			source = file.Source
		}
		return fmt.Errorf("%s:%d:%s: files.file.on_change is only supported inside component", source.File, source.Line, source.Path)
	}
	return nil
}

func validateFileOnChangeRefs(files map[string]ir.ManagedFile, scripts map[string]ir.ComponentScriptSpec) error {
	for _, path := range sortedKeys(files) {
		file := files[path]
		if file.OnChange == "" {
			continue
		}
		if _, ok := scripts[file.OnChange]; !ok {
			source := file.Source
			if file.OnChangeSource != nil {
				source = *file.OnChangeSource
			}
			if source.Path == "" {
				source = file.Source
			}
			return fmt.Errorf("%s:%d:%s: files.file.on_change references unknown script.%s", source.File, source.Line, source.Path, file.OnChange)
		}
	}
	return nil
}

func secretSpecs(secrets parser.Value) (map[string]ir.SecretFile, error) {
	objects, ok, err := objectCollection(secrets, "file")
	if err != nil || !ok {
		return map[string]ir.SecretFile{}, err
	}
	out := make(map[string]ir.SecretFile, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		path, err := objectPath(item, "path", label)
		if err != nil {
			return nil, err
		}
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s:%d:%s: secret path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if previous, exists := out[path]; exists {
			return nil, fmt.Errorf("%s:%d:%s: secret path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
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
	if err != nil {
		return nil, err
	}
	serviceObjects, serviceOK, err := objectCollection(systemd, "service_unit")
	if err != nil {
		return nil, err
	}
	out := make(map[string]ir.SystemdUnit, len(objects)+len(serviceObjects))
	if ok {
		for _, name := range sortedKeys(objects) {
			item := objects[name]
			if name == "" {
				return nil, fmt.Errorf("%s:%d:%s: systemd unit name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			unit, err := systemdUnitSpec(name, item)
			if err != nil {
				return nil, err
			}
			if err := addSystemdUnit(out, unit); err != nil {
				return nil, err
			}
		}
	}
	if serviceOK {
		for _, name := range sortedKeys(serviceObjects) {
			item := serviceObjects[name]
			if name == "" {
				return nil, fmt.Errorf("%s:%d:%s: systemd service_unit name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			unit, err := systemdServiceUnitSpec(name, item)
			if err != nil {
				return nil, err
			}
			if err := addSystemdUnit(out, unit); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func systemdTimerSpecs(systemd parser.Value) (map[string]ir.SystemdTimer, error) {
	objects, ok, err := objectCollection(systemd, "timer")
	if err != nil || !ok {
		return map[string]ir.SystemdTimer{}, err
	}
	out := make(map[string]ir.SystemdTimer, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: systemd timer name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		timer, err := systemdTimerSpec(name, item)
		if err != nil {
			return nil, err
		}
		if previous, exists := out[timer.Name]; exists {
			return nil, fmt.Errorf("%s:%d:%s: systemd timer %q conflicts with timer declared at %s:%d:%s", timer.Source.File, timer.Source.Line, timer.Source.Path, timer.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		out[timer.Name] = timer
	}
	return out, nil
}

func networkdSpec(systemd parser.Value) (*ir.NetworkdSpec, error) {
	networkd, ok, err := mapField(systemd, "networkd")
	if err != nil || !ok {
		return nil, err
	}
	spec := &ir.NetworkdSpec{
		NetDevs:  map[string]ir.NetworkdNetDev{},
		Networks: map[string]ir.NetworkdNetwork{},
		Source:   networkd.Source,
	}
	if enable, ok, err := boolField(networkd, "enable"); err != nil {
		return nil, err
	} else if ok {
		value := enable
		spec.Enable = &value
	}

	netdevs, ok, err := objectCollection(networkd, "netdev")
	if err != nil {
		return nil, err
	}
	if ok {
		for _, label := range sortedKeys(netdevs) {
			item := netdevs[label]
			if label == "" {
				return nil, fmt.Errorf("%s:%d:%s: networkd netdev label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			netdev, err := networkdNetDevSpec(label, item)
			if err != nil {
				return nil, err
			}
			spec.NetDevs[label] = netdev
		}
	}

	networks, ok, err := objectCollection(networkd, "network")
	if err != nil {
		return nil, err
	}
	if ok {
		for _, label := range sortedKeys(networks) {
			item := networks[label]
			if label == "" {
				return nil, fmt.Errorf("%s:%d:%s: networkd network label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			network, err := networkdNetworkSpec(label, item)
			if err != nil {
				return nil, err
			}
			spec.Networks[label] = network
		}
	}
	return spec, nil
}

func systemdResolvedSpec(systemd parser.Value) (*ir.SystemdResolvedSpec, error) {
	resolved, ok, err := mapField(systemd, "resolved")
	if err != nil || !ok {
		return nil, err
	}
	ensure, err := ensureField(resolved, "present")
	if err != nil {
		return nil, err
	}
	resolve, err := systemdSectionField(resolved, "resolve")
	if err != nil {
		return nil, err
	}
	if ensure == "present" && len(resolve) == 0 {
		return nil, fmt.Errorf("%s:%d:%s.resolve: systemd.resolved requires resolve section when ensure is present", resolved.Source.File, resolved.Source.Line, resolved.Source.Path)
	}
	unit, err := systemdDropInUnit(resolved, "systemd-resolved", "/etc/systemd/resolved.conf.d/debianform.conf", "Resolve", resolve, ensure)
	if err != nil {
		return nil, err
	}
	var enable *bool
	if value, ok, err := boolField(resolved, "enable"); err != nil {
		return nil, err
	} else if ok {
		enable = &value
	}
	state, _, err := stringField(resolved, "state")
	if err != nil {
		return nil, err
	}
	if state != "" && !stringIn(state, "running", "stopped", "restarted", "reloaded") {
		return nil, fmt.Errorf("%s:%d:%s.state: systemd.resolved state must be running, stopped, restarted, or reloaded", resolved.Source.File, resolved.Source.Line, resolved.Source.Path)
	}
	return &ir.SystemdResolvedSpec{Unit: unit, Resolve: resolve, Enable: enable, State: state, Source: resolved.Source}, nil
}

func systemdJournaldSpec(systemd parser.Value) (*ir.SystemdJournaldSpec, error) {
	journald, ok, err := mapField(systemd, "journald")
	if err != nil || !ok {
		return nil, err
	}
	ensure, err := ensureField(journald, "present")
	if err != nil {
		return nil, err
	}
	journal, err := systemdSectionField(journald, "journal")
	if err != nil {
		return nil, err
	}
	if ensure == "present" && len(journal) == 0 {
		return nil, fmt.Errorf("%s:%d:%s.journal: systemd.journald requires journal section when ensure is present", journald.Source.File, journald.Source.Line, journald.Source.Path)
	}
	unit, err := systemdDropInUnit(journald, "systemd-journald", "/etc/systemd/journald.conf.d/debianform.conf", "Journal", journal, ensure)
	if err != nil {
		return nil, err
	}
	state, _, err := stringField(journald, "state")
	if err != nil {
		return nil, err
	}
	if state != "" && !stringIn(state, "running", "stopped", "restarted", "reloaded") {
		return nil, fmt.Errorf("%s:%d:%s.state: systemd.journald state must be running, stopped, restarted, or reloaded", journald.Source.File, journald.Source.Line, journald.Source.Path)
	}
	return &ir.SystemdJournaldSpec{Unit: unit, Journal: journal, State: state, Source: journald.Source}, nil
}

func networkdNetDevSpec(label string, item parser.Value) (ir.NetworkdNetDev, error) {
	path, err := networkdFilePath(item, "path", "/etc/systemd/network/"+label+".netdev")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	netdev, err := networkdSectionField(item, "netdev")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	wireguard, err := networkdSectionField(item, "wireguard")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	if _, ok := wireguard["PrivateKey"]; ok {
		return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.wireguard.PrivateKey: use PrivateKeyFile instead of inline PrivateKey", item.Source.File, item.Source.Line, item.Source.Path)
	}
	peers, err := networkdSectionCollection(item, "wireguard_peer")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	for _, label := range sortedKeys(peers) {
		if _, ok := peers[label]["PresharedKey"]; ok {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.wireguard_peer[%q].PresharedKey: use PresharedKeyFile instead of inline PresharedKey", item.Source.File, item.Source.Line, item.Source.Path, label)
		}
	}
	if ensure == "present" {
		if len(netdev) == 0 {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.netdev: networkd netdev requires netdev section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if kind := firstSectionValue(netdev, "Kind"); kind == "" {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.netdev.Kind: networkd netdev requires Kind", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if name := firstSectionValue(netdev, "Name"); name == "" {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.netdev.Name: networkd netdev requires Name", item.Source.File, item.Source.Line, item.Source.Path)
		}
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	out := ir.NetworkdNetDev{
		Label:          label,
		Path:           path,
		NetDev:         netdev,
		WireGuard:      wireguard,
		WireGuardPeers: peers,
		Owner:          owner,
		Group:          group,
		Mode:           mode,
		Ensure:         ensure,
		Lifecycle:      lifecycle,
		Source:         item.Source,
	}
	if ensure == "present" {
		content, err := renderNetworkdNetDev(out)
		if err != nil {
			return ir.NetworkdNetDev{}, err
		}
		out.Content = content
		out.Summary = contentSummary([]byte(content))
	}
	return out, nil
}

func networkdNetworkSpec(label string, item parser.Value) (ir.NetworkdNetwork, error) {
	path, err := networkdFilePath(item, "path", "/etc/systemd/network/"+label+".network")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	match, err := networkdSectionField(item, "match")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	network, err := networkdSectionField(item, "network")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	if ensure == "present" {
		if len(match) == 0 {
			return ir.NetworkdNetwork{}, fmt.Errorf("%s:%d:%s.match: networkd network requires match section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if len(network) == 0 {
			return ir.NetworkdNetwork{}, fmt.Errorf("%s:%d:%s.network: networkd network requires network section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	out := ir.NetworkdNetwork{
		Label:     label,
		Path:      path,
		Match:     match,
		Network:   network,
		Owner:     owner,
		Group:     group,
		Mode:      mode,
		Ensure:    ensure,
		Lifecycle: lifecycle,
		Source:    item.Source,
	}
	if ensure == "present" {
		content, err := renderNetworkdNetwork(out)
		if err != nil {
			return ir.NetworkdNetwork{}, err
		}
		out.Content = content
		out.Summary = contentSummary([]byte(content))
	}
	return out, nil
}

func networkdFilePath(item parser.Value, field string, fallback string) (string, error) {
	path, ok, err := stringField(item, field)
	if err != nil {
		return "", err
	}
	if !ok || path == "" {
		path = fallback
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s:%d:%s.%s: networkd file path must be absolute", item.Source.File, item.Source.Line, item.Source.Path, field)
	}
	return path, nil
}

func networkdSectionField(root parser.Value, name string) (ir.NetworkdSection, error) {
	section, ok, err := mapField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	return networkdSection(section)
}

func systemdSectionField(root parser.Value, name string) (ir.NetworkdSection, error) {
	section, ok, err := mapField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	return systemdSection(section)
}

func networkdSectionCollection(root parser.Value, name string) (map[string]ir.NetworkdSection, error) {
	objects, ok, err := objectCollection(root, name)
	if err != nil || !ok {
		return map[string]ir.NetworkdSection{}, err
	}
	out := make(map[string]ir.NetworkdSection, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		if label == "" {
			return nil, fmt.Errorf("%s:%d:%s: networkd %s label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path, name)
		}
		section, err := networkdSection(item)
		if err != nil {
			return nil, err
		}
		out[label] = section
	}
	return out, nil
}

func networkdSection(root parser.Value) (ir.NetworkdSection, error) {
	if !root.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: networkd section must be a map", root.Source.File, root.Source.Line, root.Source.Path)
	}
	out := ir.NetworkdSection{}
	for _, key := range sortedKeys(root.Map) {
		item := root.Map[key]
		if item.Kind == parser.KindNull {
			continue
		}
		values, err := networkdSectionValues(item)
		if err != nil {
			return nil, err
		}
		out[key] = values
	}
	return out, nil
}

func systemdSection(root parser.Value) (ir.NetworkdSection, error) {
	if !root.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: systemd section must be a map", root.Source.File, root.Source.Line, root.Source.Path)
	}
	out := ir.NetworkdSection{}
	for _, key := range sortedKeys(root.Map) {
		item := root.Map[key]
		if item.Kind == parser.KindNull {
			continue
		}
		values, err := networkdSectionValues(item)
		if err != nil {
			return nil, err
		}
		out[key] = values
	}
	return out, nil
}

func networkdSectionValues(value parser.Value) ([]string, error) {
	if value.IsList() {
		out := make([]string, 0, len(value.List))
		for _, item := range value.List {
			str, err := networkdScalarValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, str)
		}
		return out, nil
	}
	str, err := networkdScalarValue(value)
	if err != nil {
		return nil, err
	}
	return []string{str}, nil
}

func networkdScalarValue(value parser.Value) (string, error) {
	switch value.Kind {
	case parser.KindString:
		if err := validateSystemdSingleLine(value.String, value.Source); err != nil {
			return "", err
		}
		return value.String, nil
	case parser.KindNumber:
		if err := validateSystemdSingleLine(value.Number, value.Source); err != nil {
			return "", err
		}
		return value.Number, nil
	case parser.KindBool:
		if value.Bool {
			return "yes", nil
		}
		return "no", nil
	default:
		return "", fmt.Errorf("%s:%d:%s: networkd section values must be strings, numbers, booleans, or lists of those", value.Source.File, value.Source.Line, value.Source.Path)
	}
}

func renderNetworkdNetDev(item ir.NetworkdNetDev) (string, error) {
	var lines []string
	if err := appendNetworkdSection(&lines, "NetDev", item.NetDev, item.Source); err != nil {
		return "", err
	}
	if len(item.WireGuard) > 0 {
		if err := appendNetworkdSection(&lines, "WireGuard", item.WireGuard, item.Source); err != nil {
			return "", err
		}
	}
	for _, label := range sortedKeys(item.WireGuardPeers) {
		if err := appendNetworkdSection(&lines, "WireGuardPeer", item.WireGuardPeers[label], item.Source); err != nil {
			return "", err
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func renderNetworkdNetwork(item ir.NetworkdNetwork) (string, error) {
	var lines []string
	if err := appendNetworkdSection(&lines, "Match", item.Match, item.Source); err != nil {
		return "", err
	}
	if err := appendNetworkdSection(&lines, "Network", item.Network, item.Source); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func appendNetworkdSection(lines *[]string, name string, values ir.NetworkdSection, source ir.SourceRef) error {
	if len(*lines) > 0 {
		*lines = append(*lines, "")
	}
	*lines = append(*lines, "["+name+"]")
	for _, key := range sortedKeys(values) {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00\n\r") {
			return fmt.Errorf("%s:%d:%s: networkd section key %q is invalid", source.File, source.Line, source.Path, key)
		}
		for _, value := range values[key] {
			if err := validateSystemdSingleLine(value, source); err != nil {
				return err
			}
			*lines = append(*lines, key+"="+value)
		}
	}
	return nil
}

func appendSystemdSection(lines *[]string, name string, values ir.NetworkdSection, source ir.SourceRef) error {
	if name != "" {
		if len(*lines) > 0 {
			*lines = append(*lines, "")
		}
		*lines = append(*lines, "["+name+"]")
	}
	for _, key := range sortedKeys(values) {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00\n\r") {
			return fmt.Errorf("%s:%d:%s: systemd section key %q is invalid", source.File, source.Line, source.Path, key)
		}
		for _, value := range values[key] {
			if err := validateSystemdSingleLine(value, source); err != nil {
				return err
			}
			*lines = append(*lines, key+"="+value)
		}
	}
	return nil
}

func systemdSectionNeedsRedaction(root parser.Value, name string) bool {
	value, ok := root.Map[name]
	return ok && contentNeedsRedaction(value)
}

func systemdAnySectionNeedsRedaction(root parser.Value) bool {
	for _, name := range []string{"resolve", "journal", "timer", "install", "service_config"} {
		if systemdSectionNeedsRedaction(root, name) {
			return true
		}
	}
	return false
}

func firstSectionValue(section ir.NetworkdSection, key string) string {
	values := section[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func addSystemdUnit(units map[string]ir.SystemdUnit, unit ir.SystemdUnit) error {
	if previous, exists := units[unit.Name]; exists {
		return fmt.Errorf("%s:%d:%s: systemd unit %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
	}
	units[unit.Name] = unit
	return nil
}

func systemdUnitSpec(name string, item parser.Value) (ir.SystemdUnit, error) {
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	if ensure == "present" && hasContent == hasSource {
		return ir.SystemdUnit{}, fmt.Errorf("%s:%d:%s: systemd.unit requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
	}
	sensitive := hasContent && contentNeedsRedaction(item.Map["content"])
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	unit := ir.SystemdUnit{
		Name:       name,
		Path:       "/etc/systemd/system/" + name,
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
		unit.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(unit.SourcePath, item.Source)
		if err != nil {
			return ir.SystemdUnit{}, err
		}
		unit.Summary = summary
	}
	return unit, nil
}

func systemdServiceUnitSpec(name string, item parser.Value) (ir.SystemdUnit, error) {
	unitName := serviceUnitName(name)
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	hasRawUnit := hasContent || hasSource
	rawContentSensitive := hasContent && contentNeedsRedaction(item.Map["content"])
	hasStructuredFields := systemdServiceUnitHasStructuredFields(item)
	if hasRawUnit {
		if ensure == "present" && hasContent == hasSource {
			return ir.SystemdUnit{}, fmt.Errorf("%s:%d:%s: systemd.service_unit requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if hasStructuredFields {
			return ir.SystemdUnit{}, fmt.Errorf("%s:%d:%s: systemd.service_unit cannot combine content/source with structured service fields", item.Source.File, item.Source.Line, item.Source.Path)
		}
	} else if ensure == "present" {
		content, err = renderSystemdServiceUnit(unitName, item)
		if err != nil {
			return ir.SystemdUnit{}, err
		}
		hasContent = true
	}
	sensitive := rawContentSensitive || systemdServiceUnitStructuredSensitive(item)
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	group, err := stringFieldDefault(item, "file_group", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	unit := ir.SystemdUnit{
		Name:       unitName,
		Path:       "/etc/systemd/system/" + unitName,
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
		unit.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(unit.SourcePath, item.Source)
		if err != nil {
			return ir.SystemdUnit{}, err
		}
		unit.Summary = summary
	}
	return unit, nil
}

func systemdServiceUnitHasStructuredFields(item parser.Value) bool {
	for _, name := range []string{
		"description", "run", "type", "user", "group", "working_dir",
		"environment", "restart", "restart_delay", "wants", "after",
		"wanted_by", "stdout", "stderr", "service_config",
	} {
		if _, ok := item.Map[name]; ok {
			return true
		}
	}
	return false
}

func systemdServiceUnitStructuredSensitive(item parser.Value) bool {
	for _, name := range []string{
		"description", "run", "type", "user", "group", "working_dir",
		"environment", "restart", "restart_delay", "wants", "after",
		"wanted_by", "stdout", "stderr", "service_config",
	} {
		if value, ok := item.Map[name]; ok && contentNeedsRedaction(value) {
			return true
		}
	}
	return false
}

func contentNeedsRedaction(value parser.Value) bool {
	return value.ContainsSensitive() || value.ContainsEphemeral()
}

func renderSystemdServiceUnit(unitName string, item parser.Value) (string, error) {
	run, ok, err := systemdRunCommandField(item, "run")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s:%d:%s.run: systemd.service_unit requires run unless content or source is used", item.Source.File, item.Source.Line, item.Source.Path)
	}
	description, ok, err := stringField(item, "description")
	if err != nil {
		return "", err
	}
	if !ok {
		description = strings.TrimSuffix(unitName, ".service")
	} else if strings.TrimSpace(description) == "" {
		return "", fmt.Errorf("%s:%d:%s.description: must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	unitLines := []string{}
	if err := appendSystemdAssignment(&unitLines, "Description", description, item.Source); err != nil {
		return "", err
	}
	wants, err := stringListField(item, "wants")
	if err != nil {
		return "", err
	}
	if len(wants) > 0 {
		value, err := systemdUnitListValue(wants, item.Map["wants"].Source)
		if err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&unitLines, "Wants", value, item.Map["wants"].Source); err != nil {
			return "", err
		}
	}
	after, err := stringListField(item, "after")
	if err != nil {
		return "", err
	}
	if len(after) > 0 {
		value, err := systemdUnitListValue(after, item.Map["after"].Source)
		if err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&unitLines, "After", value, item.Map["after"].Source); err != nil {
			return "", err
		}
	}

	serviceLines := []string{}
	if value, ok, err := stringField(item, "type"); err != nil {
		return "", err
	} else if ok {
		if err := validateSystemdServiceType(value, item.Map["type"].Source); err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&serviceLines, "Type", value, item.Map["type"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "user"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.user: must be non-empty", item.Source.File, item.Map["user"].Source.Line, item.Map["user"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "User", value, item.Map["user"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "group"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.group: must be non-empty", item.Source.File, item.Map["group"].Source.Line, item.Map["group"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "Group", value, item.Map["group"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "working_dir"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.working_dir: must be non-empty", item.Source.File, item.Map["working_dir"].Source.Line, item.Map["working_dir"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "WorkingDirectory", value, item.Map["working_dir"].Source); err != nil {
			return "", err
		}
	}
	environment, err := stringMapField(item, "environment")
	if err != nil {
		return "", err
	}
	if len(environment) > 0 {
		for _, name := range sortedKeys(environment) {
			if !systemdEnvironmentNamePattern.MatchString(name) {
				source := item.Map["environment"].Map[name].Source
				return "", fmt.Errorf("%s:%d:%s: environment variable name %q is invalid", source.File, source.Line, source.Path, name)
			}
			value := environment[name]
			source := item.Map["environment"].Map[name].Source
			if err := validateSystemdSingleLine(value, source); err != nil {
				return "", err
			}
			serviceLines = append(serviceLines, "Environment="+systemdQuoteToken(name+"="+value))
		}
	}
	if err := appendSystemdAssignment(&serviceLines, "ExecStart", run, item.Map["run"].Source); err != nil {
		return "", err
	}
	if value, ok, err := stringField(item, "restart"); err != nil {
		return "", err
	} else if ok {
		if err := validateSystemdRestart(value, item.Map["restart"].Source); err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&serviceLines, "Restart", value, item.Map["restart"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "restart_delay"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.restart_delay: must be non-empty", item.Source.File, item.Map["restart_delay"].Source.Line, item.Map["restart_delay"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "RestartSec", value, item.Map["restart_delay"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "stdout"); err != nil {
		return "", err
	} else if ok {
		if err := appendSystemdAssignment(&serviceLines, "StandardOutput", value, item.Map["stdout"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "stderr"); err != nil {
		return "", err
	} else if ok {
		if err := appendSystemdAssignment(&serviceLines, "StandardError", value, item.Map["stderr"].Source); err != nil {
			return "", err
		}
	}
	serviceConfig, err := systemdSectionField(item, "service_config")
	if err != nil {
		return "", err
	}
	if len(serviceConfig) > 0 {
		if err := appendSystemdSection(&serviceLines, "", serviceConfig, item.Map["service_config"].Source); err != nil {
			return "", err
		}
	}

	wantedBy, hasWantedBy, err := optionalStringListField(item, "wanted_by")
	if err != nil {
		return "", err
	}
	wantedBySource := item.Source
	if !hasWantedBy {
		wantedBy = []string{"multi-user.target"}
	} else {
		wantedBySource = item.Map["wanted_by"].Source
	}
	installLines := []string{}
	if len(wantedBy) > 0 {
		value, err := systemdUnitListValue(wantedBy, wantedBySource)
		if err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&installLines, "WantedBy", value, wantedBySource); err != nil {
			return "", err
		}
	}

	lines := []string{"[Unit]"}
	lines = append(lines, unitLines...)
	lines = append(lines, "", "[Service]")
	lines = append(lines, serviceLines...)
	if len(installLines) > 0 {
		lines = append(lines, "", "[Install]")
		lines = append(lines, installLines...)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func systemdTimerSpec(name string, item parser.Value) (ir.SystemdTimer, error) {
	timerName := timerUnitName(name)
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	timerSection, err := systemdSectionField(item, "timer")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if ensure == "present" && len(timerSection) == 0 {
		return ir.SystemdTimer{}, fmt.Errorf("%s:%d:%s.timer: systemd.timer requires timer section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
	}
	install, err := systemdSectionField(item, "install")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if install == nil {
		install = ir.NetworkdSection{}
	}
	wantedBy, hasWantedBy, err := optionalStringListField(item, "wanted_by")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if !hasWantedBy {
		wantedBy = []string{"timers.target"}
	}
	if len(wantedBy) > 0 {
		install["WantedBy"] = append([]string(nil), wantedBy...)
	}
	content := ""
	if ensure == "present" {
		description, ok, err := stringField(item, "description")
		if err != nil {
			return ir.SystemdTimer{}, err
		}
		if !ok {
			description = strings.TrimSuffix(timerName, ".timer")
		} else if strings.TrimSpace(description) == "" {
			return ir.SystemdTimer{}, fmt.Errorf("%s:%d:%s.description: must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		content, err = renderSystemdTimerUnit(description, timerSection, install, item.Source)
		if err != nil {
			return ir.SystemdTimer{}, err
		}
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	group, err := stringFieldDefault(item, "file_group", "root")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	var enable *bool
	if value, ok, err := boolField(item, "enable"); err != nil {
		return ir.SystemdTimer{}, err
	} else if ok {
		enable = &value
	}
	state, _, err := stringField(item, "state")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if state != "" && !stringIn(state, "running", "stopped") {
		return ir.SystemdTimer{}, fmt.Errorf("%s:%d:%s.state: systemd.timer state must be running or stopped", item.Source.File, item.Source.Line, item.Source.Path)
	}
	unit := ir.SystemdUnit{
		Name:      timerName,
		Path:      "/etc/systemd/system/" + timerName,
		Content:   content,
		Owner:     owner,
		Group:     group,
		Mode:      mode,
		Sensitive: systemdSectionNeedsRedaction(item, "timer") || systemdSectionNeedsRedaction(item, "install"),
		Ensure:    ensure,
		Lifecycle: lifecycle,
		Source:    item.Source,
	}
	if content != "" {
		unit.Summary = contentSummary([]byte(content))
	}
	return ir.SystemdTimer{Name: timerName, Unit: unit, Timer: timerSection, Install: install, Enable: enable, State: state, Source: item.Source}, nil
}

func renderSystemdTimerUnit(description string, timer ir.NetworkdSection, install ir.NetworkdSection, source ir.SourceRef) (string, error) {
	lines := []string{"[Unit]"}
	if err := appendSystemdAssignment(&lines, "Description", description, source); err != nil {
		return "", err
	}
	if err := appendSystemdSection(&lines, "Timer", timer, source); err != nil {
		return "", err
	}
	if len(install) > 0 {
		if err := appendSystemdSection(&lines, "Install", install, source); err != nil {
			return "", err
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func systemdDropInUnit(item parser.Value, name string, path string, sectionName string, section ir.NetworkdSection, ensure string) (ir.SystemdUnit, error) {
	content := ""
	if ensure == "present" {
		lines := []string{}
		if err := appendSystemdSection(&lines, sectionName, section, item.Source); err != nil {
			return ir.SystemdUnit{}, err
		}
		content = strings.Join(lines, "\n") + "\n"
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	group, err := stringFieldDefault(item, "file_group", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	unit := ir.SystemdUnit{
		Name:      name,
		Path:      path,
		Content:   content,
		Owner:     owner,
		Group:     group,
		Mode:      mode,
		Sensitive: systemdAnySectionNeedsRedaction(item),
		Ensure:    ensure,
		Lifecycle: lifecycle,
		Source:    item.Source,
	}
	if content != "" {
		unit.Summary = contentSummary([]byte(content))
	}
	return unit, nil
}

func systemdRunCommandField(root parser.Value, name string) (string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return "", false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return "", false, err
	}
	if str, ok := value.StringValue(); ok {
		if strings.TrimSpace(str) == "" {
			return "", false, fmt.Errorf("%s:%d:%s: must be a non-empty string or list of strings", value.Source.File, value.Source.Line, value.Source.Path)
		}
		if err := validateSystemdSingleLine(str, value.Source); err != nil {
			return "", false, err
		}
		return str, true, nil
	}
	if !value.IsList() {
		return "", false, fmt.Errorf("%s:%d:%s: must be a non-empty string or list of strings", value.Source.File, value.Source.Line, value.Source.Path)
	}
	if len(value.List) == 0 {
		return "", false, fmt.Errorf("%s:%d:%s: must be a non-empty string or list of strings", value.Source.File, value.Source.Line, value.Source.Path)
	}
	args := make([]string, 0, len(value.List))
	for _, item := range value.List {
		arg, ok := item.StringValue()
		if !ok || arg == "" {
			return "", false, fmt.Errorf("%s:%d:%s: run entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if err := validateSystemdSingleLine(arg, item.Source); err != nil {
			return "", false, err
		}
		args = append(args, systemdQuoteToken(arg))
	}
	return strings.Join(args, " "), true, nil
}

func optionalStringListField(root parser.Value, name string) ([]string, bool, error) {
	if _, ok := root.Map[name]; !ok {
		return nil, false, nil
	}
	values, err := stringListField(root, name)
	if err != nil {
		return nil, false, err
	}
	return values, true, nil
}

func stringMapField(root parser.Value, name string) (map[string]string, error) {
	value, ok, err := mapField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	if err := rejectEphemeralValue(value); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(value.Map))
	for _, key := range sortedKeys(value.Map) {
		item := value.Map[key]
		str, ok := item.StringValue()
		if !ok {
			return nil, fmt.Errorf("%s:%d:%s: map values must be strings", item.Source.File, item.Source.Line, item.Source.Path)
		}
		out[key] = str
	}
	return out, nil
}

func appendSystemdAssignment(lines *[]string, key string, value string, source ir.SourceRef) error {
	if err := validateSystemdSingleLine(value, source); err != nil {
		return err
	}
	*lines = append(*lines, key+"="+value)
	return nil
}

func validateSystemdSingleLine(value string, source ir.SourceRef) error {
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%s:%d:%s: systemd unit values must be single-line strings", source.File, source.Line, source.Path)
	}
	return nil
}

func systemdUnitListValue(values []string, source ir.SourceRef) (string, error) {
	for _, value := range values {
		if value == "" || strings.ContainsAny(value, " \t\n\r\x00") {
			return "", fmt.Errorf("%s:%d:%s: systemd unit names must be non-empty strings without whitespace", source.File, source.Line, source.Path)
		}
	}
	return strings.Join(values, " "), nil
}

func systemdQuoteToken(value string) string {
	if systemdSafeTokenPattern.MatchString(value) {
		return value
	}
	return strconv.Quote(value)
}

func validateSystemdServiceType(value string, source ir.SourceRef) error {
	switch value {
	case "simple", "exec", "forking", "oneshot", "dbus", "notify", "notify-reload", "idle":
		return nil
	default:
		return fmt.Errorf("%s:%d:%s: service type must be simple, exec, forking, oneshot, dbus, notify, notify-reload, or idle", source.File, source.Line, source.Path)
	}
}

func validateSystemdRestart(value string, source ir.SourceRef) error {
	switch value {
	case "no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always":
		return nil
	default:
		return fmt.Errorf("%s:%d:%s: restart must be no, on-success, on-failure, on-abnormal, on-watchdog, on-abort, or always", source.File, source.Line, source.Path)
	}
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

func nftablesSpec(nftables parser.Value) (ir.NftablesSpec, error) {
	spec := ir.NftablesSpec{
		Files:  map[string]ir.NftablesFileSpec{},
		Source: nftables.Source,
	}
	if enable, ok, err := boolField(nftables, "enable"); err != nil {
		return spec, err
	} else if ok {
		value := enable
		spec.Enable = &value
	}
	if main, ok, err := mapField(nftables, "main"); err != nil {
		return spec, err
	} else if ok {
		compiled, err := nftablesFileSpec("main", main, "/etc/nftables.conf")
		if err != nil {
			return spec, err
		}
		spec.Main = &compiled
	}
	objects, ok, err := objectCollection(nftables, "file")
	if err != nil {
		return spec, err
	}
	if ok {
		for _, label := range sortedKeys(objects) {
			item := objects[label]
			if label == "" {
				return spec, fmt.Errorf("%s:%d:%s: nftables file label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			compiled, err := nftablesFileSpec(label, item, "/etc/nftables.d/"+label+".nft")
			if err != nil {
				return spec, err
			}
			spec.Files[label] = compiled
		}
	}
	return spec, nil
}

func nftablesFileSpec(label string, item parser.Value, defaultPath string) (ir.NftablesFileSpec, error) {
	path, hasPath, err := stringField(item, "path")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	if !hasPath || path == "" {
		path = defaultPath
	}
	if !filepath.IsAbs(path) {
		return ir.NftablesFileSpec{}, fmt.Errorf("%s:%d:%s.path: nftables file path must be absolute", item.Source.File, item.Source.Line, item.Source.Path)
	}
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	if hasContent && hasSource {
		return ir.NftablesFileSpec{}, fmt.Errorf("%s:%d:%s: nftables file requires exactly one of content or source", item.Source.File, item.Source.Line, item.Source.Path)
	}
	if ensure == "present" && !hasContent && !hasSource {
		return ir.NftablesFileSpec{}, fmt.Errorf("%s:%d:%s: nftables file requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	sensitive, ok, err := boolField(item, "sensitive")
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	if !ok {
		sensitive = false
	}
	validate, err := boolFieldDefault(item, "validate", true)
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	activate, err := boolFieldDefault(item, "activate", true)
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.NftablesFileSpec{}, err
	}
	file := ir.NftablesFileSpec{
		Label:      label,
		Path:       path,
		Content:    content,
		SourcePath: resolvePath(item.Source.File, sourcePath),
		Owner:      owner,
		Group:      group,
		Mode:       mode,
		Sensitive:  sensitive,
		Validate:   validate,
		Activate:   activate,
		Ensure:     ensure,
		Lifecycle:  lifecycle,
		Source:     item.Source,
	}
	if hasContent {
		file.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(file.SourcePath, item.Source)
		if err != nil {
			return ir.NftablesFileSpec{}, err
		}
		file.Summary = summary
	}
	return file, nil
}

func dockerSpec(docker parser.Value) (*ir.DockerSpec, error) {
	enable, ok, err := boolField(docker, "enable")
	if err != nil {
		return nil, err
	}
	if !ok {
		enable = false
	}
	users, err := dockerUsersField(docker)
	if err != nil {
		return nil, err
	}
	packageSpec, err := dockerPackageSpec(docker)
	if err != nil {
		return nil, err
	}
	serviceSpec, err := dockerServiceSpec(docker)
	if err != nil {
		return nil, err
	}
	daemon, err := dockerDaemonSpec(docker)
	if err != nil {
		return nil, err
	}
	composes, err := dockerComposeSpecs(docker)
	if err != nil {
		return nil, err
	}
	return &ir.DockerSpec{
		Enable:   enable,
		Package:  packageSpec,
		Service:  serviceSpec,
		Daemon:   daemon,
		Users:    users,
		Composes: composes,
		Source:   docker.Source,
	}, nil
}

func dockerUsersField(docker parser.Value) ([]string, error) {
	list, ok, err := listField(docker, "users")
	if err != nil || !ok {
		return nil, err
	}
	out := make([]string, 0, len(list.List))
	seen := map[string]struct{}{}
	for _, item := range list.List {
		value, ok := item.StringValue()
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s:%d:%s: docker users entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate docker users entry %q", item.Source.File, item.Source.Line, item.Source.Path, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func dockerPackageSpec(docker parser.Value) (ir.DockerPackageSpec, error) {
	packageBlock, ok, err := mapField(docker, "package")
	if err != nil {
		return ir.DockerPackageSpec{}, err
	}
	sourceRef := docker.Source
	source := "official"
	channel := "stable"
	removeConflicts := "auto"
	var version *string
	if ok {
		sourceRef = packageBlock.Source
		if value, hasValue, err := stringField(packageBlock, "source"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			source = value
		}
		if value, hasValue, err := stringField(packageBlock, "channel"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			channel = value
		}
		if value, hasValue, err := optionalStringField(packageBlock, "version"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			version = value
		}
		if value, hasValue, err := dockerRemoveConflictsField(packageBlock, "remove_conflicts"); err != nil {
			return ir.DockerPackageSpec{}, err
		} else if hasValue {
			removeConflicts = value
		}
	}
	if !stringIn(source, "official", "debian", "none", "custom") {
		errSource := sourceRef
		if ok {
			errSource = packageBlock.Map["source"].Source
		}
		return ir.DockerPackageSpec{}, enumError(errSource, "official, debian, none, or custom")
	}
	if channel == "" {
		return ir.DockerPackageSpec{}, fmt.Errorf("%s:%d:%s.channel: docker package channel must be non-empty", sourceRef.File, sourceRef.Line, sourceRef.Path)
	}
	return ir.DockerPackageSpec{
		Source:          source,
		Channel:         channel,
		Version:         version,
		RemoveConflicts: removeConflicts,
		SourceRef:       sourceRef,
	}, nil
}

func dockerServiceSpec(docker parser.Value) (ir.DockerServiceSpec, error) {
	serviceBlock, ok, err := mapField(docker, "service")
	if err != nil {
		return ir.DockerServiceSpec{}, err
	}
	sourceRef := docker.Source
	enable := true
	state := "running"
	if ok {
		sourceRef = serviceBlock.Source
		if value, hasValue, err := boolField(serviceBlock, "enable"); err != nil {
			return ir.DockerServiceSpec{}, err
		} else if hasValue {
			enable = value
		}
		if value, hasValue, err := stringField(serviceBlock, "state"); err != nil {
			return ir.DockerServiceSpec{}, err
		} else if hasValue {
			state = value
		}
	}
	if !stringIn(state, "running", "stopped") {
		errSource := sourceRef
		if ok {
			errSource = serviceBlock.Map["state"].Source
		}
		return ir.DockerServiceSpec{}, enumError(errSource, "running or stopped")
	}
	return ir.DockerServiceSpec{
		Enable:    enable,
		State:     state,
		Name:      "docker.service",
		SourceRef: sourceRef,
	}, nil
}

func dockerDaemonSpec(docker parser.Value) (*ir.DockerDaemonSpec, error) {
	daemon, ok, err := mapField(docker, "daemon")
	if err != nil || !ok {
		return nil, err
	}
	settingsValue, ok := daemon.Map["settings"]
	if !ok {
		settingsValue = parser.MapValue(nil, ir.SourceRef{File: daemon.Source.File, Line: daemon.Source.Line, Path: daemon.Source.Path + ".settings"})
	}
	if !settingsValue.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: docker daemon settings must be a map", settingsValue.Source.File, settingsValue.Source.Line, settingsValue.Source.Path)
	}
	settings, err := jsonCompatibleAny(settingsValue)
	if err != nil {
		return nil, err
	}
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s:%d:%s: docker daemon settings must be a map", settingsValue.Source.File, settingsValue.Source.Line, settingsValue.Source.Path)
	}
	summary, err := jsonContentSummary(settingsMap, daemon.Source)
	if err != nil {
		return nil, err
	}
	return &ir.DockerDaemonSpec{Settings: settingsMap, Source: daemon.Source, Summary: summary}, nil
}

func dockerComposeSpecs(docker parser.Value) (map[string]ir.DockerComposeSpec, error) {
	objects, ok, err := objectCollection(docker, "compose")
	if err != nil || !ok {
		return map[string]ir.DockerComposeSpec{}, err
	}
	out := make(map[string]ir.DockerComposeSpec, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: docker compose label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if err := validateDockerStableName("docker compose label", name, item.Source); err != nil {
			return nil, err
		}
		compose, err := dockerComposeSpec(name, item)
		if err != nil {
			return nil, err
		}
		out[name] = compose
	}
	return out, nil
}

func dockerComposeSpec(name string, item parser.Value) (ir.DockerComposeSpec, error) {
	enable, ok, err := boolField(item, "enable")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !ok {
		enable = true
	}
	state, err := stringFieldDefault(item, "state", "running")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !stringIn(state, "running", "stopped", "absent") {
		return ir.DockerComposeSpec{}, enumError(item.Map["state"].Source, "running, stopped, or absent")
	}
	directory, _, err := stringField(item, "directory")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if enable && (directory == "" || !filepath.IsAbs(directory)) {
		return ir.DockerComposeSpec{}, fmt.Errorf("%s:%d:%s.directory: docker compose directory must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	project, err := stringFieldDefault(item, "project", name)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if project == "" {
		return ir.DockerComposeSpec{}, fmt.Errorf("%s:%d:%s.project: docker compose project must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	projectSource := item.Source
	if value, ok := item.Map["project"]; ok {
		projectSource = value.Source
	}
	if err := validateDockerStableName("docker compose project", project, projectSource); err != nil {
		return ir.DockerComposeSpec{}, err
	}
	pull, err := stringFieldDefault(item, "pull", "missing")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !stringIn(pull, "never", "missing", "always") {
		return ir.DockerComposeSpec{}, enumError(item.Map["pull"].Source, "never, missing, or always")
	}
	recreate, err := stringFieldDefault(item, "recreate", "auto")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !stringIn(recreate, "auto", "always", "never") {
		return ir.DockerComposeSpec{}, enumError(item.Map["recreate"].Source, "auto, always, or never")
	}
	removeOrphans, ok, err := boolField(item, "remove_orphans")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if !ok {
		removeOrphans = false
	}
	after, err := stringListField(item, "after")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if len(after) == 0 {
		after = []string{"docker.service", "network-online.target"}
	}
	wantedBy, err := stringListField(item, "wanted_by")
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if len(wantedBy) == 0 {
		wantedBy = []string{"multi-user.target"}
	}
	composeFile, err := dockerComposeMainFileSpec(item)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	envFiles, err := dockerComposeEnvFileSpecs(item)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	if enable && composeFile == nil {
		return ir.DockerComposeSpec{}, fmt.Errorf("%s:%d:%s.file: docker compose file block is required", item.Source.File, item.Source.Line, item.Source.Path)
	}
	service, err := dockerComposeServiceSpec(name, item)
	if err != nil {
		return ir.DockerComposeSpec{}, err
	}
	return ir.DockerComposeSpec{
		Name:          name,
		Enable:        enable,
		State:         state,
		Directory:     directory,
		Project:       project,
		File:          composeFile,
		EnvFiles:      envFiles,
		Pull:          pull,
		Recreate:      recreate,
		RemoveOrphans: removeOrphans,
		Service:       service,
		After:         after,
		WantedBy:      wantedBy,
		Source:        item.Source,
	}, nil
}

func dockerComposeMainFileSpec(compose parser.Value) (*ir.DockerComposeFileSpec, error) {
	fileBlock, ok, err := mapField(compose, "file")
	if err != nil || !ok {
		return nil, err
	}
	file, err := dockerComposeFileSpec("", fileBlock, "0644", false)
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func dockerComposeEnvFileSpecs(compose parser.Value) (map[string]ir.DockerComposeFileSpec, error) {
	objects, ok, err := objectCollection(compose, "env_file")
	if err != nil || !ok {
		return map[string]ir.DockerComposeFileSpec{}, err
	}
	out := make(map[string]ir.DockerComposeFileSpec, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		if label == "" {
			return nil, fmt.Errorf("%s:%d:%s: docker compose env_file label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		file, err := dockerComposeFileSpec(label, item, "0600", true)
		if err != nil {
			return nil, err
		}
		out[label] = file
	}
	return out, nil
}

func dockerComposeFileSpec(label string, item parser.Value, defaultMode string, defaultSensitive bool) (ir.DockerComposeFileSpec, error) {
	path, ok, err := stringField(item, "path")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	if !ok || path == "" || !filepath.IsAbs(path) {
		return ir.DockerComposeFileSpec{}, fmt.Errorf("%s:%d:%s.path: docker compose file path must be absolute and non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	if hasContent == hasSource {
		return ir.DockerComposeFileSpec{}, fmt.Errorf("%s:%d:%s: docker compose file requires exactly one of content or source", item.Source.File, item.Source.Line, item.Source.Path)
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	mode, err := modeFieldDefault(item, "mode", defaultMode)
	if err != nil {
		return ir.DockerComposeFileSpec{}, err
	}
	sensitive := defaultSensitive
	if hasContent && contentNeedsRedaction(item.Map["content"]) {
		sensitive = true
	}
	out := ir.DockerComposeFileSpec{
		Label:      label,
		Path:       path,
		Content:    content,
		SourcePath: resolvePath(item.Source.File, sourcePath),
		Owner:      owner,
		Group:      group,
		Mode:       mode,
		Sensitive:  sensitive,
		Source:     item.Source,
	}
	if hasContent {
		out.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(out.SourcePath, item.Source)
		if err != nil {
			return ir.DockerComposeFileSpec{}, err
		}
		out.Summary = summary
	}
	return out, nil
}

func dockerComposeServiceSpec(name string, compose parser.Value) (ir.DockerComposeServiceSpec, error) {
	service, ok, err := mapField(compose, "service")
	if err != nil {
		return ir.DockerComposeServiceSpec{}, err
	}
	enable := true
	unitName := "debianform-compose-" + name
	if ok {
		if value, hasValue, err := boolField(service, "enable"); err != nil {
			return ir.DockerComposeServiceSpec{}, err
		} else if hasValue {
			enable = value
		}
		if value, hasValue, err := stringField(service, "name"); err != nil {
			return ir.DockerComposeServiceSpec{}, err
		} else if hasValue {
			unitName = value
		}
	}
	if unitName == "" {
		return ir.DockerComposeServiceSpec{}, fmt.Errorf("%s:%d:%s.service.name: docker compose service name must be non-empty", compose.Source.File, compose.Source.Line, compose.Source.Path)
	}
	serviceSource := compose.Source
	if ok {
		if value, exists := service.Map["name"]; exists {
			serviceSource = value.Source
		}
	}
	if err := validateDockerStableName("docker compose service name", unitName, serviceSource); err != nil {
		return ir.DockerComposeServiceSpec{}, err
	}
	return ir.DockerComposeServiceSpec{Enable: enable, Name: unitName}, nil
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
	for _, label := range sortedKeys(spec.APT.SourceFiles) {
		item := spec.APT.SourceFiles[label]
		if previous, exists := files[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: apt source_file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: apt source_file path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		files[item.Path] = item.Source
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
	for _, unit := range spec.Systemd.Units {
		units[unit.Name] = unit
		files[unit.Path] = unit.Source
	}
	for name, timer := range spec.Systemd.Timers {
		unit := timer.Unit
		if previous, exists := units[unit.Name]; exists {
			return fmt.Errorf("%s:%d:%s: systemd timer %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		if previous, exists := files[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd timer path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd timer path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		units[name] = unit
		files[unit.Path] = unit.Source
	}
	if spec.Systemd.Resolved != nil {
		unit := spec.Systemd.Resolved.Unit
		if previous, exists := files[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd resolved path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd resolved path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		files[unit.Path] = unit.Source
	}
	if spec.Systemd.Journald != nil {
		unit := spec.Systemd.Journald.Unit
		if previous, exists := files[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd journald path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		if previous, exists := secrets[unit.Path]; exists {
			return fmt.Errorf("%s:%d:%s: systemd journald path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Path, previous.File, previous.Line, previous.Path)
		}
		files[unit.Path] = unit.Source
	}
	if spec.Systemd.Networkd != nil {
		if err := validateNetworkdSpecPaths(*spec.Systemd.Networkd, files, secrets); err != nil {
			return err
		}
		for _, label := range sortedKeys(spec.Systemd.Networkd.NetDevs) {
			item := spec.Systemd.Networkd.NetDevs[label]
			files[item.Path] = item.Source
		}
		for _, label := range sortedKeys(spec.Systemd.Networkd.Networks) {
			item := spec.Systemd.Networkd.Networks[label]
			files[item.Path] = item.Source
		}
	}
	for name, service := range spec.Services.Services {
		services[name] = service
	}
	if spec.Nftables.Main != nil {
		if err := validateNftablesPath(spec.Nftables.Main, files, secrets); err != nil {
			return err
		}
		files[spec.Nftables.Main.Path] = spec.Nftables.Main.Source
	}
	for _, label := range sortedKeys(spec.Nftables.Files) {
		item := spec.Nftables.Files[label]
		if err := validateNftablesPath(&item, files, secrets); err != nil {
			return err
		}
		files[item.Path] = item.Source
	}
	if spec.Docker != nil {
		if spec.Docker.Daemon != nil {
			daemonSource := spec.Docker.Daemon.Source
			if previous, exists := files["/etc/docker/daemon.json"]; exists {
				return fmt.Errorf("%s:%d:%s: docker daemon path %q conflicts with file declared at %s:%d:%s", daemonSource.File, daemonSource.Line, daemonSource.Path, "/etc/docker/daemon.json", previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets["/etc/docker/daemon.json"]; exists {
				return fmt.Errorf("%s:%d:%s: docker daemon path %q conflicts with secret declared at %s:%d:%s", daemonSource.File, daemonSource.Line, daemonSource.Path, "/etc/docker/daemon.json", previous.File, previous.Line, previous.Path)
			}
			files["/etc/docker/daemon.json"] = daemonSource
		}
		for _, name := range sortedKeys(spec.Docker.Composes) {
			compose := spec.Docker.Composes[name]
			if compose.Service.Enable {
				unitName := serviceUnitName(compose.Service.Name)
				unitPath := "/etc/systemd/system/" + unitName
				if previous, exists := files[unitPath]; exists {
					return fmt.Errorf("%s:%d:%s.service.name: docker compose systemd unit path %q conflicts with file declared at %s:%d:%s", compose.Source.File, compose.Source.Line, compose.Source.Path, unitPath, previous.File, previous.Line, previous.Path)
				}
				if previous, exists := secrets[unitPath]; exists {
					return fmt.Errorf("%s:%d:%s.service.name: docker compose systemd unit path %q conflicts with secret declared at %s:%d:%s", compose.Source.File, compose.Source.Line, compose.Source.Path, unitPath, previous.File, previous.Line, previous.Path)
				}
				files[unitPath] = compose.Source
			}
			if compose.File != nil {
				if err := validateDockerComposePath("docker compose file", *compose.File, files, secrets); err != nil {
					return err
				}
				files[compose.File.Path] = compose.File.Source
			}
			for _, label := range sortedKeys(compose.EnvFiles) {
				envFile := compose.EnvFiles[label]
				if err := validateDockerComposePath("docker compose env_file", envFile, files, secrets); err != nil {
					return err
				}
				files[envFile.Path] = envFile.Source
			}
		}
	}

	for _, component := range spec.Components {
		if component.Install != nil {
			path := component.Install.Path
			if previous, exists := files[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q artifact path %q conflicts with file declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q artifact path %q conflicts with secret declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := directories[path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q artifact path %q conflicts with directory declared at %s:%d:%s", component.Install.Source.File, component.Install.Source.Line, component.Install.Source.Path, component.Name, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
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
		for _, label := range sortedKeys(component.APT.SourceFiles) {
			item := component.APT.SourceFiles[label]
			if previous, exists := files[item.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q apt source_file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, component.Name, item.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[item.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q apt source_file path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, component.Name, item.Path, previous.File, previous.Line, previous.Path)
			}
			files[item.Path] = item.Source
		}
		for _, pkg := range component.Packages.Install {
			if previous, exists := packages[pkg.Name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q package %q conflicts with package declared at %s:%d:%s", pkg.Source.File, pkg.Source.Line, pkg.Source.Path, component.Name, pkg.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			packages[pkg.Name] = pkg
		}
		for path, directory := range component.Directories.Directories {
			if previous, exists := directories[path]; exists {
				if !sameManagedDirectory(directory, previous) {
					return fmt.Errorf("%s:%d:%s: component %q directory %q conflicts with directory declared at %s:%d:%s", directory.Source.File, directory.Source.Line, directory.Source.Path, component.Name, path, previous.Source.File, previous.Source.Line, previous.Source.Path)
				}
				continue
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
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd unit path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd unit path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			units[unit.Name] = unit
			files[unit.Path] = unit.Source
		}
		for name, timer := range component.Systemd.Timers {
			unit := timer.Unit
			if previous, exists := units[unit.Name]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd timer %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
			}
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd timer path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd timer path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			units[name] = unit
			files[unit.Path] = unit.Source
		}
		if component.Systemd.Resolved != nil {
			unit := component.Systemd.Resolved.Unit
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd resolved path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd resolved path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			files[unit.Path] = unit.Source
		}
		if component.Systemd.Journald != nil {
			unit := component.Systemd.Journald.Unit
			if previous, exists := files[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd journald path %q conflicts with file declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			if previous, exists := secrets[unit.Path]; exists {
				return fmt.Errorf("%s:%d:%s: component %q systemd journald path %q conflicts with secret declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, component.Name, unit.Path, previous.File, previous.Line, previous.Path)
			}
			files[unit.Path] = unit.Source
		}
		if component.Systemd.Networkd != nil {
			if err := validateNetworkdSpecPaths(*component.Systemd.Networkd, files, secrets); err != nil {
				return err
			}
			for _, label := range sortedKeys(component.Systemd.Networkd.NetDevs) {
				item := component.Systemd.Networkd.NetDevs[label]
				files[item.Path] = item.Source
			}
			for _, label := range sortedKeys(component.Systemd.Networkd.Networks) {
				item := component.Systemd.Networkd.Networks[label]
				files[item.Path] = item.Source
			}
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

func sameManagedDirectory(a, b ir.ManagedDirectory) bool {
	return a.Path == b.Path &&
		a.Owner == b.Owner &&
		a.Group == b.Group &&
		a.Mode == b.Mode &&
		a.Ensure == b.Ensure &&
		reflect.DeepEqual(a.Lifecycle, b.Lifecycle)
}

func validateNftablesPath(item *ir.NftablesFileSpec, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	if item == nil {
		return nil
	}
	if previous, exists := files[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: nftables file path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
	}
	if previous, exists := secrets[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: nftables file path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
	}
	return nil
}

func validateDockerComposePath(kind string, item ir.DockerComposeFileSpec, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	if previous, exists := files[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, kind, item.Path, previous.File, previous.Line, previous.Path)
	}
	if previous, exists := secrets[item.Path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with secret declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, kind, item.Path, previous.File, previous.Line, previous.Path)
	}
	return nil
}

func validateNetworkdSpecPaths(spec ir.NetworkdSpec, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	seen := map[string]ir.SourceRef{}
	for _, label := range sortedKeys(spec.NetDevs) {
		item := spec.NetDevs[label]
		if err := validateNetworkdPath("networkd netdev", item.Path, item.Source, files, secrets); err != nil {
			return err
		}
		if previous, exists := seen[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: networkd netdev path %q conflicts with networkd file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		seen[item.Path] = item.Source
	}
	for _, label := range sortedKeys(spec.Networks) {
		item := spec.Networks[label]
		if err := validateNetworkdPath("networkd network", item.Path, item.Source, files, secrets); err != nil {
			return err
		}
		if previous, exists := seen[item.Path]; exists {
			return fmt.Errorf("%s:%d:%s: networkd network path %q conflicts with networkd file declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, item.Path, previous.File, previous.Line, previous.Path)
		}
		seen[item.Path] = item.Source
	}
	return nil
}

func validateNetworkdPath(kind string, path string, source ir.SourceRef, files map[string]ir.SourceRef, secrets map[string]ir.SourceRef) error {
	if previous, exists := files[path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with file declared at %s:%d:%s", source.File, source.Line, source.Path, kind, path, previous.File, previous.Line, previous.Path)
	}
	if previous, exists := secrets[path]; exists {
		return fmt.Errorf("%s:%d:%s: %s path %q conflicts with secret declared at %s:%d:%s", source.File, source.Line, source.Path, kind, path, previous.File, previous.Line, previous.Path)
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

func optionalStringField(root parser.Value, name string) (*string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return nil, false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return nil, false, err
	}
	if value.Kind == parser.KindNull {
		return nil, true, nil
	}
	str, ok := value.StringValue()
	if !ok {
		return nil, false, fmt.Errorf("%s:%d: %s must be a string or null", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return &str, true, nil
}

func dockerRemoveConflictsField(root parser.Value, name string) (string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return "", false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return "", false, err
	}
	switch value.Kind {
	case parser.KindString:
		if !stringIn(value.String, "auto", "true", "false") {
			return "", false, enumError(value.Source, "auto, true, or false")
		}
		return value.String, true, nil
	case parser.KindBool:
		if value.Bool {
			return "true", true, nil
		}
		return "false", true, nil
	default:
		return "", false, fmt.Errorf("%s:%d:%s: %s must be auto, true, or false", value.Source.File, value.Source.Line, value.Source.Path, name)
	}
}

func jsonCompatibleAny(value parser.Value) (any, error) {
	if value.ContainsSensitive() || value.ContainsEphemeral() {
		return nil, fmt.Errorf("%s:%d:%s: docker daemon settings cannot contain sensitive or ephemeral values", value.Source.File, value.Source.Line, value.Source.Path)
	}
	switch value.Kind {
	case parser.KindNull:
		return nil, nil
	case parser.KindString:
		return value.String, nil
	case parser.KindBool:
		return value.Bool, nil
	case parser.KindNumber:
		return json.Number(value.Number), nil
	case parser.KindList:
		out := make([]any, 0, len(value.List))
		for _, item := range value.List {
			converted, err := jsonCompatibleAny(item)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	case parser.KindMap:
		out := make(map[string]any, len(value.Map))
		for _, key := range sortedKeys(value.Map) {
			if key == "" {
				item := value.Map[key]
				return nil, fmt.Errorf("%s:%d:%s: docker daemon settings keys must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			converted, err := jsonCompatibleAny(value.Map[key])
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s:%d:%s: unsupported docker daemon settings value", value.Source.File, value.Source.Line, value.Source.Path)
	}
}

func jsonContentSummary(value any, source ir.SourceRef) (ir.ContentSummary, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return ir.ContentSummary{}, fmt.Errorf("%s:%d:%s: json marshal failed: %w", source.File, source.Line, source.Path, err)
	}
	return contentSummary(data), nil
}

func validateDockerStableName(kind string, value string, source ir.SourceRef) error {
	if value == "" {
		return fmt.Errorf("%s:%d:%s: %s must be non-empty", source.File, source.Line, source.Path, kind)
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			(i > 0 && (r == '_' || r == '.' || r == '@' || r == '%' || r == '+' || r == '-'))
		if !valid {
			return fmt.Errorf("%s:%d:%s: %s %q must use only letters, digits, _, ., @, %%, +, or - and must start with a letter or digit", source.File, source.Line, source.Path, kind, value)
		}
	}
	return nil
}

func stringIn(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func enumError(source ir.SourceRef, allowed string) error {
	return fmt.Errorf("%s:%d:%s: must be %s", source.File, source.Line, source.Path, allowed)
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

func boolFieldDefault(root parser.Value, name string, fallback bool) (bool, error) {
	value, ok, err := boolField(root, name)
	if err != nil {
		return false, err
	}
	if !ok {
		return fallback, nil
	}
	return value, nil
}

var (
	modePattern                   = regexp.MustCompile(`^0[0-7]{3}$`)
	sha256Pattern                 = regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)
	systemdEnvironmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	systemdSafeTokenPattern       = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)
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

func timerUnitName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".timer"
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
