package graph

import (
	"fmt"
	"sort"
	"strings"
)

type ScheduleItem struct {
	Address      string
	Host         string
	Kind         string
	Operation    bool
	SafeParallel bool
	DependsOn    []string
}

type scheduleEntry struct {
	item      ScheduleItem
	dependsOn []string
	order     int
}

func (g *ResourceGraph) Validate() error {
	entries, err := g.scheduleEntries()
	if err != nil {
		return err
	}
	return validateAcyclic(entries)
}

func (g *ResourceGraph) TopologicalSort() ([]ScheduleItem, error) {
	waves, err := g.Waves()
	if err != nil {
		return nil, err
	}
	var out []ScheduleItem
	for _, wave := range waves {
		out = append(out, wave...)
	}
	return out, nil
}

func (g *ResourceGraph) Waves() ([][]ScheduleItem, error) {
	return g.scheduleWaves(nil)
}

func (g *ResourceGraph) ActiveWaves(active map[string]bool) ([][]ScheduleItem, error) {
	return g.scheduleWaves(active)
}

func SafeParallelKind(kind string) bool {
	switch kind {
	case "file", "secret", "systemd_unit", "nftables_file",
		"networkd_netdev", "networkd_network",
		"directory",
		"component_download", "component_binary", "component_file",
		"component_ca_certificate", "component_archive":
		return true
	default:
		return false
	}
}

func (g *ResourceGraph) scheduleWaves(active map[string]bool) ([][]ScheduleItem, error) {
	entries, err := g.scheduleEntries()
	if err != nil {
		return nil, err
	}
	if err := validateAcyclic(entries); err != nil {
		return nil, err
	}

	useAll := active == nil
	if !useAll {
		for address, enabled := range active {
			if !enabled {
				continue
			}
			if _, ok := entries[address]; !ok {
				return nil, fmt.Errorf("active resource graph address %q does not exist", address)
			}
		}
	}

	indegree := map[string]int{}
	dependents := map[string][]string{}
	for address, entry := range entries {
		if !useAll && !active[address] {
			continue
		}
		activeDeps := activeDependencies(entry.dependsOn, active, useAll)
		indegree[address] = len(activeDeps)
		for _, dep := range activeDeps {
			dependents[dep] = append(dependents[dep], address)
		}
	}
	if len(indegree) == 0 {
		return nil, nil
	}

	ready := sortedReady(indegree, entries)
	var waves [][]ScheduleItem
	emitted := 0
	for len(ready) > 0 {
		waveAddresses := ready
		ready = nil
		wave := make([]ScheduleItem, 0, len(waveAddresses))
		for _, address := range waveAddresses {
			entry := entries[address]
			item := entry.item
			item.DependsOn = activeDependencies(entry.dependsOn, active, useAll)
			wave = append(wave, item)
			emitted++
			for _, dependent := range dependents[address] {
				indegree[dependent]--
				if indegree[dependent] == 0 {
					ready = append(ready, dependent)
				}
			}
		}
		sortScheduleItems(wave)
		waves = append(waves, wave)
		sortAddressesByOrder(ready, entries)
	}
	if emitted != len(indegree) {
		return nil, fmt.Errorf("resource graph dependency cycle detected")
	}
	return waves, nil
}

func (g *ResourceGraph) scheduleEntries() (map[string]scheduleEntry, error) {
	entries := map[string]scheduleEntry{}
	order := 0
	for _, node := range g.Nodes {
		if node.Address == "" {
			return nil, fmt.Errorf("resource graph node has empty address")
		}
		if _, exists := entries[node.Address]; exists {
			return nil, fmt.Errorf("duplicate resource graph address %q", node.Address)
		}
		deps := dedupeStrings(node.DependsOn)
		entries[node.Address] = scheduleEntry{
			item: ScheduleItem{
				Address:      node.Address,
				Host:         node.Host,
				Kind:         node.Kind,
				SafeParallel: SafeParallelKind(node.Kind),
			},
			dependsOn: deps,
			order:     order,
		}
		order++
	}
	for _, op := range g.Operations {
		if op.Address == "" {
			return nil, fmt.Errorf("resource graph operation has empty address")
		}
		if _, exists := entries[op.Address]; exists {
			return nil, fmt.Errorf("duplicate resource graph address %q", op.Address)
		}
		deps := dedupeStrings(op.DependsOn)
		entries[op.Address] = scheduleEntry{
			item: ScheduleItem{
				Address:   op.Address,
				Host:      hostFromAddress(op.Address),
				Kind:      "operation",
				Operation: true,
			},
			dependsOn: deps,
			order:     order,
		}
		order++
	}
	for address, entry := range entries {
		for _, dep := range entry.dependsOn {
			if _, exists := entries[dep]; !exists {
				return nil, fmt.Errorf("%s depends on unknown resource graph address %q", address, dep)
			}
		}
	}
	for _, op := range g.Operations {
		for _, trigger := range dedupeStrings(op.TriggeredBy) {
			if _, exists := entries[trigger]; !exists {
				return nil, fmt.Errorf("%s is triggered by unknown resource graph address %q", op.Address, trigger)
			}
		}
	}
	return entries, nil
}

func validateAcyclic(entries map[string]scheduleEntry) error {
	state := map[string]int{}
	stackIndex := map[string]int{}
	var stack []string
	var visit func(string) error
	visit = func(address string) error {
		switch state[address] {
		case 1:
			start := stackIndex[address]
			cycle := append([]string(nil), stack[start:]...)
			cycle = append(cycle, address)
			return fmt.Errorf("resource graph dependency cycle: %s", strings.Join(cycle, " -> "))
		case 2:
			return nil
		}
		state[address] = 1
		stackIndex[address] = len(stack)
		stack = append(stack, address)
		for _, dep := range entries[address].dependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		delete(stackIndex, address)
		state[address] = 2
		return nil
	}

	addresses := make([]string, 0, len(entries))
	for address := range entries {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		if err := visit(address); err != nil {
			return err
		}
	}
	return nil
}

func activeDependencies(deps []string, active map[string]bool, useAll bool) []string {
	out := make([]string, 0, len(deps))
	for _, dep := range deps {
		if useAll || active[dep] {
			out = append(out, dep)
		}
	}
	return out
}

func sortedReady(indegree map[string]int, entries map[string]scheduleEntry) []string {
	var ready []string
	for address, count := range indegree {
		if count == 0 {
			ready = append(ready, address)
		}
	}
	sortAddressesByOrder(ready, entries)
	return ready
}

func sortAddressesByOrder(addresses []string, entries map[string]scheduleEntry) {
	sort.SliceStable(addresses, func(i, j int) bool {
		left := entries[addresses[i]]
		right := entries[addresses[j]]
		if left.order != right.order {
			return left.order < right.order
		}
		return addresses[i] < addresses[j]
	})
}

func sortScheduleItems(items []ScheduleItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Address < items[j].Address
	})
}

func hostFromAddress(address string) string {
	if !strings.HasPrefix(address, "host.") {
		return ""
	}
	rest := strings.TrimPrefix(address, "host.")
	if idx := strings.Index(rest, "."); idx >= 0 {
		return rest[:idx]
	}
	return ""
}
