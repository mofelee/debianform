package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func inSyncPlan(node graph.Node, prior *corestate.Resource, summary string, observed map[string]any) ProviderPlan {
	if observed == nil {
		observed = map[string]any{}
	}
	observed = cloneMap(observed)
	observed["desired_digest"] = corestate.DesiredDigest(node.Desired)
	if prior == nil {
		return ProviderPlan{Action: ActionAdopt, Summary: "adopt existing " + node.Kind + " " + identity(node), Observed: observed, Ownership: "adopted"}
	}
	return ProviderPlan{Action: ActionNoOp, Summary: summary, Observed: observed, Ownership: ownership(prior)}
}

func absentInSyncPlan(prior *corestate.Resource, summary string, observed map[string]any) ProviderPlan {
	if observed == nil {
		observed = map[string]any{}
	}
	if prior != nil {
		return ProviderPlan{Action: ActionDelete, Summary: summary, Observed: cloneMap(observed), Ownership: ownership(prior)}
	}
	return ProviderPlan{Action: ActionNoOp, Summary: summary, Observed: cloneMap(observed), Ownership: ownership(prior)}
}

func ownership(prior *corestate.Resource) string {
	if prior != nil && prior.Ownership != "" {
		return prior.Ownership
	}
	return "managed"
}

func ensureAbsent(node graph.Node) bool {
	return stringDesired(node, "ensure") == "absent"
}

func stringDesired(node graph.Node, name string) string {
	return stringMapValue(node.Desired, name)
}

func stringMapValue(values map[string]any, name string) string {
	if value, ok := values[name]; ok {
		switch v := value.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		}
	}
	return ""
}

func boolMapValue(values map[string]any, name string) bool {
	value, _ := values[name].(bool)
	return value
}

func intMapValue(values map[string]any, name string) int {
	switch value := values[name].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func boolDesired(node graph.Node, name string) bool {
	value, _ := node.Desired[name].(bool)
	return value
}

func boolObserved(step Step, name string) bool {
	value, _ := step.Observed[name].(bool)
	return value
}

func stringListDesired(node graph.Node, name string) []string {
	value, ok := node.Desired[name]
	if !ok || value == nil {
		return nil
	}
	return stringListAnyValue(value)
}

func stringListMapValue(values map[string]any, name string) []string {
	value, ok := values[name]
	if !ok || value == nil {
		return nil
	}
	return stringListAnyValue(value)
}

func stringListAnyValue(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func commandMatrixDesired(node graph.Node, name string) [][]string {
	value, ok := node.Desired[name]
	if !ok || value == nil {
		return nil
	}
	switch v := value.(type) {
	case [][]string:
		out := make([][]string, 0, len(v))
		for _, command := range v {
			out = append(out, append([]string(nil), command...))
		}
		return out
	case []any:
		out := make([][]string, 0, len(v))
		for _, rawCommand := range v {
			switch command := rawCommand.(type) {
			case []string:
				out = append(out, append([]string(nil), command...))
			case []any:
				args := make([]string, 0, len(command))
				for _, rawArg := range command {
					if arg, ok := rawArg.(string); ok {
						args = append(args, arg)
					}
				}
				out = append(out, args)
			}
		}
		return out
	default:
		return nil
	}
}

func modulePath(name string) string {
	return "/etc/modules-load.d/dbf-" + safeName(name) + ".conf"
}

func sysctlPath(key string) string {
	return "/etc/sysctl.d/99-dbf-" + safeName(key) + ".conf"
}

func safeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "resource"
	}
	return strings.ToLower(out)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeMode(mode string) string {
	return strings.TrimLeft(mode, "0")
}

func displayMode(mode string) string {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= 4 {
		return trimmed
	}
	return strings.Repeat("0", 4-len(trimmed)) + trimmed
}

func nonEmptyLines(output string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
