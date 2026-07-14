package parser

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/zclconf/go-cty/cty"
)

type Config struct {
	Files                  []string
	Locals                 map[string]Value
	localDefinitions       map[string]localDefinition
	Variables              map[string]Variable
	VariableValues         map[string]Value
	ExplicitVariableValues map[string]bool
	Profiles               map[string]Profile
	Hosts                  map[string]Host
	Components             map[string]Component
	Scripts                map[string]ComponentScript
}

type Profile struct {
	Name    string
	Source  ir.SourceRef
	Imports []string
	Body    Value
	Asserts []Assert
}

type Host struct {
	Name       string
	Source     ir.SourceRef
	Imports    []string
	Components []ComponentInstance
	Body       Value
	Asserts    []Assert
}

type Component struct {
	Name      string
	Source    ir.SourceRef
	ModuleDir string
	Body      *hclsyntax.Body
	Inputs    map[string]ComponentInput
	Scripts   map[string]ComponentScript
	Type      string
	Version   string
	Sources   map[string]ComponentArtifactSource
	Extract   *ComponentArtifactExtract
	Build     *ComponentArtifactBuild
	Install   *ComponentArtifactInstall
}

type ComponentInput struct {
	Name        string
	Type        string
	TypeExpr    string
	TypeSpec    ComponentInputTypeSpec
	Description string
	Default     *Value
	Sensitive   bool
	Nullable    bool
	Deprecated  string
	Validations []ComponentInputValidation
	Source      ir.SourceRef
}

type Variable struct {
	Name        string
	Type        string
	TypeExpr    string
	TypeSpec    ComponentInputTypeSpec
	Description string
	Default     *Value
	Sensitive   bool
	Nullable    bool
	Ephemeral   bool
	Const       bool
	Deprecated  string
	Validations []VariableValidation
	Source      ir.SourceRef
}

type VariableValidation struct {
	Source          ir.SourceRef
	Condition       hcl.Expression
	ConditionSource ir.SourceRef
	Message         string
	MessageSource   ir.SourceRef
}

type ComponentInputValidation struct {
	Source          ir.SourceRef
	Condition       hcl.Expression
	ConditionSource ir.SourceRef
	Message         string
	MessageSource   ir.SourceRef
}

type ComponentScript struct {
	Name              string
	ModuleDir         string
	Mode              string
	Interpreter       []string
	Outputs           []string
	Run               string
	Content           string
	Commands          [][]string
	Sensitive         bool
	ModeSet           bool
	InterpreterSet    bool
	OutputsSet        bool
	RunSet            bool
	ContentSet        bool
	CommandsSet       bool
	ModeExpr          hcl.Expression
	InterpreterExpr   hcl.Expression
	OutputsExpr       hcl.Expression
	RunExpr           hcl.Expression
	ContentExpr       hcl.Expression
	CommandsExpr      hcl.Expression
	ModeSource        ir.SourceRef
	InterpreterSource ir.SourceRef
	OutputsSource     ir.SourceRef
	RunSource         ir.SourceRef
	ContentSource     ir.SourceRef
	CommandsSource    ir.SourceRef
	Source            ir.SourceRef
}

type ScriptReferenceScope string

const (
	ScriptReferenceAuto   ScriptReferenceScope = "auto"
	ScriptReferenceGlobal ScriptReferenceScope = "global"
)

type ScriptReference struct {
	Name   string
	Scope  ScriptReferenceScope
	Source ir.SourceRef
}

type ComponentArtifactSource struct {
	Architecture string
	URL          string
	SHA256       string
	Source       ir.SourceRef
}

type ComponentArtifactExtract struct {
	Format          string
	StripComponents int
	Include         string
	Source          ir.SourceRef
}

type ComponentArtifactBuild struct {
	Commands   [][]string
	Packages   []string
	WorkingDir string
	Output     string
	SourceName string
	Source     ir.SourceRef
}

type ComponentArtifactInstall struct {
	Path   string
	Owner  string
	Group  string
	Mode   string
	Source ir.SourceRef
}

type ComponentInstance struct {
	Name     string
	Template string
	Inputs   map[string]Value
	Source   ir.SourceRef
}

type Assert struct {
	Source          ir.SourceRef
	Condition       hcl.Expression
	ConditionSource ir.SourceRef
	Message         string
	MessageSource   ir.SourceRef
}

type ParseOptions struct {
	VariableValues        []ExternalVariableValue
	AllowMissingVariables bool
	SkipTopLevel          bool
}

type ExternalVariableValue struct {
	Name          string
	Value         string
	ParsedValue   *Value
	Source        ir.SourceRef
	IgnoreUnknown bool
}

func ParseFiles(files []string) (*Config, error) {
	return ParseFilesWithOptions(files, ParseOptions{})
}

