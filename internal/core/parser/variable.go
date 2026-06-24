package parser

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mofelee/debianform/internal/core/ir"
)

func NormalizeVariableValue(variable Variable, value Value) (Value, error) {
	if value.Kind == KindNull && !variable.Nullable {
		return Value{}, fmt.Errorf("%s:%d:%s: variable %q must not be null", value.Source.File, value.Source.Line, value.Source.Path, variable.Name)
	}
	normalized, err := normalizeValueForType(variable.Name, variable.TypeSpec, value, "")
	if err != nil {
		return Value{}, err
	}
	if variable.Sensitive {
		normalized.Sensitive = true
	}
	if variable.Ephemeral {
		normalized.Ephemeral = true
	}
	return normalized, nil
}

func ParseVariableFile(path string) ([]ExternalVariableValue, error) {
	if strings.HasSuffix(path, ".json") {
		return parseJSONVariableFile(path)
	}
	return parseHCLVariableFile(path)
}

func parseHCLVariableFile(path string) ([]ExternalVariableValue, error) {
	hclParser := hclparse.NewParser()
	hclFile, diags := hclParser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	body, ok := hclFile.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("%s: unsupported HCL body type %T", path, hclFile.Body)
	}
	if len(body.Blocks) != 0 {
		block := body.Blocks[0]
		return nil, fmt.Errorf("%s:%d: var file does not support blocks", path, block.DefRange().Start.Line)
	}

	names := make([]string, 0, len(body.Attributes))
	for name := range body.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)

	ctx := EvalContext{ModuleDir: filepath.Dir(path)}
	values := make([]ExternalVariableValue, 0, len(names))
	for _, name := range names {
		attr := body.Attributes[name]
		source := ir.SourceRef{File: path, Line: attr.NameRange.Start.Line, Path: "varfile." + name}
		value, err := evalValue(attr.Expr, ctx, source)
		if err != nil {
			return nil, fmt.Errorf("%s:%d:%s: var file value %q: %w", source.File, source.Line, source.Path, name, err)
		}
		values = append(values, ExternalVariableValue{
			Name:        name,
			ParsedValue: &value,
			Source:      source,
		})
	}
	return values, nil
}

func parseJSONVariableFile(path string) ([]ExternalVariableValue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	source := ir.SourceRef{File: path, Line: 1, Path: "varfile"}
	value, err := parseJSONVariableValue(string(data), source)
	if err != nil {
		return nil, fmt.Errorf("%s:%d:%s: invalid JSON var file: %w", source.File, source.Line, source.Path, err)
	}
	if !value.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: JSON var file must contain an object", source.File, source.Line, source.Path)
	}

	values := make([]ExternalVariableValue, 0, len(value.Map))
	for _, name := range sortedValueKeys(value.Map) {
		item := value.Map[name]
		values = append(values, ExternalVariableValue{
			Name:        name,
			ParsedValue: &item,
			Source:      item.Source,
		})
	}
	return values, nil
}

func parseExternalVariableValue(variable Variable, item ExternalVariableValue) (Value, error) {
	source := item.Source
	if source.Path == "" {
		source.Path = "cli.var." + variable.Name
	}
	if variable.TypeSpec.Kind == ComponentInputTypeString {
		return Value{Kind: KindString, String: item.Value, Source: source}, nil
	}
	if isComplexType(variable.TypeSpec.Kind) && looksLikeJSON(item.Value) {
		value, err := parseJSONVariableValue(item.Value, source)
		if err != nil {
			if variable.Sensitive {
				return Value{}, fmt.Errorf("%s:%d:%s: invalid value for sensitive variable %q", source.File, source.Line, source.Path, variable.Name)
			}
			return Value{}, fmt.Errorf("%s:%d:%s: invalid JSON value for variable %q: %w", source.File, source.Line, source.Path, variable.Name, err)
		}
		return value, nil
	}

	expr, diags := hclsyntax.ParseExpression([]byte(item.Value), source.File, hclPos(source.Line))
	if diags.HasErrors() {
		if variable.Sensitive {
			return Value{}, fmt.Errorf("%s:%d:%s: invalid value for sensitive variable %q", source.File, source.Line, source.Path, variable.Name)
		}
		return Value{}, fmt.Errorf("%s:%d:%s: invalid value for variable %q: %s", source.File, source.Line, source.Path, variable.Name, diags.Error())
	}
	value, err := evalValue(expr, EvalContext{}, source)
	if err != nil {
		if variable.Sensitive {
			return Value{}, fmt.Errorf("%s:%d:%s: invalid value for sensitive variable %q", source.File, source.Line, source.Path, variable.Name)
		}
		return Value{}, fmt.Errorf("%s:%d:%s: invalid value for variable %q: %w", source.File, source.Line, source.Path, variable.Name, err)
	}
	return value, nil
}

func isComplexType(kind ComponentInputTypeKind) bool {
	switch kind {
	case ComponentInputTypeList, ComponentInputTypeSet, ComponentInputTypeMap, ComponentInputTypeObject, ComponentInputTypeTuple, ComponentInputTypeAny:
		return true
	default:
		return false
	}
}

func looksLikeJSON(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || trimmed == "null"
}

func parseJSONVariableValue(raw string, source ir.SourceRef) (Value, error) {
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return Value{}, err
	}
	var trailing struct{}
	err := decoder.Decode(&trailing)
	if err == nil {
		return Value{}, fmt.Errorf("invalid trailing JSON data")
	}
	if err != io.EOF {
		return Value{}, err
	}
	return jsonValueToValue(decoded, source)
}

func jsonValueToValue(decoded any, source ir.SourceRef) (Value, error) {
	switch value := decoded.(type) {
	case nil:
		return NullValue(source), nil
	case string:
		return Value{Kind: KindString, String: value, Source: source}, nil
	case bool:
		return Value{Kind: KindBool, Bool: value, Source: source}, nil
	case json.Number:
		if _, err := strconv.ParseFloat(value.String(), 64); err != nil {
			return Value{}, err
		}
		return Value{Kind: KindNumber, Number: value.String(), Source: source}, nil
	case []any:
		items := make([]Value, 0, len(value))
		for i, item := range value {
			itemSource := source
			itemSource.Path = fmt.Sprintf("%s[%d]", source.Path, i)
			converted, err := jsonValueToValue(item, itemSource)
			if err != nil {
				return Value{}, err
			}
			items = append(items, converted)
		}
		return Value{Kind: KindList, List: items, Source: source}, nil
	case map[string]any:
		items := make(map[string]Value, len(value))
		for _, key := range sortedJSONKeys(value) {
			itemSource := source
			itemSource.Path = fmt.Sprintf("%s[%q]", source.Path, key)
			converted, err := jsonValueToValue(value[key], itemSource)
			if err != nil {
				return Value{}, err
			}
			items[key] = converted
		}
		return MapValue(items, source), nil
	default:
		return Value{}, fmt.Errorf("unsupported JSON value %T", decoded)
	}
}

func sortedJSONKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hclPos(line int) hcl.Pos {
	if line <= 0 {
		line = 1
	}
	return hcl.Pos{Line: line, Column: 1, Byte: 0}
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
