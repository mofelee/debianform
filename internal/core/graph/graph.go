package graph

import (
	"encoding/json"
	"sort"

	"github.com/mofelee/debianform/internal/core/ir"
)

type ResourceGraph struct {
	Nodes      []Node      `json:"nodes"`
	Operations []Operation `json:"operations,omitempty"`
}

type Node struct {
	Host            string            `json:"host,omitempty"`
	Address         string            `json:"address"`
	Kind            string            `json:"kind"`
	Summary         string            `json:"summary"`
	Source          ir.SourceRef      `json:"source"`
	Lifecycle       *ir.LifecycleSpec `json:"lifecycle,omitempty"`
	Desired         map[string]any    `json:"desired,omitempty"`
	ProviderType    string            `json:"provider_type,omitempty"`
	ProviderAddress string            `json:"provider_address,omitempty"`
	ProviderPayload map[string]any    `json:"provider_payload,omitempty"`
	DependsOn       []string          `json:"depends_on,omitempty"`
}

func (n Node) MarshalJSON() ([]byte, error) {
	type nodeJSON Node
	out := nodeJSON(n)
	if nodeSensitive(n) {
		out.Desired = cloneMap(n.Desired)
		delete(out.Desired, "content")
	}
	if nodeContentWriteOnly(n) || nodeSensitive(n) {
		out.ProviderPayload = nil
	}
	return json.Marshal(out)
}

func nodeContentWriteOnly(n Node) bool {
	value, _ := n.Desired["content_write_only"].(bool)
	return value
}

func nodeSensitive(n Node) bool {
	value, _ := n.Desired["sensitive"].(bool)
	return value
}

type Operation struct {
	Host           string         `json:"host"`
	Address        string         `json:"address"`
	Action         string         `json:"action"`
	Summary        string         `json:"summary"`
	Sensitive      bool           `json:"sensitive,omitempty"`
	DependsOn      []string       `json:"depends_on,omitempty"`
	TriggeredBy    []string       `json:"triggered_by,omitempty"`
	CommandPreview string         `json:"command_preview,omitempty"`
	ScriptPayload  *ScriptPayload `json:"-"`
	Source         ir.SourceRef   `json:"source,omitempty"`
}

type ScriptPayload struct {
	Name             string
	ComponentName    string
	Mode             string
	Kind             string
	Interpreter      []string
	Outputs          []ScriptOutputPayload
	Run              string
	Content          string
	Commands         [][]string
	TriggerAddresses []string
	TriggerPaths     []string
}

type ScriptOutputPayload struct {
	Address         string
	Path            string
	ScriptDigest    string
	ProviderAddress string
}

func Compile(program *ir.Program) (*ResourceGraph, error) {
	graph := &ResourceGraph{}
	for _, host := range program.Hosts {
		nodes, operations, err := compileHost(host)
		if err != nil {
			return nil, err
		}
		graph.Nodes = append(graph.Nodes, nodes...)
		graph.Operations = append(graph.Operations, operations...)
	}
	sort.SliceStable(graph.Nodes, func(i, j int) bool {
		return graph.Nodes[i].Address < graph.Nodes[j].Address
	})
	sort.SliceStable(graph.Operations, func(i, j int) bool {
		return graph.Operations[i].Address < graph.Operations[j].Address
	})
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}
