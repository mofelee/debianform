package parser

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/zclconf/go-cty/cty"
)

type localDefinition struct {
	Name      string
	Expr      hcl.Expression
	Source    ir.SourceRef
	ModuleDir string
}

type localVisitState int

const (
	localUnvisited localVisitState = iota
	localVisiting
	localVisited
)

var errLocalRequiresVariables = errors.New("local requires variables")

func evaluateStaticLocals(cfg *Config) error {
	values, err := evaluateLocalDefinitions(cfg, false)
	if err != nil {
		return err
	}
	cfg.Locals = values
	return nil
}

func evaluateLocals(cfg *Config) error {
	values, err := evaluateLocalDefinitions(cfg, true)
	if err != nil {
		return err
	}
	cfg.Locals = values
	return nil
}

func evaluateLocalDefinitions(cfg *Config, allowVariables bool) (map[string]Value, error) {
	values := map[string]Value{}
	state := map[string]localVisitState{}

	var variables map[string]cty.Value
	if allowVariables {
		varNamespace, err := variableNamespaceValue(cfg)
		if err != nil {
			return nil, err
		}
		variables = map[string]cty.Value{"var": varNamespace}
	}

	var eval func(string, []string) (Value, error)
	eval = func(name string, stack []string) (Value, error) {
		if value, ok := values[name]; ok {
			return value, nil
		}
		def, ok := cfg.localDefinitions[name]
		if !ok {
			return Value{}, fmt.Errorf("unknown local local.%s", name)
		}
		switch state[name] {
		case localVisited:
			return values[name], nil
		case localVisiting:
			return Value{}, localCycleError(stack, name)
		}
		if !allowVariables && localExprReferencesVar(def.Expr) {
			return Value{}, errLocalRequiresVariables
		}

		state[name] = localVisiting
		defer func() {
			if state[name] == localVisiting {
				state[name] = localUnvisited
			}
		}()

		if err := validateLocalReferences(cfg, def, allowVariables); err != nil {
			return Value{}, localDefinitionError(def, err)
		}

		stack = append(stack, name)
		for _, dep := range localDependencies(def.Expr) {
			if _, err := eval(dep, stack); err != nil {
				return Value{}, err
			}
		}

		value, err := evalValue(def.Expr, EvalContext{
			ModuleDir: def.ModuleDir,
			Locals:    values,
			Variables: variables,
		}, def.Source)
		if err != nil {
			return Value{}, localDefinitionError(def, err)
		}
		values[name] = value
		state[name] = localVisited
		return value, nil
	}

	for _, name := range sortedLocalDefinitionNames(cfg.localDefinitions) {
		if _, ok := values[name]; ok {
			continue
		}
		if _, err := eval(name, nil); err != nil {
			if errors.Is(err, errLocalRequiresVariables) {
				continue
			}
			return nil, err
		}
	}
	return values, nil
}

func variableNamespaceValue(cfg *Config) (cty.Value, error) {
	varValues := make(map[string]cty.Value, len(cfg.VariableValues))
	for _, name := range sortedVariableValueNames(cfg.VariableValues) {
		converted, err := cfg.VariableValues[name].ToCty()
		if err != nil {
			return cty.NilVal, fmt.Errorf("var.%s: %w", name, err)
		}
		varValues[name] = converted
	}
	if len(varValues) == 0 {
		return cty.EmptyObjectVal, nil
	}
	return cty.ObjectVal(varValues), nil
}

func validateLocalReferences(cfg *Config, def localDefinition, allowVariables bool) error {
	for _, traversal := range def.Expr.Variables() {
		root, ok := traversalRoot(traversal)
		if !ok {
			continue
		}
		switch root {
		case "local":
			name, ok := traversalFirstAttr(traversal)
			if !ok {
				continue
			}
			if _, exists := cfg.localDefinitions[name]; !exists {
				return fmt.Errorf("unknown local local.%s", name)
			}
		case "var":
			if !allowVariables {
				continue
			}
			name, ok := traversalFirstAttr(traversal)
			if !ok {
				continue
			}
			if _, exists := cfg.Variables[name]; !exists {
				return fmt.Errorf("unknown variable var.%s", name)
			}
			if _, exists := cfg.VariableValues[name]; !exists {
				return fmt.Errorf("unknown variable var.%s", name)
			}
		}
	}
	return nil
}

func localExprReferencesVar(expr hcl.Expression) bool {
	for _, traversal := range expr.Variables() {
		root, ok := traversalRoot(traversal)
		if ok && root == "var" {
			return true
		}
	}
	return false
}

func localDependencies(expr hcl.Expression) []string {
	deps := map[string]bool{}
	for _, traversal := range expr.Variables() {
		root, ok := traversalRoot(traversal)
		if !ok || root != "local" {
			continue
		}
		name, ok := traversalFirstAttr(traversal)
		if ok {
			deps[name] = true
		}
	}
	names := make([]string, 0, len(deps))
	for name := range deps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func traversalRoot(traversal hcl.Traversal) (string, bool) {
	if len(traversal) == 0 {
		return "", false
	}
	root, ok := traversal[0].(hcl.TraverseRoot)
	if !ok {
		return "", false
	}
	return root.Name, true
}

func traversalFirstAttr(traversal hcl.Traversal) (string, bool) {
	if len(traversal) < 2 {
		return "", false
	}
	attr, ok := traversal[1].(hcl.TraverseAttr)
	if !ok {
		return "", false
	}
	return attr.Name, true
}

func localCycleError(stack []string, repeated string) error {
	start := 0
	for i, name := range stack {
		if name == repeated {
			start = i
			break
		}
	}
	chain := append([]string{}, stack[start:]...)
	chain = append(chain, repeated)
	for i, name := range chain {
		chain[i] = "local." + name
	}
	return fmt.Errorf("locals cycle detected: %s", strings.Join(chain, " -> "))
}

func localDefinitionError(def localDefinition, err error) error {
	return fmt.Errorf("%s:%d: local.%s: %w", def.Source.File, def.Source.Line, def.Name, err)
}

func sortedLocalDefinitionNames(defs map[string]localDefinition) []string {
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
