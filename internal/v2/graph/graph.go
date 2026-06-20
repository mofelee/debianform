package graph

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/v2/ir"
)

type ResourceGraph struct {
	Nodes []Node `json:"nodes"`
}

type Node struct {
	Address         string         `json:"address"`
	Kind            string         `json:"kind"`
	Summary         string         `json:"summary"`
	Source          ir.SourceRef   `json:"source"`
	Desired         map[string]any `json:"desired,omitempty"`
	ProviderType    string         `json:"provider_type,omitempty"`
	ProviderAddress string         `json:"provider_address,omitempty"`
	ProviderPayload map[string]any `json:"provider_payload,omitempty"`
	DependsOn       []string       `json:"depends_on,omitempty"`
}

func Compile(program *ir.Program) (*ResourceGraph, error) {
	graph := &ResourceGraph{}
	for _, host := range program.Hosts {
		nodes, err := compileHost(host)
		if err != nil {
			return nil, err
		}
		graph.Nodes = append(graph.Nodes, nodes...)
	}
	sort.SliceStable(graph.Nodes, func(i, j int) bool {
		return graph.Nodes[i].Address < graph.Nodes[j].Address
	})
	return graph, nil
}

func compileHost(host ir.HostSpec) ([]Node, error) {
	nodes := []Node{}
	moduleAddresses := map[string]string{}

	for _, module := range host.Kernel.Modules {
		address := fmt.Sprintf("host.%s.kernel.module[%s]", host.Name, strconv.Quote(module.Name))
		moduleAddresses[module.Name] = address
		nodes = append(nodes, Node{
			Address: address,
			Kind:    "kernel_module",
			Summary: "create kernel module " + module.Name,
			Source:  module.Source,
			Desired: map[string]any{
				"name":    module.Name,
				"persist": module.Persist,
				"ensure":  module.Ensure,
			},
			ProviderType:    "debian_kernel_module",
			ProviderAddress: "debian_kernel_module." + providerName(host.Name, module.Name),
			ProviderPayload: map[string]any{
				"name":    module.Name,
				"persist": module.Persist,
				"ensure":  module.Ensure,
			},
		})
	}

	sysctlKeys := make([]string, 0, len(host.Kernel.Sysctl))
	for key := range host.Kernel.Sysctl {
		sysctlKeys = append(sysctlKeys, key)
	}
	sort.Strings(sysctlKeys)
	for _, key := range sysctlKeys {
		sysctl := host.Kernel.Sysctl[key]
		address := fmt.Sprintf("host.%s.kernel.sysctl[%s]", host.Name, strconv.Quote(sysctl.Key))
		deps := []string{}
		if sysctl.Key == "net.ipv4.tcp_congestion_control" && sysctl.Value == "bbr" {
			if moduleAddress, ok := moduleAddresses["tcp_bbr"]; ok {
				deps = append(deps, moduleAddress)
			}
		}
		nodes = append(nodes, Node{
			Address:   address,
			Kind:      "sysctl",
			Summary:   "create sysctl " + sysctl.Key,
			Source:    sysctl.Source,
			DependsOn: deps,
			Desired: map[string]any{
				"key":           sysctl.Key,
				"value":         sysctl.Value,
				"persist":       sysctl.Persist,
				"apply_runtime": sysctl.ApplyRuntime,
			},
			ProviderType:    "debian_sysctl",
			ProviderAddress: "debian_sysctl." + providerName(host.Name, sysctl.Key),
			ProviderPayload: map[string]any{
				"key":           sysctl.Key,
				"value":         sysctl.Value,
				"persist":       sysctl.Persist,
				"apply_runtime": sysctl.ApplyRuntime,
			},
		})
	}

	for _, item := range host.Packages.Install {
		address := fmt.Sprintf("host.%s.packages.install[%s]", host.Name, strconv.Quote(item.Name))
		desired := map[string]any{
			"name":   item.Name,
			"ensure": "present",
		}
		if len(item.Repositories) > 0 {
			desired["repositories"] = append([]string(nil), item.Repositories...)
		}
		nodes = append(nodes, Node{
			Address:         address,
			Kind:            "package",
			Summary:         "create package " + item.Name,
			Source:          item.Source,
			Desired:         desired,
			ProviderType:    "debian_package",
			ProviderAddress: "debian_package." + providerName(host.Name, item.Name),
			ProviderPayload: desired,
		})
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Address < nodes[j].Address
	})
	return nodes, nil
}

var providerNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func providerName(parts ...string) string {
	joined := strings.Join(parts, "_")
	normalized := providerNamePattern.ReplaceAllString(joined, "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		return "resource"
	}
	return strings.ToLower(normalized)
}
