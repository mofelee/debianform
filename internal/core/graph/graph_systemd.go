package graph

import (
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

func systemdUnitNode(hostName, address, component string, item ir.SystemdUnit, deps []string) Node {
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
	if component != "" {
		desired["component"] = component
	}
	if item.Sensitive {
		desired["sensitive"] = true
		delete(desired, "source_path")
	}
	if item.Content != "" && !item.Sensitive {
		desired["content"] = item.Content
	}
	if item.Content != "" && item.Sensitive {
		desired["content_sha256"] = item.Summary.SHA256
		desired["content_bytes"] = item.Summary.Bytes
	}
	payload := cloneMap(desired)
	if item.SourcePath != "" {
		payload["source_path"] = item.SourcePath
	}
	if item.Content != "" {
		payload["content"] = item.Content
	}
	providerParts := []string{hostName}
	if component != "" {
		providerParts = append(providerParts, component)
	}
	providerParts = append(providerParts, item.Name)
	return Node{
		Host:            hostName,
		Address:         address,
		Kind:            "systemd_unit",
		Summary:         "create systemd unit " + item.Name,
		Source:          item.Source,
		Lifecycle:       lifecyclePtr(item.Lifecycle),
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "systemd_unit",
		ProviderAddress: "systemd_unit." + providerName(providerParts...),
		ProviderPayload: payload,
	}
}

func systemdDropInFileNode(hostName, address, component string, item ir.SystemdUnit, summary string, deps []string) Node {
	desired, payload := fileResourceDesiredPayload(fileResourceSpec{
		Path:       item.Path,
		Component:  component,
		Content:    item.Content,
		SourcePath: item.SourcePath,
		Owner:      item.Owner,
		Group:      item.Group,
		Mode:       item.Mode,
		Sensitive:  item.Sensitive,
		Ensure:     item.Ensure,
		Summary:    item.Summary,
	})
	return Node{
		Host:            hostName,
		Address:         address,
		Kind:            "file",
		Summary:         summary,
		Source:          item.Source,
		Lifecycle:       lifecyclePtr(item.Lifecycle),
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "file",
		ProviderAddress: "file." + providerName(hostName, item.Path),
		ProviderPayload: payload,
	}
}

func systemdStateServiceNode(hostName, address, component, name, unit string, enabled *bool, state, summary string, source ir.SourceRef, deps []string) (Node, bool) {
	if enabled == nil && state == "" {
		return Node{}, false
	}
	return systemdManagedServiceNode(hostName, address, component, name, unit, "", enabled, state, summary, source, deps), true
}

func systemdDropInShouldRestartOnChange(enabled *bool, state string) bool {
	switch state {
	case "running":
		return true
	case "stopped", "restarted", "reloaded":
		return false
	default:
		return enabled == nil || *enabled
	}
}

func systemdManagedServiceNode(hostName, address, component, name, unit, packageName string, enabled *bool, state, summary string, source ir.SourceRef, deps []string) Node {
	desired := map[string]any{
		"name":  name,
		"unit":  unit,
		"state": state,
	}
	if component != "" {
		desired["component"] = component
	}
	if packageName != "" {
		desired["package"] = packageName
	}
	if enabled != nil {
		desired["enabled"] = *enabled
	}
	providerParts := []string{hostName}
	if component != "" {
		providerParts = append(providerParts, component)
	}
	providerParts = append(providerParts, name)
	return Node{
		Host:            hostName,
		Address:         address,
		Kind:            "service",
		Summary:         summary,
		Source:          source,
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "service",
		ProviderAddress: "service." + providerName(providerParts...),
		ProviderPayload: desired,
	}
}

func networkdNetDevNode(hostName, address, component string, item ir.NetworkdNetDev, deps []string) Node {
	desired := map[string]any{
		"label":   item.Label,
		"path":    item.Path,
		"owner":   item.Owner,
		"group":   item.Group,
		"mode":    item.Mode,
		"ensure":  item.Ensure,
		"summary": item.Summary,
	}
	if component != "" {
		desired["component"] = component
	}
	if item.Content != "" {
		desired["content"] = item.Content
	}
	return Node{
		Host:            hostName,
		Address:         address,
		Kind:            "networkd_netdev",
		Summary:         "manage networkd netdev " + item.Label,
		Source:          item.Source,
		Lifecycle:       lifecyclePtr(item.Lifecycle),
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "file",
		ProviderAddress: "file." + providerName(hostName, item.Path),
		ProviderPayload: desired,
	}
}

func networkdNetworkNode(hostName, address, component string, item ir.NetworkdNetwork, deps []string) Node {
	desired := map[string]any{
		"label":   item.Label,
		"path":    item.Path,
		"owner":   item.Owner,
		"group":   item.Group,
		"mode":    item.Mode,
		"ensure":  item.Ensure,
		"summary": item.Summary,
	}
	if component != "" {
		desired["component"] = component
	}
	if item.Content != "" {
		desired["content"] = item.Content
	}
	return Node{
		Host:            hostName,
		Address:         address,
		Kind:            "networkd_network",
		Summary:         "manage networkd network " + item.Label,
		Source:          item.Source,
		Lifecycle:       lifecyclePtr(item.Lifecycle),
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "file",
		ProviderAddress: "file." + providerName(hostName, item.Path),
		ProviderPayload: desired,
	}
}

func networkdNetworkDependencies(item ir.NetworkdNetwork, networkdAddresses map[string]string) []string {
	var deps []string
	for path, address := range networkdAddresses {
		if strings.HasSuffix(path, ".netdev") && strings.Contains(address, ".networkd.netdev[") {
			deps = append(deps, address)
		}
	}
	return dedupeStrings(deps)
}

func networkdNetDevName(item ir.NetworkdNetDev) string {
	if values := item.NetDev["Name"]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func networkdReloadCommand(deleteNames []string) string {
	lines := []string{"systemctl start systemd-networkd.service", "networkctl reload"}
	for _, name := range dedupeStrings(deleteNames) {
		if name == "" {
			continue
		}
		lines = append(lines, "ip link delete "+shellQuoteGraph(name)+" 2>/dev/null || true")
	}
	return strings.Join(lines, "\n")
}

func shellQuoteGraph(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
