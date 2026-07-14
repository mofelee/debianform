package merge

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/zclconf/go-cty/cty"
)

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
		spec.DeclarationID = script.Source.Path
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
	outputs := append([]string(nil), script.Outputs...)
	if script.OutputsSet {
		if len(outputs) == 0 {
			return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s: script outputs must contain at least one path", script.OutputsSource.File, script.OutputsSource.Line, script.OutputsSource.Path)
		}
		seen := map[string]struct{}{}
		for i, path := range outputs {
			if path == "" || !filepath.IsAbs(path) {
				return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s[%d]: script output path must be absolute and non-empty", script.OutputsSource.File, script.OutputsSource.Line, script.OutputsSource.Path, i)
			}
			if _, ok := seen[path]; ok {
				return ir.ComponentScriptSpec{}, fmt.Errorf("%s:%d:%s[%d]: duplicate script output path %q", script.OutputsSource.File, script.OutputsSource.Line, script.OutputsSource.Path, i, path)
			}
			seen[path] = struct{}{}
		}
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
		Outputs:     outputs,
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
		description := "component script"
		if strings.HasPrefix(script.Source.Path, "script[") {
			description = "root script"
		}
		return fmt.Errorf("%s:%d:%s: %s requires exactly one of run, content, or commands", script.Source.File, script.Source.Line, script.Source.Path, description)
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
		component, err := c.buildComponentSpec(instance, template, componentCtx, raw, target.Scripts)
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
		component, err := c.buildComponentSpec(instance, template, componentCtx, raw, target.Scripts)
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
	distributionValue := effectivePlatformDistribution(host)
	distribution := cty.StringVal(distributionValue)
	if distributionValue == "" {
		distribution = cty.UnknownVal(cty.String)
	}
	versionValue := effectivePlatformVersion(host)
	version := cty.StringVal(versionValue)
	if versionValue == "" {
		version = cty.UnknownVal(cty.String)
	}
	architectureValue := effectivePlatformArchitecture(host)
	architecture := cty.StringVal(architectureValue)
	if architectureValue == "" {
		architecture = cty.UnknownVal(cty.String)
	}
	codenameValue := effectivePlatformCodename(host)
	codename := cty.StringVal(codenameValue)
	if codenameValue == "" {
		codename = cty.UnknownVal(cty.String)
	}
	platform := cty.ObjectVal(map[string]cty.Value{
		"distribution": distribution,
		"version":      version,
		"architecture": architecture,
		"codename":     codename,
	})
	system := cty.ObjectVal(map[string]cty.Value{
		"hostname": cty.StringVal(host.System.Hostname),
		"timezone": cty.StringVal(host.System.Timezone),
		"locale":   cty.StringVal(host.System.Locale),
	})
	return cty.ObjectVal(map[string]cty.Value{
		"name":        cty.StringVal(host.Name),
		"ssh":         sshSpecToCty(host.SSH),
		"state":       stateSpecToCty(host.State),
		"platform":    platform,
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
	architecture := target.PlatformArchitecture()
	if architecture == "" {
		return parser.ComponentArtifactSource{}, fmt.Errorf("%s:%d:%s.platform.architecture: host %q must declare platform.architecture to select a component source", target.Source.File, target.Source.Line, target.Source.Path, target.Name)
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

func (c *compiler) buildComponentSpec(instance parser.ComponentInstance, template parser.Component, ctx parser.EvalContext, raw parser.Value, rootScripts map[string]ir.ComponentScriptSpec) (ir.ComponentInstanceSpec, error) {
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
		if err := validateFileOnChangeRefs(managedFiles, spec.Scripts, rootScripts); err != nil {
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
