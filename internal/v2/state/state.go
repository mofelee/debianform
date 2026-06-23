package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/mofelee/debianform/internal/v2/ir"
)

const Version = 2

type State struct {
	Version   int                 `json:"version"`
	Host      string              `json:"host"`
	Serial    int                 `json:"serial"`
	UpdatedAt string              `json:"updated_at,omitempty"`
	Facts     *ir.HostFacts       `json:"facts,omitempty"`
	Resources map[string]Resource `json:"resources"`
}

type Resource struct {
	Host            string            `json:"host,omitempty"`
	Kind            string            `json:"kind"`
	ProviderType    string            `json:"provider_type,omitempty"`
	ProviderAddress string            `json:"provider_address,omitempty"`
	Ownership       string            `json:"ownership"`
	Lifecycle       *ir.LifecycleSpec `json:"lifecycle,omitempty"`
	Desired         map[string]any    `json:"desired,omitempty"`
	DesiredDigest   string            `json:"desired_digest"`
	Observed        map[string]any    `json:"observed,omitempty"`
	UpdatedAt       string            `json:"updated_at,omitempty"`
	Order           int               `json:"order"`
}

func Empty(host string) State {
	return State{
		Version:   Version,
		Host:      host,
		Resources: map[string]Resource{},
	}
}

func Normalize(st *State, host string) {
	if st.Version == 0 {
		st.Version = Version
	}
	if st.Host == "" {
		st.Host = host
	}
	if st.Resources == nil {
		st.Resources = map[string]Resource{}
	}
}

func Decode(data []byte, host string) (State, error) {
	if len(data) == 0 {
		return Empty(host), nil
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, err
	}
	Normalize(&st, host)
	return st, nil
}

func Encode(st State) ([]byte, error) {
	Normalize(&st, st.Host)
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	st.Serial++
	return json.MarshalIndent(st, "", "  ")
}

func DesiredDigest(desired map[string]any) string {
	return Digest(SanitizeDesired(desired))
}

func Digest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(fmt.Sprintf("%#v", value))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func SanitizeDesired(desired map[string]any) map[string]any {
	out := cloneMap(desired)
	sensitive, _ := out["sensitive"].(bool)
	if content, ok := out["content"].(string); ok {
		delete(out, "content")
		sum := sha256.Sum256([]byte(content))
		out["content_sha256"] = hex.EncodeToString(sum[:])
		out["content_bytes"] = len([]byte(content))
	}
	if sensitive {
		delete(out, "source_path")
		delete(out, "summary")
	}
	return out
}

func SanitizeObserved(observed map[string]any) map[string]any {
	return SanitizeDesired(observed)
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
	case nil:
		return nil
	default:
		return v
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(in))
	for _, key := range keys {
		out[key] = cloneValue(in[key])
	}
	return out
}
