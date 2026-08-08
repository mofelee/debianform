package state

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

type MoveTarget struct {
	ProviderAddress string
	Desired         map[string]any
}

type RealizedMove struct {
	Host string `json:"host"`
	From string `json:"from"`
	To   string `json:"to"`
}

type MoveResult struct {
	State State
	Moves []RealizedMove
}

func ResolveMoves(st State, declarations []ir.MovedSpec, desiredComponents map[string]bool, targets map[string]MoveTarget) (MoveResult, error) {
	normalized, err := Normalize(st, st.Host)
	if err != nil {
		return MoveResult{}, err
	}
	result := MoveResult{State: cloneState(normalized)}
	if len(declarations) == 0 {
		return result, nil
	}
	ordered, err := validateAndOrderMoves(st.Host, declarations)
	if err != nil {
		return MoveResult{}, err
	}
	finalTargets := finalMoveTargets(ordered)

	for _, declaration := range ordered {
		addresses := matchingAddresses(result.State.Resources, declaration.From)
		if len(addresses) == 0 {
			continue
		}
		finalTarget := finalTargets[declaration.From]
		sourceDesired := desiredComponents[declaration.From]
		targetDesired := desiredComponents[finalTarget]
		if sourceDesired {
			if targetDesired {
				return MoveResult{}, moveDiagnostic(declaration.FromSource, "%s has matching state while both source and destination components are desired", declaration.From)
			}
			for _, from := range addresses {
				to := replaceAddressPrefix(from, declaration.From, declaration.To)
				if _, exists := result.State.Resources[to]; exists {
					return MoveResult{}, moveDiagnostic(declaration.FromSource, "cannot move %s to %s because the destination state entry already exists", from, to)
				}
			}
			continue
		}
		if !targetDesired {
			return MoveResult{}, moveDiagnostic(declaration.FromSource, "%s has matching state but destination component %s is not present in the desired graph", declaration.From, finalTarget)
		}

		fromComponent, _ := componentFromPrefix(st.Host, declaration.From)
		toComponent, _ := componentFromPrefix(st.Host, declaration.To)
		for _, from := range addresses {
			to := replaceAddressPrefix(from, declaration.From, declaration.To)
			if _, exists := result.State.Resources[to]; exists {
				return MoveResult{}, moveDiagnostic(declaration.FromSource, "cannot move %s to %s because the destination state entry already exists", from, to)
			}
			resource := result.State.Resources[from]
			delete(result.State.Resources, from)
			resource = rebaseMovedResource(resource, fromComponent, toComponent, targets[to])
			result.State.Resources[to] = resource
			result.Moves = append(result.Moves, RealizedMove{Host: st.Host, From: from, To: to})
		}
		rebaseResourceDependencies(result.State.Resources, declaration.From, declaration.To)
	}
	return result, nil
}

func validateAndOrderMoves(host string, declarations []ir.MovedSpec) ([]ir.MovedSpec, error) {
	moves := append([]ir.MovedSpec(nil), declarations...)
	sort.SliceStable(moves, func(i, j int) bool {
		if moves[i].From != moves[j].From {
			return moves[i].From < moves[j].From
		}
		return moves[i].To < moves[j].To
	})
	bySource := make(map[string]ir.MovedSpec, len(moves))
	byTarget := make(map[string]ir.MovedSpec, len(moves))
	validated := make([]ir.MovedSpec, 0, len(moves))
	for _, move := range moves {
		if _, ok := componentFromPrefix(host, move.From); !ok {
			return nil, moveDiagnostic(move.FromSource, "move source %s is not a component root for host %q", move.From, host)
		}
		if _, ok := componentFromPrefix(host, move.To); !ok {
			return nil, moveDiagnostic(move.ToSource, "move destination %s is not a component root for host %q", move.To, host)
		}
		if move.From == move.To {
			return nil, moveDiagnostic(move.FromSource, "%s cannot move to itself", move.From)
		}
		if previous, ok := bySource[move.From]; ok {
			return nil, moveDiagnostic(move.FromSource, "move source %s is declared more than once; first destination is %s", move.From, previous.To)
		}
		if previous, ok := byTarget[move.To]; ok {
			return nil, moveDiagnostic(move.FromSource, "move sources %s and %s both target %s", previous.From, move.From, move.To)
		}
		for _, previous := range validated {
			if prefixesOverlap(previous.From, move.From) || prefixesOverlap(previous.To, move.To) {
				return nil, moveDiagnostic(move.FromSource, "move %s to %s overlaps mapping %s to %s", move.From, move.To, previous.From, previous.To)
			}
		}
		bySource[move.From] = move
		byTarget[move.To] = move
		validated = append(validated, move)
	}

	depths := map[string]int{}
	visiting := map[string]bool{}
	var depth func(string) (int, error)
	depth = func(source string) (int, error) {
		if value, ok := depths[source]; ok {
			return value, nil
		}
		if visiting[source] {
			move := bySource[source]
			return 0, moveDiagnostic(move.FromSource, "moved mappings contain a cycle through %s", source)
		}
		visiting[source] = true
		value := 1
		if next, ok := bySource[bySource[source].To]; ok {
			nextDepth, err := depth(next.From)
			if err != nil {
				return 0, err
			}
			value += nextDepth
		}
		delete(visiting, source)
		depths[source] = value
		return value, nil
	}
	sources := make([]string, 0, len(bySource))
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		if _, err := depth(source); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(moves, func(i, j int) bool {
		if depths[moves[i].From] != depths[moves[j].From] {
			return depths[moves[i].From] > depths[moves[j].From]
		}
		return moves[i].From < moves[j].From
	})
	return moves, nil
}

