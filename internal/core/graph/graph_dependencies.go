package graph

import (
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

func applyExplicitDependencies(host string, nodes []Node, dependencies []ir.ResourceDependencySpec) error {
	if len(dependencies) == 0 {
		return nil
	}
	prefix := "host." + host + "."
	byAddress := make(map[string]int, len(nodes))
	for i := range nodes {
		byAddress[nodes[i].Address] = i
	}
	for _, dependency := range dependencies {
		if !strings.HasPrefix(dependency.From, prefix) || !strings.HasPrefix(dependency.DependsOn, prefix) {
			return dependencyError(dependency, "explicit dependency crosses host scope")
		}
		from, exists := byAddress[dependency.From]
		if !exists {
			return dependencyError(dependency, fmt.Sprintf("dependent resource graph address %q does not exist", dependency.From))
		}
		if _, exists := byAddress[dependency.DependsOn]; !exists {
			return dependencyError(dependency, fmt.Sprintf("dependency resource graph address %q does not exist", dependency.DependsOn))
		}
		nodes[from].DependsOn = dedupeStrings(append(nodes[from].DependsOn, dependency.DependsOn))
		nodes[from].ExplicitDependsOn = dedupeStrings(append(nodes[from].ExplicitDependsOn, dependency.DependsOn))
		if nodes[from].DependencySources == nil {
			nodes[from].DependencySources = map[string]ir.SourceRef{}
		}
		nodes[from].DependencySources[dependency.DependsOn] = dependency.Source
	}
	return nil
}

func dependencyError(dependency ir.ResourceDependencySpec, message string) error {
	if dependency.Source.File != "" {
		return fmt.Errorf("%s:%d:%s: %s", dependency.Source.File, dependency.Source.Line, dependency.Source.Path, message)
	}
	return fmt.Errorf("%s", message)
}
