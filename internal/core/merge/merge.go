package merge

import (
	"fmt"
	"sort"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
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
	scripts, err := rootScriptDefinitionSpecs(cfg.Scripts)
	if err != nil {
		return nil, err
	}
	program := &ir.Program{
		Hosts:      make([]ir.HostSpec, 0, len(names)),
		Variables:  variables,
		Components: components,
		Scripts:    scripts,
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
		hostScripts, err := compiler.hostScriptSpecs(spec)
		if err != nil {
			return nil, err
		}
		spec.Scripts = hostScripts
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
