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
			if item.OnChange != "" {
				triggers, ok := componentScriptTriggers[component.Name]
				if !ok {
					triggers = map[string][]string{}
					componentScriptTriggers[component.Name] = triggers
				}
				triggers[item.OnChange] = append(triggers[item.OnChange], address)
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

func scriptOutputPayloads(hostName, componentName, scriptName string, script ir.ComponentScriptSpec) []ScriptOutputPayload {
	paths := script.Outputs
	if len(paths) == 0 {
		return nil
	}
	out := make([]ScriptOutputPayload, 0, len(paths))
	componentPrefix := fmt.Sprintf("host.%s.components.%s", hostName, componentName)
	digest := componentScriptDigest(script)
	for _, path := range paths {
		out = append(out, ScriptOutputPayload{
			Address:         fmt.Sprintf("%s.script[%s].outputs[%s]", componentPrefix, strconv.Quote(scriptName), strconv.Quote(path)),
			Path:            path,
			ScriptDigest:    digest,
			ProviderAddress: "component_script_output." + providerName(hostName, componentName, scriptName, path),
		})
	}
	return out
}

func componentScriptDigest(script ir.ComponentScriptSpec) string {
	data, err := json.Marshal(map[string]any{
		"name":        script.Name,
		"mode":        script.Mode,
		"body":        script.Body,
		"interpreter": script.Interpreter,
		"run":         script.Run,
		"content":     script.Content,
		"commands":    script.Commands,
	})
	if err != nil {
		return shortHash(script.Name + "\x00" + script.Run + "\x00" + script.Content)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func scriptPayloadKind(script ir.ComponentScriptSpec) string {
	if script.Body != "" {
		return script.Body
	}
	switch {
	case len(script.Commands) > 0:
		return "commands"
	case script.Content != "":
		return "content"
	default:
		return "run"
	}
}

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

func aptRepositorySourcePath(name string) string {
	return "/etc/apt/sources.list.d/" + providerName(name) + ".sources"
}

const (
	dockerOfficialRepositoryName = "docker-official"
	dockerOfficialKeyPath        = "/etc/apt/keyrings/docker.asc"
)

var dockerOfficialPackages = []string{
	"docker-ce",
	"docker-ce-cli",
	"containerd.io",
	"docker-buildx-plugin",
	"docker-compose-plugin",
}

var dockerDebianPackages = []string{
	"docker.io",
	"docker-compose-plugin",
}

var dockerConflictPackages = []string{
	"docker.io",
	"docker-doc",
	"docker-compose",
	"podman-docker",
	"containerd",
	"runc",
}

type dockerEngineGraph struct {
	Nodes                    []Node
	Operations               []Operation
	RepositoryTriggers       []string
	PackageAddresses         []string
	ServiceAddress           string
	DaemonFileAddress        string
	ComposeValidateAddresses map[string]string
}

func dockerEngineNodes(host ir.HostSpec, repositoryAddresses map[string]string, groupAddresses map[string]string, userAddresses map[string]string) (dockerEngineGraph, error) {
	docker := host.Docker
	if docker == nil || !docker.Enable {
		return dockerEngineGraph{}, nil
	}
	installPackages := true
	if docker.Package.Channel != "stable" {
		return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: docker package channel %q is not implemented in this loop", docker.Package.SourceRef.File, docker.Package.SourceRef.Line, docker.Package.SourceRef.Path, docker.Package.Channel)
	}
	if docker.Package.Version != nil {
		return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: docker package version pinning is not implemented in this loop", docker.Package.SourceRef.File, docker.Package.SourceRef.Line, docker.Package.SourceRef.Path)
	}

	switch docker.Package.Source {
	case "official":
	case "none":
		installPackages = false
	case "custom":
		installPackages = false
	case "debian":
	default:
		return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: docker package source %q is not supported", docker.Package.SourceRef.File, docker.Package.SourceRef.Line, docker.Package.SourceRef.Path, docker.Package.Source)
	}

	out := dockerEngineGraph{}
	repositoryAddress := ""

	switch docker.Package.Source {
	case "official":
		architecture := host.PlatformArchitecture()
		if architecture == "" {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s.platform.architecture: host %q must declare platform.architecture to compile docker official repository", host.Source.File, host.Source.Line, host.Source.Path, host.Name)
		}
		codename := host.PlatformCodename()
		if codename == "" {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s.platform.codename: host %q must declare platform.codename to compile docker official repository", host.Source.File, host.Source.Line, host.Source.Path, host.Name)
		}
		if _, exists := host.APT.Repositories[dockerOfficialRepositoryName]; exists {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: apt repository %q conflicts with docker official repository", host.Source.File, host.Source.Line, host.Source.Path, dockerOfficialRepositoryName)
		}
		if _, exists := repositoryAddresses[dockerOfficialRepositoryName]; exists {
			return dockerEngineGraph{}, fmt.Errorf("%s:%d:%s: apt repository %q conflicts with docker official repository", host.Source.File, host.Source.Line, host.Source.Path, dockerOfficialRepositoryName)
		}

		keyAddress := fmt.Sprintf("host.%s.docker.apt.signing_key[%s]", host.Name, strconv.Quote(dockerOfficialRepositoryName))
		repositoryAddress = fmt.Sprintf("host.%s.docker.apt.repository[%s]", host.Name, strconv.Quote(dockerOfficialRepositoryName))
		sourcePath := aptRepositorySourcePath(dockerOfficialRepositoryName)
		repositoryURL := docker.Package.RepositoryURL
		if repositoryURL == "" {
			repositoryURL = ir.DockerOfficialRepositoryURL
		}
		gpgURL := docker.Package.GPGURL
		if gpgURL == "" {
			gpgURL = ir.DockerOfficialGPGURL
		}
		gpgSHA256 := docker.Package.GPGSHA256
		if gpgSHA256 == "" && gpgURL == ir.DockerOfficialGPGURL {
			gpgSHA256 = ir.DockerOfficialGPGSHA256
		}

		keyDesired := map[string]any{
			"name":   dockerOfficialRepositoryName,
			"path":   dockerOfficialKeyPath,
			"owner":  "root",
			"group":  "root",
			"mode":   "0644",
			"ensure": "present",
			"url":    gpgURL,
		}
		if gpgSHA256 != "" {
			keyDesired["sha256"] = gpgSHA256
		}
		repositorySpec := ir.APTRepositorySpec{
			Name:          dockerOfficialRepositoryName,
			URIs:          []string{repositoryURL},
			Suites:        []string{codename},
			Components:    []string{docker.Package.Channel},
			Architectures: []string{architecture},
			Ensure:        "present",
			SigningKey: &ir.APTSigningKeySpec{
				Path: dockerOfficialKeyPath,
			},
		}
		repositoryDesired := map[string]any{
			"name":    dockerOfficialRepositoryName,
			"path":    sourcePath,
			"owner":   "root",
			"group":   "root",
			"mode":    "0644",
			"ensure":  "present",
			"content": aptRepositorySourceContent(repositorySpec),
		}
		out.Nodes = append(out.Nodes, Node{
			Host:            host.Name,
			Address:         keyAddress,
			Kind:            "apt_signing_key",
			Summary:         "manage docker official apt signing key",
			Source:          docker.Package.SourceRef,
			Desired:         keyDesired,
			ProviderType:    "apt_signing_key",
			ProviderAddress: "apt_signing_key." + providerName(host.Name, dockerOfficialRepositoryName),
			ProviderPayload: keyDesired,
		}, Node{
			Host:            host.Name,
			Address:         repositoryAddress,
			Kind:            "file",
			Summary:         "manage docker official apt repository",
			Source:          docker.Package.SourceRef,
			Desired:         repositoryDesired,
			DependsOn:       []string{keyAddress},
			ProviderType:    "file",
			ProviderAddress: "file." + providerName(host.Name, sourcePath),
			ProviderPayload: repositoryDesired,
		})
		out.RepositoryTriggers = append(out.RepositoryTriggers, keyAddress, repositoryAddress)

		conflictAddress := fmt.Sprintf("host.%s.docker.package_conflicts", host.Name)
		conflictDesired := map[string]any{
			"packages":         append([]string(nil), dockerConflictPackages...),
			"remove_conflicts": docker.Package.RemoveConflicts,
			"ensure":           "absent",
		}
		out.Nodes = append(out.Nodes, Node{
			Host:            host.Name,
			Address:         conflictAddress,
			Kind:            "docker_package_conflicts",
			Summary:         "remove docker conflict packages",
			Source:          docker.Package.SourceRef,
			Desired:         conflictDesired,
			ProviderType:    "docker_package_conflicts",
			ProviderAddress: "docker_package_conflicts." + providerName(host.Name, "docker"),
			ProviderPayload: conflictDesired,
		})

		for _, name := range dockerOfficialPackages {
			address := fmt.Sprintf("host.%s.docker.package[%s]", host.Name, strconv.Quote(name))
			desired := map[string]any{
				"name":         name,
				"ensure":       "present",
				"repositories": []string{dockerOfficialRepositoryName},
			}
			out.Nodes = append(out.Nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "package",
				Summary:         "install docker package " + name,
				Source:          docker.Package.SourceRef,
				Desired:         desired,
				DependsOn:       []string{repositoryAddress, conflictAddress},
				ProviderType:    "package",
				ProviderAddress: "package." + providerName(host.Name, "docker", name),
				ProviderPayload: desired,
			})
			out.PackageAddresses = append(out.PackageAddresses, address)
		}
	case "debian":
		for _, name := range dockerDebianPackages {
			address := fmt.Sprintf("host.%s.docker.package[%s]", host.Name, strconv.Quote(name))
			desired := map[string]any{
				"name":   name,
				"ensure": "present",
			}
			out.Nodes = append(out.Nodes, Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "package",
				Summary:         "install docker package " + name,
				Source:          docker.Package.SourceRef,
				Desired:         desired,
				ProviderType:    "package",
				ProviderAddress: "package." + providerName(host.Name, "docker", name),
				ProviderPayload: desired,
			})
			out.PackageAddresses = append(out.PackageAddresses, address)
		}
	}

	if len(docker.Users) > 0 {
		out.Nodes = append(out.Nodes, dockerUserMembershipNodes(host, groupAddresses, userAddresses)...)
	}

	if docker.Daemon != nil {
		fileAddress, fileNode, restartOperation, err := dockerDaemonGraph(host)
		if err != nil {
			return dockerEngineGraph{}, err
		}
		if len(out.PackageAddresses) > 0 {
			fileNode.DependsOn = dedupeStrings(append(fileNode.DependsOn, out.PackageAddresses...))
		}
		out.Nodes = append(out.Nodes, fileNode)
		out.Operations = append(out.Operations, restartOperation)
		out.DaemonFileAddress = fileAddress
	}

	serviceAddress := fmt.Sprintf("host.%s.docker.service[%s]", host.Name, strconv.Quote("docker"))
	serviceDesired := map[string]any{
		"name":    "docker",
		"unit":    "docker.service",
		"enabled": docker.Service.Enable,
		"state":   docker.Service.State,
	}
	out.Nodes = append(out.Nodes, Node{
		Host:            host.Name,
		Address:         serviceAddress,
		Kind:            "service",
		Summary:         "manage docker service",
		Source:          docker.Service.SourceRef,
		Desired:         serviceDesired,
		ProviderType:    "service",
		ProviderAddress: "service." + providerName(host.Name, "docker"),
		ProviderPayload: serviceDesired,
	})
	out.ServiceAddress = serviceAddress
	if len(out.Operations) > 0 {
		for i := range out.Operations {
			if out.Operations[i].Address == "host."+host.Name+".docker.daemon.restart" {
				out.Operations[i].DependsOn = dedupeStrings(append(out.Operations[i].DependsOn, serviceAddress))
			}
		}
	}

	composeServiceAddress := serviceAddress
	if !installPackages {
		composeServiceAddress = ""
	}
	composeNodes, composeOperations, composeValidates, err := dockerComposeGraph(host, out.PackageAddresses, serviceAddress, composeServiceAddress)
	if err != nil {
		return dockerEngineGraph{}, err
	}
	out.Nodes = append(out.Nodes, composeNodes...)
	out.Operations = append(out.Operations, composeOperations...)
	out.ComposeValidateAddresses = composeValidates

	return out, nil
}

func dockerUserMembershipNodes(host ir.HostSpec, groupAddresses map[string]string, userAddresses map[string]string) []Node {
	docker := host.Docker
	if docker == nil || len(docker.Users) == 0 {
		return nil
	}
	nodes := []Node{}
	groupAddress, groupDeclared := groupAddresses["docker"]
	if !groupDeclared {
		groupAddress = fmt.Sprintf("host.%s.docker.group[%s]", host.Name, strconv.Quote("docker"))
		desired := map[string]any{
			"name":   "docker",
			"gid":    "",
			"system": false,
			"ensure": "present",
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         groupAddress,
			Kind:            "group",
			Summary:         "create docker group",
			Source:          docker.Source,
			Desired:         desired,
			ProviderType:    "group",
			ProviderAddress: "group." + providerName(host.Name, "docker"),
			ProviderPayload: desired,
		})
	}
	for _, user := range dedupeStrings(docker.Users) {
		address := fmt.Sprintf("host.%s.docker.user_group_membership[%s]", host.Name, strconv.Quote(user+":docker"))
		deps := []string{groupAddress}
		if userAddress, ok := userAddresses[user]; ok {
			deps = append(deps, userAddress)
		}
		desired := map[string]any{
			"user":   user,
			"group":  "docker",
			"ensure": "present",
			"note":   "user must log out and back in for docker group membership to affect existing sessions",
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         address,
			Kind:            "user_group_membership",
			Summary:         "add user " + user + " to docker group",
			Source:          docker.Source,
			Desired:         desired,
			DependsOn:       dedupeStrings(deps),
			ProviderType:    "user_group_membership",
			ProviderAddress: "user_group_membership." + providerName(host.Name, user, "docker"),
			ProviderPayload: desired,
		})
	}
	return nodes
}

