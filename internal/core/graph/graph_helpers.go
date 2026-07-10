package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

var providerNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_]+`)
var shellSafeArgPattern = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func providerName(parts ...string) string {
	joined := strings.Join(parts, "_")
	normalized := providerNamePattern.ReplaceAllString(joined, "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		return "resource"
	}
	return strings.ToLower(normalized)
}

func quoteCommand(args []string) string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, shellQuoteCommandArg(arg))
	}
	return strings.Join(out, " ")
}

func shellQuoteCommandArg(value string) string {
	if value != "" && shellSafeArgPattern.MatchString(value) {
		return value
	}
	return shellQuoteGraph(value)
}

func deterministicJSON(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func contentSummary(data []byte) ir.ContentSummary {
	sum := sha256.Sum256(data)
	return ir.ContentSummary{SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
}

func packageNameFromDockerPackageAddress(address string) string {
	start := strings.LastIndex(address, "[")
	end := strings.LastIndex(address, "]")
	if start < 0 || end <= start+1 {
		return ""
	}
	name, err := strconv.Unquote(address[start+1 : end])
	if err != nil {
		return ""
	}
	return name
}

func contentResourceDesiredPayload(desired map[string]any, content, sourcePath string, sensitive bool, summary ir.ContentSummary) (map[string]any, map[string]any) {
	hasContent := content != "" || (sourcePath == "" && summary.SHA256 != "")
	if sensitive {
		desired["sensitive"] = true
		if summary.SHA256 != "" {
			desired["content_sha256"] = summary.SHA256
			desired["content_bytes"] = summary.Bytes
		}
	} else {
		if hasContent {
			desired["content"] = content
		}
		if sourcePath != "" {
			desired["source_path"] = sourcePath
		}
	}
	payload := cloneMap(desired)
	if hasContent {
		payload["content"] = content
	}
	if sourcePath != "" {
		payload["source_path"] = sourcePath
	}
	return desired, payload
}

func ownershipDependencies(owner, group string, userAddresses, groupAddresses map[string]string) []string {
	deps := []string{}
	if owner != "" && owner != "root" {
		if address, ok := userAddresses[owner]; ok {
			deps = append(deps, address)
		}
	}
	if group != "" && group != "root" {
		if address, ok := groupAddresses[group]; ok {
			deps = append(deps, address)
		}
	}
	return dedupeStrings(deps)
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func dedupeStrings(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func lifecyclePtr(lifecycle *ir.LifecycleSpec) *ir.LifecycleSpec {
	if lifecycle == nil || !lifecycle.PreventDestroy {
		return nil
	}
	copy := *lifecycle
	return &copy
}

func cloneStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string(nil), values...)
}
