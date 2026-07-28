package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func systemdSpecs(systemd parser.Value) (map[string]ir.SystemdUnit, error) {
	objects, ok, err := objectCollection(systemd, "unit")
	if err != nil {
		return nil, err
	}
	serviceObjects, serviceOK, err := objectCollection(systemd, "service_unit")
	if err != nil {
		return nil, err
	}
	out := make(map[string]ir.SystemdUnit, len(objects)+len(serviceObjects))
	if ok {
		for _, name := range sortedKeys(objects) {
			item := objects[name]
			if name == "" {
				return nil, fmt.Errorf("%s:%d:%s: systemd unit name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			unit, err := systemdUnitSpec(name, item)
			if err != nil {
				return nil, err
			}
			if err := addSystemdUnit(out, unit); err != nil {
				return nil, err
			}
		}
	}
	if serviceOK {
		for _, name := range sortedKeys(serviceObjects) {
			item := serviceObjects[name]
			if name == "" {
				return nil, fmt.Errorf("%s:%d:%s: systemd service_unit name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			unit, err := systemdServiceUnitSpec(name, item)
			if err != nil {
				return nil, err
			}
			if err := addSystemdUnit(out, unit); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func systemdTimerSpecs(systemd parser.Value) (map[string]ir.SystemdTimer, error) {
	objects, ok, err := objectCollection(systemd, "timer")
	if err != nil || !ok {
		return map[string]ir.SystemdTimer{}, err
	}
	out := make(map[string]ir.SystemdTimer, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: systemd timer name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		timer, err := systemdTimerSpec(name, item)
		if err != nil {
			return nil, err
		}
		if previous, exists := out[timer.Name]; exists {
			return nil, fmt.Errorf("%s:%d:%s: systemd timer %q conflicts with timer declared at %s:%d:%s", timer.Source.File, timer.Source.Line, timer.Source.Path, timer.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		out[timer.Name] = timer
	}
	return out, nil
}

func networkdSpec(systemd parser.Value) (*ir.NetworkdSpec, error) {
	networkd, ok, err := mapField(systemd, "networkd")
	if err != nil || !ok {
		return nil, err
	}
	spec := &ir.NetworkdSpec{
		NetDevs:  map[string]ir.NetworkdNetDev{},
		Networks: map[string]ir.NetworkdNetwork{},
		Source:   networkd.Source,
	}
	if enable, ok, err := boolField(networkd, "enable"); err != nil {
		return nil, err
	} else if ok {
		value := enable
		spec.Enable = &value
	}

	netdevs, ok, err := objectCollection(networkd, "netdev")
	if err != nil {
		return nil, err
	}
	if ok {
		for _, label := range sortedKeys(netdevs) {
			item := netdevs[label]
			if label == "" {
				return nil, fmt.Errorf("%s:%d:%s: networkd netdev label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			netdev, err := networkdNetDevSpec(label, item)
			if err != nil {
				return nil, err
			}
			spec.NetDevs[label] = netdev
		}
	}

	networks, ok, err := objectCollection(networkd, "network")
	if err != nil {
		return nil, err
	}
	if ok {
		for _, label := range sortedKeys(networks) {
			item := networks[label]
			if label == "" {
				return nil, fmt.Errorf("%s:%d:%s: networkd network label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
			}
			network, err := networkdNetworkSpec(label, item)
			if err != nil {
				return nil, err
			}
			spec.Networks[label] = network
		}
	}
	return spec, nil
}

func systemdResolvedSpec(systemd parser.Value) (*ir.SystemdResolvedSpec, error) {
	resolved, ok, err := mapField(systemd, "resolved")
	if err != nil || !ok {
		return nil, err
	}
	ensure, err := ensureField(resolved, "present")
	if err != nil {
		return nil, err
	}
	resolve, err := systemdSectionField(resolved, "resolve")
	if err != nil {
		return nil, err
	}
	if ensure == "present" && len(resolve) == 0 {
		return nil, fmt.Errorf("%s:%d:%s.resolve: systemd.resolved requires resolve section when ensure is present", resolved.Source.File, resolved.Source.Line, resolved.Source.Path)
	}
	unit, err := systemdDropInUnit(resolved, "systemd-resolved", "/etc/systemd/resolved.conf.d/debianform.conf", "Resolve", resolve, ensure)
	if err != nil {
		return nil, err
	}
	var enable *bool
	if value, ok, err := boolField(resolved, "enable"); err != nil {
		return nil, err
	} else if ok {
		enable = &value
	}
	state, _, err := stringField(resolved, "state")
	if err != nil {
		return nil, err
	}
	if state != "" && !stringIn(state, "running", "stopped", "restarted", "reloaded") {
		return nil, fmt.Errorf("%s:%d:%s.state: systemd.resolved state must be running, stopped, restarted, or reloaded", resolved.Source.File, resolved.Source.Line, resolved.Source.Path)
	}
	return &ir.SystemdResolvedSpec{Unit: unit, Resolve: resolve, Enable: enable, State: state, Source: resolved.Source}, nil
}

func systemdJournaldSpec(systemd parser.Value) (*ir.SystemdJournaldSpec, error) {
	journald, ok, err := mapField(systemd, "journald")
	if err != nil || !ok {
		return nil, err
	}
	ensure, err := ensureField(journald, "present")
	if err != nil {
		return nil, err
	}
	journal, err := systemdSectionField(journald, "journal")
	if err != nil {
		return nil, err
	}
	if ensure == "present" && len(journal) == 0 {
		return nil, fmt.Errorf("%s:%d:%s.journal: systemd.journald requires journal section when ensure is present", journald.Source.File, journald.Source.Line, journald.Source.Path)
	}
	unit, err := systemdDropInUnit(journald, "systemd-journald", "/etc/systemd/journald.conf.d/debianform.conf", "Journal", journal, ensure)
	if err != nil {
		return nil, err
	}
	state, _, err := stringField(journald, "state")
	if err != nil {
		return nil, err
	}
	if state != "" && !stringIn(state, "running", "stopped", "restarted", "reloaded") {
		return nil, fmt.Errorf("%s:%d:%s.state: systemd.journald state must be running, stopped, restarted, or reloaded", journald.Source.File, journald.Source.Line, journald.Source.Path)
	}
	return &ir.SystemdJournaldSpec{Unit: unit, Journal: journal, State: state, Source: journald.Source}, nil
}

func networkdNetDevSpec(label string, item parser.Value) (ir.NetworkdNetDev, error) {
	path, err := networkdFilePath(item, "path", "/etc/systemd/network/"+label+".netdev")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	sections, hasSections, sectionsSensitive, err := networkdGenericSections(item)
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	raw, err := networkdRawFile(item)
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	netdev, err := networkdSectionField(item, "netdev")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	wireguard, err := networkdSectionField(item, "wireguard")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	if _, ok := wireguard["PrivateKey"]; ok {
		return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.wireguard.PrivateKey: use PrivateKeyFile instead of inline PrivateKey", item.Source.File, item.Source.Line, item.Source.Path)
	}
	peers, err := networkdSectionCollection(item, "wireguard_peer")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	for _, label := range sortedKeys(peers) {
		if _, ok := peers[label]["PresharedKey"]; ok {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.wireguard_peer[%q].PresharedKey: use PresharedKeyFile instead of inline PresharedKey", item.Source.File, item.Source.Line, item.Source.Path, label)
		}
	}
	hasCompatibility := hasNetworkdCompatibilityFields(item, "netdev", "wireguard", "wireguard_peer")
	if raw.Present && (hasSections || hasCompatibility) {
		return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s: networkd netdev raw content/source is mutually exclusive with section blocks and compatibility attributes", item.Source.File, item.Source.Line, item.Source.Path)
	}
	if hasSections && hasCompatibility {
		return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s: networkd netdev section blocks are mutually exclusive with compatibility attributes", item.Source.File, item.Source.Line, item.Source.Path)
	}
	name := ""
	if raw.Present {
		name, err = validateRawNetworkdNetDev(raw.ValidationContent, item.Source)
		if err != nil {
			return ir.NetworkdNetDev{}, err
		}
	} else if hasSections {
		name, err = validateGenericNetworkdNetDev(sections, item.Source)
		if err != nil {
			return ir.NetworkdNetDev{}, err
		}
	} else if ensure == "present" {
		if len(netdev) == 0 {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.netdev: networkd netdev requires netdev section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if kind := firstSectionValue(netdev, "Kind"); kind == "" {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.netdev.Kind: networkd netdev requires Kind", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if name := firstSectionValue(netdev, "Name"); name == "" {
			return ir.NetworkdNetDev{}, fmt.Errorf("%s:%d:%s.netdev.Name: networkd netdev requires Name", item.Source.File, item.Source.Line, item.Source.Path)
		}
	}
	activation, err := networkdActivationSpec(item)
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	sensitive := raw.Sensitive || sectionsSensitive
	defaultMode := "0644"
	if sensitive {
		defaultMode = "0600"
	}
	mode, err := modeFieldDefault(item, "mode", defaultMode)
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.NetworkdNetDev{}, err
	}
	out := ir.NetworkdNetDev{
		Label:          label,
		Path:           path,
		NetDev:         netdev,
		WireGuard:      wireguard,
		WireGuardPeers: peers,
		Sections:       sections,
		SourcePath:     raw.SourcePath,
		Owner:          owner,
		Group:          group,
		Mode:           mode,
		Sensitive:      sensitive,
		Ensure:         ensure,
		Activation:     activation,
		Lifecycle:      lifecycle,
		Source:         item.Source,
	}
	if hasSections {
		out.Name = name
		out.ContentMode = "structured"
	}
	if raw.Present {
		out.Name = name
		out.ContentMode = "raw"
		out.Content = raw.Content
		out.Summary = raw.Summary
	}
	if ensure == "present" && !raw.Present {
		content, err := renderNetworkdNetDev(out)
		if err != nil {
			return ir.NetworkdNetDev{}, err
		}
		out.Content = content
		out.Summary = contentSummary([]byte(content))
	}
	return out, nil
}

func networkdNetworkSpec(label string, item parser.Value) (ir.NetworkdNetwork, error) {
	path, err := networkdFilePath(item, "path", "/etc/systemd/network/"+label+".network")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	sections, hasSections, sectionsSensitive, err := networkdGenericSections(item)
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	raw, err := networkdRawFile(item)
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	match, err := networkdSectionField(item, "match")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	network, err := networkdSectionField(item, "network")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	hasCompatibility := hasNetworkdCompatibilityFields(item, "match", "network")
	if raw.Present && (hasSections || hasCompatibility) {
		return ir.NetworkdNetwork{}, fmt.Errorf("%s:%d:%s: networkd network raw content/source is mutually exclusive with section blocks and compatibility attributes", item.Source.File, item.Source.Line, item.Source.Path)
	}
	if hasSections && hasCompatibility {
		return ir.NetworkdNetwork{}, fmt.Errorf("%s:%d:%s: networkd network section blocks are mutually exclusive with compatibility attributes", item.Source.File, item.Source.Line, item.Source.Path)
	}
	if ensure == "present" && !raw.Present && !hasSections {
		if len(match) == 0 {
			return ir.NetworkdNetwork{}, fmt.Errorf("%s:%d:%s.match: networkd network requires match section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if len(network) == 0 {
			return ir.NetworkdNetwork{}, fmt.Errorf("%s:%d:%s.network: networkd network requires network section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
	}
	activation, err := networkdActivationSpec(item)
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	sensitive := raw.Sensitive || sectionsSensitive
	defaultMode := "0644"
	if sensitive {
		defaultMode = "0600"
	}
	mode, err := modeFieldDefault(item, "mode", defaultMode)
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.NetworkdNetwork{}, err
	}
	out := ir.NetworkdNetwork{
		Label:      label,
		Path:       path,
		Match:      match,
		Network:    network,
		Sections:   sections,
		SourcePath: raw.SourcePath,
		Owner:      owner,
		Group:      group,
		Mode:       mode,
		Sensitive:  sensitive,
		Ensure:     ensure,
		Activation: activation,
		Lifecycle:  lifecycle,
		Source:     item.Source,
	}
	if hasSections {
		out.ContentMode = "structured"
	}
	if raw.Present {
		out.ContentMode = "raw"
		out.Content = raw.Content
		out.Summary = raw.Summary
	}
	if ensure == "present" && !raw.Present {
		content, err := renderNetworkdNetwork(out)
		if err != nil {
			return ir.NetworkdNetwork{}, err
		}
		out.Content = content
		out.Summary = contentSummary([]byte(content))
	}
	return out, nil
}

type networkdRawFileSpec struct {
	Present           bool
	Content           string
	SourcePath        string
	Sensitive         bool
	Summary           ir.ContentSummary
	ValidationContent string
}

func networkdRawFile(item parser.Value) (networkdRawFileSpec, error) {
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return networkdRawFileSpec{}, err
	}
	if hasContent {
		if err := rejectEphemeralValue(item.Map["content"]); err != nil {
			return networkdRawFileSpec{}, err
		}
	}
	source, hasSource, err := stringField(item, "source")
	if err != nil {
		return networkdRawFileSpec{}, err
	}
	if hasContent && hasSource {
		return networkdRawFileSpec{}, fmt.Errorf("%s:%d:%s: networkd resource accepts only one of content or source", item.Source.File, item.Source.Line, item.Source.Path)
	}
	if !hasContent && !hasSource {
		return networkdRawFileSpec{}, nil
	}
	sensitive, ok, err := boolField(item, "sensitive")
	if err != nil {
		return networkdRawFileSpec{}, err
	}
	if !ok {
		sensitive = false
	}
	if hasContent && contentNeedsRedaction(item.Map["content"]) {
		sensitive = true
	}
	if hasSource && item.Map["source"].ContainsSensitive() {
		sensitive = true
	}
	out := networkdRawFileSpec{Present: true, Content: content, Sensitive: sensitive}
	if hasContent {
		out.ValidationContent = content
		out.Summary = contentSummary([]byte(content))
		return out, nil
	}
	out.SourcePath = resolvePath(item.Source.File, source)
	data, err := os.ReadFile(out.SourcePath)
	if err != nil {
		return networkdRawFileSpec{}, fmt.Errorf("%s:%d:%s: read source %q: %w", item.Source.File, item.Source.Line, item.Source.Path, out.SourcePath, err)
	}
	out.ValidationContent = string(data)
	out.Summary = contentSummary(data)
	return out, nil
}

func hasNetworkdCompatibilityFields(item parser.Value, names ...string) bool {
	for _, name := range names {
		if _, ok := item.Map[name]; ok {
			return true
		}
	}
	return false
}

func networkdGenericSections(item parser.Value) ([]ir.NetworkdNamedSection, bool, bool, error) {
	collection, ok := item.Map["section"]
	if !ok {
		return nil, false, false, nil
	}
	if !collection.IsMap() {
		return nil, false, false, fmt.Errorf("%s:%d:%s: networkd section collection must be a map", collection.Source.File, collection.Source.Line, collection.Source.Path)
	}
	order := append([]string(nil), collection.Order...)
	seen := map[string]struct{}{}
	for _, label := range order {
		seen[label] = struct{}{}
	}
	for _, label := range sortedKeys(collection.Map) {
		if _, exists := seen[label]; !exists {
			order = append(order, label)
		}
	}
	sections := make([]ir.NetworkdNamedSection, 0, len(collection.Map))
	sensitive := false
	for _, label := range order {
		section := collection.Map[label]
		if label == "" {
			return nil, false, false, fmt.Errorf("%s:%d:%s: networkd section identity must be non-empty", section.Source.File, section.Source.Line, section.Source.Path)
		}
		name, hasName, err := stringField(section, "name")
		if err != nil {
			return nil, false, false, err
		}
		if !hasName || !validNetworkdIdentifier(name) {
			return nil, false, false, fmt.Errorf("%s:%d:%s.name: networkd section name %q is invalid", section.Source.File, section.Source.Line, section.Source.Path, name)
		}
		settingsValue, hasSettings, err := mapField(section, "settings")
		if err != nil {
			return nil, false, false, err
		}
		if !hasSettings {
			return nil, false, false, fmt.Errorf("%s:%d:%s.settings: networkd section settings map is required", section.Source.File, section.Source.Line, section.Source.Path)
		}
		if settingsValue.ContainsEphemeral() {
			return nil, false, false, fmt.Errorf("%s:%d:%s: ephemeral value is not allowed in networkd section settings", settingsValue.Source.File, settingsValue.Source.Line, settingsValue.Source.Path)
		}
		for _, key := range sortedKeys(settingsValue.Map) {
			value := settingsValue.Map[key]
			if !validNetworkdIdentifier(key) {
				return nil, false, false, fmt.Errorf("%s:%d:%s: networkd section key %q is invalid", value.Source.File, value.Source.Line, value.Source.Path, key)
			}
			if (key == "PrivateKey" || key == "PresharedKey") && !value.ContainsSensitive() {
				return nil, false, false, fmt.Errorf("%s:%d:%s: inline %s must use a sensitive value or its file-backed directive", value.Source.File, value.Source.Line, value.Source.Path, key)
			}
		}
		settings, err := networkdSection(settingsValue)
		if err != nil {
			return nil, false, false, err
		}
		sensitive = sensitive || settingsValue.ContainsSensitive()
		sections = append(sections, ir.NetworkdNamedSection{Identity: label, Name: name, Settings: settings, Source: section.Source})
	}
	return sections, true, sensitive, nil
}

func validNetworkdIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '.' || char == ':' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func networkdActivationSpec(item parser.Value) (*ir.NetworkdActivationSpec, error) {
	activation, ok, err := mapField(item, "activation")
	if err != nil || !ok {
		return nil, err
	}
	reconfigure, err := stringListField(activation, "reconfigure")
	if err != nil {
		return nil, err
	}
	for i, name := range reconfigure {
		if err := validateSystemdSingleLine(name, activation.Map["reconfigure"].List[i].Source); err != nil {
			return nil, err
		}
	}
	var postReload *ir.ScriptReferenceSpec
	if value, exists := activation.Map["post_reload"]; exists {
		name, _, err := stringField(activation, "post_reload")
		if err != nil {
			return nil, err
		}
		scope := string(parser.ScriptReferenceAuto)
		if value.ScriptReference != nil {
			scope = string(value.ScriptReference.Scope)
			name = value.ScriptReference.Name
		}
		postReload = &ir.ScriptReferenceSpec{Name: name, Scope: scope, Source: value.Source}
	}
	if len(reconfigure) == 0 && postReload == nil {
		return nil, fmt.Errorf("%s:%d:%s: networkd activation requires reconfigure or post_reload", activation.Source.File, activation.Source.Line, activation.Source.Path)
	}
	return &ir.NetworkdActivationSpec{Reconfigure: reconfigure, PostReload: postReload, Source: activation.Source}, nil
}

func validateNetworkdActivationRefs(spec *ir.NetworkdSpec, localScripts, rootScripts map[string]ir.ComponentScriptSpec) error {
	if spec == nil {
		return nil
	}
	for _, label := range sortedKeys(spec.NetDevs) {
		item := spec.NetDevs[label]
		if err := resolveNetworkdActivationRef(item.Activation, localScripts, rootScripts); err != nil {
			return err
		}
		spec.NetDevs[label] = item
	}
	for _, label := range sortedKeys(spec.Networks) {
		item := spec.Networks[label]
		if err := resolveNetworkdActivationRef(item.Activation, localScripts, rootScripts); err != nil {
			return err
		}
		spec.Networks[label] = item
	}
	return nil
}

func resolveNetworkdActivationRef(activation *ir.NetworkdActivationSpec, localScripts, rootScripts map[string]ir.ComponentScriptSpec) error {
	if activation == nil || activation.PostReload == nil {
		return nil
	}
	ref := *activation.PostReload
	if ref.Scope != string(parser.ScriptReferenceGlobal) {
		if script, ok := localScripts[ref.Name]; ok {
			if err := validateNetworkdPostReloadScript(script, ref.Source); err != nil {
				return err
			}
			ref.Scope = "component"
			ref.DeclarationID = script.DeclarationID
			activation.PostReload = &ref
			return nil
		}
	}
	if script, ok := rootScripts[ref.Name]; ok {
		if err := validateNetworkdPostReloadScript(script, ref.Source); err != nil {
			return err
		}
		ref.Scope = "root"
		ref.DeclarationID = script.DeclarationID
		activation.PostReload = &ref
		return nil
	}
	traversal := "script." + ref.Name
	if ref.Scope == string(parser.ScriptReferenceGlobal) {
		traversal = "global.script." + ref.Name
	}
	return fmt.Errorf("%s:%d:%s: networkd activation.post_reload references unknown %s", ref.Source.File, ref.Source.Line, ref.Source.Path, traversal)
}

func validateNetworkdPostReloadScript(script ir.ComponentScriptSpec, source ir.SourceRef) error {
	if script.Mode != "once" {
		return fmt.Errorf("%s:%d:%s: networkd activation.post_reload requires script mode once", source.File, source.Line, source.Path)
	}
	if len(script.Outputs) != 0 {
		return fmt.Errorf("%s:%d:%s: networkd activation.post_reload does not support script outputs", source.File, source.Line, source.Path)
	}
	return nil
}

func validateGenericNetworkdNetDev(sections []ir.NetworkdNamedSection, source ir.SourceRef) (string, error) {
	name := ""
	found := 0
	for _, section := range sections {
		if section.Name != "NetDev" {
			continue
		}
		found++
		name = firstSectionValue(section.Settings, "Name")
		if firstSectionValue(section.Settings, "Kind") == "" {
			return "", fmt.Errorf("%s:%d:%s: networkd netdev [NetDev] section requires Kind", section.Source.File, section.Source.Line, section.Source.Path)
		}
		if name == "" {
			return "", fmt.Errorf("%s:%d:%s: networkd netdev [NetDev] section requires Name", section.Source.File, section.Source.Line, section.Source.Path)
		}
	}
	if found != 1 {
		return "", fmt.Errorf("%s:%d:%s: networkd netdev requires exactly one NetDev section", source.File, source.Line, source.Path)
	}
	return name, nil
}

func validateRawNetworkdNetDev(content string, source ir.SourceRef) (string, error) {
	inNetDev := false
	name := ""
	kind := ""
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inNetDev = line == "[NetDev]"
			continue
		}
		if !inNetDev {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Name":
			name = strings.TrimSpace(value)
		case "Kind":
			kind = strings.TrimSpace(value)
		}
	}
	if name == "" || kind == "" {
		return "", fmt.Errorf("%s:%d:%s: raw networkd netdev requires [NetDev] Name and Kind", source.File, source.Line, source.Path)
	}
	if err := validateSystemdSingleLine(name, source); err != nil {
		return "", err
	}
	return name, nil
}

func networkdFilePath(item parser.Value, field string, fallback string) (string, error) {
	path, ok, err := stringField(item, field)
	if err != nil {
		return "", err
	}
	if !ok || path == "" {
		path = fallback
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s:%d:%s.%s: networkd file path must be absolute", item.Source.File, item.Source.Line, item.Source.Path, field)
	}
	return path, nil
}

func networkdSectionField(root parser.Value, name string) (ir.NetworkdSection, error) {
	section, ok, err := mapField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	return networkdSection(section)
}

func systemdSectionField(root parser.Value, name string) (ir.NetworkdSection, error) {
	section, ok, err := mapField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	return systemdSection(section)
}

func networkdSectionCollection(root parser.Value, name string) (map[string]ir.NetworkdSection, error) {
	objects, ok, err := objectCollection(root, name)
	if err != nil || !ok {
		return map[string]ir.NetworkdSection{}, err
	}
	out := make(map[string]ir.NetworkdSection, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		if label == "" {
			return nil, fmt.Errorf("%s:%d:%s: networkd %s label must be non-empty", item.Source.File, item.Source.Line, item.Source.Path, name)
		}
		section, err := networkdSection(item)
		if err != nil {
			return nil, err
		}
		out[label] = section
	}
	return out, nil
}

func networkdSection(root parser.Value) (ir.NetworkdSection, error) {
	if !root.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: networkd section must be a map", root.Source.File, root.Source.Line, root.Source.Path)
	}
	out := ir.NetworkdSection{}
	for _, key := range sortedKeys(root.Map) {
		item := root.Map[key]
		if item.Kind == parser.KindNull {
			continue
		}
		values, err := networkdSectionValues(item)
		if err != nil {
			return nil, err
		}
		out[key] = values
	}
	return out, nil
}

func systemdSection(root parser.Value) (ir.NetworkdSection, error) {
	if !root.IsMap() {
		return nil, fmt.Errorf("%s:%d:%s: systemd section must be a map", root.Source.File, root.Source.Line, root.Source.Path)
	}
	out := ir.NetworkdSection{}
	for _, key := range sortedKeys(root.Map) {
		item := root.Map[key]
		if item.Kind == parser.KindNull {
			continue
		}
		values, err := networkdSectionValues(item)
		if err != nil {
			return nil, err
		}
		out[key] = values
	}
	return out, nil
}

func networkdSectionValues(value parser.Value) ([]string, error) {
	if value.IsList() {
		out := make([]string, 0, len(value.List))
		for _, item := range value.List {
			str, err := networkdScalarValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, str)
		}
		return out, nil
	}
	str, err := networkdScalarValue(value)
	if err != nil {
		return nil, err
	}
	return []string{str}, nil
}

func networkdScalarValue(value parser.Value) (string, error) {
	if value.ContainsEphemeral() {
		return "", fmt.Errorf("%s:%d:%s: ephemeral value is not allowed in networkd section settings", value.Source.File, value.Source.Line, value.Source.Path)
	}
	switch value.Kind {
	case parser.KindString:
		if err := validateSystemdSingleLine(value.String, value.Source); err != nil {
			return "", err
		}
		return value.String, nil
	case parser.KindNumber:
		if err := validateSystemdSingleLine(value.Number, value.Source); err != nil {
			return "", err
		}
		return value.Number, nil
	case parser.KindBool:
		if value.Bool {
			return "yes", nil
		}
		return "no", nil
	default:
		return "", fmt.Errorf("%s:%d:%s: networkd section values must be strings, numbers, booleans, or lists of those", value.Source.File, value.Source.Line, value.Source.Path)
	}
}

func renderNetworkdNetDev(item ir.NetworkdNetDev) (string, error) {
	var lines []string
	if len(item.Sections) > 0 {
		if err := appendNetworkdNamedSections(&lines, item.Sections); err != nil {
			return "", err
		}
		return strings.Join(lines, "\n") + "\n", nil
	}
	if err := appendNetworkdSection(&lines, "NetDev", item.NetDev, item.Source); err != nil {
		return "", err
	}
	if len(item.WireGuard) > 0 {
		if err := appendNetworkdSection(&lines, "WireGuard", item.WireGuard, item.Source); err != nil {
			return "", err
		}
	}
	for _, label := range sortedKeys(item.WireGuardPeers) {
		if err := appendNetworkdSection(&lines, "WireGuardPeer", item.WireGuardPeers[label], item.Source); err != nil {
			return "", err
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func renderNetworkdNetwork(item ir.NetworkdNetwork) (string, error) {
	var lines []string
	if len(item.Sections) > 0 {
		if err := appendNetworkdNamedSections(&lines, item.Sections); err != nil {
			return "", err
		}
		return strings.Join(lines, "\n") + "\n", nil
	}
	if err := appendNetworkdSection(&lines, "Match", item.Match, item.Source); err != nil {
		return "", err
	}
	if err := appendNetworkdSection(&lines, "Network", item.Network, item.Source); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func appendNetworkdNamedSections(lines *[]string, sections []ir.NetworkdNamedSection) error {
	for _, section := range sections {
		if !validNetworkdIdentifier(section.Name) {
			return fmt.Errorf("%s:%d:%s: networkd section name %q is invalid", section.Source.File, section.Source.Line, section.Source.Path, section.Name)
		}
		if err := appendNetworkdSection(lines, section.Name, section.Settings, section.Source); err != nil {
			return err
		}
	}
	return nil
}

func appendNetworkdSection(lines *[]string, name string, values ir.NetworkdSection, source ir.SourceRef) error {
	if len(*lines) > 0 {
		*lines = append(*lines, "")
	}
	*lines = append(*lines, "["+name+"]")
	for _, key := range sortedKeys(values) {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00\n\r") {
			return fmt.Errorf("%s:%d:%s: networkd section key %q is invalid", source.File, source.Line, source.Path, key)
		}
		for _, value := range values[key] {
			if err := validateSystemdSingleLine(value, source); err != nil {
				return err
			}
			*lines = append(*lines, key+"="+value)
		}
	}
	return nil
}

func appendSystemdSection(lines *[]string, name string, values ir.NetworkdSection, source ir.SourceRef) error {
	if name != "" {
		if len(*lines) > 0 {
			*lines = append(*lines, "")
		}
		*lines = append(*lines, "["+name+"]")
	}
	for _, key := range sortedKeys(values) {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "=\x00\n\r") {
			return fmt.Errorf("%s:%d:%s: systemd section key %q is invalid", source.File, source.Line, source.Path, key)
		}
		for _, value := range values[key] {
			if err := validateSystemdSingleLine(value, source); err != nil {
				return err
			}
			*lines = append(*lines, key+"="+value)
		}
	}
	return nil
}

func systemdSectionNeedsRedaction(root parser.Value, name string) bool {
	value, ok := root.Map[name]
	return ok && contentNeedsRedaction(value)
}

func systemdAnySectionNeedsRedaction(root parser.Value) bool {
	for _, name := range []string{"resolve", "journal", "timer", "install", "service_config"} {
		if systemdSectionNeedsRedaction(root, name) {
			return true
		}
	}
	return false
}

func firstSectionValue(section ir.NetworkdSection, key string) string {
	values := section[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func addSystemdUnit(units map[string]ir.SystemdUnit, unit ir.SystemdUnit) error {
	if previous, exists := units[unit.Name]; exists {
		return fmt.Errorf("%s:%d:%s: systemd unit %q conflicts with unit declared at %s:%d:%s", unit.Source.File, unit.Source.Line, unit.Source.Path, unit.Name, previous.Source.File, previous.Source.Line, previous.Source.Path)
	}
	units[unit.Name] = unit
	return nil
}

func systemdUnitSpec(name string, item parser.Value) (ir.SystemdUnit, error) {
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	if ensure == "present" && hasContent == hasSource {
		return ir.SystemdUnit{}, fmt.Errorf("%s:%d:%s: systemd.unit requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
	}
	sensitive := hasContent && contentNeedsRedaction(item.Map["content"])
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	group, err := stringFieldDefault(item, "group", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	unit := ir.SystemdUnit{
		Name:       name,
		Path:       "/etc/systemd/system/" + name,
		Content:    content,
		SourcePath: resolvePath(item.Source.File, sourcePath),
		Owner:      owner,
		Group:      group,
		Mode:       mode,
		Sensitive:  sensitive,
		Ensure:     ensure,
		Lifecycle:  lifecycle,
		Source:     item.Source,
	}
	if hasContent {
		unit.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(unit.SourcePath, item.Source)
		if err != nil {
			return ir.SystemdUnit{}, err
		}
		unit.Summary = summary
	}
	return unit, nil
}

func systemdServiceUnitSpec(name string, item parser.Value) (ir.SystemdUnit, error) {
	unitName := serviceUnitName(name)
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	content, hasContent, err := stringFieldAllowEphemeral(item, "content")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	sourcePath, hasSource, err := stringField(item, "source")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	hasRawUnit := hasContent || hasSource
	rawContentSensitive := hasContent && contentNeedsRedaction(item.Map["content"])
	hasStructuredFields := systemdServiceUnitHasStructuredFields(item)
	if hasRawUnit {
		if ensure == "present" && hasContent == hasSource {
			return ir.SystemdUnit{}, fmt.Errorf("%s:%d:%s: systemd.service_unit requires exactly one of content or source when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if hasStructuredFields {
			return ir.SystemdUnit{}, fmt.Errorf("%s:%d:%s: systemd.service_unit cannot combine content/source with structured service fields", item.Source.File, item.Source.Line, item.Source.Path)
		}
	} else if ensure == "present" {
		content, err = renderSystemdServiceUnit(unitName, item)
		if err != nil {
			return ir.SystemdUnit{}, err
		}
		hasContent = true
	}
	sensitive := rawContentSensitive || systemdServiceUnitStructuredSensitive(item)
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	group, err := stringFieldDefault(item, "file_group", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	unit := ir.SystemdUnit{
		Name:       unitName,
		Path:       "/etc/systemd/system/" + unitName,
		Content:    content,
		SourcePath: resolvePath(item.Source.File, sourcePath),
		Owner:      owner,
		Group:      group,
		Mode:       mode,
		Sensitive:  sensitive,
		Ensure:     ensure,
		Lifecycle:  lifecycle,
		Source:     item.Source,
	}
	if hasContent {
		unit.Summary = contentSummary([]byte(content))
	} else if hasSource {
		summary, err := fileSummary(unit.SourcePath, item.Source)
		if err != nil {
			return ir.SystemdUnit{}, err
		}
		unit.Summary = summary
	}
	return unit, nil
}

func systemdServiceUnitHasStructuredFields(item parser.Value) bool {
	for _, name := range []string{
		"description", "run", "type", "user", "group", "working_dir",
		"environment", "restart", "restart_delay", "wants", "after",
		"wanted_by", "stdout", "stderr", "service_config",
	} {
		if _, ok := item.Map[name]; ok {
			return true
		}
	}
	return false
}

func systemdServiceUnitStructuredSensitive(item parser.Value) bool {
	for _, name := range []string{
		"description", "run", "type", "user", "group", "working_dir",
		"environment", "restart", "restart_delay", "wants", "after",
		"wanted_by", "stdout", "stderr", "service_config",
	} {
		if value, ok := item.Map[name]; ok && contentNeedsRedaction(value) {
			return true
		}
	}
	return false
}

func contentNeedsRedaction(value parser.Value) bool {
	return value.ContainsSensitive() || value.ContainsEphemeral()
}

func renderSystemdServiceUnit(unitName string, item parser.Value) (string, error) {
	run, ok, err := systemdRunCommandField(item, "run")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s:%d:%s.run: systemd.service_unit requires run unless content or source is used", item.Source.File, item.Source.Line, item.Source.Path)
	}
	description, ok, err := stringField(item, "description")
	if err != nil {
		return "", err
	}
	if !ok {
		description = strings.TrimSuffix(unitName, ".service")
	} else if strings.TrimSpace(description) == "" {
		return "", fmt.Errorf("%s:%d:%s.description: must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
	}
	unitLines := []string{}
	if err := appendSystemdAssignment(&unitLines, "Description", description, item.Source); err != nil {
		return "", err
	}
	wants, err := stringListField(item, "wants")
	if err != nil {
		return "", err
	}
	if len(wants) > 0 {
		value, err := systemdUnitListValue(wants, item.Map["wants"].Source)
		if err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&unitLines, "Wants", value, item.Map["wants"].Source); err != nil {
			return "", err
		}
	}
	after, err := stringListField(item, "after")
	if err != nil {
		return "", err
	}
	if len(after) > 0 {
		value, err := systemdUnitListValue(after, item.Map["after"].Source)
		if err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&unitLines, "After", value, item.Map["after"].Source); err != nil {
			return "", err
		}
	}

	serviceLines := []string{}
	if value, ok, err := stringField(item, "type"); err != nil {
		return "", err
	} else if ok {
		if err := validateSystemdServiceType(value, item.Map["type"].Source); err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&serviceLines, "Type", value, item.Map["type"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "user"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.user: must be non-empty", item.Source.File, item.Map["user"].Source.Line, item.Map["user"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "User", value, item.Map["user"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "group"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.group: must be non-empty", item.Source.File, item.Map["group"].Source.Line, item.Map["group"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "Group", value, item.Map["group"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "working_dir"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.working_dir: must be non-empty", item.Source.File, item.Map["working_dir"].Source.Line, item.Map["working_dir"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "WorkingDirectory", value, item.Map["working_dir"].Source); err != nil {
			return "", err
		}
	}
	environment, err := stringMapField(item, "environment")
	if err != nil {
		return "", err
	}
	if len(environment) > 0 {
		for _, name := range sortedKeys(environment) {
			if !systemdEnvironmentNamePattern.MatchString(name) {
				source := item.Map["environment"].Map[name].Source
				return "", fmt.Errorf("%s:%d:%s: environment variable name %q is invalid", source.File, source.Line, source.Path, name)
			}
			value := environment[name]
			source := item.Map["environment"].Map[name].Source
			if err := validateSystemdSingleLine(value, source); err != nil {
				return "", err
			}
			serviceLines = append(serviceLines, "Environment="+systemdQuoteToken(name+"="+value))
		}
	}
	if err := appendSystemdAssignment(&serviceLines, "ExecStart", run, item.Map["run"].Source); err != nil {
		return "", err
	}
	if value, ok, err := stringField(item, "restart"); err != nil {
		return "", err
	} else if ok {
		if err := validateSystemdRestart(value, item.Map["restart"].Source); err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&serviceLines, "Restart", value, item.Map["restart"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "restart_delay"); err != nil {
		return "", err
	} else if ok {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s:%d:%s.restart_delay: must be non-empty", item.Source.File, item.Map["restart_delay"].Source.Line, item.Map["restart_delay"].Source.Path)
		}
		if err := appendSystemdAssignment(&serviceLines, "RestartSec", value, item.Map["restart_delay"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "stdout"); err != nil {
		return "", err
	} else if ok {
		if err := appendSystemdAssignment(&serviceLines, "StandardOutput", value, item.Map["stdout"].Source); err != nil {
			return "", err
		}
	}
	if value, ok, err := stringField(item, "stderr"); err != nil {
		return "", err
	} else if ok {
		if err := appendSystemdAssignment(&serviceLines, "StandardError", value, item.Map["stderr"].Source); err != nil {
			return "", err
		}
	}
	serviceConfig, err := systemdSectionField(item, "service_config")
	if err != nil {
		return "", err
	}
	if len(serviceConfig) > 0 {
		if err := appendSystemdSection(&serviceLines, "", serviceConfig, item.Map["service_config"].Source); err != nil {
			return "", err
		}
	}

	wantedBy, hasWantedBy, err := optionalStringListField(item, "wanted_by")
	if err != nil {
		return "", err
	}
	wantedBySource := item.Source
	if !hasWantedBy {
		wantedBy = []string{"multi-user.target"}
	} else {
		wantedBySource = item.Map["wanted_by"].Source
	}
	installLines := []string{}
	if len(wantedBy) > 0 {
		value, err := systemdUnitListValue(wantedBy, wantedBySource)
		if err != nil {
			return "", err
		}
		if err := appendSystemdAssignment(&installLines, "WantedBy", value, wantedBySource); err != nil {
			return "", err
		}
	}

	lines := []string{"[Unit]"}
	lines = append(lines, unitLines...)
	lines = append(lines, "", "[Service]")
	lines = append(lines, serviceLines...)
	if len(installLines) > 0 {
		lines = append(lines, "", "[Install]")
		lines = append(lines, installLines...)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func systemdTimerSpec(name string, item parser.Value) (ir.SystemdTimer, error) {
	timerName := timerUnitName(name)
	ensure, err := ensureField(item, "present")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	timerSection, err := systemdSectionField(item, "timer")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if ensure == "present" && len(timerSection) == 0 {
		return ir.SystemdTimer{}, fmt.Errorf("%s:%d:%s.timer: systemd.timer requires timer section when ensure is present", item.Source.File, item.Source.Line, item.Source.Path)
	}
	install, err := systemdSectionField(item, "install")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if install == nil {
		install = ir.NetworkdSection{}
	}
	wantedBy, hasWantedBy, err := optionalStringListField(item, "wanted_by")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if !hasWantedBy {
		wantedBy = []string{"timers.target"}
	}
	if len(wantedBy) > 0 {
		install["WantedBy"] = append([]string(nil), wantedBy...)
	}
	content := ""
	if ensure == "present" {
		description, ok, err := stringField(item, "description")
		if err != nil {
			return ir.SystemdTimer{}, err
		}
		if !ok {
			description = strings.TrimSuffix(timerName, ".timer")
		} else if strings.TrimSpace(description) == "" {
			return ir.SystemdTimer{}, fmt.Errorf("%s:%d:%s.description: must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		content, err = renderSystemdTimerUnit(description, timerSection, install, item.Source)
		if err != nil {
			return ir.SystemdTimer{}, err
		}
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	group, err := stringFieldDefault(item, "file_group", "root")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	var enable *bool
	if value, ok, err := boolField(item, "enable"); err != nil {
		return ir.SystemdTimer{}, err
	} else if ok {
		enable = &value
	}
	state, _, err := stringField(item, "state")
	if err != nil {
		return ir.SystemdTimer{}, err
	}
	if state != "" && !stringIn(state, "running", "stopped") {
		return ir.SystemdTimer{}, fmt.Errorf("%s:%d:%s.state: systemd.timer state must be running or stopped", item.Source.File, item.Source.Line, item.Source.Path)
	}
	unit := ir.SystemdUnit{
		Name:      timerName,
		Path:      "/etc/systemd/system/" + timerName,
		Content:   content,
		Owner:     owner,
		Group:     group,
		Mode:      mode,
		Sensitive: systemdSectionNeedsRedaction(item, "timer") || systemdSectionNeedsRedaction(item, "install"),
		Ensure:    ensure,
		Lifecycle: lifecycle,
		Source:    item.Source,
	}
	if content != "" {
		unit.Summary = contentSummary([]byte(content))
	}
	return ir.SystemdTimer{Name: timerName, Unit: unit, Timer: timerSection, Install: install, Enable: enable, State: state, Source: item.Source}, nil
}

func renderSystemdTimerUnit(description string, timer ir.NetworkdSection, install ir.NetworkdSection, source ir.SourceRef) (string, error) {
	lines := []string{"[Unit]"}
	if err := appendSystemdAssignment(&lines, "Description", description, source); err != nil {
		return "", err
	}
	if err := appendSystemdSection(&lines, "Timer", timer, source); err != nil {
		return "", err
	}
	if len(install) > 0 {
		if err := appendSystemdSection(&lines, "Install", install, source); err != nil {
			return "", err
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func systemdDropInUnit(item parser.Value, name string, path string, sectionName string, section ir.NetworkdSection, ensure string) (ir.SystemdUnit, error) {
	content := ""
	if ensure == "present" {
		lines := []string{}
		if err := appendSystemdSection(&lines, sectionName, section, item.Source); err != nil {
			return ir.SystemdUnit{}, err
		}
		content = strings.Join(lines, "\n") + "\n"
	}
	owner, err := stringFieldDefault(item, "owner", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	group, err := stringFieldDefault(item, "file_group", "root")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	mode, err := modeFieldDefault(item, "mode", "0644")
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	lifecycle, err := lifecycleSpec(item)
	if err != nil {
		return ir.SystemdUnit{}, err
	}
	unit := ir.SystemdUnit{
		Name:      name,
		Path:      path,
		Content:   content,
		Owner:     owner,
		Group:     group,
		Mode:      mode,
		Sensitive: systemdAnySectionNeedsRedaction(item),
		Ensure:    ensure,
		Lifecycle: lifecycle,
		Source:    item.Source,
	}
	if content != "" {
		unit.Summary = contentSummary([]byte(content))
	}
	return unit, nil
}

func systemdRunCommandField(root parser.Value, name string) (string, bool, error) {
	value, ok := root.Map[name]
	if !ok {
		return "", false, nil
	}
	if err := rejectEphemeralValue(value); err != nil {
		return "", false, err
	}
	if str, ok := value.StringValue(); ok {
		if strings.TrimSpace(str) == "" {
			return "", false, fmt.Errorf("%s:%d:%s: must be a non-empty string or list of strings", value.Source.File, value.Source.Line, value.Source.Path)
		}
		if err := validateSystemdSingleLine(str, value.Source); err != nil {
			return "", false, err
		}
		return str, true, nil
	}
	if !value.IsList() {
		return "", false, fmt.Errorf("%s:%d:%s: must be a non-empty string or list of strings", value.Source.File, value.Source.Line, value.Source.Path)
	}
	if len(value.List) == 0 {
		return "", false, fmt.Errorf("%s:%d:%s: must be a non-empty string or list of strings", value.Source.File, value.Source.Line, value.Source.Path)
	}
	args := make([]string, 0, len(value.List))
	for _, item := range value.List {
		arg, ok := item.StringValue()
		if !ok || arg == "" {
			return "", false, fmt.Errorf("%s:%d:%s: run entries must be non-empty strings", item.Source.File, item.Source.Line, item.Source.Path)
		}
		if err := validateSystemdSingleLine(arg, item.Source); err != nil {
			return "", false, err
		}
		args = append(args, systemdQuoteToken(arg))
	}
	return strings.Join(args, " "), true, nil
}

func optionalStringListField(root parser.Value, name string) ([]string, bool, error) {
	if _, ok := root.Map[name]; !ok {
		return nil, false, nil
	}
	values, err := stringListField(root, name)
	if err != nil {
		return nil, false, err
	}
	return values, true, nil
}

func stringMapField(root parser.Value, name string) (map[string]string, error) {
	value, ok, err := mapField(root, name)
	if err != nil || !ok {
		return nil, err
	}
	if err := rejectEphemeralValue(value); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(value.Map))
	for _, key := range sortedKeys(value.Map) {
		item := value.Map[key]
		str, ok := item.StringValue()
		if !ok {
			return nil, fmt.Errorf("%s:%d:%s: map values must be strings", item.Source.File, item.Source.Line, item.Source.Path)
		}
		out[key] = str
	}
	return out, nil
}

func appendSystemdAssignment(lines *[]string, key string, value string, source ir.SourceRef) error {
	if err := validateSystemdSingleLine(value, source); err != nil {
		return err
	}
	*lines = append(*lines, key+"="+value)
	return nil
}

func validateSystemdSingleLine(value string, source ir.SourceRef) error {
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%s:%d:%s: systemd unit values must be single-line strings", source.File, source.Line, source.Path)
	}
	return nil
}

func systemdUnitListValue(values []string, source ir.SourceRef) (string, error) {
	for _, value := range values {
		if value == "" || strings.ContainsAny(value, " \t\n\r\x00") {
			return "", fmt.Errorf("%s:%d:%s: systemd unit names must be non-empty strings without whitespace", source.File, source.Line, source.Path)
		}
	}
	return strings.Join(values, " "), nil
}

func systemdQuoteToken(value string) string {
	if systemdSafeTokenPattern.MatchString(value) {
		return value
	}
	return strconv.Quote(value)
}

func validateSystemdServiceType(value string, source ir.SourceRef) error {
	switch value {
	case "simple", "exec", "forking", "oneshot", "dbus", "notify", "notify-reload", "idle":
		return nil
	default:
		return fmt.Errorf("%s:%d:%s: service type must be simple, exec, forking, oneshot, dbus, notify, notify-reload, or idle", source.File, source.Line, source.Path)
	}
}

func validateSystemdRestart(value string, source ir.SourceRef) error {
	switch value {
	case "no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always":
		return nil
	default:
		return fmt.Errorf("%s:%d:%s: restart must be no, on-success, on-failure, on-abnormal, on-watchdog, on-abort, or always", source.File, source.Line, source.Path)
	}
}
