package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planKernelModule(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "name")
	path := modulePath(name)
	script := fmt.Sprintf(`module=%s
kmod=$(printf '%%s' "$module" | tr '-' '_')
if awk -v m="$kmod" '$1 == m { found = 1 } END { exit found ? 0 : 1 }' /proc/modules; then echo loaded; else echo missing; fi
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(name), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, node.Host, script)
	if err != nil {
		return ProviderPlan{}, err
	}
	lines := nonEmptyLines(result.Stdout)
	loaded := len(lines) > 0 && lines[0] == "loaded"
	persisted := len(lines) > 1 && lines[1] == sha256Hex([]byte(name+"\n"))
	observed := map[string]any{"loaded": loaded, "persisted": persisted}
	if ensureAbsent(node) {
		if loaded || persisted {
			return ProviderPlan{Action: ActionDelete, Summary: "unload kernel module " + name, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent kernel module "+name, observed), nil
	}
	if !loaded || !persisted {
		return ProviderPlan{Action: ActionUpdate, Summary: "modprobe " + name + " and write " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for kernel module "+name, observed), nil
}

func (p NativeProvider) applyKernelModule(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "name")
	path := modulePath(name)
	var lines []string
	lines = append(lines, "set -eu")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "rm -f -- "+shellQuote(path), "modprobe -r "+shellQuote(name)+" 2>/dev/null || true")
	} else {
		lines = append(lines,
			"modprobe "+shellQuote(name),
			"mkdir -p -- \"$(dirname "+shellQuote(path)+")\"",
			"printf '%s\\n' "+shellQuote(name)+" > "+shellQuote(path),
			"chown root:root "+shellQuote(path),
			"chmod 0644 "+shellQuote(path),
		)
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"loaded": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planSysctl(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	key := stringDesired(node, "key")
	value := stringDesired(node, "value")
	path := sysctlPath(key)
	script := fmt.Sprintf(`sysctl -n %s 2>/dev/null || true
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(key), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, node.Host, script)
	if err != nil {
		return ProviderPlan{}, err
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	current := ""
	if len(lines) > 0 {
		current = strings.TrimSpace(lines[0])
	}
	persisted := len(lines) > 1 && strings.TrimSpace(lines[1]) == sha256Hex([]byte(key+" = "+value+"\n"))
	observed := map[string]any{"value": current, "persisted": persisted}
	if ensureAbsent(node) {
		if persisted {
			return ProviderPlan{Action: ActionDelete, Summary: "remove sysctl " + key, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent sysctl "+key, observed), nil
	}
	if current != value || !persisted {
		return ProviderPlan{Action: ActionUpdate, Summary: "sysctl -w " + key + "=" + value + " and write " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for sysctl "+key, observed), nil
}

func (p NativeProvider) applySysctl(ctx context.Context, step Step) (map[string]any, error) {
	key := stringDesired(step.Node, "key")
	value := stringDesired(step.Node, "value")
	path := sysctlPath(key)
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"persisted": false, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
	}
	lines := []string{
		"set -eu",
		"sysctl -w " + shellQuote(key+"="+value),
		"mkdir -p -- \"$(dirname " + shellQuote(path) + ")\"",
		"printf '%s\\n' " + shellQuote(key+" = "+value) + " > " + shellQuote(path),
		"chown root:root " + shellQuote(path),
		"chmod 0644 " + shellQuote(path),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"value": value, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}
