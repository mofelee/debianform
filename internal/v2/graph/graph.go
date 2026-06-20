package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/v2/ir"
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

type Operation struct {
	Address        string       `json:"address"`
	Action         string       `json:"action"`
	Summary        string       `json:"summary"`
	DependsOn      []string     `json:"depends_on,omitempty"`
	TriggeredBy    []string     `json:"triggered_by,omitempty"`
	CommandPreview string       `json:"command_preview,omitempty"`
	Source         ir.SourceRef `json:"source,omitempty"`
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
	return graph, nil
}

func compileHost(host ir.HostSpec) ([]Node, []Operation, error) {
	nodes := []Node{}
	operations := []Operation{}
	moduleAddresses := map[string]string{}
	packageAddresses := map[string]string{}
	groupAddresses := map[string]string{}
	unitAddresses := map[string]string{}

	for _, module := range host.Kernel.Modules {
		address := fmt.Sprintf("host.%s.kernel.module[%s]", host.Name, strconv.Quote(module.Name))
		moduleAddresses[module.Name] = address
		desired := map[string]any{
			"name":    module.Name,
			"persist": module.Persist,
			"ensure":  module.Ensure,
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "kernel_module",
			Summary:         "create kernel module " + module.Name,
			Source:          module.Source,
			Desired:         desired,
			ProviderType:    "kernel_module",
			ProviderAddress: "kernel_module." + providerName(host.Name, module.Name),
			ProviderPayload: desired,
		})
	}

	for _, key := range sortedKeys(host.Kernel.Sysctl) {
		sysctl := host.Kernel.Sysctl[key]
		address := fmt.Sprintf("host.%s.kernel.sysctl[%s]", host.Name, strconv.Quote(sysctl.Key))
		deps := []string{}
		if sysctl.Key == "net.ipv4.tcp_congestion_control" && sysctl.Value == "bbr" {
			if moduleAddress, ok := moduleAddresses["tcp_bbr"]; ok {
				deps = append(deps, moduleAddress)
			}
		}
		desired := map[string]any{
			"key":           sysctl.Key,
			"value":         sysctl.Value,
			"persist":       sysctl.Persist,
			"apply_runtime": sysctl.ApplyRuntime,
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "sysctl",
			Summary:         "create sysctl " + sysctl.Key,
			Source:          sysctl.Source,
			Desired:         desired,
			DependsOn:       deps,
			ProviderType:    "sysctl",
			ProviderAddress: "sysctl." + providerName(host.Name, sysctl.Key),
			ProviderPayload: desired,
		})
	}

	for _, item := range host.Packages.Install {
		address := fmt.Sprintf("host.%s.packages.install[%s]", host.Name, strconv.Quote(item.Name))
		packageAddresses[item.Name] = address
		desired := map[string]any{
			"name":   item.Name,
			"ensure": "present",
		}
		if len(item.Repositories) > 0 {
			desired["repositories"] = append([]string(nil), item.Repositories...)
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "package",
			Summary:         "create package " + item.Name,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			ProviderType:    "package",
			ProviderAddress: "package." + providerName(host.Name, item.Name),
			ProviderPayload: desired,
		})
	}

	for _, path := range sortedKeys(host.Directories.Directories) {
		item := host.Directories.Directories[path]
		address := fmt.Sprintf("host.%s.directories.directory[%s]", host.Name, strconv.Quote(path))
		desired := map[string]any{"path": item.Path, "owner": item.Owner, "group": item.Group, "mode": item.Mode, "ensure": item.Ensure}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "directory",
			Summary:         "create directory " + item.Path,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			ProviderType:    "directory",
			ProviderAddress: "directory." + providerName(host.Name, item.Path),
			ProviderPayload: desired,
		})
	}

	for _, path := range sortedKeys(host.Files.Files) {
		item := host.Files.Files[path]
		address := fmt.Sprintf("host.%s.files.file[%s]", host.Name, strconv.Quote(path))
		desired := map[string]any{
			"path":      item.Path,
			"owner":     item.Owner,
			"group":     item.Group,
			"mode":      item.Mode,
			"ensure":    item.Ensure,
			"sensitive": item.Sensitive,
			"summary":   item.Summary,
		}
		if item.Content != "" && !item.Sensitive {
			desired["content"] = item.Content
		}
		if item.SourcePath != "" {
			desired["source_path"] = item.SourcePath
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "file",
			Summary:         "create file " + item.Path,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			ProviderType:    "file",
			ProviderAddress: "file." + providerName(host.Name, item.Path),
			ProviderPayload: desired,
		})
	}

	for _, path := range sortedKeys(host.Secrets.Files) {
		item := host.Secrets.Files[path]
		address := fmt.Sprintf("host.%s.secrets.file[%s]", host.Name, strconv.Quote(path))
		desired := map[string]any{
			"path":        item.Path,
			"source_path": item.SourcePath,
			"owner":       item.Owner,
			"group":       item.Group,
			"mode":        item.Mode,
			"ensure":      item.Ensure,
			"sensitive":   true,
			"summary":     item.Summary,
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "secret",
			Summary:         "create secret file " + item.Path,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			ProviderType:    "file",
			ProviderAddress: "file." + providerName(host.Name, item.Path),
			ProviderPayload: desired,
		})
	}

	for _, name := range sortedKeys(host.Groups.Groups) {
		item := host.Groups.Groups[name]
		address := fmt.Sprintf("host.%s.groups.group[%s]", host.Name, strconv.Quote(name))
		groupAddresses[name] = address
		desired := map[string]any{"name": item.Name, "gid": item.GID, "system": item.System, "ensure": item.Ensure}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "group",
			Summary:         "create group " + item.Name,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			ProviderType:    "group",
			ProviderAddress: "group." + providerName(host.Name, item.Name),
			ProviderPayload: desired,
		})
	}

	for _, name := range sortedKeys(host.Users.Users) {
		item := host.Users.Users[name]
		address := fmt.Sprintf("host.%s.users.user[%s]", host.Name, strconv.Quote(name))
		deps := []string{}
		if groupAddress, ok := groupAddresses[item.PrimaryGroup]; ok {
			deps = append(deps, groupAddress)
		}
		for _, group := range item.Groups {
			if groupAddress, ok := groupAddresses[group]; ok {
				deps = append(deps, groupAddress)
			}
		}
		deps = dedupeStrings(deps)
		desired := map[string]any{
			"name":                item.Name,
			"uid":                 item.UID,
			"group":               item.PrimaryGroup,
			"groups":              cloneStrings(item.Groups),
			"system":              item.System,
			"home":                item.Home,
			"shell":               item.Shell,
			"ssh_authorized_keys": cloneStrings(item.SSHAuthorizedKeys),
			"ensure":              item.Ensure,
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "user",
			Summary:         "create user " + item.Name,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			DependsOn:       deps,
			ProviderType:    "user",
			ProviderAddress: "user." + providerName(host.Name, item.Name),
			ProviderPayload: desired,
		})
		for _, key := range item.SSHAuthorizedKeys {
			keyID := shortHash(key)
			keyAddress := fmt.Sprintf("host.%s.users.user[%s].ssh_authorized_key[%s]", host.Name, strconv.Quote(name), strconv.Quote(keyID))
			keyDesired := map[string]any{"user": item.Name, "key": key, "ensure": item.Ensure}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         keyAddress,
				Kind:            "ssh_authorized_key",
				Summary:         "create authorized key for " + item.Name,
				Source:          item.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         keyDesired,
				DependsOn:       []string{address},
				ProviderType:    "ssh_authorized_key",
				ProviderAddress: "ssh_authorized_key." + providerName(host.Name, item.Name, keyID),
				ProviderPayload: keyDesired,
			})
		}
	}

	unitTriggers := []string{}
	for _, name := range sortedKeys(host.Systemd.Units) {
		item := host.Systemd.Units[name]
		address := fmt.Sprintf("host.%s.systemd.unit[%s]", host.Name, strconv.Quote(name))
		unitAddresses[name] = address
		unitTriggers = append(unitTriggers, address)
		desired := map[string]any{
			"name":        item.Name,
			"path":        item.Path,
			"owner":       item.Owner,
			"group":       item.Group,
			"mode":        item.Mode,
			"ensure":      item.Ensure,
			"summary":     item.Summary,
			"source_path": item.SourcePath,
		}
		if item.Content != "" {
			desired["content"] = item.Content
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "systemd_unit",
			Summary:         "create systemd unit " + item.Name,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			ProviderType:    "systemd_unit",
			ProviderAddress: "systemd_unit." + providerName(host.Name, item.Name),
			ProviderPayload: desired,
		})
	}

	daemonReloadAddress := ""
	if len(unitTriggers) > 0 {
		daemonReloadAddress = "host." + host.Name + ".systemd.daemon_reload"
		operations = append(operations, Operation{
			Address:        daemonReloadAddress,
			Action:         "run",
			Summary:        "reload systemd manager configuration",
			DependsOn:      append([]string(nil), unitTriggers...),
			TriggeredBy:    append([]string(nil), unitTriggers...),
			CommandPreview: "systemctl daemon-reload",
			Source:         host.Systemd.Source,
		})
	}

	for _, name := range sortedKeys(host.Services.Services) {
		item := host.Services.Services[name]
		address := fmt.Sprintf("host.%s.services.service[%s]", host.Name, strconv.Quote(name))
		deps := []string{}
		if packageAddress, ok := packageAddresses[item.Package]; ok {
			deps = append(deps, packageAddress)
		}
		if unitAddress, ok := unitAddresses[item.Unit]; ok {
			deps = append(deps, unitAddress)
			if daemonReloadAddress != "" {
				deps = append(deps, daemonReloadAddress)
			}
		}
		deps = dedupeStrings(deps)
		desired := map[string]any{
			"name":    item.Name,
			"unit":    item.Unit,
			"package": item.Package,
			"state":   item.State,
		}
		if item.Enabled != nil {
			desired["enabled"] = *item.Enabled
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "service",
			Summary:         "manage service " + item.Name,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			DependsOn:       deps,
			ProviderType:    "service",
			ProviderAddress: "service." + providerName(host.Name, item.Name),
			ProviderPayload: desired,
		})
		switch item.State {
		case "restarted", "reloaded":
			action := "restart"
			if item.State == "reloaded" {
				action = "reload"
			}
			operations = append(operations, Operation{
				Address:        fmt.Sprintf("host.%s.services.service[%s].%s", host.Name, strconv.Quote(name), action),
				Action:         "run",
				Summary:        action + " service " + item.Name,
				DependsOn:      append([]string{address}, deps...),
				TriggeredBy:    []string{address},
				CommandPreview: "systemctl " + action + " " + item.Unit,
				Source:         item.Source,
			})
		}
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Address < nodes[j].Address
	})
	sort.SliceStable(operations, func(i, j int) bool {
		return operations[i].Address < operations[j].Address
	})
	return nodes, operations, nil
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
