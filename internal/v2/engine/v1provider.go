package engine

import (
	"context"
	"fmt"
	"strings"

	v1config "github.com/mofelee/debianform/internal/v1/config"
	v1engine "github.com/mofelee/debianform/internal/v1/engine"
	v1state "github.com/mofelee/debianform/internal/v1/state"
	"github.com/mofelee/debianform/internal/v2/graph"
	v2state "github.com/mofelee/debianform/internal/v2/state"
)

type V1Provider struct {
	Runner v1engine.Runner
}

func NewV1Provider(runner v1engine.Runner) V1Provider {
	return V1Provider{Runner: runner}
}

func (p V1Provider) Plan(ctx context.Context, node graph.Node, prior *v2state.Resource) (ProviderPlan, error) {
	res := v1ResourceForNode(node)
	change, err := v1engine.PlanResource(ctx, p.Runner, res)
	if err != nil {
		return ProviderPlan{}, err
	}
	action := change.Action
	ownership := "managed"
	if action == ActionNoOp && prior == nil {
		action = ActionAdopt
		ownership = "adopted"
	} else if prior != nil && prior.Ownership != "" {
		ownership = prior.Ownership
	}
	observed := map[string]any{
		"provider_action": change.Action,
	}
	if action == ActionNoOp || action == ActionAdopt {
		observed["desired_digest"] = v2state.DesiredDigest(node.Desired)
	}
	return ProviderPlan{
		Action:    action,
		Summary:   change.Summary,
		Observed:  observed,
		Ownership: ownership,
	}, nil
}

func (p V1Provider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	res := v1ResourceForNode(step.Node)
	desired, err := v1engine.DesiredForResource(res)
	if err != nil {
		return nil, err
	}
	change := v1engine.Change{
		Address:  step.Address,
		Action:   step.Action,
		Summary:  step.Summary,
		Resource: res,
		Desired:  desired,
	}
	if err := v1engine.ApplyResource(ctx, p.Runner, change); err != nil {
		return nil, err
	}
	if step.Action == ActionDelete {
		return map[string]any{"exists": false}, nil
	}
	return map[string]any{
		"exists":         true,
		"desired_digest": v2state.DesiredDigest(step.Node.Desired),
	}, nil
}

func (p V1Provider) Destroy(ctx context.Context, step Step) error {
	if step.Prior == nil {
		return nil
	}
	return v1engine.DestroyResource(ctx, p.Runner, v1StateForPrior(*step.Prior))
}

func (p V1Provider) RunOperation(ctx context.Context, operation graph.Operation) error {
	if operation.CommandPreview == "" {
		return nil
	}
	host := hostFromAddress(operation.Address)
	if host == "" {
		return fmt.Errorf("cannot infer host from operation address %s", operation.Address)
	}
	_, err := p.Runner.RunCommand(ctx, host, operation.CommandPreview)
	return err
}

func v1ResourceForNode(node graph.Node) v1config.Resource {
	attrs := cloneMap(node.ProviderPayload)
	if len(attrs) == 0 {
		attrs = cloneMap(node.Desired)
	}
	normalizeV1Attrs(node.ProviderType, attrs)
	name := providerResourceName(node)
	return v1config.Resource{
		Type:      node.ProviderType,
		Name:      name,
		Address:   node.Address,
		Host:      node.Host,
		Attrs:     attrs,
		DependsOn: append([]string(nil), node.DependsOn...),
	}
}

func normalizeV1Attrs(providerType string, attrs map[string]any) {
	if sourcePath, ok := attrs["source_path"]; ok {
		attrs["source"] = sourcePath
		delete(attrs, "source_path")
	}
	if applyRuntime, ok := attrs["apply_runtime"]; ok {
		attrs["apply"] = applyRuntime
		delete(attrs, "apply_runtime")
	}
	if providerType == "debian_user" {
		if group, ok := attrs["group"]; ok {
			attrs["gid"] = group
			delete(attrs, "group")
		}
	}
	if providerType == "debian_authorized_key" {
		if key, ok := attrs["key"]; ok {
			attrs["key"] = key
		}
	}
}

func providerResourceName(node graph.Node) string {
	if node.ProviderAddress != "" {
		if idx := strings.LastIndex(node.ProviderAddress, "."); idx >= 0 && idx+1 < len(node.ProviderAddress) {
			return node.ProviderAddress[idx+1:]
		}
	}
	if name, ok := node.Desired["name"].(string); ok && name != "" {
		return safeName(name)
	}
	if path, ok := node.Desired["path"].(string); ok && path != "" {
		return safeName(path)
	}
	return safeName(node.Address)
}

func safeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "resource"
	}
	return strings.ToLower(out)
}

func v1StateForPrior(prior v2state.Resource) v1state.ResourceState {
	out := v1state.ResourceState{
		"type": prior.ProviderType,
		"host": prior.Host,
	}
	for key, value := range prior.Desired {
		out[key] = value
	}
	if prior.ProviderType == "debian_authorized_key" {
		if key, ok := prior.Desired["key"]; ok {
			out["public_key"] = key
		}
	}
	return out
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
