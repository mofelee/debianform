package engine

import (
	"context"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planService(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "unit")
	result, err := p.Runner.Run(ctx, node.Host, "printf 'enabled='; systemctl is-enabled "+shellQuote(name)+" 2>/dev/null || true\nprintf 'active='; systemctl is-active "+shellQuote(name)+" 2>/dev/null || true\n")
	if err != nil {
		return ProviderPlan{}, err
	}
	enabled := strings.Contains(result.Stdout, "enabled=enabled")
	active := strings.Contains(result.Stdout, "active=active")
	observed := map[string]any{"enabled": enabled, "active": active}
	var changes []string
	if want, ok := node.Desired["enabled"].(bool); ok && enabled != want {
		if want {
			changes = append(changes, "enable")
		} else {
			changes = append(changes, "disable")
		}
	}
	switch stringDesired(node, "state") {
	case "running":
		if !active {
			changes = append(changes, "start")
		}
	case "stopped":
		if active {
			changes = append(changes, "stop")
		}
	case "restarted":
		changes = append(changes, "restart")
	case "reloaded":
		changes = append(changes, "reload")
	}
	if len(changes) > 0 {
		return ProviderPlan{Action: ActionUpdate, Summary: strings.Join(changes, " ") + " service " + name, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for service "+name, observed), nil
}

func (p NativeProvider) applyService(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "unit")
	lines := []string{"set -eu"}
	if enabled, ok := step.Node.Desired["enabled"].(bool); ok {
		if enabled {
			lines = append(lines, "systemctl enable "+shellQuote(name))
		} else {
			lines = append(lines, "systemctl disable "+shellQuote(name))
		}
	}
	switch stringDesired(step.Node, "state") {
	case "running":
		lines = append(lines, "systemctl start "+shellQuote(name))
	case "stopped":
		lines = append(lines, "systemctl stop "+shellQuote(name))
	case "restarted":
		lines = append(lines, "systemctl restart "+shellQuote(name))
	case "reloaded":
		lines = append(lines, "systemctl reload "+shellQuote(name))
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}
