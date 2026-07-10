package merge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

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