func dockerDaemonGraph(host ir.HostSpec) (string, Node, Operation, error) {
	daemon := host.Docker.Daemon
	if daemon == nil {
		return "", Node{}, Operation{}, nil
	}
	content, err := deterministicJSON(daemon.Settings)
	if err != nil {
		return "", Node{}, Operation{}, err
	}
	address := fmt.Sprintf("host.%s.docker.daemon.file[%s]", host.Name, strconv.Quote("/etc/docker/daemon.json"))
	desired := map[string]any{
		"path":    "/etc/docker/daemon.json",
		"content": content,
		"owner":   "root",
		"group":   "root",
		"mode":    "0644",
		"ensure":  "present",
		"summary": daemon.Summary,
	}
	node := Node{
		Host:            host.Name,
		Address:         address,
		Kind:            "file",
		Summary:         "manage docker daemon configuration",
		Source:          daemon.Source,
		Desired:         desired,
		ProviderType:    "file",
		ProviderAddress: "file." + providerName(host.Name, "/etc/docker/daemon.json"),
		ProviderPayload: desired,
	}
	operation := Operation{
		Address:        "host." + host.Name + ".docker.daemon.restart",
		Action:         "run",
		Summary:        "restart docker service",
		DependsOn:      []string{address},
		TriggeredBy:    []string{address},
		CommandPreview: "systemctl restart docker.service",
		Source:         daemon.Source,
	}
	return address, node, operation, nil
}