func finalMoveTargets(moves []ir.MovedSpec) map[string]string {
	next := make(map[string]string, len(moves))
	for _, move := range moves {
		next[move.From] = move.To
	}
	out := make(map[string]string, len(moves))
	for _, move := range moves {
		target := move.To
		for {
			nextTarget, ok := next[target]
			if !ok {
				break
			}
			target = nextTarget
		}
		out[move.From] = target
	}
	return out
}

func matchingAddresses(resources map[string]Resource, prefix string) []string {
	out := make([]string, 0)
	for address := range resources {
		if address == prefix || strings.HasPrefix(address, prefix+".") {
			out = append(out, address)
		}
	}
	sort.Strings(out)
	return out
}

func replaceAddressPrefix(address, from, to string) string {
	return to + strings.TrimPrefix(address, from)
}

func prefixesOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+".") || strings.HasPrefix(right, left+".")
}

func componentFromPrefix(host, prefix string) (string, bool) {
	root := "host." + host + ".components."
	if !strings.HasPrefix(prefix, root) {
		return "", false
	}
	component := strings.TrimPrefix(prefix, root)
	if component == "" || strings.Contains(component, ".") || strings.ContainsAny(component, "[]") {
		return "", false
	}
	return component, true
}

func rebaseMovedResource(resource Resource, fromComponent, toComponent string, target MoveTarget) Resource {
	oldDigest := resource.DesiredDigest
	resource.Desired = cloneMap(resource.Desired)
	resource.Observed = cloneMap(resource.Observed)
	resource.Lifecycle = cloneLifecycle(resource.Lifecycle)
	desiredChanged := false
	if _, exists := resource.Desired["component"]; exists || resource.Kind == "component_script_output" {
		component := toComponent
		if targetComponent, ok := target.Desired["component"].(string); ok && targetComponent != "" {
			component = targetComponent
		}
		if current, _ := resource.Desired["component"].(string); current != component || current == fromComponent {
			resource.Desired["component"] = component
			desiredChanged = true
		}
	}
	if target.ProviderAddress != "" {
		resource.ProviderAddress = target.ProviderAddress
	}
	if desiredChanged {
		resource.DesiredDigest = DesiredDigest(resource.Desired)
		if observedDigest, ok := resource.Observed["desired_digest"].(string); ok && observedDigest == oldDigest {
			resource.Observed["desired_digest"] = resource.DesiredDigest
		}
	}
	return resource
}

func rebaseResourceDependencies(resources map[string]Resource, from, to string) {
	for address, resource := range resources {
		dependsOn := append([]string(nil), resource.DependsOn...)
		changed := false
		for i, dependency := range dependsOn {
			if dependency != from && !strings.HasPrefix(dependency, from+".") {
				continue
			}
			dependsOn[i] = replaceAddressPrefix(dependency, from, to)
			changed = true
		}
		if changed {
			resource.DependsOn = dependsOn
			resources[address] = resource
		}
	}
}

func cloneState(st State) State {
	out := st
	if st.Facts != nil {
		facts := *st.Facts
		out.Facts = &facts
	}
	out.Resources = make(map[string]Resource, len(st.Resources))
	for address, resource := range st.Resources {
		resource.Desired = cloneMap(resource.Desired)
		resource.Observed = cloneMap(resource.Observed)
		resource.Lifecycle = cloneLifecycle(resource.Lifecycle)
		resource.DependsOn = append([]string(nil), resource.DependsOn...)
		out.Resources[address] = resource
	}
	return out
}

func cloneLifecycle(lifecycle *ir.LifecycleSpec) *ir.LifecycleSpec {
	if lifecycle == nil {
		return nil
	}
	copy := *lifecycle
	return &copy
}

func moveDiagnostic(source ir.SourceRef, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if source.File == "" {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s:%d:%s: %s", source.File, source.Line, source.Path, message)
}
