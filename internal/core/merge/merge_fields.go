package merge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func mapField(root parser.Value, name string) (parser.Value, bool, error) {
	if !root.IsMap() {
		return parser.Value{}, false, fmt.Errorf("%s:%d: expected object at %s", root.Source.File, root.Source.Line, root.Source.Path)
	}
	value, ok := root.Map[name]
	if !ok {
		return parser.Value{}, false, nil
	}
	if !value.IsMap() {
		return parser.Value{}, false, fmt.Errorf("%s:%d: %s must be an object", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return value, true, nil
}

func stringField(root parser.Value, name string) (string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return "", false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return "", false, err
	}
	str, ok := value.StringValue()
	if !ok {
		return "", false, fmt.Errorf("%s:%d: %s must be a string", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return str, true, nil
}

func intField(root parser.Value, name string) (int, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return 0, false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return 0, false, err
	}
	str, ok := value.StringValue()
	if !ok {
		return 0, false, fmt.Errorf("%s:%d: %s must be a number", value.Source.File, value.Source.Line, value.Source.Path)
	}
	n, err := strconv.Atoi(str)
	if err != nil {
		return 0, false, fmt.Errorf("%s:%d: %s must be an integer", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return n, true, nil
}

func boolField(root parser.Value, name string) (bool, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return false, false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return false, false, err
	}
	if value.Kind != parser.KindBool {
		return false, false, fmt.Errorf("%s:%d:%s: must be a boolean", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return value.Bool, true, nil
}

func listField(root parser.Value, name string) (parser.Value, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return parser.Value{}, false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return parser.Value{}, false, err
	}
	if !value.IsList() {
		return parser.Value{}, false, fmt.Errorf("%s:%d: %s must be a list", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return value, true, nil
}

func objectPath(root parser.Value, name string, defaultPath string) (string, error) {
	path, ok, err := stringField(root, name)
	if err != nil {
		return "", err
	}
	if ok {
		return path, nil
	}
	return defaultPath, nil
}

func objectCollection(root parser.Value, field string) (map[string]parser.Value, bool, error) {
	collection, ok := root.Map[field]
	if !ok {
		return nil, false, nil
	}
	if !collection.IsMap() {
		return nil, false, fmt.Errorf("%s:%d:%s: must be a map", collection.Source.File, collection.Source.Line, collection.Source.Path)
	}
	return collection.Map, true, nil
}

func stringFieldAllowEphemeral(root parser.Value, name string) (string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return "", false, nil
	}
	str, ok := value.StringValue()
	if !ok {
		return "", false, fmt.Errorf("%s:%d: %s must be a string", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return str, true, nil
}

func rejectEphemeralValue(value parser.Value) error {
	if !value.ContainsEphemeral() {
		return nil
	}
	return fmt.Errorf("%s:%d:%s: ephemeral value is not allowed in this field", value.Source.File, value.Source.Line, value.Source.Path)
}

func stringListField(root parser.Value, name string) ([]string, error) {
	list, ok, err := listField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	out := make([]string, 0, len(list.List))
	seen := map[string]struct{}{}
	for _, item := range list.List {
		value, ok := item.StringValue()
		if !ok || value == "" {
			return nil, fmt.Errorf("%s:%d:%s: %s entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path, list.Source.Path)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%s:%d:%s: duplicate %s entry %q", item.Source.File, item.Source.Line, item.Source.Path, list.Source.Path, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func requiredStringListField(root parser.Value, name string) ([]string, error) {
	value, ok := root.Map[name]
	if !ok {
		return nil, fmt.Errorf("%s:%d:%s.%s: required non-empty string list", root.Source.File, root.Source.Line, root.Source.Path, name)
	}
	values, err := stringListField(root, name)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s:%d:%s: required non-empty string list", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return values, nil
}

func stringFieldDefault(root parser.Value, name string, fallback string) (string, error) {
	value, ok, err := stringField(root, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return fallback, nil
	}
	return value, nil
}

func optionalStringField(root parser.Value, name string) (*string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return nil, false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return nil, false, err
	}
	if value.Kind == parser.KindNull {
		return nil, true, nil
	}
	str, ok := value.StringValue()
	if !ok {
		return nil, false, fmt.Errorf("%s:%d: %s must be a string or null", value.Source.File, value.Source.Line, value.Source.Path)
	}
	return &str, true, nil
}

func dockerRemoveConflictsField(root parser.Value, name string) (string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return "", false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return "", false, err
	}
	switch value.Kind {
	case parser.KindString:
		if !stringIn(value.String, "auto", "true", "false") {
			return "", false, enumError(value.Source, "auto, true, or false")
		}
		return value.String, true, nil
	case parser.KindBool:
		if value.Bool {
			return "true", true, nil
		}
		return "false", true, nil
	default:
		return "", false, fmt.Errorf("%s:%d:%s: %s must be auto, true, or false", value.Source.File, value.Source.Line, value.Source.Path, name)
	}
}

func jsonCompatibleAny(value parser.Value) (any, error) {
	if value.ContainsSensitive() || value.ContainsEphemeral() {
		return nil, fmt.Errorf("%s:%d:%s: docker daemon settings cannot contain sensitive or ephemeral values", value.Source.File, value.Source.Line, value.Source.Path)
	}
	switch value.Kind {
	case parser.KindNull:
		return nil, nil
	case parser.KindString:
		return value.String, nil
	case parser.KindBool:
		return value.Bool, nil
	case parser.KindNumber:
		return json.Number(value.Number), nil
	case parser.KindList:
		out := make([]any, 0, len(value.List))
		for _, item := range value.List {
			converted, err := jsonCompatibleAny(item)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	case parser.KindMap:
		out := make(map[string]any, len(value.Map))
		for _, key := range sortedKeys(value.Map) {
			if key == "" {
				item := value.Map[key]
				return nil, fmt.Errorf("%s:%d:%s: docker daemon settings keys must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			converted, err := jsonCompatibleAny(value.Map[key])
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s:%d:%s: unsupported docker daemon settings value", value.Source.File, value.Source.Line, value.Source.Path)
	}
}

func jsonContentSummary(value any, source ir.SourceRef) (ir.ContentSummary, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return ir.ContentSummary{}, fmt.Errorf("%s:%d:%s: json marshal failed: %w", source.File, source.Line, source.Path, err)
	}
	return contentSummary(data), nil
}

func validateDockerStableName(kind string, value string, source ir.SourceRef) error {
	if value == "" {
		return fmt.Errorf("%s:%d:%s: %s must be non-empty", source.File, source.Line, source.Path, kind)
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			(i > 0 && (r == '_' || r == '.' || r == '@' || r == '%' || r == '+' || r == '-'))
		if !valid {
			return fmt.Errorf("%s:%d:%s: %s %q must use only letters, digits, _, ., @, %%, +, or - and must start with a letter or digit", source.File, source.Line, source.Path, kind, value)
		}
	}
	return nil
}

func stringIn(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func enumError(source ir.SourceRef, allowed string) error {
	return fmt.Errorf("%s:%d:%s: must be %s", source.File, source.Line, source.Path, allowed)
}

func ensureField(root parser.Value, fallback string) (string, error) {
	ensure, ok, err := stringField(root, "ensure")
	if err != nil {
		return "", err
	}
	if !ok || ensure == "" {
		return fallback, nil
	}
	if ensure != "present" && ensure != "absent" {
		return "", fmt.Errorf("%s:%d:%s.ensure: ensure must be present or absent", root.Source.File, root.Source.Line, root.Source.Path)
	}
	return ensure, nil
}

func lifecycleSpec(root parser.Value) (*ir.LifecycleSpec, error) {
	lifecycle, ok, err := mapField(root, "lifecycle")
	if err != nil || !ok {
		return nil, err
	}
	preventDestroy, ok, err := boolField(lifecycle, "prevent_destroy")
	if err != nil {
		return nil, err
	}
	if !ok || !preventDestroy {
		return nil, nil
	}
	return &ir.LifecycleSpec{PreventDestroy: preventDestroy, Source: lifecycle.Source}, nil
}

func modeFieldDefault(root parser.Value, name string, fallback string) (string, error) {
	mode, err := stringFieldDefault(root, name, fallback)
	if err != nil {
		return "", err
	}
	if !modePattern.MatchString(mode) {
		return "", fmt.Errorf("%s:%d:%s.%s: mode must be a four digit octal string", root.Source.File, root.Source.Line, root.Source.Path, name)
	}
	return mode, nil
}

func boolFieldDefault(root parser.Value, name string, fallback bool) (bool, error) {
	value, ok, err := boolField(root, name)
	if err != nil {
		return false, err
	}
	if !ok {
		return fallback, nil
	}
	return value, nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