func dockerComposeGraph(host ir.HostSpec, packageAddresses []string, fileServiceAddress string, projectServiceAddress string) ([]Node, []Operation, map[string]string, error) {
	if host.Docker == nil || len(host.Docker.Composes) == 0 {
		return nil, nil, map[string]string{}, nil
	}
	validateAddresses := map[string]string{}
	var nodes []Node
	var operations []Operation
	for _, name := range sortedKeys(host.Docker.Composes) {
		compose := host.Docker.Composes[name]
		if !compose.Enable {
			continue
		}
		prefix := fmt.Sprintf("host.%s.docker.compose[%s]", host.Name, strconv.Quote(name))
		directoryAddress := prefix + ".directory"
		directoryDesired := map[string]any{
			"path":   compose.Directory,
			"owner":  "root",
			"group":  "root",
			"mode":   "0755",
			"ensure": "present",
		}
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         directoryAddress,
			Kind:            "directory",
			Summary:         "create docker compose directory " + compose.Directory,
			Source:          compose.Source,
			Desired:         directoryDesired,
			ProviderType:    "directory",
			ProviderAddress: "directory." + providerName(host.Name, "docker", "compose", name, compose.Directory),
			ProviderPayload: directoryDesired,
		})

		fileAddresses := []string{}
		engineDeps := []string{}
		if len(packageAddresses) > 0 {
			engineDeps = append(engineDeps, packageAddresses...)
		}
		if fileServiceAddress != "" {
			engineDeps = append(engineDeps, fileServiceAddress)
		}
		fileDeps := dedupeStrings(append([]string{directoryAddress}, engineDeps...))

		if compose.File == nil {
			return nil, nil, nil, fmt.Errorf("%s:%d:%s.file: docker compose file block is required", compose.Source.File, compose.Source.Line, compose.Source.Path)
		}
		fileAddress, fileNode := dockerComposeFileNode(host.Name, prefix+".file", name, *compose.File, "manage docker compose file", fileDeps)
		nodes = append(nodes, fileNode)
		fileAddresses = append(fileAddresses, fileAddress)

		for _, label := range sortedKeys(compose.EnvFiles) {
			envFile := compose.EnvFiles[label]
			envAddress, envNode := dockerComposeFileNode(host.Name, fmt.Sprintf("%s.env_file[%s]", prefix, strconv.Quote(label)), name, envFile, "manage docker compose env file "+label, fileDeps)
			nodes = append(nodes, envNode)
			fileAddresses = append(fileAddresses, envAddress)
		}

		validateAddress := prefix + ".validate"
		validateAddresses[name] = validateAddress
		operations = append(operations, Operation{
			Address:        validateAddress,
			Action:         "run",
			Summary:        "validate docker compose project " + compose.Project,
			DependsOn:      dedupeStrings(fileAddresses),
			TriggeredBy:    dedupeStrings(fileAddresses),
			CommandPreview: dockerComposeConfigCommand(compose),
			Source:         compose.Source,
		})

		unitAddress := ""
		daemonReloadAddress := ""
		serviceAddress := ""
		if compose.Service.Enable {
			unitAddress = prefix + ".systemd_unit"
			daemonReloadAddress = prefix + ".daemon_reload"
			serviceAddress = prefix + ".service"
			unitName := composeSystemdUnitName(compose)
			unitDesired := dockerComposeSystemdUnitDesired(compose, unitName)
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         unitAddress,
				Kind:            "systemd_unit",
				Summary:         "create docker compose systemd unit " + unitName,
				Source:          compose.Source,
				Desired:         unitDesired,
				DependsOn:       []string{validateAddress},
				ProviderType:    "systemd_unit",
				ProviderAddress: "systemd_unit." + providerName(host.Name, "docker", "compose", name, unitName),
				ProviderPayload: unitDesired,
			})
			operations = append(operations, Operation{
				Address:        daemonReloadAddress,
				Action:         "run",
				Summary:        "reload systemd manager configuration",
				DependsOn:      []string{unitAddress},
				TriggeredBy:    []string{unitAddress},
				CommandPreview: "systemctl daemon-reload",
				Source:         compose.Source,
			})
			serviceDesired := dockerComposeServiceDesired(compose, unitName)
			serviceDeps := []string{unitAddress, daemonReloadAddress}
			if projectServiceAddress != "" {
				serviceDeps = append(serviceDeps, projectServiceAddress)
			}
			nodes = append(nodes, Node{
				Host:            host.Name,
				Address:         serviceAddress,
				Kind:            "service",
				Summary:         "manage docker compose service " + compose.Service.Name,
				Source:          compose.Source,
				Desired:         serviceDesired,
				DependsOn:       dedupeStrings(serviceDeps),
				ProviderType:    "service",
				ProviderAddress: "service." + providerName(host.Name, "docker", "compose", name),
				ProviderPayload: serviceDesired,
			})
		}

		projectAddress := prefix + ".project"
		projectDeps := []string{validateAddress}
		if serviceAddress != "" {
			projectDeps = append(projectDeps, serviceAddress)
		} else if projectServiceAddress != "" {
			projectDeps = append(projectDeps, projectServiceAddress)
		}
		projectDesired := dockerComposeProjectDesired(compose)
		nodes = append(nodes, Node{
			Host:            host.Name,
			Address:         projectAddress,
			Kind:            "docker_compose_project",
			Summary:         "converge docker compose project " + compose.Project,
			Source:          compose.Source,
			Desired:         projectDesired,
			DependsOn:       dedupeStrings(projectDeps),
			ProviderType:    "docker_compose_project",
			ProviderAddress: "docker_compose_project." + providerName(host.Name, name),
			ProviderPayload: projectDesired,
		})
	}
	return nodes, operations, validateAddresses, nil
}

