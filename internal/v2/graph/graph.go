package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func (n Node) MarshalJSON() ([]byte, error) {
	type nodeJSON Node
	out := nodeJSON(n)
	if nodeContentWriteOnly(n) {
		out.ProviderPayload = nil
	}
	return json.Marshal(out)
}

func nodeContentWriteOnly(n Node) bool {
	value, _ := n.Desired["content_write_only"].(bool)
	return value
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
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

func compileHost(host ir.HostSpec) ([]Node, []Operation, error) {
	nodes := []Node{}
	operations := []Operation{}
	moduleAddresses := map[string]string{}
	packageAddresses := map[string]string{}
	repositoryAddresses := map[string]string{}
	repositoryTriggers := []string{}
	aptCacheSource := host.APT.Source
	groupAddresses := map[string]string{}
	userAddresses := map[string]string{}
	unitAddresses := map[string]string{}
	componentArtifactInstallAddresses := map[string]string{}

	for _, name := range sortedKeys(host.Groups.Groups) {
		groupAddresses[name] = fmt.Sprintf("host.%s.groups.group[%s]", host.Name, strconv.Quote(name))
	}
	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Groups.Groups) {
			groupAddresses[name] = fmt.Sprintf("%s.groups.group[%s]", componentPrefix, strconv.Quote(name))
		}
	}
	for _, name := range sortedKeys(host.Users.Users) {
		userAddresses[name] = fmt.Sprintf("host.%s.users.user[%s]", host.Name, strconv.Quote(name))
	}
	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Users.Users) {
			userAddresses[name] = fmt.Sprintf("%s.users.user[%s]", componentPrefix, strconv.Quote(name))
		}
	}
	for _, name := range sortedKeys(host.Systemd.Units) {
		unitAddresses[name] = fmt.Sprintf("host.%s.systemd.unit[%s]", host.Name, strconv.Quote(name))
	}
	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Systemd.Units) {
			unitAddresses[name] = fmt.Sprintf("%s.systemd.unit[%s]", componentPrefix, strconv.Quote(name))
		}
	}
	networkdAddresses := map[string]string{}
	if host.Systemd.Networkd != nil {
		for _, label := range sortedKeys(host.Systemd.Networkd.NetDevs) {
			networkdAddresses[host.Systemd.Networkd.NetDevs[label].Path] = fmt.Sprintf("host.%s.systemd.networkd.netdev[%s]", host.Name, strconv.Quote(label))
		}
		for _, label := range sortedKeys(host.Systemd.Networkd.Networks) {
			networkdAddresses[host.Systemd.Networkd.Networks[label].Path] = fmt.Sprintf("host.%s.systemd.networkd.network[%s]", host.Name, strconv.Quote(label))
		}
	}
	for _, component := range host.Components {
		if component.Systemd.Networkd == nil {
			continue
		}
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, label := range sortedKeys(component.Systemd.Networkd.NetDevs) {
			networkdAddresses[component.Systemd.Networkd.NetDevs[label].Path] = fmt.Sprintf("%s.systemd.networkd.netdev[%s]", componentPrefix, strconv.Quote(label))
		}
		for _, label := range sortedKeys(component.Systemd.Networkd.Networks) {
			networkdAddresses[component.Systemd.Networkd.Networks[label].Path] = fmt.Sprintf("%s.systemd.networkd.network[%s]", componentPrefix, strconv.Quote(label))
		}
	}

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

	for _, name := range sortedKeys(host.APT.Repositories) {
		item := host.APT.Repositories[name]
		sourceAddress := fmt.Sprintf("host.%s.apt.repository[%s]", host.Name, strconv.Quote(name))
		if aptCacheSource.File == "" {
			aptCacheSource = item.Source
		}
		repositoryAddresses[name] = sourceAddress
		sourcePath := aptRepositorySourcePath(name)
		sourceDesired := map[string]any{
			"name":   item.Name,
			"path":   sourcePath,
			"owner":  "root",
			"group":  "root",
			"mode":   "0644",
			"ensure": item.Ensure,
		}
		var deps []string
		if item.Ensure != "absent" {
			sourceDesired["content"] = aptRepositorySourceContent(item)
		}
		if item.SigningKey != nil {
			keyAddress := fmt.Sprintf("host.%s.apt.signing_key[%s]", host.Name, strconv.Quote(name))
			keyDesired := map[string]any{
				"name":   item.Name,
				"path":   item.SigningKey.Path,
				"owner":  "root",
				"group":  "root",
				"mode":   "0644",
				"ensure": item.Ensure,
			}
			if item.SigningKey.URL != "" {
				keyDesired["url"] = item.SigningKey.URL
			}
			if item.SigningKey.Content != "" {
				keyDesired["content"] = item.SigningKey.Content
			}
			if item.SigningKey.SHA256 != "" {
				keyDesired["sha256"] = item.SigningKey.SHA256
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         keyAddress,
				Kind:            "apt_signing_key",
				Summary:         "manage apt signing key " + item.Name,
				Source:          item.SigningKey.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         keyDesired,
				ProviderType:    "apt_signing_key",
				ProviderAddress: "apt_signing_key." + providerName(host.Name, item.Name),
				ProviderPayload: keyDesired,
			})
			deps = append(deps, keyAddress)
			repositoryTriggers = append(repositoryTriggers, keyAddress)
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         sourceAddress,
			Kind:            "file",
			Summary:         "manage apt repository " + item.Name,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         sourceDesired,
			DependsOn:       deps,
			ProviderType:    "file",
			ProviderAddress: "file." + providerName(host.Name, sourcePath),
			ProviderPayload: sourceDesired,
		})
		repositoryTriggers = append(repositoryTriggers, sourceAddress)
	}
	for _, label := range sortedKeys(host.APT.SourceFiles) {
		item := host.APT.SourceFiles[label]
		address := fmt.Sprintf("host.%s.apt.source_file[%s]", host.Name, strconv.Quote(label))
		if aptCacheSource.File == "" {
			aptCacheSource = item.Source
		}
		desired := map[string]any{
			"label":      item.Label,
			"path":       item.Path,
			"owner":      item.Owner,
			"group":      item.Group,
			"mode":       item.Mode,
			"ensure":     item.Ensure,
			"on_destroy": item.OnDestroy,
		}
		if item.Ensure != "absent" {
			if item.Content != "" {
				desired["content"] = item.Content
			}
			if item.SourcePath != "" {
				desired["source_path"] = item.SourcePath
			}
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "apt_source_file",
			Summary:         "manage apt source file " + item.Label,
			Source:          item.Source,
			Lifecycle:       lifecyclePtr(item.Lifecycle),
			Desired:         desired,
			ProviderType:    "apt_source_file",
			ProviderAddress: "apt_source_file." + providerName(host.Name, item.Label),
			ProviderPayload: desired,
		})
		repositoryTriggers = append(repositoryTriggers, address)
	}

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.APT.Repositories) {
			item := component.APT.Repositories[name]
			sourceAddress := fmt.Sprintf("%s.apt.repository[%s]", componentPrefix, strconv.Quote(name))
			if aptCacheSource.File == "" {
				aptCacheSource = item.Source
			}
			repositoryAddresses[name] = sourceAddress
			sourcePath := aptRepositorySourcePath(name)
			sourceDesired := map[string]any{
				"name":      item.Name,
				"component": component.Name,
				"path":      sourcePath,
				"owner":     "root",
				"group":     "root",
				"mode":      "0644",
				"ensure":    item.Ensure,
			}
			var deps []string
			if item.Ensure != "absent" {
				sourceDesired["content"] = aptRepositorySourceContent(item)
			}
			if item.SigningKey != nil {
				keyAddress := fmt.Sprintf("%s.apt.signing_key[%s]", componentPrefix, strconv.Quote(name))
				keyDesired := map[string]any{
					"name":      item.Name,
					"component": component.Name,
					"path":      item.SigningKey.Path,
					"owner":     "root",
					"group":     "root",
					"mode":      "0644",
					"ensure":    item.Ensure,
				}
				if item.SigningKey.URL != "" {
					keyDesired["url"] = item.SigningKey.URL
				}
				if item.SigningKey.Content != "" {
					keyDesired["content"] = item.SigningKey.Content
				}
				if item.SigningKey.SHA256 != "" {
					keyDesired["sha256"] = item.SigningKey.SHA256
				}
				nodes = append(nodes, Node{
					Host:            host.Name,
					Address:         keyAddress,
					Kind:            "apt_signing_key",
					Summary:         "manage apt signing key " + item.Name,
					Source:          item.SigningKey.Source,
					Lifecycle:       lifecyclePtr(item.Lifecycle),
					Desired:         keyDesired,
					ProviderType:    "apt_signing_key",
					ProviderAddress: "apt_signing_key." + providerName(host.Name, component.Name, item.Name),
					ProviderPayload: keyDesired,
				})
				deps = append(deps, keyAddress)
				repositoryTriggers = append(repositoryTriggers, keyAddress)
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         sourceAddress,
				Kind:            "file",
				Summary:         "manage apt repository " + item.Name,
				Source:          item.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         sourceDesired,
				DependsOn:       deps,
				ProviderType:    "file",
				ProviderAddress: "file." + providerName(host.Name, sourcePath),
				ProviderPayload: sourceDesired,
			})
			repositoryTriggers = append(repositoryTriggers, sourceAddress)
		}
		for _, label := range sortedKeys(component.APT.SourceFiles) {
			item := component.APT.SourceFiles[label]
			address := fmt.Sprintf("%s.apt.source_file[%s]", componentPrefix, strconv.Quote(label))
			if aptCacheSource.File == "" {
				aptCacheSource = item.Source
			}
			desired := map[string]any{
				"label":      item.Label,
				"component":  component.Name,
				"path":       item.Path,
				"owner":      item.Owner,
				"group":      item.Group,
				"mode":       item.Mode,
				"ensure":     item.Ensure,
				"on_destroy": item.OnDestroy,
			}
			if item.Ensure != "absent" {
				if item.Content != "" {
					desired["content"] = item.Content
				}
				if item.SourcePath != "" {
					desired["source_path"] = item.SourcePath
				}
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "apt_source_file",
				Summary:         "manage apt source file " + item.Label,
				Source:          item.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         desired,
				ProviderType:    "apt_source_file",
				ProviderAddress: "apt_source_file." + providerName(host.Name, component.Name, item.Label),
				ProviderPayload: desired,
			})
			repositoryTriggers = append(repositoryTriggers, address)
		}
	}

	aptCacheRefreshAddress := ""
	if len(repositoryTriggers) > 0 {
		aptCacheRefreshAddress = "host." + host.Name + ".apt.cache_refresh"
		operations = append(operations, Operation{
			Address:        aptCacheRefreshAddress,
			Action:         "run",
			Summary:        "refresh apt package cache",
			DependsOn:      append([]string(nil), repositoryTriggers...),
			TriggeredBy:    append([]string(nil), repositoryTriggers...),
			CommandPreview: "apt-get update",
			Source:         aptCacheSource,
		})
	}

	for _, item := range host.Packages.Install {
		address := fmt.Sprintf("host.%s.packages.install[%s]", host.Name, strconv.Quote(item.Name))
		packageAddresses[item.Name] = address
		deps := []string{}
		for _, repository := range item.Repositories {
			if repositoryAddress, ok := repositoryAddresses[repository]; ok {
				deps = append(deps, repositoryAddress)
			}
		}
		if len(deps) > 0 && aptCacheRefreshAddress != "" {
			deps = append(deps, aptCacheRefreshAddress)
		}
		deps = dedupeStrings(deps)
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
			DependsOn:       deps,
			ProviderType:    "package",
			ProviderAddress: "package." + providerName(host.Name, item.Name),
			ProviderPayload: desired,
		})
	}

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, item := range component.Packages.Install {
			address := fmt.Sprintf("%s.packages.install[%s]", componentPrefix, strconv.Quote(item.Name))
			packageAddresses[item.Name] = address
			deps := []string{}
			for _, repository := range item.Repositories {
				if repositoryAddress, ok := repositoryAddresses[repository]; ok {
					deps = append(deps, repositoryAddress)
				}
			}
			if len(deps) > 0 && aptCacheRefreshAddress != "" {
				deps = append(deps, aptCacheRefreshAddress)
			}
			deps = dedupeStrings(deps)
			desired := map[string]any{
				"name":      item.Name,
				"component": component.Name,
				"ensure":    "present",
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
				DependsOn:       deps,
				ProviderType:    "package",
				ProviderAddress: "package." + providerName(host.Name, component.Name, item.Name),
				ProviderPayload: desired,
			})
		}
	}

	nftablesTriggers := []string{}
	nftablesSource := host.Nftables.Source
	nftablesMainPath := "/etc/nftables.conf"
	nftablesValidate := false
	nftablesActivate := false
	nftablesFileDeps := []string{}
	if packageAddress, ok := packageAddresses["nftables"]; ok {
		nftablesFileDeps = append(nftablesFileDeps, packageAddress)
	}
	if host.Nftables.Main != nil {
		item := *host.Nftables.Main
		nftablesMainPath = item.Path
		nftablesValidate = nftablesValidate || (item.Ensure != "absent" && item.Validate)
		nftablesActivate = nftablesActivate || (item.Ensure != "absent" && item.Activate)
		if nftablesSource.File == "" {
			nftablesSource = item.Source
		}
		address := fmt.Sprintf("host.%s.nftables.file[%s]", host.Name, strconv.Quote(item.Label))
		nodes = append(nodes, nftablesFileNode(host.Name, address, item, nftablesFileDeps))
		nftablesTriggers = append(nftablesTriggers, address)
	}
	for _, label := range sortedKeys(host.Nftables.Files) {
		item := host.Nftables.Files[label]
		nftablesValidate = nftablesValidate || (item.Ensure != "absent" && item.Validate)
		nftablesActivate = nftablesActivate || (item.Ensure != "absent" && item.Activate)
		if nftablesSource.File == "" {
			nftablesSource = item.Source
		}
		address := fmt.Sprintf("host.%s.nftables.file[%s]", host.Name, strconv.Quote(item.Label))
		nodes = append(nodes, nftablesFileNode(host.Name, address, item, nftablesFileDeps))
		nftablesTriggers = append(nftablesTriggers, address)
	}
	nftablesActivateAddress := ""
	if len(nftablesTriggers) > 0 && nftablesValidate {
		validateAddress := "host." + host.Name + ".nftables.validate"
		operations = append(operations, Operation{
			Address:        validateAddress,
			Action:         "run",
			Summary:        "validate nftables ruleset",
			DependsOn:      append([]string(nil), nftablesTriggers...),
			TriggeredBy:    append([]string(nil), nftablesTriggers...),
			CommandPreview: "nft -c -f " + nftablesMainPath,
			Source:         nftablesSource,
		})
		if nftablesActivate {
			nftablesActivateAddress = "host." + host.Name + ".nftables.activate"
			operations = append(operations, Operation{
				Address:        nftablesActivateAddress,
				Action:         "run",
				Summary:        "activate nftables ruleset",
				DependsOn:      []string{validateAddress},
				TriggeredBy:    append([]string(nil), nftablesTriggers...),
				CommandPreview: "nft -f " + nftablesMainPath,
				Source:         nftablesSource,
			})
		}
	} else if len(nftablesTriggers) > 0 && nftablesActivate {
		nftablesActivateAddress = "host." + host.Name + ".nftables.activate"
		operations = append(operations, Operation{
			Address:        nftablesActivateAddress,
			Action:         "run",
			Summary:        "activate nftables ruleset",
			DependsOn:      append([]string(nil), nftablesTriggers...),
			TriggeredBy:    append([]string(nil), nftablesTriggers...),
			CommandPreview: "nft -f " + nftablesMainPath,
			Source:         nftablesSource,
		})
	}
	if host.Nftables.Enable != nil {
		address := "host." + host.Name + ".nftables.enable"
		deps := []string{}
		if packageAddress, ok := packageAddresses["nftables"]; ok {
			deps = append(deps, packageAddress)
		}
		if *host.Nftables.Enable && nftablesActivateAddress != "" {
			deps = append(deps, nftablesActivateAddress)
		}
		state := "stopped"
		if *host.Nftables.Enable {
			state = "running"
		}
		desired := map[string]any{
			"name":    "nftables",
			"unit":    "nftables.service",
			"enabled": *host.Nftables.Enable,
			"state":   state,
		}
		summary := "disable nftables service"
		if *host.Nftables.Enable {
			summary = "enable nftables service"
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "service",
			Summary:         summary,
			Source:          host.Nftables.Source,
			Desired:         desired,
			DependsOn:       dedupeStrings(deps),
			ProviderType:    "service",
			ProviderAddress: "service." + providerName(host.Name, "nftables"),
			ProviderPayload: desired,
		})
	}

	caCertificateTriggers := []string{}
	caCertificateSource := ir.SourceRef{}
	for _, component := range host.Components {
		if component.ArtifactType == "" || component.SelectedSource == nil || component.Install == nil {
			continue
		}
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		sourceLabel := componentArtifactSourceLabel(*component.SelectedSource)
		cachePath := componentArtifactCachePath(component)
		downloadAddress := fmt.Sprintf("%s.artifact.download[%s]", componentPrefix, strconv.Quote(sourceLabel))
		downloadDesired := map[string]any{
			"component":    component.Name,
			"type":         component.ArtifactType,
			"version":      component.Version,
			"architecture": component.SelectedSource.Architecture,
			"url":          component.SelectedSource.URL,
			"sha256":       component.SelectedSource.SHA256,
			"path":         cachePath,
			"owner":        "root",
			"group":        "root",
			"mode":         "0644",
			"ensure":       "present",
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         downloadAddress,
			Kind:            "component_download",
			Summary:         "download component " + component.Name + " source",
			Source:          component.SelectedSource.Source,
			Desired:         downloadDesired,
			ProviderType:    "component_download",
			ProviderAddress: "component_download." + providerName(host.Name, component.Name, sourceLabel),
			ProviderPayload: downloadDesired,
		})

		buildAddress := ""
		buildOutputPath := ""
		if component.ArtifactType == "source" && component.Build != nil {
			buildOutputPath = componentArtifactBuildOutputPath(component)
			buildAddress = fmt.Sprintf("%s.artifact.build[%s]", componentPrefix, strconv.Quote(buildOutputPath))
			buildDeps := []string{downloadAddress}
			for _, packageName := range component.Build.Packages {
				packageAddress := fmt.Sprintf("%s.build.package[%s]", componentPrefix, strconv.Quote(packageName))
				packageDesired := map[string]any{
					"name":      packageName,
					"component": component.Name,
					"scope":     "build",
					"ensure":    "present",
				}
				nodes = append(nodes, Node{
					Host:            host.Name,
					Address:         packageAddress,
					Kind:            "package",
					Summary:         "install build package " + packageName,
					Source:          component.Build.Source,
					Desired:         packageDesired,
					ProviderType:    "package",
					ProviderAddress: "package." + providerName(host.Name, component.Name, "build", packageName),
					ProviderPayload: packageDesired,
				})
				buildDeps = append(buildDeps, packageAddress)
			}
			buildDesired := map[string]any{
				"component":     component.Name,
				"type":          component.ArtifactType,
				"version":       component.Version,
				"architecture":  component.SelectedSource.Architecture,
				"source_url":    component.SelectedSource.URL,
				"source_sha256": component.SelectedSource.SHA256,
				"cache_path":    cachePath,
				"build_path":    componentArtifactBuildPath(component),
				"output_path":   buildOutputPath,
				"commands":      cloneCommandMatrix(component.Build.Commands),
				"packages":      append([]string(nil), component.Build.Packages...),
				"working_dir":   component.Build.WorkingDir,
				"output":        component.Build.Output,
				"owner":         "root",
				"group":         "root",
				"mode":          "0644",
				"ensure":        "present",
			}
			if component.Build.SourceName != "" {
				buildDesired["source_name"] = component.Build.SourceName
			}
			if component.Extract != nil {
				buildDesired["extract_format"] = component.Extract.Format
				buildDesired["strip_components"] = component.Extract.StripComponents
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         buildAddress,
				Kind:            "component_build",
				Summary:         "build component " + component.Name + " from source",
				Source:          component.Build.Source,
				Desired:         buildDesired,
				DependsOn:       dedupeStrings(buildDeps),
				ProviderType:    "component_build",
				ProviderAddress: "component_build." + providerName(host.Name, component.Name),
				ProviderPayload: buildDesired,
			})
		}

		installAddress := fmt.Sprintf("%s.artifact.install[%s]", componentPrefix, strconv.Quote(component.Install.Path))
		installKind := componentArtifactInstallKind(component.ArtifactType)
		installCachePath := cachePath
		if buildOutputPath != "" {
			installCachePath = buildOutputPath
		}
		installDesired := map[string]any{
			"component":     component.Name,
			"type":          component.ArtifactType,
			"version":       component.Version,
			"architecture":  component.SelectedSource.Architecture,
			"source_url":    component.SelectedSource.URL,
			"source_sha256": component.SelectedSource.SHA256,
			"cache_path":    installCachePath,
			"path":          component.Install.Path,
			"owner":         component.Install.Owner,
			"group":         component.Install.Group,
			"mode":          component.Install.Mode,
			"ensure":        "present",
		}
		source := component.Install.Source
		if component.Extract != nil {
			installDesired["extract_format"] = component.Extract.Format
			installDesired["strip_components"] = component.Extract.StripComponents
			installDesired["include"] = component.Extract.Include
			source = component.Extract.Source
		}
		deps := []string{downloadAddress}
		if buildAddress != "" {
			deps = []string{buildAddress}
			source = component.Build.Source
			installDesired["build_path"] = componentArtifactBuildPath(component)
			installDesired["build_output"] = component.Build.Output
			delete(installDesired, "extract_format")
			delete(installDesired, "strip_components")
			delete(installDesired, "include")
		}
		deps = append(deps, ownershipDependencies(component.Install.Owner, component.Install.Group, userAddresses, groupAddresses)...)
		deps = dedupeStrings(deps)
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         installAddress,
			Kind:            installKind,
			Summary:         componentArtifactInstallSummary(component),
			Source:          source,
			Desired:         installDesired,
			DependsOn:       deps,
			ProviderType:    installKind,
			ProviderAddress: installKind + "." + providerName(host.Name, component.Name, component.Install.Path),
			ProviderPayload: installDesired,
		})
		componentArtifactInstallAddresses[component.Name] = installAddress
		if component.ArtifactType == "ca_certificate" {
			caCertificateTriggers = append(caCertificateTriggers, installAddress)
			if caCertificateSource.File == "" {
				caCertificateSource = source
			}
		}
	}

	if len(caCertificateTriggers) > 0 {
		operations = append(operations, Operation{
			Address:        "host." + host.Name + ".ca_certificates.update",
			Action:         "run",
			Summary:        "update ca certificates",
			DependsOn:      append([]string(nil), caCertificateTriggers...),
			TriggeredBy:    append([]string(nil), caCertificateTriggers...),
			CommandPreview: "update-ca-certificates",
			Source:         caCertificateSource,
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

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, path := range sortedKeys(component.Directories.Directories) {
			item := component.Directories.Directories[path]
			address := fmt.Sprintf("%s.directories.directory[%s]", componentPrefix, strconv.Quote(path))
			desired := map[string]any{"path": item.Path, "component": component.Name, "owner": item.Owner, "group": item.Group, "mode": item.Mode, "ensure": item.Ensure}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "directory",
				Summary:         "create directory " + item.Path,
				Source:          item.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         desired,
				DependsOn:       ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses),
				ProviderType:    "directory",
				ProviderAddress: "directory." + providerName(host.Name, component.Name, item.Path),
				ProviderPayload: desired,
			})
		}
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
		}
		if !item.ContentWriteOnly {
			desired["summary"] = item.Summary
		}
		if item.ContentWriteOnly {
			desired["content_write_only"] = true
		}
		if item.ContentVersion != "" {
			desired["content_version"] = item.ContentVersion
		}
		if item.Content != "" && !item.Sensitive {
			desired["content"] = item.Content
		}
		if item.Content != "" && item.Sensitive && !item.ContentWriteOnly {
			desired["content_sha256"] = item.Summary.SHA256
			desired["content_bytes"] = item.Summary.Bytes
		}
		if item.SourcePath != "" {
			desired["source_path"] = item.SourcePath
		}
		payload := cloneMap(desired)
		if item.Content != "" {
			payload["content"] = item.Content
		}
		if item.SourcePath != "" {
			payload["source_path"] = item.SourcePath
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
			ProviderPayload: payload,
		})
	}

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, path := range sortedKeys(component.Files.Files) {
			item := component.Files.Files[path]
			address := fmt.Sprintf("%s.files.file[%s]", componentPrefix, strconv.Quote(path))
			desired := map[string]any{
				"path":      item.Path,
				"component": component.Name,
				"owner":     item.Owner,
				"group":     item.Group,
				"mode":      item.Mode,
				"ensure":    item.Ensure,
				"sensitive": item.Sensitive,
			}
			if !item.ContentWriteOnly {
				desired["summary"] = item.Summary
			}
			if item.ContentWriteOnly {
				desired["content_write_only"] = true
			}
			if item.ContentVersion != "" {
				desired["content_version"] = item.ContentVersion
			}
			if item.Content != "" && !item.Sensitive {
				desired["content"] = item.Content
			}
			if item.Content != "" && item.Sensitive && !item.ContentWriteOnly {
				desired["content_sha256"] = item.Summary.SHA256
				desired["content_bytes"] = item.Summary.Bytes
			}
			if item.SourcePath != "" {
				desired["source_path"] = item.SourcePath
			}
			payload := cloneMap(desired)
			if item.Content != "" {
				payload["content"] = item.Content
			}
			if item.SourcePath != "" {
				payload["source_path"] = item.SourcePath
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "file",
				Summary:         "create file " + item.Path,
				Source:          item.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         desired,
				DependsOn:       ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses),
				ProviderType:    "file",
				ProviderAddress: "file." + providerName(host.Name, component.Name, item.Path),
				ProviderPayload: payload,
			})
		}
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

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, path := range sortedKeys(component.Secrets.Files) {
			item := component.Secrets.Files[path]
			address := fmt.Sprintf("%s.secrets.file[%s]", componentPrefix, strconv.Quote(path))
			desired := map[string]any{
				"path":        item.Path,
				"component":   component.Name,
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
				DependsOn:       ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses),
				ProviderType:    "file",
				ProviderAddress: "file." + providerName(host.Name, component.Name, item.Path),
				ProviderPayload: desired,
			})
		}
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

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Groups.Groups) {
			item := component.Groups.Groups[name]
			address := fmt.Sprintf("%s.groups.group[%s]", componentPrefix, strconv.Quote(name))
			groupAddresses[name] = address
			desired := map[string]any{"name": item.Name, "component": component.Name, "gid": item.GID, "system": item.System, "ensure": item.Ensure}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "group",
				Summary:         "create group " + item.Name,
				Source:          item.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         desired,
				ProviderType:    "group",
				ProviderAddress: "group." + providerName(host.Name, component.Name, item.Name),
				ProviderPayload: desired,
			})
		}
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

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Users.Users) {
			item := component.Users.Users[name]
			address := fmt.Sprintf("%s.users.user[%s]", componentPrefix, strconv.Quote(name))
			userAddresses[name] = address
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
				"component":           component.Name,
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
				ProviderAddress: "user." + providerName(host.Name, component.Name, item.Name),
				ProviderPayload: desired,
			})
			for _, key := range item.SSHAuthorizedKeys {
				keyID := shortHash(key)
				keyAddress := fmt.Sprintf("%s.users.user[%s].ssh_authorized_key[%s]", componentPrefix, strconv.Quote(name), strconv.Quote(keyID))
				keyDesired := map[string]any{"user": item.Name, "component": component.Name, "key": key, "ensure": item.Ensure}
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
					ProviderAddress: "ssh_authorized_key." + providerName(host.Name, component.Name, item.Name, keyID),
					ProviderPayload: keyDesired,
				})
			}
		}
	}

	unitTriggers := []string{}
	networkdTriggers := []string{}
	networkdDeleteNames := []string{}
	networkdSource := ir.SourceRef{}
	var networkdEnable *bool
	networkdEnableSource := ir.SourceRef{}
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
			ProviderPayload: payload,
		})
	}

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Systemd.Units) {
			item := component.Systemd.Units[name]
			address := fmt.Sprintf("%s.systemd.unit[%s]", componentPrefix, strconv.Quote(name))
			unitAddresses[name] = address
			unitTriggers = append(unitTriggers, address)
			desired := map[string]any{
				"name":        item.Name,
				"component":   component.Name,
				"path":        item.Path,
				"owner":       item.Owner,
				"group":       item.Group,
				"mode":        item.Mode,
				"ensure":      item.Ensure,
				"summary":     item.Summary,
				"source_path": item.SourcePath,
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
			deps := ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses)
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			deps = dedupeStrings(deps)
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "systemd_unit",
				Summary:         "create systemd unit " + item.Name,
				Source:          item.Source,
				Lifecycle:       lifecyclePtr(item.Lifecycle),
				Desired:         desired,
				DependsOn:       deps,
				ProviderType:    "systemd_unit",
				ProviderAddress: "systemd_unit." + providerName(host.Name, component.Name, item.Name),
				ProviderPayload: payload,
			})
		}
	}

	if host.Systemd.Networkd != nil {
		if networkdSource.File == "" {
			networkdSource = host.Systemd.Networkd.Source
		}
		if host.Systemd.Networkd.Enable != nil {
			value := *host.Systemd.Networkd.Enable
			networkdEnable = &value
			networkdEnableSource = host.Systemd.Networkd.Source
		}
		for _, label := range sortedKeys(host.Systemd.Networkd.NetDevs) {
			item := host.Systemd.Networkd.NetDevs[label]
			address := fmt.Sprintf("host.%s.systemd.networkd.netdev[%s]", host.Name, strconv.Quote(label))
			networkdTriggers = append(networkdTriggers, address)
			if item.Ensure == "absent" {
				networkdDeleteNames = append(networkdDeleteNames, networkdNetDevName(item))
			}
			nodes = append(nodes, networkdNetDevNode(host.Name, address, "", item, nil))
		}
		for _, label := range sortedKeys(host.Systemd.Networkd.Networks) {
			item := host.Systemd.Networkd.Networks[label]
			address := fmt.Sprintf("host.%s.systemd.networkd.network[%s]", host.Name, strconv.Quote(label))
			networkdTriggers = append(networkdTriggers, address)
			deps := networkdNetworkDependencies(item, networkdAddresses)
			nodes = append(nodes, networkdNetworkNode(host.Name, address, "", item, deps))
		}
	}

	for _, component := range host.Components {
		if component.Systemd.Networkd == nil {
			continue
		}
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		if networkdSource.File == "" {
			networkdSource = component.Systemd.Networkd.Source
		}
		if component.Systemd.Networkd.Enable != nil {
			value := *component.Systemd.Networkd.Enable
			networkdEnable = &value
			networkdEnableSource = component.Systemd.Networkd.Source
		}
		for _, label := range sortedKeys(component.Systemd.Networkd.NetDevs) {
			item := component.Systemd.Networkd.NetDevs[label]
			address := fmt.Sprintf("%s.systemd.networkd.netdev[%s]", componentPrefix, strconv.Quote(label))
			networkdTriggers = append(networkdTriggers, address)
			if item.Ensure == "absent" {
				networkdDeleteNames = append(networkdDeleteNames, networkdNetDevName(item))
			}
			deps := []string{}
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			deps = append(deps, ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses)...)
			nodes = append(nodes, networkdNetDevNode(host.Name, address, component.Name, item, dedupeStrings(deps)))
		}
		for _, label := range sortedKeys(component.Systemd.Networkd.Networks) {
			item := component.Systemd.Networkd.Networks[label]
			address := fmt.Sprintf("%s.systemd.networkd.network[%s]", componentPrefix, strconv.Quote(label))
			networkdTriggers = append(networkdTriggers, address)
			deps := networkdNetworkDependencies(item, networkdAddresses)
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			deps = append(deps, ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses)...)
			nodes = append(nodes, networkdNetworkNode(host.Name, address, component.Name, item, dedupeStrings(deps)))
		}
	}

	if len(networkdTriggers) > 0 {
		deps := append([]string(nil), networkdTriggers...)
		if networkdEnable != nil && *networkdEnable {
			deps = append(deps, "host."+host.Name+".systemd.networkd.enable")
		}
		operations = append(operations, Operation{
			Address:        "host." + host.Name + ".systemd.networkd.restart",
			Action:         "run",
			Summary:        "reload systemd-networkd",
			DependsOn:      dedupeStrings(deps),
			TriggeredBy:    append([]string(nil), networkdTriggers...),
			CommandPreview: networkdReloadCommand(networkdDeleteNames),
			Source:         networkdSource,
		})
	}

	if networkdEnable != nil {
		address := "host." + host.Name + ".systemd.networkd.enable"
		deps := append([]string(nil), networkdTriggers...)
		state := "stopped"
		if *networkdEnable {
			state = "running"
		}
		desired := map[string]any{
			"name":    "systemd-networkd",
			"unit":    "systemd-networkd.service",
			"enabled": *networkdEnable,
			"state":   state,
		}
		summary := "disable systemd-networkd"
		if *networkdEnable {
			summary = "enable systemd-networkd"
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "service",
			Summary:         summary,
			Source:          networkdEnableSource,
			Desired:         desired,
			DependsOn:       dedupeStrings(deps),
			ProviderType:    "service",
			ProviderAddress: "service." + providerName(host.Name, "systemd-networkd"),
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

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Services.Services) {
			item := component.Services.Services[name]
			address := fmt.Sprintf("%s.services.service[%s]", componentPrefix, strconv.Quote(name))
			deps := []string{}
			if packageAddress, ok := packageAddresses[item.Package]; ok {
				deps = append(deps, packageAddress)
			}
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			if unitAddress, ok := unitAddresses[item.Unit]; ok {
				deps = append(deps, unitAddress)
				if daemonReloadAddress != "" {
					deps = append(deps, daemonReloadAddress)
				}
			}
			deps = dedupeStrings(deps)
			desired := map[string]any{
				"name":      item.Name,
				"component": component.Name,
				"unit":      item.Unit,
				"package":   item.Package,
				"state":     item.State,
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
				ProviderAddress: "service." + providerName(host.Name, component.Name, item.Name),
				ProviderPayload: desired,
			})
			switch item.State {
			case "restarted", "reloaded":
				action := "restart"
				if item.State == "reloaded" {
					action = "reload"
				}
				operations = append(operations, Operation{
					Address:        fmt.Sprintf("%s.services.service[%s].%s", componentPrefix, strconv.Quote(name), action),
					Action:         "run",
					Summary:        action + " service " + item.Name,
					DependsOn:      append([]string{address}, deps...),
					TriggeredBy:    []string{address},
					CommandPreview: "systemctl " + action + " " + item.Unit,
					Source:         item.Source,
				})
			}
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

func aptRepositorySourcePath(name string) string {
	return "/etc/apt/sources.list.d/" + providerName(name) + ".sources"
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

func aptRepositorySourceContent(repo ir.APTRepositorySpec) string {
	lines := []string{
		"Types: deb",
		"URIs: " + strings.Join(repo.URIs, " "),
		"Suites: " + strings.Join(repo.Suites, " "),
		"Components: " + strings.Join(repo.Components, " "),
	}
	if len(repo.Architectures) > 0 {
		lines = append(lines, "Architectures: "+strings.Join(repo.Architectures, " "))
	}
	if repo.SigningKey != nil && repo.SigningKey.Path != "" {
		lines = append(lines, "Signed-By: "+repo.SigningKey.Path)
	}
	return strings.Join(lines, "\n") + "\n"
}

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
	if item.Content != "" {
		desired["content"] = item.Content
	}
	if item.SourcePath != "" {
		desired["source_path"] = item.SourcePath
	}
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
		ProviderPayload: desired,
	}
}

