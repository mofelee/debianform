package graph

import (
	"fmt"
	"strconv"

	"github.com/mofelee/debianform/internal/core/ir"
)

type networkdPostReloadGroup struct {
	Component     string
	Name          string
	DeclarationID string
	Source        ir.SourceRef
	Triggers      []string
}

func collectNetworkdActivation(activation *ir.NetworkdActivationSpec, address, component string, reconfigure map[string][]string, postReload map[string]*networkdPostReloadGroup) error {
	if activation == nil {
		return nil
	}
	for _, name := range activation.Reconfigure {
		reconfigure[name] = append(reconfigure[name], address)
	}
	ref := activation.PostReload
	if ref == nil {
		return nil
	}
	if ref.DeclarationID == "" {
		return fmt.Errorf("%s has unresolved networkd post_reload reference %q", address, ref.Name)
	}
	key := ref.Scope + ":" + ref.DeclarationID
	if ref.Scope == "component" {
		key = component + ":" + key
	}
	group, exists := postReload[key]
	if !exists {
		group = &networkdPostReloadGroup{
			Component:     component,
			Name:          ref.Name,
			DeclarationID: ref.DeclarationID,
			Source:        ref.Source,
		}
		postReload[key] = group
	}
	group.Triggers = append(group.Triggers, address)
	return nil
}

func appendNetworkdReconfigureOperations(host, reloadAddress string, triggers map[string][]string, source ir.SourceRef, operations *[]Operation) []string {
	addresses := make([]string, 0, len(triggers))
	previous := reloadAddress
	for _, name := range sortedKeys(triggers) {
		address := fmt.Sprintf("host.%s.systemd.networkd.reconfigure[%s]", host, strconv.Quote(name))
		dependencies := []string{reloadAddress}
		if previous != reloadAddress {
			dependencies = append(dependencies, previous)
		}
		*operations = append(*operations, Operation{
			Host:           host,
			Address:        address,
			Action:         "run",
			Summary:        "reconfigure networkd interface " + name,
			DependsOn:      dedupeStrings(dependencies),
			TriggeredBy:    dedupeStrings(triggers[name]),
			CommandPreview: "networkctl reconfigure " + shellQuoteGraph(name),
			Source:         source,
		})
		addresses = append(addresses, address)
		previous = address
	}
	return addresses
}

func appendNetworkdPostReloadOperations(host ir.HostSpec, groups map[string]*networkdPostReloadGroup, after string, operations *[]Operation) error {
	previous := after
	for _, key := range sortedKeys(groups) {
		group := groups[key]
		script, address, err := networkdPostReloadScript(host, group)
		if err != nil {
			return err
		}
		index := operationIndex(*operations, address)
		if index >= 0 {
			op := (*operations)[index]
			op.TriggeredBy = dedupeStrings(append(op.TriggeredBy, group.Triggers...))
			op.DependsOn = dedupeStrings(append(op.DependsOn, previous))
			(*operations)[index] = op
			previous = address
			continue
		}
		*operations = append(*operations, Operation{
			Host:           host.Name,
			Address:        address,
			Action:         "run",
			Summary:        "run networkd post-reload script " + group.Name,
			Sensitive:      script.Sensitive,
			DependsOn:      dedupeStrings(append([]string{previous}, group.Triggers...)),
			TriggeredBy:    dedupeStrings(group.Triggers),
			CommandPreview: "script " + group.Name + " (once)",
			ScriptPayload: &ScriptPayload{
				Name:          script.Name,
				ComponentName: group.Component,
				Mode:          "once",
				Kind:          scriptPayloadKind(script),
				Interpreter:   append([]string(nil), script.Interpreter...),
				Run:           script.Run,
				Content:       script.Content,
				Commands:      cloneCommandMatrix(script.Commands),
			},
			Source: script.Source,
		})
		previous = address
	}
	return nil
}

func networkdPostReloadScript(host ir.HostSpec, group *networkdPostReloadGroup) (ir.ComponentScriptSpec, string, error) {
	if group.Component == "" {
		script, ok := host.Scripts[group.Name]
		if !ok || script.DeclarationID != group.DeclarationID {
			return ir.ComponentScriptSpec{}, "", fmt.Errorf("host %s networkd post_reload %q has no matching declaration %q", host.Name, group.Name, group.DeclarationID)
		}
		return script, fmt.Sprintf("host.%s.script[%s]", host.Name, strconv.Quote(group.Name)), nil
	}
	for _, component := range host.Components {
		if component.Name != group.Component {
			continue
		}
		script, ok := component.Scripts[group.Name]
		if !ok || script.DeclarationID != group.DeclarationID {
			break
		}
		address := fmt.Sprintf("host.%s.components.%s.script[%s]", host.Name, component.Name, strconv.Quote(group.Name))
		return script, address, nil
	}
	return ir.ComponentScriptSpec{}, "", fmt.Errorf("host %s component %s networkd post_reload %q has no matching declaration %q", host.Name, group.Component, group.Name, group.DeclarationID)
}

func operationIndex(operations []Operation, address string) int {
	for i := range operations {
		if operations[i].Address == address {
			return i
		}
	}
	return -1
}
