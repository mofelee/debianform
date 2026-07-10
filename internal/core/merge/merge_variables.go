package merge

import (
	"fmt"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
	"github.com/zclconf/go-cty/cty"
)

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
