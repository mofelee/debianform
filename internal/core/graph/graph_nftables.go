package graph

import (
	"github.com/mofelee/debianform/internal/core/ir"
)

func nftablesFileNode(hostName string, address string, item ir.NftablesFileSpec, deps []string) Node {
	desired := map[string]any{
		"label":     item.Label,
		"path":      item.Path,
		"owner":     item.Owner,
		"group":     item.Group,
		"mode":      item.Mode,
		"ensure":    item.Ensure,
		"sensitive": item.Sensitive,
		"summary":   item.Summary,
	}
	desired, payload := contentResourceDesiredPayload(desired, item.Content, item.SourcePath, item.Sensitive, item.Summary)
	summary := "manage nftables snippet " + item.Label
	if item.Label == "main" {
		summary = "manage nftables main ruleset"
	}
	return Node{
		Host:            hostName,
		Address:         address,
		Kind:            "nftables_file",
		Summary:         summary,
		Source:          item.Source,
		Lifecycle:       lifecyclePtr(item.Lifecycle),
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "nftables_file",
		ProviderAddress: "nftables_file." + providerName(hostName, item.Label),
		ProviderPayload: payload,
	}
}
