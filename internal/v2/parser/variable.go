package parser

import (
	"fmt"
	"sort"

	"github.com/mofelee/debianform/internal/v2/ir"
)

func NormalizeVariableValue(variable Variable, value Value) (Value, error) {
	if value.Kind == KindNull && !variable.Nullable {
		return Value{}, fmt.Errorf("%s:%d:%s: variable %q must not be null", value.Source.File, value.Source.Line, value.Source.Path, variable.Name)
	}
	normalized, err := normalizeValueForType(variable.Name, variable.TypeSpec, value, "")
	if err != nil {
		return Value{}, err
	}
	return normalized, nil
}

func normalizeValueForType(name string, spec ComponentInputTypeSpec, value Value, path string) (Value, error) {
	if value.Kind == KindNull {
		return value, nil
	}
	switch spec.Kind {
	case ComponentInputTypeAny:
		return value, nil
	case ComponentInputTypeString:
		if value.Kind != KindString {
			return Value{}, typeConstraintError(name, value, path, "string")
		}
		return value, nil
	case ComponentInputTypeNumber:
		if value.Kind != KindNumber {
			return Value{}, typeConstraintError(name, value, path, "number")
		}
		return value, nil
	case ComponentInputTypeBool:
		if value.Kind != KindBool {
			return Value{}, typeConstraintError(name, value, path, "bool")
		}
		return value, nil
	case ComponentInputTypeList, ComponentInputTypeSet:
		if !value.IsList() {
			want := string(spec.Kind) + "(" + elementTypeName(spec) + ")"
			return Value{}, typeConstraintError(name, value, path, want)
		}
		out := value
		out.List = make([]Value, 0, len(value.List))
		for i, item := range value.List {
			normalized, err := normalizeValueForType(name, *spec.Element, item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return Value{}, err
			}
			out.List = append(out.List, normalized)
		}
		if spec.Kind == ComponentInputTypeSet {
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
	case ComponentInputTypeMap:
		if !value.IsMap() {
			return Value{}, typeConstraintError(name, value, path, "map("+elementTypeName(spec)+")")
		}
		out := value
		out.Map = make(map[string]Value, len(value.Map))
		for _, key := range sortedValueKeys(value.Map) {
			normalized, err := normalizeValueForType(name, *spec.Element, value.Map[key], fmt.Sprintf("%s[%q]", path, key))
			if err != nil {
				return Value{}, err
			}
			out.Map[key] = normalized
		}
		return out, nil
	case ComponentInputTypeObject:
		if !value.IsMap() {
			return Value{}, typeConstraintError(name, value, path, "object")
		}
		out := value
		out.Map = make(map[string]Value, len(spec.Attributes))
		for key := range value.Map {
			if _, ok := spec.Attributes[key]; !ok {
				return Value{}, fmt.Errorf("%s:%d:%s: variable %q has unsupported attribute %s%s", value.Map[key].Source.File, value.Map[key].Source.Line, value.Map[key].Source.Path, name, pathPrefix(path), attributePath(key))
			}
		}
		for _, attrName := range sortedTypeAttrKeys(spec.Attributes) {
			attr := spec.Attributes[attrName]
			item, ok := value.Map[attrName]
			attrPath := path + "." + attrName
			if !ok {
				if attr.Default != nil {
					normalized, err := normalizeValueForType(name, attr.Type, *attr.Default, attrPath)
					if err != nil {
						return Value{}, err
					}
					out.Map[attrName] = normalized
					continue
				}
				if attr.Optional {
					out.Map[attrName] = NullValue(missingObjectAttributeSource(value, attrName, attrPath))
					continue
				}
				return Value{}, fmt.Errorf("%s:%d:%s: variable %q missing required attribute %s", value.Source.File, value.Source.Line, value.Source.Path, name, attrPath)
			}
			normalized, err := normalizeValueForType(name, attr.Type, item, attrPath)
			if err != nil {
				return Value{}, err
			}
			out.Map[attrName] = normalized
		}
		return out, nil
	case ComponentInputTypeTuple:
		if !value.IsList() {
			return Value{}, typeConstraintError(name, value, path, "tuple")
		}
		if len(value.List) != len(spec.Tuple) {
			return Value{}, fmt.Errorf("%s:%d:%s: variable %q%s must have %d entries, got %d", value.Source.File, value.Source.Line, value.Source.Path, name, path, len(spec.Tuple), len(value.List))
		}
		out := value
		out.List = make([]Value, 0, len(value.List))
		for i, item := range value.List {
			normalized, err := normalizeValueForType(name, spec.Tuple[i], item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return Value{}, err
			}
			out.List = append(out.List, normalized)
		}
		return out, nil
	default:
		return Value{}, fmt.Errorf("%s:%d:%s: unsupported variable type %q", value.Source.File, value.Source.Line, value.Source.Path, spec.Kind)
	}
}

func typeConstraintError(name string, value Value, path string, want string) error {
	return fmt.Errorf("%s:%d:%s: variable %q%s must be %s, got %s", value.Source.File, value.Source.Line, value.Source.Path, name, path, want, value.Kind)
}

func elementTypeName(spec ComponentInputTypeSpec) string {
	if spec.Element == nil {
		return "any"
	}
	return spec.Element.String()
}

func sortedValueKeys(values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedTypeAttrKeys(values map[string]ComponentObjectAttrSpec) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func pathPrefix(path string) string {
	if path == "" {
		return ""
	}
	return path
}

func attributePath(name string) string {
	return "." + name
}

func missingObjectAttributeSource(parent Value, name string, path string) ir.SourceRef {
	source := parent.Source
	source.Path += path
	if item, ok := parent.Map[name]; ok {
		source = item.Source
	}
	return source
}
