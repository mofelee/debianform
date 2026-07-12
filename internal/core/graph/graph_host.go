package graph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
)

func compileHost(host ir.HostSpec) ([]Node, []Operation, error) {
	nodes := []Node{}
	operations := []Operation{}
	moduleAddresses := map[string]string{}
	packageAddresses := map[string]string{}
	repositoryAddresses := map[string]string{}
	repositoryTriggers := []string{}
	repositorySensitive := false
	aptCacheSource := host.APT.Source
	aptCacheRefreshAddress := ""
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

	if host.System.HostnameSet {
		desired := map[string]any{
			"hostname": host.System.Hostname,
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         "host." + host.Name + ".system.hostname",
			Kind:            "system_hostname",
			Summary:         "manage system hostname " + host.System.Hostname,
			Source:          host.System.Source,
			Desired:         desired,
			ProviderType:    "system_hostname",
			ProviderAddress: "system_hostname." + providerName(host.Name),
			ProviderPayload: desired,
		})
	}
	if host.System.TimezoneSet {
		desired := map[string]any{
			"timezone": host.System.Timezone,
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         "host." + host.Name + ".system.timezone",
			Kind:            "system_timezone",
			Summary:         "manage system timezone " + host.System.Timezone,
			Source:          host.System.Source,
			Desired:         desired,
			ProviderType:    "system_timezone",
			ProviderAddress: "system_timezone." + providerName(host.Name),
			ProviderPayload: desired,
		})
	}
	if host.System.LocaleSet {
		desired := map[string]any{
			"locale": host.System.Locale,
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         "host." + host.Name + ".system.locale",
			Kind:            "system_locale",
			Summary:         "manage system locale " + host.System.Locale,
			Source:          host.System.Source,
			Desired:         desired,
			ProviderType:    "system_locale",
			ProviderAddress: "system_locale." + providerName(host.Name),
			ProviderPayload: desired,
		})
	}

	dockerPackageAddresses := []string{}
	dockerServiceAddress := ""
	dockerDaemonFileAddress := ""
	if host.Docker != nil {
		dockerGraph, err := dockerEngineNodes(host, repositoryAddresses, groupAddresses, userAddresses)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, dockerGraph.Nodes...)
		operations = append(operations, dockerGraph.Operations...)
		repositoryTriggers = append(repositoryTriggers, dockerGraph.RepositoryTriggers...)
		dockerPackageAddresses = dockerGraph.PackageAddresses
		dockerServiceAddress = dockerGraph.ServiceAddress
		dockerDaemonFileAddress = dockerGraph.DaemonFileAddress
		if len(dockerGraph.RepositoryTriggers) > 0 && aptCacheSource.File == "" {
			aptCacheSource = host.Docker.Source
		}
		for _, packageAddress := range dockerGraph.PackageAddresses {
			packageName := packageNameFromDockerPackageAddress(packageAddress)
			if packageName != "" {
				packageAddresses[packageName] = packageAddress
			}
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
			repositorySensitive = repositorySensitive || item.SigningKey.Sensitive
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
			if item.SigningKey.SHA256 != "" {
				keyDesired["sha256"] = item.SigningKey.SHA256
			}
			keyDesired, keyPayload := contentResourceDesiredPayload(
				keyDesired,
				item.SigningKey.Content,
				"",
				item.SigningKey.Sensitive,
				optionalContentSummary(item.SigningKey.Content),
			)
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
				ProviderPayload: keyPayload,
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
		repositorySensitive = repositorySensitive || item.Sensitive
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
		var payload map[string]any
		if item.Ensure != "absent" {
			desired, payload = contentResourceDesiredPayload(desired, item.Content, item.SourcePath, item.Sensitive, item.Summary)
		} else {
			payload = cloneMap(desired)
			if item.Sensitive {
				desired["sensitive"] = true
				payload["sensitive"] = true
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
			ProviderPayload: payload,
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
				repositorySensitive = repositorySensitive || item.SigningKey.Sensitive
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
				if item.SigningKey.SHA256 != "" {
					keyDesired["sha256"] = item.SigningKey.SHA256
				}
				keyDesired, keyPayload := contentResourceDesiredPayload(
					keyDesired,
					item.SigningKey.Content,
					"",
					item.SigningKey.Sensitive,
					optionalContentSummary(item.SigningKey.Content),
				)
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
					ProviderPayload: keyPayload,
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
			repositorySensitive = repositorySensitive || item.Sensitive
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
			var payload map[string]any
			if item.Ensure != "absent" {
				desired, payload = contentResourceDesiredPayload(desired, item.Content, item.SourcePath, item.Sensitive, item.Summary)
			} else {
				payload = cloneMap(desired)
				if item.Sensitive {
					desired["sensitive"] = true
					payload["sensitive"] = true
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
				ProviderPayload: payload,
			})
			repositoryTriggers = append(repositoryTriggers, address)
		}
	}

	if len(repositoryTriggers) > 0 {
		aptCacheRefreshAddress = "host." + host.Name + ".apt.cache_refresh"
		operations = append(operations, Operation{
			Address:        aptCacheRefreshAddress,
			Action:         "run",
			Summary:        "refresh apt package cache",
			Sensitive:      repositorySensitive,
			DependsOn:      append([]string(nil), repositoryTriggers...),
			TriggeredBy:    append([]string(nil), repositoryTriggers...),
			CommandPreview: "apt-get update",
			Source:         aptCacheSource,
		})
	}
	if aptCacheRefreshAddress != "" {
		for i := range nodes {
			if strings.HasPrefix(nodes[i].Address, "host."+host.Name+".docker.package[") {
				nodes[i].DependsOn = dedupeStrings(append(nodes[i].DependsOn, aptCacheRefreshAddress))
			}
		}
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
	nftablesSensitive := false
	nftablesFileDeps := []string{}
	if packageAddress, ok := packageAddresses["nftables"]; ok {
		nftablesFileDeps = append(nftablesFileDeps, packageAddress)
	}
	if host.Nftables.Main != nil {
		item := *host.Nftables.Main
		nftablesSensitive = nftablesSensitive || item.Sensitive
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
		nftablesSensitive = nftablesSensitive || item.Sensitive
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
			Sensitive:      nftablesSensitive,
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
				Sensitive:      nftablesSensitive,
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
			Sensitive:      nftablesSensitive,
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

	if dockerServiceAddress != "" {
		for i := range nodes {
			if nodes[i].Address != dockerServiceAddress {
				continue
			}
			deps := append([]string(nil), dockerPackageAddresses...)
			if dockerDaemonFileAddress != "" {
				deps = append(deps, dockerDaemonFileAddress)
			}
			nodes[i].DependsOn = dedupeStrings(append(nodes[i].DependsOn, deps...))
		}
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
			DependsOn:       ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses),
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
		desired, payload := fileResourceDesiredPayload(fileResourceSpec{
			Path:             item.Path,
			Content:          item.Content,
			ContentVersion:   item.ContentVersion,
			ContentWriteOnly: item.ContentWriteOnly,
			SourcePath:       item.SourcePath,
			Owner:            item.Owner,
			Group:            item.Group,
			Mode:             item.Mode,
			Sensitive:        item.Sensitive,
			Ensure:           item.Ensure,
			Summary:          item.Summary,
		})
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
			ProviderAddress: "file." + providerName(host.Name, item.Path),
			ProviderPayload: payload,
		})
	}

	componentScriptTriggers := map[string]map[string][]string{}
	rootScriptTriggers := map[string][]string{}
	rootScriptNames := map[string]string{}
	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, path := range sortedKeys(component.Files.Files) {
			item := component.Files.Files[path]
			address := fmt.Sprintf("%s.files.file[%s]", componentPrefix, strconv.Quote(path))
			desired, payload := fileResourceDesiredPayload(fileResourceSpec{
				Path:             item.Path,
				Component:        component.Name,
				Content:          item.Content,
				ContentVersion:   item.ContentVersion,
				ContentWriteOnly: item.ContentWriteOnly,
				SourcePath:       item.SourcePath,
				Owner:            item.Owner,
				Group:            item.Group,
				Mode:             item.Mode,
				Sensitive:        item.Sensitive,
				Ensure:           item.Ensure,
				Summary:          item.Summary,
			})
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
			if item.OnChange != nil {
				if item.OnChange.Scope == "root" {
					if item.OnChange.DeclarationID == "" {
						return nil, nil, fmt.Errorf("%s has unresolved root script reference %q", address, item.OnChange.Name)
					}
					rootScriptTriggers[item.OnChange.DeclarationID] = append(rootScriptTriggers[item.OnChange.DeclarationID], address)
					rootScriptNames[item.OnChange.DeclarationID] = item.OnChange.Name
					continue
				}
				triggers, ok := componentScriptTriggers[component.Name]
				if !ok {
					triggers = map[string][]string{}
					componentScriptTriggers[component.Name] = triggers
				}
				triggers[item.OnChange.Name] = append(triggers[item.OnChange.Name], address)
			}
		}
	}
	for _, component := range host.Components {
		triggers := componentScriptTriggers[component.Name]
		if len(triggers) == 0 {
			continue
		}
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(triggers) {
			script, ok := component.Scripts[name]
			if !ok {
				continue
			}
			triggeredBy := dedupeStrings(triggers[name])
			outputs := scriptOutputPayloads(host.Name, component.Name, name, script)
			for _, output := range outputs {
				desired := map[string]any{
					"path":      output.Path,
					"component": component.Name,
					"script":    name,
				}
				if output.ScriptDigest != "" {
					desired["script_digest"] = output.ScriptDigest
				}
				nodes = append(nodes, Node{
					Host:            host.Name,
					Address:         output.Address,
					Kind:            "component_script_output",
					Summary:         "check script output " + output.Path,
					Source:          script.Source,
					Desired:         desired,
					DependsOn:       append([]string(nil), triggeredBy...),
					ProviderType:    "component_script_output",
					ProviderAddress: output.ProviderAddress,
					ProviderPayload: desired,
				})
				triggeredBy = append(triggeredBy, output.Address)
			}
			triggeredBy = dedupeStrings(triggeredBy)
			operations = append(operations, Operation{
				Address:        fmt.Sprintf("%s.script[%s]", componentPrefix, strconv.Quote(name)),
				Action:         "run",
				Summary:        "run component script " + name,
				DependsOn:      append([]string(nil), triggeredBy...),
				TriggeredBy:    append([]string(nil), triggeredBy...),
				CommandPreview: "script " + name + " (" + script.Mode + ")",
				Sensitive:      script.Sensitive,
				ScriptPayload: &ScriptPayload{
					Name:          script.Name,
					ComponentName: component.Name,
					Mode:          script.Mode,
					Kind:          scriptPayloadKind(script),
					Interpreter:   append([]string(nil), script.Interpreter...),
					Outputs:       outputs,
					Run:           script.Run,
					Content:       script.Content,
					Commands:      cloneCommandMatrix(script.Commands),
				},
				Source: script.Source,
			})
		}
	}
	for _, declarationID := range sortedKeys(rootScriptTriggers) {
		name := rootScriptNames[declarationID]
		script, ok := host.Scripts[name]
		if !ok || script.DeclarationID != declarationID {
			return nil, nil, fmt.Errorf("host %s root script reference %q has no matching declaration %q", host.Name, name, declarationID)
		}
		triggeredBy := dedupeStrings(rootScriptTriggers[declarationID])
		outputs := hostScriptOutputPayloads(host.Name, name, script)
		for _, output := range outputs {
			desired := map[string]any{
				"path":   output.Path,
				"scope":  "host",
				"script": name,
			}
			if output.ScriptDigest != "" {
				desired["script_digest"] = output.ScriptDigest
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         output.Address,
				Kind:            "component_script_output",
				Summary:         "check host script output " + output.Path,
				Source:          script.Source,
				Desired:         desired,
				DependsOn:       append([]string(nil), triggeredBy...),
				ProviderType:    "component_script_output",
				ProviderAddress: output.ProviderAddress,
				ProviderPayload: desired,
			})
			triggeredBy = append(triggeredBy, output.Address)
		}
		triggeredBy = dedupeStrings(triggeredBy)
		operations = append(operations, Operation{
			Address:        fmt.Sprintf("host.%s.script[%s]", host.Name, strconv.Quote(name)),
			Action:         "run",
			Summary:        "run host script " + name,
			Sensitive:      script.Sensitive,
			DependsOn:      append([]string(nil), triggeredBy...),
			TriggeredBy:    append([]string(nil), triggeredBy...),
			CommandPreview: "script " + name + " (once)",
			ScriptPayload: &ScriptPayload{
				Name:        script.Name,
				Mode:        "once",
				Kind:        scriptPayloadKind(script),
				Interpreter: append([]string(nil), script.Interpreter...),
				Outputs:     outputs,
				Run:         script.Run,
				Content:     script.Content,
				Commands:    cloneCommandMatrix(script.Commands),
			},
			Source: script.Source,
		})
	}

	for _, path := range sortedKeys(host.Secrets.Files) {
		item := host.Secrets.Files[path]
		address := fmt.Sprintf("host.%s.secrets.file[%s]", host.Name, strconv.Quote(path))
		desired, payload := fileResourceDesiredPayload(fileResourceSpec{
			Path:       item.Path,
			SourcePath: item.SourcePath,
			Owner:      item.Owner,
			Group:      item.Group,
			Mode:       item.Mode,
			Sensitive:  true,
			Ensure:     item.Ensure,
			Summary:    item.Summary,
		})
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
			ProviderAddress: "file." + providerName(host.Name, item.Path),
			ProviderPayload: payload,
		})
	}

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, path := range sortedKeys(component.Secrets.Files) {
			item := component.Secrets.Files[path]
			address := fmt.Sprintf("%s.secrets.file[%s]", componentPrefix, strconv.Quote(path))
			desired, payload := fileResourceDesiredPayload(fileResourceSpec{
				Path:       item.Path,
				Component:  component.Name,
				SourcePath: item.SourcePath,
				Owner:      item.Owner,
				Group:      item.Group,
				Mode:       item.Mode,
				Sensitive:  true,
				Ensure:     item.Ensure,
				Summary:    item.Summary,
			})
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
				ProviderPayload: payload,
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
	timerServiceNodes := []Node{}
	networkdTriggers := []string{}
	resolvedTriggers := []string{}
	journaldTriggers := []string{}
	resolvedServiceAddress := ""
	journaldServiceAddress := ""
	resolvedSource := ir.SourceRef{}
	journaldSource := ir.SourceRef{}
	resolvedRestartOnChange := false
	journaldRestartOnChange := false
	networkdDeleteNames := []string{}
	networkdSource := ir.SourceRef{}
	var networkdEnable *bool
	networkdEnableSource := ir.SourceRef{}
	for _, name := range sortedKeys(host.Systemd.Units) {
		item := host.Systemd.Units[name]
		address := fmt.Sprintf("host.%s.systemd.unit[%s]", host.Name, strconv.Quote(name))
		unitAddresses[name] = address
		unitTriggers = append(unitTriggers, address)
		nodes = append(nodes, systemdUnitNode(host.Name, address, "", item, ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses)))
	}

	for _, name := range sortedKeys(host.Systemd.Timers) {
		timer := host.Systemd.Timers[name]
		item := timer.Unit
		address := fmt.Sprintf("host.%s.systemd.timer[%s]", host.Name, strconv.Quote(name))
		unitAddresses[item.Name] = address
		unitTriggers = append(unitTriggers, address)
		nodes = append(nodes, systemdUnitNode(host.Name, address, "", item, ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses)))
		if timer.Enable != nil || timer.State != "" {
			serviceAddress := fmt.Sprintf("host.%s.systemd.timer[%s].service", host.Name, strconv.Quote(name))
			deps := []string{address}
			timerServiceNodes = append(timerServiceNodes, systemdManagedServiceNode(host.Name, serviceAddress, "", item.Name, item.Name, "", timer.Enable, timer.State, "manage timer "+item.Name, timer.Source, deps))
		}
	}

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		for _, name := range sortedKeys(component.Systemd.Units) {
			item := component.Systemd.Units[name]
			address := fmt.Sprintf("%s.systemd.unit[%s]", componentPrefix, strconv.Quote(name))
			unitAddresses[name] = address
			unitTriggers = append(unitTriggers, address)
			deps := ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses)
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			nodes = append(nodes, systemdUnitNode(host.Name, address, component.Name, item, dedupeStrings(deps)))
		}
		for _, name := range sortedKeys(component.Systemd.Timers) {
			timer := component.Systemd.Timers[name]
			item := timer.Unit
			address := fmt.Sprintf("%s.systemd.timer[%s]", componentPrefix, strconv.Quote(name))
			unitAddresses[item.Name] = address
			unitTriggers = append(unitTriggers, address)
			deps := ownershipDependencies(item.Owner, item.Group, userAddresses, groupAddresses)
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			nodes = append(nodes, systemdUnitNode(host.Name, address, component.Name, item, dedupeStrings(deps)))
			if timer.Enable != nil || timer.State != "" {
				serviceAddress := fmt.Sprintf("%s.systemd.timer[%s].service", componentPrefix, strconv.Quote(name))
				serviceDeps := append([]string{address}, deps...)
				timerServiceNodes = append(timerServiceNodes, systemdManagedServiceNode(host.Name, serviceAddress, component.Name, item.Name, item.Name, "", timer.Enable, timer.State, "manage timer "+item.Name, timer.Source, serviceDeps))
			}
		}
	}

	if host.Systemd.Resolved != nil {
		item := *host.Systemd.Resolved
		address := "host." + host.Name + ".systemd.resolved"
		if resolvedSource.File == "" {
			resolvedSource = item.Source
		}
		resolvedRestartOnChange = systemdDropInShouldRestartOnChange(item.Enable, item.State)
		deps := ownershipDependencies(item.Unit.Owner, item.Unit.Group, userAddresses, groupAddresses)
		if packageAddress, ok := packageAddresses["systemd-resolved"]; ok {
			deps = append(deps, packageAddress)
		}
		nodes = append(nodes, systemdDropInFileNode(host.Name, address, "", item.Unit, "manage systemd-resolved drop-in", deps))
		resolvedTriggers = append(resolvedTriggers, address)
		if serviceNode, ok := systemdStateServiceNode(host.Name, address+".service", "", "systemd-resolved", "systemd-resolved.service", item.Enable, item.State, "manage systemd-resolved", item.Source, []string{address}); ok {
			resolvedServiceAddress = serviceNode.Address
			nodes = append(nodes, serviceNode)
		}
	}

	if host.Systemd.Journald != nil {
		item := *host.Systemd.Journald
		address := "host." + host.Name + ".systemd.journald"
		if journaldSource.File == "" {
			journaldSource = item.Source
		}
		journaldRestartOnChange = systemdDropInShouldRestartOnChange(nil, item.State)
		deps := ownershipDependencies(item.Unit.Owner, item.Unit.Group, userAddresses, groupAddresses)
		nodes = append(nodes, systemdDropInFileNode(host.Name, address, "", item.Unit, "manage systemd-journald drop-in", deps))
		journaldTriggers = append(journaldTriggers, address)
		if serviceNode, ok := systemdStateServiceNode(host.Name, address+".service", "", "systemd-journald", "systemd-journald.service", nil, item.State, "manage systemd-journald", item.Source, []string{address}); ok {
			journaldServiceAddress = serviceNode.Address
			nodes = append(nodes, serviceNode)
		}
	}

	for _, component := range host.Components {
		componentPrefix := fmt.Sprintf("host.%s.components.%s", host.Name, component.Name)
		if component.Systemd.Resolved != nil {
			item := *component.Systemd.Resolved
			address := componentPrefix + ".systemd.resolved"
			if resolvedSource.File == "" {
				resolvedSource = item.Source
			}
			resolvedRestartOnChange = systemdDropInShouldRestartOnChange(item.Enable, item.State)
			deps := ownershipDependencies(item.Unit.Owner, item.Unit.Group, userAddresses, groupAddresses)
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			if packageAddress, ok := packageAddresses["systemd-resolved"]; ok {
				deps = append(deps, packageAddress)
			}
			nodes = append(nodes, systemdDropInFileNode(host.Name, address, component.Name, item.Unit, "manage systemd-resolved drop-in", dedupeStrings(deps)))
			resolvedTriggers = append(resolvedTriggers, address)
			if serviceNode, ok := systemdStateServiceNode(host.Name, address+".service", component.Name, "systemd-resolved", "systemd-resolved.service", item.Enable, item.State, "manage systemd-resolved", item.Source, []string{address}); ok {
				resolvedServiceAddress = serviceNode.Address
				nodes = append(nodes, serviceNode)
			}
		}
		if component.Systemd.Journald != nil {
			item := *component.Systemd.Journald
			address := componentPrefix + ".systemd.journald"
			if journaldSource.File == "" {
				journaldSource = item.Source
			}
			journaldRestartOnChange = systemdDropInShouldRestartOnChange(nil, item.State)
			deps := ownershipDependencies(item.Unit.Owner, item.Unit.Group, userAddresses, groupAddresses)
			if installAddress, ok := componentArtifactInstallAddresses[component.Name]; ok {
				deps = append(deps, installAddress)
			}
			nodes = append(nodes, systemdDropInFileNode(host.Name, address, component.Name, item.Unit, "manage systemd-journald drop-in", dedupeStrings(deps)))
			journaldTriggers = append(journaldTriggers, address)
			if serviceNode, ok := systemdStateServiceNode(host.Name, address+".service", component.Name, "systemd-journald", "systemd-journald.service", nil, item.State, "manage systemd-journald", item.Source, []string{address}); ok {
				journaldServiceAddress = serviceNode.Address
				nodes = append(nodes, serviceNode)
			}
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
	if daemonReloadAddress != "" {
		for i := range timerServiceNodes {
			timerServiceNodes[i].DependsOn = dedupeStrings(append(timerServiceNodes[i].DependsOn, daemonReloadAddress))
		}
	}
	nodes = append(nodes, timerServiceNodes...)

	if len(resolvedTriggers) > 0 && resolvedRestartOnChange {
		deps := append([]string(nil), resolvedTriggers...)
		if resolvedServiceAddress != "" {
			deps = append(deps, resolvedServiceAddress)
		}
		operations = append(operations, Operation{
			Address:        "host." + host.Name + ".systemd.resolved.restart",
			Action:         "run",
			Summary:        "restart systemd-resolved",
			DependsOn:      dedupeStrings(deps),
			TriggeredBy:    append([]string(nil), resolvedTriggers...),
			CommandPreview: "systemctl restart systemd-resolved.service",
			Source:         resolvedSource,
		})
	}
	if len(journaldTriggers) > 0 && journaldRestartOnChange {
		deps := append([]string(nil), journaldTriggers...)
		if journaldServiceAddress != "" {
			deps = append(deps, journaldServiceAddress)
		}
		operations = append(operations, Operation{
			Address:        "host." + host.Name + ".systemd.journald.restart",
			Action:         "run",
			Summary:        "restart systemd-journald",
			DependsOn:      dedupeStrings(deps),
			TriggeredBy:    append([]string(nil), journaldTriggers...),
			CommandPreview: "systemctl restart systemd-journald.service",
			Source:         journaldSource,
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

	for i := range operations {
		operations[i].Host = host.Name
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Address < nodes[j].Address
	})
	sort.SliceStable(operations, func(i, j int) bool {
		return operations[i].Address < operations[j].Address
	})
	return nodes, operations, nil
}
