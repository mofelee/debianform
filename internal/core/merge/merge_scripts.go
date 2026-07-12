package merge

import (
	"fmt"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/zclconf/go-cty/cty"
)

func rootScriptDefinitionSpecs(scripts map[string]parser.ComponentScript) (map[string]ir.ScriptDefinitionSpec, error) {
	if len(scripts) == 0 {
		return nil, nil
	}
	out := make(map[string]ir.ScriptDefinitionSpec, len(scripts))
	for _, name := range sortedKeys(scripts) {
		script := scripts[name]
		if err := validateComponentScriptShape(script); err != nil {
			return nil, err
		}
		out[name] = ir.ScriptDefinitionSpec{
			Name:          name,
			DeclarationID: script.Source.Path,
			Source:        script.Source,
		}
	}
	return out, nil
}

func (c *compiler) hostScriptSpecs(target ir.HostSpec) (map[string]ir.ComponentScriptSpec, error) {
	if len(c.cfg.Scripts) == 0 {
		return nil, nil
	}
	variables, err := c.evalVariables(map[string]cty.Value{"target": hostSpecToCty(target)})
	if err != nil {
		return nil, err
	}
	out := make(map[string]ir.ComponentScriptSpec, len(c.cfg.Scripts))
	for _, name := range sortedKeys(c.cfg.Scripts) {
		definition := c.cfg.Scripts[name]
		evaluated, err := parser.EvaluateComponentScript(definition, parser.EvalContext{
			ModuleDir: definition.ModuleDir,
			Locals:    c.cfg.Locals,
			Variables: variables,
		})
		if err != nil {
			return nil, err
		}
		spec, err := componentScriptSpec(evaluated)
		if err != nil {
			return nil, err
		}
		if spec.Mode != "once" {
			source := evaluated.Source
			if evaluated.ModeSet {
				source = evaluated.ModeSource
			}
			return nil, fmt.Errorf("%s:%d:%s: root script mode must be once", source.File, source.Line, source.Path)
		}
		spec.DeclarationID = definition.Source.Path
		out[name] = spec
	}
	return out, nil
}

func scriptReferenceName(ref *ir.ScriptReferenceSpec) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}
