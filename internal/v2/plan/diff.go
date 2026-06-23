package plan

import (
	"encoding/json"
	"github.com/mofelee/debianform/internal/v2/ir"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

var setFieldNames = map[string]struct{}{
	"groups":              {},
	"repositories":        {},
	"ssh_authorized_keys": {},
}

func BuildDiff(action string, before, after any) DiffNode {
	before = normalizeDiffValue(before)
	after = normalizeDiffValue(after)
	sensitive := valueSensitive(before) || valueSensitive(after)
	rawBefore := before
	rawAfter := after

	if sensitive {
		beforeMap, _ := before.(map[string]any)
		afterMap, _ := after.(map[string]any)
		before = sanitizeSensitiveMap(beforeMap)
		after = sanitizeSensitiveMap(afterMap)
	}

	root := buildDiffNode(nil, action, before, after, false)
	root.Kind = "object"
	if action != "" {
		root.Action = action
	}
	root.Sensitive = sensitive
	if sensitive {
		root.Children = append(root.Children, sensitiveContentDiff(action, rawBefore, rawAfter))
		sort.SliceStable(root.Children, func(i, j int) bool {
			return strings.Join(root.Children[i].Path, ".") < strings.Join(root.Children[j].Path, ".")
		})
	}
	return root
}

func buildDiffNode(path []string, action string, before, after any, nestedMap bool) DiffNode {
	if path == nil {
		path = []string{}
	}
	nodeAction := diffAction(action, before, after)
	node := DiffNode{
		Path:      append([]string{}, path...),
		Action:    nodeAction,
		Sensitive: false,
	}

	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap || afterIsMap {
		node.Kind = "object"
		if nestedMap {
			node.Kind = "map"
		}
		node.Children = mapChildren(path, action, beforeMap, afterMap)
		return node
	}

	beforeList, beforeIsList := before.([]any)
	afterList, afterIsList := after.([]any)
	if beforeIsList || afterIsList {
		node.Kind = listKind(path)
		node.Children = listChildren(path, action, beforeList, afterList)
		return node
	}

	if isTextPath(path) {
		beforeText, _ := before.(string)
		afterText, _ := after.(string)
		node.Kind = "text"
		node.Hunks = textHunks(beforeText, afterText)
		return node
	}

	node.Kind = "scalar"
	node.Before = before
	node.After = after
	return node
}

func mapChildren(path []string, action string, before, after map[string]any) []DiffNode {
	keys := map[string]struct{}{}
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)

	children := make([]DiffNode, 0, len(names))
	for _, key := range names {
		beforeValue, beforeOK := before[key]
		afterValue, afterOK := after[key]
		if !beforeOK {
			beforeValue = nil
		}
		if !afterOK {
			afterValue = nil
		}
		if beforeOK && afterOK && reflect.DeepEqual(beforeValue, afterValue) {
			continue
		}
		childPath := appendPath(path, key)
		_, childIsMap := beforeValue.(map[string]any)
		if _, ok := afterValue.(map[string]any); ok {
			childIsMap = true
		}
		children = append(children, buildDiffNode(childPath, action, beforeValue, afterValue, childIsMap))
	}
	return children
}

func listChildren(path []string, action string, before, after []any) []DiffNode {
	length := len(before)
	if len(after) > length {
		length = len(after)
	}
	children := make([]DiffNode, 0, length)
	for i := 0; i < length; i++ {
		var beforeValue any
		var afterValue any
		if i < len(before) {
			beforeValue = before[i]
		}
		if i < len(after) {
			afterValue = after[i]
		}
		if i < len(before) && i < len(after) && reflect.DeepEqual(beforeValue, afterValue) {
			continue
		}
		childPath := appendPath(path, strconv.Itoa(i))
		children = append(children, buildDiffNode(childPath, action, beforeValue, afterValue, false))
	}
	return children
}

func sensitiveContentDiff(action string, before, after any) DiffNode {
	return DiffNode{
		Path:          []string{"content"},
		Kind:          "sensitive",
		Action:        diffAction(action, before, after),
		Sensitive:     true,
		BeforeSummary: contentSummary(before),
		AfterSummary:  contentSummary(after),
	}
}

func sanitizeSensitiveMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		switch key {
		case "content", "source_path", "summary", "content_sha256", "content_bytes":
			continue
		default:
			out[key] = value
		}
	}
	return out
}

func contentSummary(value any) map[string]any {
	values, ok := value.(map[string]any)
	if !ok || values == nil {
		return nil
	}
	if summary, ok := values["summary"].(map[string]any); ok {
		return cloneSummary(summary)
	}
	if summary, ok := values["summary"].(ir.ContentSummary); ok {
		out := map[string]any{}
		if summary.SHA256 != "" {
			out["sha256"] = summary.SHA256
		}
		if summary.Bytes != 0 {
			out["bytes"] = summary.Bytes
		}
		if len(out) > 0 {
			return out
		}
	}
	out := map[string]any{}
	if sha, ok := values["content_sha256"]; ok {
		out["sha256"] = sha
	} else if sha, ok := values["sha256"]; ok {
		out["sha256"] = sha
	}
	if size, ok := values["content_bytes"]; ok {
		out["bytes"] = size
	} else if size, ok := values["bytes"]; ok {
		out["bytes"] = size
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneSummary(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func textHunks(before, after string) []TextHunk {
	beforeLines := splitTextLines(before)
	afterLines := splitTextLines(after)
	prefix := commonPrefix(beforeLines, afterLines)
	suffix := commonSuffix(beforeLines[prefix:], afterLines[prefix:])
	beforeChanged := beforeLines[prefix : len(beforeLines)-suffix]
	afterChanged := afterLines[prefix : len(afterLines)-suffix]
	if len(beforeChanged) == 0 && len(afterChanged) == 0 {
		return nil
	}

	lines := make([]DiffLine, 0, len(beforeChanged)+len(afterChanged))
	for _, line := range beforeChanged {
		lines = append(lines, DiffLine{Op: "delete", Text: line})
	}
	for _, line := range afterChanged {
		lines = append(lines, DiffLine{Op: "create", Text: line})
	}
	return []TextHunk{{
		OldStart: prefix + 1,
		OldLines: len(beforeChanged),
		NewStart: prefix + 1,
		NewLines: len(afterChanged),
		Lines:    lines,
	}}
}

func splitTextLines(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(value, "\n"), "\n")
}

func commonPrefix(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

func commonSuffix(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return i
}

func diffAction(fallback string, before, after any) string {
	switch {
	case before == nil && after != nil:
		return "create"
	case before != nil && after == nil:
		return "delete"
	case reflect.DeepEqual(before, after):
		return "no-op"
	case fallback != "":
		return fallback
	default:
		return "update"
	}
}

func listKind(path []string) string {
	if len(path) > 0 {
		if _, ok := setFieldNames[path[len(path)-1]]; ok {
			return "set"
		}
	}
	return "list"
}

func isTextPath(path []string) bool {
	return len(path) > 0 && path[len(path)-1] == "content"
}

func appendPath(path []string, segment string) []string {
	out := make([]string, 0, len(path)+1)
	out = append(out, path...)
	out = append(out, segment)
	return out
}

func valueSensitive(value any) bool {
	values, ok := normalizeDiffValue(value).(map[string]any)
	if !ok {
		return false
	}
	sensitive, _ := values["sensitive"].(bool)
	return sensitive
}

func normalizeDiffValue(value any) any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return value
	}
	return normalized
}