func componentArtifactSourceLabel(source ir.ComponentArtifactSourceSpec) string {
	if source.Architecture != "" {
		return source.Architecture
	}
	return "default"
}

func componentArtifactInstallKind(artifactType string) string {
	switch artifactType {
	case "archive":
		return "component_archive"
	case "file":
		return "component_file"
	case "ca_certificate":
		return "component_ca_certificate"
	case "source":
		return "component_binary"
	default:
		return "component_binary"
	}
}

func componentArtifactInstallSummary(component ir.ComponentInstanceSpec) string {
	artifact := component.ArtifactType
	if artifact == "ca_certificate" {
		artifact = "ca certificate"
	}
	return "install component " + component.Name + " " + artifact + " " + component.Install.Path
}

func componentArtifactCachePath(component ir.ComponentInstanceSpec) string {
	if component.SelectedSource == nil {
		return ""
	}
	name := providerName(component.Name)
	if component.Template != "" && component.Template != component.Name {
		name = providerName(component.Template, component.Name)
	}
	return "/var/cache/debianform/components/" + name + "/" + component.SelectedSource.SHA256 + "/source"
}

func componentArtifactBuildPath(component ir.ComponentInstanceSpec) string {
	if component.SelectedSource == nil {
		return ""
	}
	name := providerName(component.Name)
	if component.Template != "" && component.Template != component.Name {
		name = providerName(component.Template, component.Name)
	}
	return "/var/cache/debianform/components/" + name + "/" + component.SelectedSource.SHA256 + "/build"
}

func componentArtifactBuildOutputPath(component ir.ComponentInstanceSpec) string {
	if component.Build == nil {
		return ""
	}
	return componentArtifactBuildPath(component) + "/out/" + shortHash(component.Build.Output) + "/" + component.Build.Output
}

func cloneCommandMatrix(in [][]string) [][]string {
	if len(in) == 0 {
		return nil
	}
	out := make([][]string, 0, len(in))
	for _, command := range in {
		out = append(out, append([]string(nil), command...))
	}
	return out
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