func composeSystemdUnitName(compose ir.DockerComposeSpec) string {
	return serviceUnitName(compose.Service.Name)
}

func serviceUnitName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".service"
}

func dockerComposeSystemdUnitDesired(compose ir.DockerComposeSpec, unitName string) map[string]any {
	content := dockerComposeSystemdUnitContent(compose)
	summary := contentSummary([]byte(content))
	return map[string]any{
		"name":    unitName,
		"path":    "/etc/systemd/system/" + unitName,
		"content": content,
		"owner":   "root",
		"group":   "root",
		"mode":    "0644",
		"ensure":  "present",
		"summary": summary,
	}
}

func dockerComposeSystemdUnitContent(compose ir.DockerComposeSpec) string {
	lines := []string{
		"[Unit]",
		"Description=DebianForm Compose Project " + compose.Project,
		"Requires=docker.service",
		"After=" + strings.Join(compose.After, " "),
		"",
		"[Service]",
		"Type=oneshot",
		"RemainAfterExit=yes",
		"WorkingDirectory=" + compose.Directory,
		"ExecStart=" + dockerComposeUnitCommand(compose, "up", "-d"),
		"ExecStop=" + dockerComposeUnitCommand(compose, "stop"),
		"TimeoutStartSec=0",
		"",
		"[Install]",
		"WantedBy=" + strings.Join(compose.WantedBy, " "),
	}
	return strings.Join(lines, "\n") + "\n"
}