func ParseFilesWithOptions(files []string, opts ParseOptions) (*Config, error) {
	cfg := &Config{
		Files:                  append([]string(nil), files...),
		Locals:                 map[string]Value{},
		localDefinitions:       map[string]localDefinition{},
		Variables:              map[string]Variable{},
		VariableValues:         map[string]Value{},
		ExplicitVariableValues: map[string]bool{},
		Profiles:               map[string]Profile{},
		Hosts:                  map[string]Host{},
		Components:             map[string]Component{},
		Scripts:                map[string]ComponentScript{},
	}

	type parsedFile struct {
		file string
		body *hclsyntax.Body
	}

	parsed := make([]parsedFile, 0, len(files))
	hclParser := hclparse.NewParser()
	for _, file := range files {
		hclFile, diags := hclParser.ParseHCLFile(file)
		if diags.HasErrors() {
			return nil, fmt.Errorf("%s", diags.Error())
		}
		body, ok := hclFile.Body.(*hclsyntax.Body)
		if !ok {
			return nil, fmt.Errorf("%s: unsupported HCL body type %T", file, hclFile.Body)
		}
		parsed = append(parsed, parsedFile{file: file, body: body})
	}

	for _, file := range parsed {
		if err := collectLocals(cfg, file.file, file.body); err != nil {
			return nil, err
		}
	}
	if err := evaluateStaticLocals(cfg); err != nil {
		return nil, err
	}

	for _, file := range parsed {
		if err := parseVariables(cfg, file.file, file.body); err != nil {
			return nil, err
		}
	}
	if err := resolveVariableValues(cfg, opts.VariableValues, opts.AllowMissingVariables); err != nil {
		return nil, err
	}
	if opts.SkipTopLevel {
		return cfg, nil
	}
	if err := validateVariableValues(cfg); err != nil {
		return nil, err
	}
	if err := evaluateLocals(cfg); err != nil {
		return nil, err
	}

	for _, file := range parsed {
		if err := parseTopLevel(cfg, file.file, file.body); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func collectLocals(cfg *Config, file string, body *hclsyntax.Body) error {
	for _, block := range body.Blocks {
		if block.Type != "locals" {
			continue
		}
		source := blockSource(file, "locals", block)
		if len(block.Labels) != 0 {
			return fmt.Errorf("%s:%d: locals block must not have labels", file, source.Line)
		}
		if len(block.Body.Blocks) != 0 {
			return fmt.Errorf("%s:%d: locals block does not support nested blocks", file, source.Line)
		}
		names := sortedAttrNames(block.Body.Attributes)
		for _, name := range names {
			attr := block.Body.Attributes[name]
			source := ir.SourceRef{
				File: file,
				Line: attr.NameRange.Start.Line,
				Path: "local." + name,
			}
			cfg.localDefinitions[name] = localDefinition{
				Name:      name,
				Expr:      attr.Expr,
				Source:    source,
				ModuleDir: filepath.Dir(file),
			}
		}
	}
	return nil
}

func parseVariables(cfg *Config, file string, body *hclsyntax.Body) error {
	ctx := EvalContext{ModuleDir: filepath.Dir(file), Locals: cfg.Locals}
	for _, block := range body.Blocks {
		if block.Type != "variable" {
			continue
		}
		variable, err := parseVariable(file, block, ctx)
		if err != nil {
			return err
		}
		if previous, exists := cfg.Variables[variable.Name]; exists {
			return fmt.Errorf("%s:%d: duplicate variable %q; first defined at %s:%d", file, variable.Source.Line, variable.Name, previous.Source.File, previous.Source.Line)
		}
		cfg.Variables[variable.Name] = variable
	}
	return nil
}

func resolveVariableValues(cfg *Config, external []ExternalVariableValue, allowMissing bool) error {
	values := make(map[string]Value, len(cfg.Variables))
	explicit := map[string]Value{}
	for i, item := range external {
		variable, ok := cfg.Variables[item.Name]
		if !ok {
			if item.IgnoreUnknown {
				continue
			}
			source := item.Source
			if source.Path == "" {
				source.Path = fmt.Sprintf("cli.var[%d]", i)
			}
			return fmt.Errorf("%s:%d:%s: unknown variable %q", source.File, source.Line, source.Path, item.Name)
		}
		var value Value
		if item.ParsedValue != nil {
			value = *item.ParsedValue
		} else {
			parsed, err := parseExternalVariableValue(variable, item)
			if err != nil {
				return err
			}
			value = parsed
		}
		normalized, err := NormalizeVariableValue(variable, value)
		if err != nil {
			return err
		}
		explicit[item.Name] = normalized
		cfg.ExplicitVariableValues[item.Name] = true
	}
	for _, name := range sortedVariableNames(cfg.Variables) {
		variable := cfg.Variables[name]
		if value, ok := explicit[name]; ok {
			values[name] = value
			continue
		}
		if variable.Default == nil {
			if allowMissing {
				continue
			}
			return fmt.Errorf("%s:%d:%s: variable %q is required", variable.Source.File, variable.Source.Line, variable.Source.Path, variable.Name)
		}
		normalized, err := NormalizeVariableValue(variable, *variable.Default)
		if err != nil {
			return err
		}
		values[name] = normalized
	}
	cfg.VariableValues = values
	return nil
}

func evalContextForProgram(cfg *Config, moduleDir string) (EvalContext, error) {
	varNamespace, err := variableNamespaceValue(cfg)
	if err != nil {
		return EvalContext{}, err
	}
	return EvalContext{
		ModuleDir: moduleDir,
		Locals:    cfg.Locals,
		Variables: map[string]cty.Value{
			"var": varNamespace,
		},
	}, nil
}

func sortedVariableNames(values map[string]Variable) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedVariableValueNames(values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func parseTopLevel(cfg *Config, file string, body *hclsyntax.Body) error {
	ctx, err := evalContextForProgram(cfg, filepath.Dir(file))
	if err != nil {
		return err
	}
	for _, block := range body.Blocks {
		switch block.Type {
		case "locals", "variable":
			continue
		case "profile":
			profile, err := parseProfile(file, block, ctx)
			if err != nil {
				return err
			}
			if previous, exists := cfg.Profiles[profile.Name]; exists {
				return fmt.Errorf("%s:%d: duplicate profile %q; first defined at %s:%d", file, profile.Source.Line, profile.Name, previous.Source.File, previous.Source.Line)
			}
			cfg.Profiles[profile.Name] = profile
		case "host":
			host, err := parseHost(file, block, ctx)
			if err != nil {
				return err
			}
			if previous, exists := cfg.Hosts[host.Name]; exists {
				return fmt.Errorf("%s:%d: duplicate host %q; first defined at %s:%d", file, host.Source.Line, host.Name, previous.Source.File, previous.Source.Line)
			}
			cfg.Hosts[host.Name] = host
		case "component":
			component, err := parseComponent(file, block, ctx)
			if err != nil {
				return err
			}
			if previous, exists := cfg.Components[component.Name]; exists {
				return fmt.Errorf("%s:%d: duplicate component %q; first defined at %s:%d", file, component.Source.Line, component.Name, previous.Source.File, previous.Source.Line)
			}
			cfg.Components[component.Name] = component
		case "script":
			script, err := parseRootScriptBlock(file, block, ctx)
			if err != nil {
				return err
			}
			if previous, exists := cfg.Scripts[script.Name]; exists {
				return fmt.Errorf("%s:%d: duplicate root script %q; first defined at %s:%d", file, script.Source.Line, script.Name, previous.Source.File, previous.Source.Line)
			}
			cfg.Scripts[script.Name] = script
		default:
			return fmt.Errorf("%s:%d: unknown top-level block %q", file, block.TypeRange.Start.Line, block.Type)
		}
	}
	return nil
}

func parseVariable(file string, block *hclsyntax.Block, ctx EvalContext) (Variable, error) {
	if len(block.Labels) != 1 {
		return Variable{}, fmt.Errorf("%s:%d: variable block requires exactly one label", file, block.TypeRange.Start.Line)
	}
	name := block.Labels[0]
	path := fmt.Sprintf("variable[%s]", strconv.Quote(name))
	for attrName, attr := range block.Body.Attributes {
		switch attrName {
		case "type", "default", "description", "nullable", "sensitive", "ephemeral", "const", "deprecated":
		default:
			return Variable{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, attrName)
		}
	}
	for _, child := range block.Body.Blocks {
		if child.Type != "validation" {
			return Variable{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	typeAttr, ok := block.Body.Attributes["type"]
	if !ok {
		return Variable{}, fmt.Errorf("%s:%d: %s.type is required", file, block.TypeRange.Start.Line, path)
	}
	typeCtx := ctx
	typeCtx.RestrictVariableDefaultReferences = true
	typeSpec, typeName, err := parseComponentInputType(typeAttr.Expr, typeCtx, path+".type")
	if err != nil {
		return Variable{}, fmt.Errorf("%s:%d: %s.type: %w", file, typeAttr.NameRange.Start.Line, path, err)
	}
	variable := Variable{
		Name:     name,
		Type:     typeName,
		TypeExpr: typeName,
		TypeSpec: typeSpec,
		Nullable: true,
		Source:   ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path},
	}
	if descriptionAttr, ok := block.Body.Attributes["description"]; ok {
		descriptionSource := ir.SourceRef{File: file, Line: descriptionAttr.NameRange.Start.Line, Path: path + ".description"}
		value, err := evalValue(descriptionAttr.Expr, ctx, descriptionSource)
		if err != nil {
			return Variable{}, fmt.Errorf("%s:%d: %s.description: %w", file, descriptionSource.Line, path, err)
		}
		if value.Kind != KindString {
			return Variable{}, fmt.Errorf("%s:%d: %s.description must be a string", file, descriptionSource.Line, path)
		}
		variable.Description = value.String
	}
	if deprecatedAttr, ok := block.Body.Attributes["deprecated"]; ok {
		deprecatedSource := ir.SourceRef{File: file, Line: deprecatedAttr.NameRange.Start.Line, Path: path + ".deprecated"}
		value, err := evalValue(deprecatedAttr.Expr, ctx, deprecatedSource)
		if err != nil {
			return Variable{}, fmt.Errorf("%s:%d: %s.deprecated: %w", file, deprecatedSource.Line, path, err)
		}
		if value.Kind != KindString {
			return Variable{}, fmt.Errorf("%s:%d: %s.deprecated must be a string", file, deprecatedSource.Line, path)
		}
		if value.String == "" {
			return Variable{}, fmt.Errorf("%s:%d: %s.deprecated must be a non-empty string", file, deprecatedSource.Line, path)
		}
		variable.Deprecated = value.String
	}
	if defaultAttr, ok := block.Body.Attributes["default"]; ok {
		defaultSource := ir.SourceRef{File: file, Line: defaultAttr.NameRange.Start.Line, Path: path + ".default"}
		if err := validateVariableDefaultReferences(defaultAttr.Expr, defaultSource); err != nil {
			return Variable{}, err
		}
		value, err := evalValue(defaultAttr.Expr, ctx, defaultSource)
		if err != nil {
			return Variable{}, fmt.Errorf("%s:%d: %s.default: %w", file, defaultSource.Line, path, err)
		}
		variable.Default = &value
	}
	if sensitiveAttr, ok := block.Body.Attributes["sensitive"]; ok {
		value, err := parseBoolVariableAttribute(file, path, "sensitive", sensitiveAttr, ctx)
		if err != nil {
			return Variable{}, err
		}
		variable.Sensitive = value
	}
	if nullableAttr, ok := block.Body.Attributes["nullable"]; ok {
		value, err := parseBoolVariableAttribute(file, path, "nullable", nullableAttr, ctx)
		if err != nil {
			return Variable{}, err
		}
		variable.Nullable = value
	}
	if ephemeralAttr, ok := block.Body.Attributes["ephemeral"]; ok {
		value, err := parseBoolVariableAttribute(file, path, "ephemeral", ephemeralAttr, ctx)
		if err != nil {
			return Variable{}, err
		}
		variable.Ephemeral = value
	}
	if constAttr, ok := block.Body.Attributes["const"]; ok {
		value, err := parseBoolVariableAttribute(file, path, "const", constAttr, ctx)
		if err != nil {
			return Variable{}, err
		}
		variable.Const = value
	}
	for i, child := range block.Body.Blocks {
		validation, err := parseVariableValidationBlock(file, fmt.Sprintf("%s.validation[%d]", path, i), child, ctx)
		if err != nil {
			return Variable{}, err
		}
		variable.Validations = append(variable.Validations, validation)
	}
	return variable, nil
}

func validateVariableDefaultReferences(expr hcl.Expression, source ir.SourceRef) error {
	for _, traversal := range expr.Variables() {
		if len(traversal) == 0 {
			continue
		}
		root, ok := traversal[0].(hcl.TraverseRoot)
		if !ok {
			continue
		}
		if root.Name == "local" {
			continue
		}
		return fmt.Errorf("%s:%d:%s: variable default cannot reference %s", source.File, source.Line, source.Path, root.Name)
	}
	return nil
}

func parseBoolVariableAttribute(file, variablePath, name string, attr *hclsyntax.Attribute, ctx EvalContext) (bool, error) {
	source := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: variablePath + "." + name}
	value, err := evalValue(attr.Expr, ctx, source)
	if err != nil {
		return false, fmt.Errorf("%s:%d: %s.%s: %w", file, source.Line, variablePath, name, err)
	}
	if value.Kind != KindBool {
		return false, fmt.Errorf("%s:%d: %s.%s must be a boolean", file, source.Line, variablePath, name)
	}
	return value.Bool, nil
}

func parseProfile(file string, block *hclsyntax.Block, ctx EvalContext) (Profile, error) {
	source, name, err := parseLabeledHeader(file, "profile", block)
	if err != nil {
		return Profile{}, err
	}
	imports, _, body, asserts, err := parseHostLikeBody(file, source.Path, block.Body, ctx, false)
	if err != nil {
		return Profile{}, err
	}
	if err := validateProfileBody(source, body); err != nil {
		return Profile{}, err
	}
	return Profile{Name: name, Source: source, Imports: imports, Body: body, Asserts: asserts}, nil
}

func parseHost(file string, block *hclsyntax.Block, ctx EvalContext) (Host, error) {
	source, name, err := parseLabeledHeader(file, "host", block)
	if err != nil {
		return Host{}, err
	}
	imports, components, body, asserts, err := parseHostLikeBody(file, source.Path, block.Body, ctx, true)
	if err != nil {
		return Host{}, err
	}
	return Host{Name: name, Source: source, Imports: imports, Components: components, Body: body, Asserts: asserts}, nil
}

func parseComponent(file string, block *hclsyntax.Block, ctx EvalContext) (Component, error) {
	source, name, err := parseLabeledHeader(file, "component", block)
	if err != nil {
		return Component{}, err
	}
	component := Component{
		Name:      name,
		Source:    source,
		ModuleDir: filepath.Dir(file),
		Body:      block.Body,
		Inputs:    map[string]ComponentInput{},
		Scripts:   map[string]ComponentScript{},
		Sources:   map[string]ComponentArtifactSource{},
	}
	for name, attr := range block.Body.Attributes {
		switch name {
		case "type":
			value, err := parseStringAttribute(file, source.Path+"."+name, attr, ctx)
			if err != nil {
				return Component{}, err
			}
			component.Type = value
		case "version":
			value, err := parseStringAttribute(file, source.Path+"."+name, attr, ctx)
			if err != nil {
				return Component{}, err
			}
			component.Version = value
		default:
			return Component{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, source.Path, name)
		}
	}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "input":
			input, err := parseComponentInputBlock(file, source.Path, child, ctx)
			if err != nil {
				return Component{}, err
			}
			if previous, exists := component.Inputs[input.Name]; exists {
				return Component{}, fmt.Errorf("%s:%d: duplicate component input %q; first defined at %s:%d", file, input.Source.Line, input.Name, previous.Source.File, previous.Source.Line)
			}
			component.Inputs[input.Name] = input
		case "script":
			script, err := parseComponentScriptBlock(file, source.Path, child, ctx)
			if err != nil {
				return Component{}, err
			}
			if previous, exists := component.Scripts[script.Name]; exists {
				return Component{}, fmt.Errorf("%s:%d: duplicate component script %q; first defined at %s:%d", file, script.Source.Line, script.Name, previous.Source.File, previous.Source.Line)
			}
			component.Scripts[script.Name] = script
		case "source":
			sourceBlock, err := parseComponentSourceBlock(file, source.Path, child, ctx)
			if err != nil {
				return Component{}, err
			}
			if previous, exists := component.Sources[sourceBlock.Architecture]; exists {
				return Component{}, fmt.Errorf("%s:%d: duplicate component source %q; first defined at %s:%d", file, sourceBlock.Source.Line, sourceBlock.Architecture, previous.Source.File, previous.Source.Line)
			}
			component.Sources[sourceBlock.Architecture] = sourceBlock
		case "extract":
			if component.Extract != nil {
				return Component{}, fmt.Errorf("%s:%d: duplicate %s.extract block", file, child.TypeRange.Start.Line, source.Path)
			}
			extract, err := parseComponentExtractBlock(file, source.Path, child, ctx)
			if err != nil {
				return Component{}, err
			}
			component.Extract = &extract
		case "build":
			if component.Build != nil {
				return Component{}, fmt.Errorf("%s:%d: duplicate %s.build block", file, child.TypeRange.Start.Line, source.Path)
			}
			build, err := parseComponentBuildBlock(file, source.Path, child, ctx)
			if err != nil {
				return Component{}, err
			}
			component.Build = &build
		case "install":
			if component.Install != nil {
				return Component{}, fmt.Errorf("%s:%d: duplicate %s.install block", file, child.TypeRange.Start.Line, source.Path)
			}
			install, err := parseComponentInstallBlock(file, source.Path, child, ctx)
			if err != nil {
				return Component{}, err
			}
			component.Install = &install
		case "apt", "packages", "files", "secrets", "directories", "groups", "users", "systemd", "services":
			continue
		default:
			return Component{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, source.Path, child.Type)
		}
	}
	return component, nil
}

func parseComponentScriptBlock(file, componentPath string, block *hclsyntax.Block, ctx EvalContext) (ComponentScript, error) {
	return parseScriptBlock(file, componentPath, "component script", block, ctx)
}

func parseRootScriptBlock(file string, block *hclsyntax.Block, ctx EvalContext) (ComponentScript, error) {
	script, err := parseScriptBlock(file, "", "root script", block, ctx)
	if err != nil {
		return ComponentScript{}, err
	}
	if !hclsyntax.ValidIdentifier(script.Name) {
		return ComponentScript{}, fmt.Errorf("%s:%d:%s: root script label %q must be a valid HCL identifier", file, script.Source.Line, script.Source.Path, script.Name)
	}
	for _, expr := range []hcl.Expression{script.ModeExpr, script.InterpreterExpr, script.OutputsExpr, script.RunExpr, script.ContentExpr, script.CommandsExpr} {
		if expr == nil {
			continue
		}
		for _, traversal := range expr.Variables() {
			root, ok := traversalRoot(traversal)
			if ok && root == "input" {
				return ComponentScript{}, fmt.Errorf("%s:%d:%s: root script cannot reference component input.*", file, traversal.SourceRange().Start.Line, script.Source.Path)
			}
		}
	}
	return script, nil
}

func parseScriptBlock(file, parentPath, description string, block *hclsyntax.Block, ctx EvalContext) (ComponentScript, error) {
	if len(block.Labels) != 1 {
		return ComponentScript{}, fmt.Errorf("%s:%d: %s block requires exactly one label", file, block.TypeRange.Start.Line, description)
	}
	name := block.Labels[0]
	path := fmt.Sprintf("script[%s]", strconv.Quote(name))
	if parentPath != "" {
		path = fmt.Sprintf("%s.script[%s]", parentPath, strconv.Quote(name))
	}
	if len(block.Body.Blocks) != 0 {
		return ComponentScript{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	script := ComponentScript{Name: name, ModuleDir: filepath.Dir(file), Source: ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}}
	for attrName, attr := range block.Body.Attributes {
		source := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + attrName}
		switch attrName {
		case "mode":
			script.ModeSet = true
			script.ModeExpr = attr.Expr
			script.ModeSource = source
		case "interpreter":
			script.InterpreterSet = true
			script.InterpreterExpr = attr.Expr
			script.InterpreterSource = source
		case "outputs":
			script.OutputsSet = true
			script.OutputsExpr = attr.Expr
			script.OutputsSource = source
		case "run":
			script.RunSet = true
			script.RunExpr = attr.Expr
			script.RunSource = source
		case "content":
			script.ContentSet = true
			script.ContentExpr = attr.Expr
			script.ContentSource = source
		case "commands":
			script.CommandsSet = true
			script.CommandsExpr = attr.Expr
			script.CommandsSource = source
		default:
			return ComponentScript{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, attrName)
		}
	}
	return script, nil
}

func parseComponentSourceBlock(file, componentPath string, block *hclsyntax.Block, ctx EvalContext) (ComponentArtifactSource, error) {
	if len(block.Labels) > 1 {
		return ComponentArtifactSource{}, fmt.Errorf("%s:%d: component source block accepts at most one architecture label", file, block.TypeRange.Start.Line)
	}
	architecture := ""
	path := componentPath + ".source"
	if len(block.Labels) == 1 {
		architecture = block.Labels[0]
		path = fmt.Sprintf("%s.source[%s]", componentPath, strconv.Quote(architecture))
	}
	if len(block.Body.Blocks) != 0 {
		return ComponentArtifactSource{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	source := ComponentArtifactSource{Architecture: architecture, Source: ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}}
	for name, attr := range block.Body.Attributes {
		switch name {
		case "url":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactSource{}, err
			}
			source.URL = value
		case "sha256":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactSource{}, err
			}
			source.SHA256 = value
		default:
			return ComponentArtifactSource{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return source, nil
}

func parseComponentExtractBlock(file, componentPath string, block *hclsyntax.Block, ctx EvalContext) (ComponentArtifactExtract, error) {
	path := componentPath + ".extract"
	if len(block.Labels) != 0 {
		return ComponentArtifactExtract{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return ComponentArtifactExtract{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	extract := ComponentArtifactExtract{Source: ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}}
	for name, attr := range block.Body.Attributes {
		switch name {
		case "format":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactExtract{}, err
			}
			extract.Format = value
		case "strip_components":
			value, err := parseIntAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactExtract{}, err
			}
			extract.StripComponents = value
		case "include":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactExtract{}, err
			}
			extract.Include = value
		default:
			return ComponentArtifactExtract{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return extract, nil
}

func parseComponentBuildBlock(file, componentPath string, block *hclsyntax.Block, ctx EvalContext) (ComponentArtifactBuild, error) {
	path := componentPath + ".build"
	if len(block.Labels) != 0 {
		return ComponentArtifactBuild{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return ComponentArtifactBuild{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	build := ComponentArtifactBuild{Source: ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}}
	for name, attr := range block.Body.Attributes {
		switch name {
		case "commands":
			commands, err := parseCommandMatrixAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactBuild{}, err
			}
			build.Commands = commands
		case "packages":
			packages, err := parseStringListAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactBuild{}, err
			}
			build.Packages = packages
		case "working_dir":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactBuild{}, err
			}
			build.WorkingDir = value
		case "output":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactBuild{}, err
			}
			build.Output = value
		case "source_name":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactBuild{}, err
			}
			build.SourceName = value
		default:
			return ComponentArtifactBuild{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return build, nil
}

func parseComponentInstallBlock(file, componentPath string, block *hclsyntax.Block, ctx EvalContext) (ComponentArtifactInstall, error) {
	path := componentPath + ".install"
	if len(block.Labels) != 0 {
		return ComponentArtifactInstall{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return ComponentArtifactInstall{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	install := ComponentArtifactInstall{Source: ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}}
	for name, attr := range block.Body.Attributes {
		switch name {
		case "path":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactInstall{}, err
			}
			install.Path = value
		case "owner":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactInstall{}, err
			}
			install.Owner = value
		case "group":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactInstall{}, err
			}
			install.Group = value
		case "mode":
			value, err := parseStringAttribute(file, path+"."+name, attr, ctx)
			if err != nil {
				return ComponentArtifactInstall{}, err
			}
			install.Mode = value
		default:
			return ComponentArtifactInstall{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return install, nil
}

func parseStringAttribute(file, path string, attr *hclsyntax.Attribute, ctx EvalContext) (string, error) {
	source := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path}
	value, err := evalValue(attr.Expr, ctx, source)
	if err != nil {
		return "", fmt.Errorf("%s:%d: %s: %w", file, source.Line, path, err)
	}
	str, ok := value.StringValue()
	if !ok {
		return "", fmt.Errorf("%s:%d: %s must be a string", file, source.Line, path)
	}
	return str, nil
}

func parseIntAttribute(file, path string, attr *hclsyntax.Attribute, ctx EvalContext) (int, error) {
	source := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path}
	value, err := evalValue(attr.Expr, ctx, source)
	if err != nil {
		return 0, fmt.Errorf("%s:%d: %s: %w", file, source.Line, path, err)
	}
	str, ok := value.StringValue()
	if !ok {
		return 0, fmt.Errorf("%s:%d: %s must be an integer", file, source.Line, path)
	}
	n, err := strconv.Atoi(str)
	if err != nil {
		return 0, fmt.Errorf("%s:%d: %s must be an integer", file, source.Line, path)
	}
	return n, nil
}

func parseCommandMatrixAttribute(file, path string, attr *hclsyntax.Attribute, ctx EvalContext) ([][]string, error) {
	source := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path}
	value, err := evalValue(attr.Expr, ctx, source)
	if err != nil {
		return nil, fmt.Errorf("%s:%d: %s: %w", file, source.Line, path, err)
	}
	return commandMatrixFromValue(file, path, value)
}

func commandMatrixFromValue(file, path string, value Value) ([][]string, error) {
	if !value.IsList() {
		return nil, fmt.Errorf("%s:%d: %s must be a list of command argument lists", file, value.Source.Line, path)
	}
	commands := make([][]string, 0, len(value.List))
	for i, item := range value.List {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if !item.IsList() {
			return nil, fmt.Errorf("%s:%d: %s must be a command argument list", file, item.Source.Line, itemPath)
		}
		command := make([]string, 0, len(item.List))
		for j, arg := range item.List {
			str, ok := arg.StringValue()
			if !ok {
				return nil, fmt.Errorf("%s:%d: %s[%d] must be a string", file, arg.Source.Line, itemPath, j)
			}
			command = append(command, str)
		}
		commands = append(commands, command)
	}
	return commands, nil
}

func parseStringListAttribute(file, path string, attr *hclsyntax.Attribute, ctx EvalContext) ([]string, error) {
	source := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path}
	value, err := evalValue(attr.Expr, ctx, source)
	if err != nil {
		return nil, fmt.Errorf("%s:%d: %s: %w", file, source.Line, path, err)
	}
	return stringListFromValue(file, path, value)
}

func stringListFromValue(file, path string, value Value) ([]string, error) {
	if !value.IsList() {
		return nil, fmt.Errorf("%s:%d: %s must be a list of strings", file, value.Source.Line, path)
	}
	out := make([]string, 0, len(value.List))
	seen := map[string]struct{}{}
	for i, item := range value.List {
		str, ok := item.StringValue()
		if !ok || str == "" {
			return nil, fmt.Errorf("%s:%d: %s[%d] must be a non-empty string", file, item.Source.Line, path, i)
		}
		if _, exists := seen[str]; exists {
			return nil, fmt.Errorf("%s:%d: duplicate %s entry %q", file, item.Source.Line, path, str)
		}
		seen[str] = struct{}{}
		out = append(out, str)
	}
	return out, nil
}

func stringListFromValueAllowDuplicates(file, path string, value Value) ([]string, error) {
	if !value.IsList() {
		return nil, fmt.Errorf("%s:%d: %s must be a list of strings", file, value.Source.Line, path)
	}
	out := make([]string, 0, len(value.List))
	for i, item := range value.List {
		str, ok := item.StringValue()
		if !ok || str == "" {
			return nil, fmt.Errorf("%s:%d: %s[%d] must be a non-empty string", file, item.Source.Line, path, i)
		}
		out = append(out, str)
	}
	return out, nil
}

func EvaluateComponentScript(script ComponentScript, ctx EvalContext) (ComponentScript, error) {
	out := script
	out.Sensitive = false
	if script.ModeSet {
		value, err := evalComponentScriptValue(script.ModeSource, script.ModeExpr, ctx)
		if err != nil {
			return ComponentScript{}, err
		}
		str, ok := value.StringValue()
		if !ok {
			return ComponentScript{}, fmt.Errorf("%s:%d: %s must be a string", script.ModeSource.File, script.ModeSource.Line, script.ModeSource.Path)
		}
		out.Mode = str
		out.Sensitive = out.Sensitive || value.ContainsSensitive()
	}
	if script.InterpreterSet {
		value, err := evalComponentScriptValue(script.InterpreterSource, script.InterpreterExpr, ctx)
		if err != nil {
			return ComponentScript{}, err
		}
		interpreter, err := stringListFromValueAllowDuplicates(script.InterpreterSource.File, script.InterpreterSource.Path, value)
		if err != nil {
			return ComponentScript{}, err
		}
		out.Interpreter = interpreter
		out.Sensitive = out.Sensitive || value.ContainsSensitive()
	}
	if script.OutputsSet {
		value, err := evalComponentScriptValue(script.OutputsSource, script.OutputsExpr, ctx)
		if err != nil {
			return ComponentScript{}, err
		}
		outputs, err := stringListFromValueAllowDuplicates(script.OutputsSource.File, script.OutputsSource.Path, value)
		if err != nil {
			return ComponentScript{}, err
		}
		out.Outputs = outputs
		out.Sensitive = out.Sensitive || value.ContainsSensitive()
	}
	if script.RunSet {
		value, err := evalComponentScriptValue(script.RunSource, script.RunExpr, ctx)
		if err != nil {
			return ComponentScript{}, err
		}
		str, ok := value.StringValue()
		if !ok {
			return ComponentScript{}, fmt.Errorf("%s:%d: %s must be a string", script.RunSource.File, script.RunSource.Line, script.RunSource.Path)
		}
		out.Run = str
		out.Sensitive = out.Sensitive || value.ContainsSensitive()
	}
	if script.ContentSet {
		value, err := evalComponentScriptValue(script.ContentSource, script.ContentExpr, ctx)
		if err != nil {
			return ComponentScript{}, err
		}
		str, ok := value.StringValue()
		if !ok {
			return ComponentScript{}, fmt.Errorf("%s:%d: %s must be a string", script.ContentSource.File, script.ContentSource.Line, script.ContentSource.Path)
		}
		out.Content = str
		out.Sensitive = out.Sensitive || value.ContainsSensitive()
	}
	if script.CommandsSet {
		value, err := evalComponentScriptValue(script.CommandsSource, script.CommandsExpr, ctx)
		if err != nil {
			return ComponentScript{}, err
		}
		commands, err := commandMatrixFromValue(script.CommandsSource.File, script.CommandsSource.Path, value)
		if err != nil {
			return ComponentScript{}, err
		}
		out.Commands = commands
		out.Sensitive = out.Sensitive || value.ContainsSensitive()
	}
	return out, nil
}

func evalComponentScriptValue(source ir.SourceRef, expr hcl.Expression, ctx EvalContext) (Value, error) {
	value, err := evalValue(expr, ctx, source)
	if err != nil {
		return Value{}, fmt.Errorf("%s:%d: %s: %w", source.File, source.Line, source.Path, err)
	}
	return value, nil
}

func parseLabeledHeader(file, typ string, block *hclsyntax.Block) (ir.SourceRef, string, error) {
	line := block.TypeRange.Start.Line
	if len(block.Labels) != 1 {
		return ir.SourceRef{}, "", fmt.Errorf("%s:%d: %s block requires exactly one label", file, line, typ)
	}
	name := block.Labels[0]
	if !hclsyntax.ValidIdentifier(name) {
		return ir.SourceRef{}, "", fmt.Errorf("%s:%d: %s block label %q must be a valid HCL identifier", file, line, typ, name)
	}
	return ir.SourceRef{File: file, Line: line, Path: typ + "." + name}, name, nil
}

func parseHostLikeBody(file, path string, body *hclsyntax.Body, ctx EvalContext, allowComponents bool) ([]string, []ComponentInstance, Value, []Assert, error) {
	imports := []string{}
	components := []ComponentInstance{}
	asserts := []Assert{}
	values := map[string]Value{}

	for name, attr := range body.Attributes {
		switch name {
		case "imports":
			refs, err := parseProfileRefs(file, attr.Expr)
			if err != nil {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: %s.imports: %w", file, attr.NameRange.Start.Line, path, err)
			}
			imports = refs
		case "components":
			if !allowComponents {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: %s.components is host-only and cannot be declared in profile", file, attr.NameRange.Start.Line, path)
			}
			refs, err := parseComponentRefs(file, path+".components", attr.Expr)
			if err != nil {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: %s.components: %w", file, attr.NameRange.Start.Line, path, err)
			}
			components = append(components, refs...)
		default:
			return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}

	for _, block := range body.Blocks {
		switch block.Type {
		case "ssh", "state", "platform", "system", "kernel", "packages", "apt", "files", "secrets", "directories", "groups", "users", "systemd", "services", "nftables":
			if len(block.Labels) != 0 {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, block.Type)
			}
			if _, exists := values[block.Type]; exists {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: duplicate %s.%s block", file, block.TypeRange.Start.Line, path, block.Type)
			}
			value, err := parseDomainBlock(file, path+"."+block.Type, block, ctx)
			if err != nil {
				return nil, nil, Value{}, nil, err
			}
			values[block.Type] = value
		case "docker":
			if len(block.Labels) != 0 {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: docker block must not have labels", file, block.TypeRange.Start.Line)
			}
			if _, exists := values["docker"]; exists {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: duplicate %s.docker block", file, block.TypeRange.Start.Line, path)
			}
			value, err := parseDockerBlock(file, path+".docker", block, ctx)
			if err != nil {
				return nil, nil, Value{}, nil, err
			}
			values["docker"] = value
		case "component":
			if !allowComponents {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: %s.component is host-only and cannot be declared in profile", file, block.TypeRange.Start.Line, path)
			}
			instance, err := parseComponentInstanceBlock(file, path, block, ctx)
			if err != nil {
				return nil, nil, Value{}, nil, err
			}
			components = append(components, instance)
		case "assert":
			if len(block.Labels) != 0 {
				return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: assert block must not have labels", file, block.TypeRange.Start.Line)
			}
			assert, err := parseAssertBlock(file, fmt.Sprintf("%s.assert[%d]", path, len(asserts)), block, ctx)
			if err != nil {
				return nil, nil, Value{}, nil, err
			}
			asserts = append(asserts, assert)
		default:
			return nil, nil, Value{}, nil, fmt.Errorf("%s:%d: unsupported block %s.%s", file, block.TypeRange.Start.Line, path, block.Type)
		}
	}

	return imports, components, MapValue(values, ir.SourceRef{File: file, Line: body.SrcRange.Start.Line, Path: path}), asserts, nil
}

func parseDockerBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	values := map[string]Value{}
	allowed := attrSet("enable", "users")
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}

	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "package", "service", "daemon":
			if len(child.Labels) != 0 {
				return Value{}, fmt.Errorf("%s:%d: %s.%s block must not have labels", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if _, exists := values[child.Type]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.%s block", file, child.TypeRange.Start.Line, path, child.Type)
			}
			object, err := parseDockerObjectBlock(file, path+"."+child.Type, child, ctx)
			if err != nil {
				return Value{}, err
			}
			values[child.Type] = object
		case "compose":
			if len(child.Labels) != 1 {
				return Value{}, fmt.Errorf("%s:%d: %s.compose block requires exactly one label", file, child.TypeRange.Start.Line, path)
			}
			label := child.Labels[0]
			objectPath := fmt.Sprintf("%s.compose[%s]", path, strconv.Quote(label))
			object, err := parseDockerComposeBlock(file, objectPath, child, ctx)
			if err != nil {
				return Value{}, err
			}
			collection, ok := values["compose"]
			if !ok {
				collection = MapValue(nil, ir.SourceRef{File: file, Line: child.TypeRange.Start.Line, Path: path + ".compose"})
			}
			if _, exists := collection.Map[label]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
			}
			collection.Map[label] = object
			values["compose"] = collection
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseDockerObjectBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	var allowed map[string]struct{}
	switch block.Type {
	case "package":
		allowed = attrSet("source", "channel", "version", "repository_url", "gpg_url", "gpg_sha256", "remove_conflicts")
	case "service":
		allowed = attrSet("enable", "state")
	case "daemon":
		allowed = attrSet("settings")
	default:
		return Value{}, fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseDockerComposeBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	values := map[string]Value{}
	allowed := attrSet("enable", "state", "directory", "project", "pull", "recreate", "remove_orphans", "after", "wanted_by")
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}

	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "file", "service":
			if len(child.Labels) != 0 {
				return Value{}, fmt.Errorf("%s:%d: %s.%s block must not have labels; multiple compose file blocks are not supported yet", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if _, exists := values[child.Type]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.%s block; multiple compose file blocks are not supported yet", file, child.TypeRange.Start.Line, path, child.Type)
			}
			object, err := parseDockerComposeObjectBlock(file, path+"."+child.Type, child, ctx)
			if err != nil {
				return Value{}, err
			}
			values[child.Type] = object
		case "env_file":
			if len(child.Labels) != 1 {
				return Value{}, fmt.Errorf("%s:%d: %s.env_file block requires exactly one label", file, child.TypeRange.Start.Line, path)
			}
			label := child.Labels[0]
			objectPath := fmt.Sprintf("%s.env_file[%s]", path, strconv.Quote(label))
			object, err := parseDockerComposeObjectBlock(file, objectPath, child, ctx)
			if err != nil {
				return Value{}, err
			}
			collection, ok := values["env_file"]
			if !ok {
				collection = MapValue(nil, ir.SourceRef{File: file, Line: child.TypeRange.Start.Line, Path: path + ".env_file"})
			}
			if _, exists := collection.Map[label]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
			}
			collection.Map[label] = object
			values["env_file"] = collection
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseDockerComposeObjectBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	var allowed map[string]struct{}
	switch block.Type {
	case "file", "env_file":
		allowed = attrSet("path", "content", "source", "owner", "group", "mode")
	case "service":
		allowed = attrSet("enable", "name")
	default:
		return Value{}, fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseComponentRefs(file, path string, expr hcl.Expression) ([]ComponentInstance, error) {
	items, diags := hcl.ExprList(expr)
	if diags.HasErrors() {
		return nil, fmt.Errorf("must be a list of component references")
	}
	refs := make([]ComponentInstance, 0, len(items))
	for i, item := range items {
		template, err := parseComponentTraversal(file, item)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ComponentInstance{
			Name:     template,
			Template: template,
			Inputs:   map[string]Value{},
			Source: ir.SourceRef{
				File: file,
				Line: item.Range().Start.Line,
				Path: fmt.Sprintf("%s[%d]", path, i),
			},
		})
	}
	return refs, nil
}

func parseComponentInstanceBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (ComponentInstance, error) {
	source, name, err := parseLabeledHeader(file, path+".component", block)
	if err != nil {
		return ComponentInstance{}, err
	}
	source.Path = fmt.Sprintf("%s.component[%s]", path, strconv.Quote(name))
	if len(block.Body.Blocks) != 0 {
		return ComponentInstance{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, source.Path)
	}
	for attrName, attr := range block.Body.Attributes {
		if attrName != "source" && attrName != "inputs" {
			return ComponentInstance{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, source.Path, attrName)
		}
	}
	sourceAttr, ok := block.Body.Attributes["source"]
	if !ok {
		return ComponentInstance{}, fmt.Errorf("%s:%d: %s.source is required", file, block.TypeRange.Start.Line, source.Path)
	}
	template, err := parseComponentTraversal(file, sourceAttr.Expr)
	if err != nil {
		return ComponentInstance{}, err
	}
	inputs := map[string]Value{}
	if inputsAttr, ok := block.Body.Attributes["inputs"]; ok {
		inputSource := ir.SourceRef{File: file, Line: inputsAttr.NameRange.Start.Line, Path: source.Path + ".inputs"}
		inputValue, err := evalValue(inputsAttr.Expr, ctx, inputSource)
		if err != nil {
			return ComponentInstance{}, fmt.Errorf("%s:%d: %s.inputs: %w", file, inputSource.Line, source.Path, err)
		}
		if !inputValue.IsMap() {
			return ComponentInstance{}, fmt.Errorf("%s:%d: %s.inputs must be a map", file, inputSource.Line, source.Path)
		}
		inputs = inputValue.Map
	}
	return ComponentInstance{Name: name, Template: template, Inputs: inputs, Source: source}, nil
}

func parseComponentTraversal(file string, expr hcl.Expression) (string, error) {
	traversal, diags := hcl.AbsTraversalForExpr(expr)
	if diags.HasErrors() {
		return "", fmt.Errorf("%s:%d: component reference must be component.<name>", file, expr.Range().Start.Line)
	}
	if len(traversal) != 2 {
		return "", fmt.Errorf("%s:%d: component reference must be component.<name>", file, expr.Range().Start.Line)
	}
	root, ok := traversal[0].(hcl.TraverseRoot)
	if !ok || root.Name != "component" {
		return "", fmt.Errorf("%s:%d: component reference must be component.<name>", file, expr.Range().Start.Line)
	}
	attr, ok := traversal[1].(hcl.TraverseAttr)
	if !ok || attr.Name == "" {
		return "", fmt.Errorf("%s:%d: component reference must be component.<name>", file, expr.Range().Start.Line)
	}
	return attr.Name, nil
}

func parseScriptTraversal(file string, expr hcl.Expression) (ScriptReference, error) {
	traversal, diags := hcl.AbsTraversalForExpr(expr)
	if diags.HasErrors() {
		return ScriptReference{}, fmt.Errorf("%s:%d: script reference must be script.<name> or global.script.<name>", file, expr.Range().Start.Line)
	}
	if len(traversal) == 2 {
		root, rootOK := traversal[0].(hcl.TraverseRoot)
		attr, attrOK := traversal[1].(hcl.TraverseAttr)
		if rootOK && root.Name == "script" && attrOK && attr.Name != "" {
			return ScriptReference{Name: attr.Name, Scope: ScriptReferenceAuto}, nil
		}
	}
	if len(traversal) == 3 {
		root, rootOK := traversal[0].(hcl.TraverseRoot)
		script, scriptOK := traversal[1].(hcl.TraverseAttr)
		name, nameOK := traversal[2].(hcl.TraverseAttr)
		if rootOK && root.Name == "global" && scriptOK && script.Name == "script" && nameOK && name.Name != "" {
			return ScriptReference{Name: name.Name, Scope: ScriptReferenceGlobal}, nil
		}
	}
	return ScriptReference{}, fmt.Errorf("%s:%d: script reference must be script.<name> or global.script.<name>", file, expr.Range().Start.Line)
}

func parseComponentInputBlock(file, componentPath string, block *hclsyntax.Block, ctx EvalContext) (ComponentInput, error) {
	if len(block.Labels) != 1 {
		return ComponentInput{}, fmt.Errorf("%s:%d: component input block requires exactly one label", file, block.TypeRange.Start.Line)
	}
	name := block.Labels[0]
	path := fmt.Sprintf("%s.input[%s]", componentPath, strconv.Quote(name))
	for attrName, attr := range block.Body.Attributes {
		if attrName != "type" && attrName != "default" && attrName != "sensitive" && attrName != "description" && attrName != "nullable" && attrName != "deprecated" {
			return ComponentInput{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, attrName)
		}
	}
	for _, child := range block.Body.Blocks {
		if child.Type != "validation" {
			return ComponentInput{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	typeAttr, ok := block.Body.Attributes["type"]
	if !ok {
		return ComponentInput{}, fmt.Errorf("%s:%d: %s.type is required", file, block.TypeRange.Start.Line, path)
	}
	typeSpec, typeName, err := parseComponentInputType(typeAttr.Expr, ctx, path+".type")
	if err != nil {
		return ComponentInput{}, fmt.Errorf("%s:%d: %s.type: %w", file, typeAttr.NameRange.Start.Line, path, err)
	}
	input := ComponentInput{
		Name:     name,
		Type:     typeName,
		TypeExpr: typeName,
		TypeSpec: typeSpec,
		Nullable: true,
		Source:   ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path},
	}
	if descriptionAttr, ok := block.Body.Attributes["description"]; ok {
		descriptionSource := ir.SourceRef{File: file, Line: descriptionAttr.NameRange.Start.Line, Path: path + ".description"}
		value, err := evalValue(descriptionAttr.Expr, ctx, descriptionSource)
		if err != nil {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.description: %w", file, descriptionSource.Line, path, err)
		}
		if value.Kind != KindString {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.description must be a string", file, descriptionSource.Line, path)
		}
		input.Description = value.String
	}
	if deprecatedAttr, ok := block.Body.Attributes["deprecated"]; ok {
		deprecatedSource := ir.SourceRef{File: file, Line: deprecatedAttr.NameRange.Start.Line, Path: path + ".deprecated"}
		value, err := evalValue(deprecatedAttr.Expr, ctx, deprecatedSource)
		if err != nil {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.deprecated: %w", file, deprecatedSource.Line, path, err)
		}
		if value.Kind != KindString {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.deprecated must be a string", file, deprecatedSource.Line, path)
		}
		if value.String == "" {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.deprecated must be a non-empty string", file, deprecatedSource.Line, path)
		}
		input.Deprecated = value.String
	}
	if defaultAttr, ok := block.Body.Attributes["default"]; ok {
		defaultSource := ir.SourceRef{File: file, Line: defaultAttr.NameRange.Start.Line, Path: path + ".default"}
		value, err := evalValue(defaultAttr.Expr, ctx, defaultSource)
		if err != nil {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.default: %w", file, defaultSource.Line, path, err)
		}
		input.Default = &value
	}
	if sensitiveAttr, ok := block.Body.Attributes["sensitive"]; ok {
		sensitiveSource := ir.SourceRef{File: file, Line: sensitiveAttr.NameRange.Start.Line, Path: path + ".sensitive"}
		value, err := evalValue(sensitiveAttr.Expr, ctx, sensitiveSource)
		if err != nil {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.sensitive: %w", file, sensitiveSource.Line, path, err)
		}
		if value.Kind != KindBool {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.sensitive must be a boolean", file, sensitiveSource.Line, path)
		}
		input.Sensitive = value.Bool
	}
	if nullableAttr, ok := block.Body.Attributes["nullable"]; ok {
		nullableSource := ir.SourceRef{File: file, Line: nullableAttr.NameRange.Start.Line, Path: path + ".nullable"}
		value, err := evalValue(nullableAttr.Expr, ctx, nullableSource)
		if err != nil {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.nullable: %w", file, nullableSource.Line, path, err)
		}
		if value.Kind != KindBool {
			return ComponentInput{}, fmt.Errorf("%s:%d: %s.nullable must be a boolean", file, nullableSource.Line, path)
		}
		input.Nullable = value.Bool
	}
	for i, child := range block.Body.Blocks {
		validation, err := parseComponentInputValidationBlock(file, fmt.Sprintf("%s.validation[%d]", path, i), child, ctx)
		if err != nil {
			return ComponentInput{}, err
		}
		input.Validations = append(input.Validations, validation)
	}
	return input, nil
}

func parseComponentInputValidationBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (ComponentInputValidation, error) {
	validation, err := parseValidationBlock(file, path, block, ctx)
	if err != nil {
		return ComponentInputValidation{}, err
	}
	return ComponentInputValidation{
		Source:          validation.Source,
		Condition:       validation.Condition,
		ConditionSource: validation.ConditionSource,
		Message:         validation.Message,
		MessageSource:   validation.MessageSource,
	}, nil
}

func parseVariableValidationBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (VariableValidation, error) {
	validation, err := parseValidationBlock(file, path, block, ctx)
	if err != nil {
		return VariableValidation{}, err
	}
	return VariableValidation{
		Source:          validation.Source,
		Condition:       validation.Condition,
		ConditionSource: validation.ConditionSource,
		Message:         validation.Message,
		MessageSource:   validation.MessageSource,
	}, nil
}

func parseValidationBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (ComponentInputValidation, error) {
	if len(block.Labels) != 0 {
		return ComponentInputValidation{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return ComponentInputValidation{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	for attrName, attr := range block.Body.Attributes {
		if attrName != "condition" && attrName != "error_message" {
			return ComponentInputValidation{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, attrName)
		}
	}
	conditionAttr, ok := block.Body.Attributes["condition"]
	if !ok {
		return ComponentInputValidation{}, fmt.Errorf("%s:%d: %s.condition is required", file, block.TypeRange.Start.Line, path)
	}
	messageAttr, ok := block.Body.Attributes["error_message"]
	if !ok {
		return ComponentInputValidation{}, fmt.Errorf("%s:%d: %s.error_message is required", file, block.TypeRange.Start.Line, path)
	}

	messageSource := ir.SourceRef{File: file, Line: messageAttr.NameRange.Start.Line, Path: path + ".error_message"}
	message, err := evalValue(messageAttr.Expr, ctx, messageSource)
	if err != nil {
		return ComponentInputValidation{}, fmt.Errorf("%s:%d: %s.error_message: %w", file, messageSource.Line, path, err)
	}
	if message.Kind != KindString {
		return ComponentInputValidation{}, fmt.Errorf("%s:%d: %s.error_message must be a string", file, messageSource.Line, path)
	}
	if message.String == "" {
		return ComponentInputValidation{}, fmt.Errorf("%s:%d: %s.error_message must be a non-empty string", file, messageSource.Line, path)
	}

	return ComponentInputValidation{
		Source:          ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path},
		Condition:       conditionAttr.Expr,
		ConditionSource: ir.SourceRef{File: file, Line: conditionAttr.NameRange.Start.Line, Path: path + ".condition"},
		Message:         message.String,
		MessageSource:   messageSource,
	}, nil
}

func ParseComponentBody(component Component, ctx EvalContext) (Value, error) {
	values := map[string]Value{}
	for name, attr := range component.Body.Attributes {
		switch name {
		case "type", "version":
			continue
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", attr.NameRange.Filename, attr.NameRange.Start.Line, component.Source.Path, name)
		}
	}

	for _, block := range component.Body.Blocks {
		switch block.Type {
		case "input":
			continue
		case "script", "source", "extract", "build", "install":
			continue
		case "apt", "packages", "files", "secrets", "directories", "groups", "users", "systemd", "services":
			if len(block.Labels) != 0 {
				return Value{}, fmt.Errorf("%s:%d: %s block must not have labels", component.Source.File, block.TypeRange.Start.Line, block.Type)
			}
			if _, exists := values[block.Type]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.%s block", component.Source.File, block.TypeRange.Start.Line, component.Source.Path, block.Type)
			}
			value, err := parseDomainBlock(component.Source.File, component.Source.Path+"."+block.Type, block, ctx)
			if err != nil {
				return Value{}, err
			}
			values[block.Type] = value
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", component.Source.File, block.TypeRange.Start.Line, component.Source.Path, block.Type)
		}
	}
	return MapValue(values, component.Source), nil
}

func ValidateComponentBodyShape(component Component) error {
	for name, attr := range component.Body.Attributes {
		switch name {
		case "type", "version":
			continue
		default:
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", attr.NameRange.Filename, attr.NameRange.Start.Line, component.Source.Path, name)
		}
	}

	values := map[string]struct{}{}
	for _, block := range component.Body.Blocks {
		switch block.Type {
		case "input", "script", "source", "extract", "build", "install":
			continue
		case "apt", "packages", "files", "secrets", "directories", "groups", "users", "systemd", "services":
			if len(block.Labels) != 0 {
				return fmt.Errorf("%s:%d: %s block must not have labels", component.Source.File, block.TypeRange.Start.Line, block.Type)
			}
			if _, exists := values[block.Type]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.%s block", component.Source.File, block.TypeRange.Start.Line, component.Source.Path, block.Type)
			}
			if err := validateDomainBlockShape(component.Source.File, component.Source.Path+"."+block.Type, block); err != nil {
				return err
			}
			values[block.Type] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", component.Source.File, block.TypeRange.Start.Line, component.Source.Path, block.Type)
		}
	}
	return nil
}

func validateDomainBlockShape(file, path string, block *hclsyntax.Block) error {
	allowed := allowedDomainAttrs(block.Type)
	allowedBlocks := allowedDomainObjectBlocks(block.Type)
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}

	values := map[string]struct{}{}
	collections := map[string]map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		if block.Type == "nftables" && child.Type == "main" {
			if _, exists := values["main"]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.main block", file, child.TypeRange.Start.Line, path)
			}
			if err := validateNftablesMainBlockShape(file, path+".main", child); err != nil {
				return err
			}
			values["main"] = struct{}{}
			continue
		}
		if block.Type == "systemd" && child.Type == "networkd" {
			if _, exists := values["networkd"]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.networkd block", file, child.TypeRange.Start.Line, path)
			}
			if err := validateNetworkdBlockShape(file, path+".networkd", child); err != nil {
				return err
			}
			values["networkd"] = struct{}{}
			continue
		}
		if block.Type == "systemd" && (child.Type == "resolved" || child.Type == "journald") {
			if _, exists := values[child.Type]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.%s block", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if err := validateSystemdSingletonBlockShape(file, path+"."+child.Type, child); err != nil {
				return err
			}
			values[child.Type] = struct{}{}
			continue
		}
		if block.Type == "docker" {
			return validateDockerBlockShape(file, path, block)
		}
		if _, ok := allowedBlocks[child.Type]; !ok {
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
		if len(child.Labels) != 1 {
			return fmt.Errorf("%s:%d: %s.%s block requires exactly one label", file, child.TypeRange.Start.Line, path, child.Type)
		}
		label := child.Labels[0]
		objectPath := fmt.Sprintf("%s.%s[%s]", path, child.Type, strconv.Quote(label))
		if err := validateLabeledObjectBlockShape(file, block.Type, objectPath, child); err != nil {
			return err
		}
		collection, ok := collections[child.Type]
		if !ok {
			collection = map[string]struct{}{}
			collections[child.Type] = collection
		}
		if _, exists := collection[label]; exists {
			return fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
		}
		collection[label] = struct{}{}
	}
	return nil
}

func validateDockerBlockShape(file, path string, block *hclsyntax.Block) error {
	if len(block.Labels) != 0 {
		return fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	allowed := attrSet("enable", "users")
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	values := map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "package", "service", "daemon":
			if len(child.Labels) != 0 {
				return fmt.Errorf("%s:%d: %s.%s block must not have labels", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if _, exists := values[child.Type]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.%s block", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if err := validateDockerObjectBlockShape(file, path+"."+child.Type, child); err != nil {
				return err
			}
			values[child.Type] = struct{}{}
		case "compose":
			if len(child.Labels) != 1 {
				return fmt.Errorf("%s:%d: %s.compose block requires exactly one label", file, child.TypeRange.Start.Line, path)
			}
			label := child.Labels[0]
			objectPath := fmt.Sprintf("%s.compose[%s]", path, strconv.Quote(label))
			if _, exists := values[objectPath]; exists {
				return fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
			}
			if err := validateDockerComposeBlockShape(file, objectPath, child); err != nil {
				return err
			}
			values[objectPath] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return nil
}

func validateDockerObjectBlockShape(file, path string, block *hclsyntax.Block) error {
	var allowed map[string]struct{}
	switch block.Type {
	case "package":
		allowed = attrSet("source", "channel", "version", "repository_url", "gpg_url", "gpg_sha256", "remove_conflicts")
	case "service":
		allowed = attrSet("enable", "state")
	case "daemon":
		allowed = attrSet("settings")
	default:
		return fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return nil
}

func validateDockerComposeBlockShape(file, path string, block *hclsyntax.Block) error {
	allowed := attrSet("enable", "state", "directory", "project", "pull", "recreate", "remove_orphans", "after", "wanted_by")
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	values := map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "file", "service":
			if len(child.Labels) != 0 {
				return fmt.Errorf("%s:%d: %s.%s block must not have labels; multiple compose file blocks are not supported yet", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if _, exists := values[child.Type]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.%s block; multiple compose file blocks are not supported yet", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if err := validateDockerComposeObjectBlockShape(file, path+"."+child.Type, child); err != nil {
				return err
			}
			values[child.Type] = struct{}{}
		case "env_file":
			if len(child.Labels) != 1 {
				return fmt.Errorf("%s:%d: %s.env_file block requires exactly one label", file, child.TypeRange.Start.Line, path)
			}
			label := child.Labels[0]
			objectPath := fmt.Sprintf("%s.env_file[%s]", path, strconv.Quote(label))
			if _, exists := values[objectPath]; exists {
				return fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
			}
			if err := validateDockerComposeObjectBlockShape(file, objectPath, child); err != nil {
				return err
			}
			values[objectPath] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return nil
}

func validateDockerComposeObjectBlockShape(file, path string, block *hclsyntax.Block) error {
	var allowed map[string]struct{}
	switch block.Type {
	case "file", "env_file":
		allowed = attrSet("path", "content", "source", "owner", "group", "mode")
	case "service":
		allowed = attrSet("enable", "name")
	default:
		return fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return nil
}

func validateNetworkdBlockShape(file, path string, block *hclsyntax.Block) error {
	if len(block.Labels) != 0 {
		return fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	allowed := attrSet("enable")
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	collections := map[string]map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "netdev", "network":
			if len(child.Labels) != 1 {
				return fmt.Errorf("%s:%d: %s.%s block requires exactly one label", file, child.TypeRange.Start.Line, path, child.Type)
			}
			label := child.Labels[0]
			objectPath := fmt.Sprintf("%s.%s[%s]", path, child.Type, strconv.Quote(label))
			if err := validateNetworkdObjectBlockShape(file, objectPath, child); err != nil {
				return err
			}
			collection, ok := collections[child.Type]
			if !ok {
				collection = map[string]struct{}{}
				collections[child.Type] = collection
			}
			if _, exists := collection[label]; exists {
				return fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
			}
			collection[label] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return nil
}

func validateSystemdSingletonBlockShape(file, path string, block *hclsyntax.Block) error {
	if len(block.Labels) != 0 {
		return fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	var allowed map[string]struct{}
	switch block.Type {
	case "resolved":
		allowed = attrSet("enable", "state", "resolve", "owner", "file_group", "mode", "ensure")
	case "journald":
		allowed = attrSet("state", "journal", "owner", "file_group", "mode", "ensure")
	default:
		return fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	values := map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			if err := validateLifecycleBlockShape(file, path+".lifecycle", child); err != nil {
				return err
			}
			values["lifecycle"] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return nil
}

func validateNetworkdObjectBlockShape(file, path string, block *hclsyntax.Block) error {
	var allowed map[string]struct{}
	switch block.Type {
	case "netdev":
		allowed = attrSet("path", "owner", "group", "mode", "ensure", "netdev", "wireguard", "wireguard_peer")
	case "network":
		allowed = attrSet("path", "owner", "group", "mode", "ensure", "match", "network")
	default:
		return fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}

	values := map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			if err := validateLifecycleBlockShape(file, path+".lifecycle", child); err != nil {
				return err
			}
			values["lifecycle"] = struct{}{}
		case "wireguard_peer":
			if block.Type != "netdev" {
				return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if len(child.Labels) != 1 {
				return fmt.Errorf("%s:%d: %s.%s block requires exactly one label", file, child.TypeRange.Start.Line, path, child.Type)
			}
			label := child.Labels[0]
			peerPath := fmt.Sprintf("%s.wireguard_peer[%s]", path, strconv.Quote(label))
			if _, exists := values[peerPath]; exists {
				return fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, peerPath)
			}
			if err := validateNetworkdSectionBlockShape(file, peerPath, child); err != nil {
				return err
			}
			values[peerPath] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return nil
}

func validateNetworkdSectionBlockShape(file, path string, block *hclsyntax.Block) error {
	if len(block.Body.Blocks) != 0 {
		return fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		if name == "" {
			return fmt.Errorf("%s:%d: unsupported empty attribute in %s", file, attr.NameRange.Start.Line, path)
		}
	}
	return nil
}

func validateNftablesMainBlockShape(file, path string, block *hclsyntax.Block) error {
	if len(block.Labels) != 0 {
		return fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	allowed := attrSet("path", "content", "source", "owner", "group", "mode", "ensure", "sensitive", "validate", "activate")
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	values := map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			if err := validateLifecycleBlockShape(file, path+".lifecycle", child); err != nil {
				return err
			}
			values["lifecycle"] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return nil
}

func validateLabeledObjectBlockShape(file, domain, path string, block *hclsyntax.Block) error {
	allowed := allowedLabeledObjectAttrs(domain, block.Type)
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}

	values := map[string]struct{}{}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			if err := validateLifecycleBlockShape(file, path+".lifecycle", child); err != nil {
				return err
			}
			values["lifecycle"] = struct{}{}
		case "signing_key":
			if domain != "apt" || block.Type != "repository" {
				return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if _, exists := values["signing_key"]; exists {
				return fmt.Errorf("%s:%d: duplicate %s.signing_key block", file, child.TypeRange.Start.Line, path)
			}
			if err := validateSigningKeyBlockShape(file, path+".signing_key", child); err != nil {
				return err
			}
			values["signing_key"] = struct{}{}
		default:
			return fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return nil
}

func validateSigningKeyBlockShape(file, path string, block *hclsyntax.Block) error {
	if len(block.Labels) != 0 {
		return fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		switch name {
		case "url", "content", "sha256", "path":
		default:
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return nil
}

func validateLifecycleBlockShape(file, path string, block *hclsyntax.Block) error {
	if len(block.Labels) != 0 {
		return fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		if name != "prevent_destroy" {
			return fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}
	return nil
}

func parseDomainBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	allowed := allowedDomainAttrs(block.Type)
	allowedBlocks := allowedDomainObjectBlocks(block.Type)
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{
			File: file,
			Line: attr.NameRange.Start.Line,
			Path: path + "." + name,
		}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}

	for _, child := range block.Body.Blocks {
		if block.Type == "nftables" && child.Type == "main" {
			if _, exists := values["main"]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.main block", file, child.TypeRange.Start.Line, path)
			}
			main, err := parseNftablesMainBlock(file, path+".main", child, ctx)
			if err != nil {
				return Value{}, err
			}
			values["main"] = main
			continue
		}
		if block.Type == "systemd" && child.Type == "networkd" {
			if _, exists := values["networkd"]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.networkd block", file, child.TypeRange.Start.Line, path)
			}
			networkd, err := parseNetworkdBlock(file, path+".networkd", child, ctx)
			if err != nil {
				return Value{}, err
			}
			values["networkd"] = networkd
			continue
		}
		if block.Type == "systemd" && (child.Type == "resolved" || child.Type == "journald") {
			if _, exists := values[child.Type]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.%s block", file, child.TypeRange.Start.Line, path, child.Type)
			}
			value, err := parseSystemdSingletonBlock(file, path+"."+child.Type, child, ctx)
			if err != nil {
				return Value{}, err
			}
			values[child.Type] = value
			continue
		}
		if _, ok := allowedBlocks[child.Type]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
		if len(child.Labels) != 1 {
			return Value{}, fmt.Errorf("%s:%d: %s.%s block requires exactly one label", file, child.TypeRange.Start.Line, path, child.Type)
		}
		label := child.Labels[0]
		objectPath := fmt.Sprintf("%s.%s[%s]", path, child.Type, strconv.Quote(label))
		object, err := parseLabeledObjectBlock(file, block.Type, objectPath, child, ctx)
		if err != nil {
			return Value{}, err
		}
		collection, ok := values[child.Type]
		if !ok {
			collection = MapValue(nil, ir.SourceRef{File: file, Line: child.TypeRange.Start.Line, Path: path + "." + child.Type})
		}
		if _, exists := collection.Map[label]; exists {
			return Value{}, fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
		}
		collection.Map[label] = object
		values[child.Type] = collection
	}

	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseNetworkdBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	if len(block.Labels) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if name != "enable" {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}

	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "netdev", "network":
			if len(child.Labels) != 1 {
				return Value{}, fmt.Errorf("%s:%d: %s.%s block requires exactly one label", file, child.TypeRange.Start.Line, path, child.Type)
			}
			label := child.Labels[0]
			objectPath := fmt.Sprintf("%s.%s[%s]", path, child.Type, strconv.Quote(label))
			object, err := parseNetworkdObjectBlock(file, objectPath, child, ctx)
			if err != nil {
				return Value{}, err
			}
			collection, ok := values[child.Type]
			if !ok {
				collection = MapValue(nil, ir.SourceRef{File: file, Line: child.TypeRange.Start.Line, Path: path + "." + child.Type})
			}
			if _, exists := collection.Map[label]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, objectPath)
			}
			collection.Map[label] = object
			values[child.Type] = collection
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseSystemdSingletonBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	if len(block.Labels) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	var allowed map[string]struct{}
	switch block.Type {
	case "resolved":
		allowed = attrSet("enable", "state", "resolve", "owner", "file_group", "mode", "ensure")
	case "journald":
		allowed = attrSet("state", "journal", "owner", "file_group", "mode", "ensure")
	default:
		return Value{}, fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}
	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			lifecycle, err := parseLifecycleBlock(file, path+".lifecycle", child, ctx)
			if err != nil {
				return Value{}, err
			}
			values["lifecycle"] = lifecycle
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseNetworkdObjectBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	var allowed map[string]struct{}
	switch block.Type {
	case "netdev":
		allowed = attrSet("path", "owner", "group", "mode", "ensure", "netdev", "wireguard", "wireguard_peer")
	case "network":
		allowed = attrSet("path", "owner", "group", "mode", "ensure", "match", "network")
	default:
		return Value{}, fmt.Errorf("%s:%d: unsupported block %s", file, block.TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}

	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			lifecycle, err := parseLifecycleBlock(file, path+".lifecycle", child, ctx)
			if err != nil {
				return Value{}, err
			}
			values["lifecycle"] = lifecycle
		case "wireguard_peer":
			if block.Type != "netdev" {
				return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if len(child.Labels) != 1 {
				return Value{}, fmt.Errorf("%s:%d: %s.%s block requires exactly one label", file, child.TypeRange.Start.Line, path, child.Type)
			}
			label := child.Labels[0]
			peerPath := fmt.Sprintf("%s.wireguard_peer[%s]", path, strconv.Quote(label))
			peer, err := parseNetworkdSectionBlock(file, peerPath, child, ctx)
			if err != nil {
				return Value{}, err
			}
			collection, ok := values["wireguard_peer"]
			if !ok {
				collection = MapValue(nil, ir.SourceRef{File: file, Line: child.TypeRange.Start.Line, Path: path + ".wireguard_peer"})
			}
			if !collection.IsMap() {
				return Value{}, fmt.Errorf("%s:%d:%s: must be a map", collection.Source.File, collection.Source.Line, collection.Source.Path)
			}
			if _, exists := collection.Map[label]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s", file, child.TypeRange.Start.Line, peerPath)
			}
			collection.Map[label] = peer
			values["wireguard_peer"] = collection
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseNetworkdSectionBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	if len(block.Body.Blocks) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		attrSource := ir.SourceRef{File: file, Line: attr.NameRange.Start.Line, Path: path + "." + name}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseNftablesMainBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	if len(block.Labels) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	allowed := attrSet("path", "content", "source", "owner", "group", "mode", "ensure", "sensitive", "validate", "activate")
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{
			File: file,
			Line: attr.NameRange.Start.Line,
			Path: path + "." + name,
		}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}

	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			lifecycle, err := parseLifecycleBlock(file, path+".lifecycle", child, ctx)
			if err != nil {
				return Value{}, err
			}
			values["lifecycle"] = lifecycle
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseLabeledObjectBlock(file, domain, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	allowed := allowedLabeledObjectAttrs(domain, block.Type)
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if _, ok := allowed[name]; !ok {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{
			File: file,
			Line: attr.NameRange.Start.Line,
			Path: path + "." + name,
		}
		if domain == "files" && block.Type == "file" && name == "on_change" {
			ref, err := parseScriptTraversal(file, attr.Expr)
			if err != nil {
				return Value{}, err
			}
			ref.Source = attrSource
			values[name] = Value{Kind: KindString, String: ref.Name, ScriptReference: &ref, Source: attrSource}
			continue
		}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}

	for _, child := range block.Body.Blocks {
		switch child.Type {
		case "lifecycle":
			if _, exists := values["lifecycle"]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.lifecycle block", file, child.TypeRange.Start.Line, path)
			}
			lifecycle, err := parseLifecycleBlock(file, path+".lifecycle", child, ctx)
			if err != nil {
				return Value{}, err
			}
			values["lifecycle"] = lifecycle
		case "signing_key":
			if domain != "apt" || block.Type != "repository" {
				return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
			}
			if _, exists := values["signing_key"]; exists {
				return Value{}, fmt.Errorf("%s:%d: duplicate %s.signing_key block", file, child.TypeRange.Start.Line, path)
			}
			signingKey, err := parseSigningKeyBlock(file, path+".signing_key", child, ctx)
			if err != nil {
				return Value{}, err
			}
			values["signing_key"] = signingKey
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported block %s.%s", file, child.TypeRange.Start.Line, path, child.Type)
		}
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseSigningKeyBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	if len(block.Labels) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		switch name {
		case "url", "content", "sha256", "path":
		default:
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{
			File: file,
			Line: attr.NameRange.Start.Line,
			Path: path + "." + name,
		}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseLifecycleBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	if len(block.Labels) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, path)
	}
	if len(block.Body.Blocks) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	values := map[string]Value{}
	for name, attr := range block.Body.Attributes {
		if name != "prevent_destroy" {
			return Value{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
		attrSource := ir.SourceRef{
			File: file,
			Line: attr.NameRange.Start.Line,
			Path: path + "." + name,
		}
		value, err := evalValue(attr.Expr, ctx, attrSource)
		if err != nil {
			return Value{}, fmt.Errorf("%s:%d: %s.%s: %w", file, attrSource.Line, path, name, err)
		}
		values[name] = value
	}
	return MapValue(values, ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}), nil
}

func parseAssertBlock(file, path string, block *hclsyntax.Block, ctx EvalContext) (Assert, error) {
	if len(block.Body.Blocks) != 0 {
		return Assert{}, fmt.Errorf("%s:%d: %s does not support nested blocks", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}
	for name, attr := range block.Body.Attributes {
		if name != "condition" && name != "message" {
			return Assert{}, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}

	condition, ok := block.Body.Attributes["condition"]
	if !ok {
		return Assert{}, fmt.Errorf("%s:%d: %s.condition is required", file, block.TypeRange.Start.Line, path)
	}
	message, ok := block.Body.Attributes["message"]
	if !ok {
		return Assert{}, fmt.Errorf("%s:%d: %s.message is required", file, block.TypeRange.Start.Line, path)
	}

	messageSource := ir.SourceRef{File: file, Line: message.NameRange.Start.Line, Path: path + ".message"}
	messageValue, err := evalValue(message.Expr, ctx, messageSource)
	if err != nil {
		return Assert{}, fmt.Errorf("%s:%d: %s.message: %w", file, messageSource.Line, path, err)
	}
	messageString, ok := messageValue.StringValue()
	if !ok || messageString == "" {
		return Assert{}, fmt.Errorf("%s:%d: %s.message must be a non-empty string", file, messageSource.Line, path)
	}

	return Assert{
		Source:          ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path},
		Condition:       condition.Expr,
		ConditionSource: ir.SourceRef{File: file, Line: condition.NameRange.Start.Line, Path: path + ".condition"},
		Message:         messageString,
		MessageSource:   messageSource,
	}, nil
}

func allowedDomainAttrs(domain string) map[string]struct{} {
	switch domain {
	case "ssh":
		return attrSet("host", "port", "user", "identity_file")
	case "state":
		return attrSet("path", "lock_path")
	case "platform":
		return attrSet("distribution", "version", "architecture", "codename")
	case "system":
		return attrSet("hostname", "architecture", "codename", "timezone", "locale")
	case "kernel":
		return attrSet("modules", "sysctl")
	case "packages":
		return attrSet("install")
	case "apt", "files", "secrets", "directories", "groups", "users", "systemd", "services":
		return attrSet()
	case "nftables":
		return attrSet("enable")
	default:
		return map[string]struct{}{}
	}
}

func allowedDomainObjectBlocks(domain string) map[string]struct{} {
	switch domain {
	case "apt":
		return attrSet("repository", "source_file")
	case "packages":
		return attrSet("package")
	case "files", "secrets":
		return attrSet("file")
	case "directories":
		return attrSet("directory")
	case "groups":
		return attrSet("group")
	case "users":
		return attrSet("user")
	case "systemd":
		return attrSet("unit", "service_unit", "timer")
	case "services":
		return attrSet("service")
	case "nftables":
		return attrSet("file")
	default:
		return map[string]struct{}{}
	}
}

func allowedLabeledObjectAttrs(domain string, blockType string) map[string]struct{} {
	switch domain + "." + blockType {
	case "apt.repository":
		return attrSet("uris", "suites", "components", "architectures", "ensure")
	case "apt.source_file":
		return attrSet("path", "content", "source", "owner", "group", "mode", "ensure", "on_destroy")
	case "packages.package":
		return attrSet("repositories")
	case "files.file":
		return attrSet("path", "content", "content_version", "source", "owner", "group", "mode", "ensure", "sensitive", "on_change")
	case "secrets.file":
		return attrSet("path", "source", "owner", "group", "mode", "ensure")
	case "directories.directory":
		return attrSet("owner", "group", "mode", "ensure")
	case "groups.group":
		return attrSet("gid", "system", "ensure")
	case "users.user":
		return attrSet("uid", "home", "shell", "group", "groups", "system", "ssh_authorized_keys", "ensure")
	case "systemd.unit":
		return attrSet("content", "source", "owner", "group", "mode", "ensure")
	case "systemd.service_unit":
		return attrSet(
			"content", "source", "owner", "file_group", "mode", "ensure",
			"description", "run", "type", "user", "group", "working_dir",
			"environment", "restart", "restart_delay", "wants", "after",
			"wanted_by", "stdout", "stderr", "service_config",
		)
	case "systemd.timer":
		return attrSet("description", "timer", "install", "wanted_by", "enable", "state", "owner", "file_group", "mode", "ensure")
	case "services.service":
		return attrSet("package", "enabled", "state")
	case "nftables.file":
		return attrSet("path", "content", "source", "owner", "group", "mode", "ensure", "sensitive", "validate", "activate")
	default:
		return map[string]struct{}{}
	}
}

func validateProfileBody(profile ir.SourceRef, body Value) error {
	if platform, ok := body.Map["platform"]; ok {
		for _, name := range []string{"distribution", "version", "architecture", "codename"} {
			if value, exists := platform.Map[name]; exists {
				return fmt.Errorf("%s:%d: %s is host-only and cannot be declared in profile %s", value.Source.File, value.Source.Line, value.Source.Path, profile.Path)
			}
		}
	}
	system, ok := body.Map["system"]
	if !ok {
		return nil
	}
	for _, name := range []string{"hostname", "architecture", "codename"} {
		if value, exists := system.Map[name]; exists {
			return fmt.Errorf("%s:%d: %s is host-only and cannot be declared in profile %s", value.Source.File, value.Source.Line, value.Source.Path, profile.Path)
		}
	}
	return nil
}

func parseProfileRefs(file string, expr hcl.Expression) ([]string, error) {
	items, diags := hcl.ExprList(expr)
	if diags.HasErrors() {
		return nil, fmt.Errorf("must be a list of profile references")
	}
	refs := make([]string, 0, len(items))
	for _, item := range items {
		traversal, diags := hcl.AbsTraversalForExpr(item)
		if diags.HasErrors() {
			return nil, fmt.Errorf("%s:%d: import must be profile.<name>", file, item.Range().Start.Line)
		}
		if len(traversal) != 2 {
			return nil, fmt.Errorf("%s:%d: import must be profile.<name>", file, item.Range().Start.Line)
		}
		root, ok := traversal[0].(hcl.TraverseRoot)
		if !ok || root.Name != "profile" {
			return nil, fmt.Errorf("%s:%d: import must be profile.<name>", file, item.Range().Start.Line)
		}
		attr, ok := traversal[1].(hcl.TraverseAttr)
		if !ok || attr.Name == "" {
			return nil, fmt.Errorf("%s:%d: import must be profile.<name>", file, item.Range().Start.Line)
		}
		refs = append(refs, attr.Name)
	}
	return refs, nil
}

func blockSource(file, path string, block *hclsyntax.Block) ir.SourceRef {
	return ir.SourceRef{File: file, Line: block.TypeRange.Start.Line, Path: path}
}

func attrSet(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}

func sortedAttrNames(attrs hclsyntax.Attributes) []string {
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
