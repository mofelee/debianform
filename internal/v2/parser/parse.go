package parser

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mofelee/debianform/internal/v2/ir"
)

type Config struct {
	Files      []string
	Locals     map[string]Value
	Profiles   map[string]Profile
	Hosts      map[string]Host
	Components map[string]ir.SourceRef
}

type Profile struct {
	Name    string
	Source  ir.SourceRef
	Imports []string
	Body    Value
	Asserts []Assert
}

type Host struct {
	Name    string
	Source  ir.SourceRef
	Imports []string
	Body    Value
	Asserts []Assert
}

type Assert struct {
	Source          ir.SourceRef
	Condition       hcl.Expression
	ConditionSource ir.SourceRef
	Message         string
	MessageSource   ir.SourceRef
}

func ParseFiles(files []string) (*Config, error) {
	cfg := &Config{
		Files:      append([]string(nil), files...),
		Locals:     map[string]Value{},
		Profiles:   map[string]Profile{},
		Hosts:      map[string]Host{},
		Components: map[string]ir.SourceRef{},
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
		if err := parseLocals(cfg, file.file, file.body); err != nil {
			return nil, err
		}
	}

	for _, file := range parsed {
		if err := parseTopLevel(cfg, file.file, file.body); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func parseLocals(cfg *Config, file string, body *hclsyntax.Body) error {
	ctx := EvalContext{ModuleDir: filepath.Dir(file), Locals: cfg.Locals}
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
			attrSource := ir.SourceRef{
				File: file,
				Line: attr.NameRange.Start.Line,
				Path: "local." + name,
			}
			value, err := evalValue(attr.Expr, ctx, attrSource)
			if err != nil {
				return fmt.Errorf("%s:%d: local.%s: %w", file, attrSource.Line, name, err)
			}
			cfg.Locals[name] = value
			ctx.Locals = cfg.Locals
		}
	}
	return nil
}

func parseTopLevel(cfg *Config, file string, body *hclsyntax.Body) error {
	ctx := EvalContext{ModuleDir: filepath.Dir(file), Locals: cfg.Locals}
	for _, block := range body.Blocks {
		switch block.Type {
		case "locals":
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
			source, name, err := parseComponentHeader(file, block)
			if err != nil {
				return err
			}
			if previous, exists := cfg.Components[name]; exists {
				return fmt.Errorf("%s:%d: duplicate component %q; first defined at %s:%d", file, source.Line, name, previous.File, previous.Line)
			}
			cfg.Components[name] = source
		default:
			return fmt.Errorf("%s:%d: unknown v2 top-level block %q", file, block.TypeRange.Start.Line, block.Type)
		}
	}
	return nil
}

func parseProfile(file string, block *hclsyntax.Block, ctx EvalContext) (Profile, error) {
	source, name, err := parseLabeledHeader(file, "profile", block)
	if err != nil {
		return Profile{}, err
	}
	imports, body, asserts, err := parseHostLikeBody(file, source.Path, block.Body, ctx)
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
	imports, body, asserts, err := parseHostLikeBody(file, source.Path, block.Body, ctx)
	if err != nil {
		return Host{}, err
	}
	return Host{Name: name, Source: source, Imports: imports, Body: body, Asserts: asserts}, nil
}

func parseComponentHeader(file string, block *hclsyntax.Block) (ir.SourceRef, string, error) {
	return parseLabeledHeader(file, "component", block)
}

func parseLabeledHeader(file, typ string, block *hclsyntax.Block) (ir.SourceRef, string, error) {
	line := block.TypeRange.Start.Line
	if len(block.Labels) != 1 {
		return ir.SourceRef{}, "", fmt.Errorf("%s:%d: %s block requires exactly one label", file, line, typ)
	}
	name := block.Labels[0]
	return ir.SourceRef{File: file, Line: line, Path: typ + "." + name}, name, nil
}

func parseHostLikeBody(file, path string, body *hclsyntax.Body, ctx EvalContext) ([]string, Value, []Assert, error) {
	imports := []string{}
	asserts := []Assert{}
	values := map[string]Value{}

	for name, attr := range body.Attributes {
		switch name {
		case "imports":
			refs, err := parseProfileRefs(file, attr.Expr)
			if err != nil {
				return nil, Value{}, nil, fmt.Errorf("%s:%d: %s.imports: %w", file, attr.NameRange.Start.Line, path, err)
			}
			imports = refs
		case "components":
			continue
		default:
			return nil, Value{}, nil, fmt.Errorf("%s:%d: unsupported attribute %s.%s", file, attr.NameRange.Start.Line, path, name)
		}
	}

	for _, block := range body.Blocks {
		switch block.Type {
		case "ssh", "state", "system", "kernel", "packages":
			if len(block.Labels) != 0 {
				return nil, Value{}, nil, fmt.Errorf("%s:%d: %s block must not have labels", file, block.TypeRange.Start.Line, block.Type)
			}
			if _, exists := values[block.Type]; exists {
				return nil, Value{}, nil, fmt.Errorf("%s:%d: duplicate %s.%s block", file, block.TypeRange.Start.Line, path, block.Type)
			}
			value, err := parseDomainBlock(file, path+"."+block.Type, block, ctx)
			if err != nil {
				return nil, Value{}, nil, err
			}
			values[block.Type] = value
		case "assert":
			if len(block.Labels) != 0 {
				return nil, Value{}, nil, fmt.Errorf("%s:%d: assert block must not have labels", file, block.TypeRange.Start.Line)
			}
			assert, err := parseAssertBlock(file, fmt.Sprintf("%s.assert[%d]", path, len(asserts)), block, ctx)
			if err != nil {
				return nil, Value{}, nil, err
			}
			asserts = append(asserts, assert)
		default:
			return nil, Value{}, nil, fmt.Errorf("%s:%d: unsupported block %s.%s", file, block.TypeRange.Start.Line, path, block.Type)
		}
	}

	return imports, MapValue(values, ir.SourceRef{File: file, Line: body.SrcRange.Start.Line, Path: path}), asserts, nil
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

func parseLabeledObjectBlock(file, domain, path string, block *hclsyntax.Block, ctx EvalContext) (Value, error) {
	if len(block.Body.Blocks) != 0 {
		return Value{}, fmt.Errorf("%s:%d: %s does not support nested blocks in this v2 implementation loop", file, block.Body.Blocks[0].TypeRange.Start.Line, path)
	}

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
	case "system":
		return attrSet("hostname", "architecture", "codename", "timezone", "locale")
	case "kernel":
		return attrSet("modules", "sysctl")
	case "packages":
		return attrSet("install")
	default:
		return map[string]struct{}{}
	}
}

func allowedDomainObjectBlocks(domain string) map[string]struct{} {
	switch domain {
	case "packages":
		return attrSet("package")
	default:
		return map[string]struct{}{}
	}
}

func allowedLabeledObjectAttrs(domain string, blockType string) map[string]struct{} {
	switch domain + "." + blockType {
	case "packages.package":
		return attrSet("repositories")
	default:
		return map[string]struct{}{}
	}
}

func validateProfileBody(profile ir.SourceRef, body Value) error {
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