func dockerComposeUnitCommand(compose ir.DockerComposeSpec, command ...string) string {
	args := []string{
		"/usr/bin/docker",
		"compose",
		"-p",
		compose.Project,
	}
	if compose.File != nil {
		args = append(args, "-f", compose.File.Path)
	}
	args = append(args, command...)
	return quoteCommand(args)
}

func dockerComposeServiceDesired(compose ir.DockerComposeSpec, unitName string) map[string]any {
	state := "stopped"
	enabled := false
	switch compose.State {
	case "running":
		enabled = true
		state = "running"
	case "stopped":
		enabled = true
		state = "stopped"
	case "absent":
		enabled = false
		state = "stopped"
	}
	return map[string]any{
		"name":    compose.Service.Name,
		"unit":    unitName,
		"enabled": enabled,
		"state":   state,
	}
}

func dockerComposeProjectDesired(compose ir.DockerComposeSpec) map[string]any {
	desired := map[string]any{
		"name":           compose.Name,
		"directory":      compose.Directory,
		"project":        compose.Project,
		"state":          compose.State,
		"pull":           compose.Pull,
		"recreate":       compose.Recreate,
		"remove_orphans": compose.RemoveOrphans,
	}
	if compose.File != nil {
		desired["files"] = []string{compose.File.Path}
	}
	if len(compose.EnvFiles) > 0 {
		envFiles := make([]string, 0, len(compose.EnvFiles))
		for _, label := range sortedKeys(compose.EnvFiles) {
			envFiles = append(envFiles, compose.EnvFiles[label].Path)
		}
		desired["env_files"] = envFiles
	}
	return desired
}

func dockerComposeFileNode(hostName, address, composeName string, item ir.DockerComposeFileSpec, summary string, deps []string) (string, Node) {
	desired, payload := fileResourceDesiredPayload(fileResourceSpec{
		Path:       item.Path,
		Content:    item.Content,
		SourcePath: item.SourcePath,
		Owner:      item.Owner,
		Group:      item.Group,
		Mode:       item.Mode,
		Sensitive:  item.Sensitive,
		Ensure:     "present",
		Summary:    item.Summary,
	})
	desired["compose"] = composeName
	payload["compose"] = composeName
	node := Node{
		Host:            hostName,
		Address:         address,
		Kind:            "file",
		Summary:         summary,
		Source:          item.Source,
		Desired:         desired,
		DependsOn:       dedupeStrings(deps),
		ProviderType:    "file",
		ProviderAddress: "file." + providerName(hostName, item.Path),
		ProviderPayload: payload,
	}
	return address, node
}

func dockerComposeConfigCommand(compose ir.DockerComposeSpec) string {
	args := []string{
		"docker",
		"compose",
		"-p",
		compose.Project,
	}
	if compose.File != nil {
		args = append(args, "-f", compose.File.Path)
	}
	args = append(args, "config")
	return quoteCommand(args)
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

func optionalContentSummary(content string) ir.ContentSummary {
	if content == "" {
		return ir.ContentSummary{}
	}
	return contentSummary([]byte(content))
}

type fileResourceSpec struct {
	Path             string
	Component        string
	Content          string
	ContentVersion   string
	ContentWriteOnly bool
	SourcePath       string
	Owner            string
	Group            string
	Mode             string
	Sensitive        bool
	Ensure           string
	Summary          ir.ContentSummary
}

func fileResourceDesiredPayload(item fileResourceSpec) (map[string]any, map[string]any) {
	desired := map[string]any{
		"path":      item.Path,
		"owner":     item.Owner,
		"group":     item.Group,
		"mode":      item.Mode,
		"ensure":    item.Ensure,
		"sensitive": item.Sensitive,
	}
	if item.Component != "" {
		desired["component"] = item.Component
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
	return desired, payload
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
