package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planAPTSourceFile(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPathWithContent(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	onDestroy := stringDesired(node, "on_destroy")
	if onDestroy == "" {
		onDestroy = "keep"
	}
	if ensureAbsent(node) {
		if prior == nil {
			return ProviderPlan{Action: ActionNoOp, Summary: "already unmanaged apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		if onDestroy == "keep" {
			return ProviderPlan{Action: ActionForget, Summary: "forget apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		if !hasAPTSourceOriginal(prior.Observed) {
			return ProviderPlan{}, fmt.Errorf("%s cannot restore apt source file: original content baseline is unavailable", node.Address)
		}
		return ProviderPlan{Action: ActionDelete, Summary: "restore apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	wantSHA, err := desiredContentSHA(node)
	if err != nil {
		return ProviderPlan{}, err
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "write apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if boolDesired(node, "sensitive") {
		if aptSourceStateNeedsRefresh(node, prior) {
			return ProviderPlan{Action: ActionAdopt, Summary: "refresh sensitive apt source file state " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return inSyncPlan(node, prior, "no changes for apt source file "+path, observed), nil
	}
	return inSyncPlan(node, prior, "no changes for apt source file "+path, withAPTSourceOriginal(observed, prior, current)), nil
}

func (p NativeProvider) applyAPTSourceFile(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if aptSourceOnDestroy(step.Node.Desired) == "restore" {
			if err := p.restoreAPTSourceFile(ctx, step.Node.Host, path, step.Prior); err != nil {
				return nil, err
			}
		}
		return map[string]any{"exists": false}, nil
	}
	original := map[string]any{}
	if !boolDesired(step.Node, "sensitive") {
		var err error
		original, err = p.aptSourceOriginal(ctx, step)
		if err != nil {
			return nil, err
		}
	}
	content, err := desiredContent(step.Node)
	if err != nil {
		return nil, err
	}
	if err := p.writePathContent(ctx, step.Node.Host, path, content, stringDesired(step.Node, "owner"), stringDesired(step.Node, "group"), stringDesired(step.Node, "mode")); err != nil {
		return nil, err
	}
	observed := map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}
	copyAPTSourceOriginal(observed, original)
	return observed, nil
}

func (p NativeProvider) destroyAPTSourceFile(ctx context.Context, step Step) error {
	if step.Prior == nil {
		return nil
	}
	if aptSourceOnDestroy(step.Prior.Desired) != "restore" {
		return nil
	}
	if err := p.restoreAPTSourceFile(ctx, step.Host, stringMapValue(step.Prior.Desired, "path"), step.Prior); err != nil {
		return err
	}
	_, err := p.Runner.Run(ctx, step.Host, "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get update\n")
	return err
}

func (p NativeProvider) restoreAPTSourceFile(ctx context.Context, host, path string, prior *corestate.Resource) error {
	if prior == nil {
		return nil
	}
	if !boolMapValue(prior.Observed, "original_exists") {
		return p.removePath(ctx, host, path, false)
	}
	content := stringMapValue(prior.Observed, "original_content")
	owner := stringMapValue(prior.Observed, "original_owner")
	if owner == "" {
		owner = "root"
	}
	group := stringMapValue(prior.Observed, "original_group")
	if group == "" {
		group = "root"
	}
	mode := stringMapValue(prior.Observed, "original_mode")
	if mode == "" {
		mode = "0644"
	}
	return p.writePathContent(ctx, host, path, []byte(content), owner, group, mode)
}

func (p NativeProvider) planAPTSigningKey(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent apt signing key "+path, observed), nil
	}
	wantSHA, err := desiredSigningKeySHA(node)
	if err != nil {
		return ProviderPlan{}, err
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		(wantSHA != "" && current.SHA256 != wantSHA) ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if wantSHA == "" && prior != nil && prior.DesiredDigest != "" && prior.DesiredDigest != corestate.DesiredDigest(node.Desired) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for apt signing key "+path, observed), nil
}

func (p NativeProvider) applyAPTSigningKey(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	payload := providerDesired(step.Node)
	if stringMapValue(payload, "content") != "" {
		return p.applyFileLike(ctx, step, false)
	}
	url := stringMapValue(payload, "url")
	sha := stringMapValue(payload, "sha256")
	if url == "" {
		return nil, fmt.Errorf("%s apt signing key requires url or content", step.Address)
	}
	tmp := path + ".dbf-tmp"
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
		"if ! command -v curl >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y ca-certificates curl",
		"fi",
		"curl -fsSL " + shellQuote(url) + " -o " + shellQuote(tmp),
	}
	if sha != "" {
		lines = append(lines, "printf '%s  %s\\n' "+shellQuote(sha)+" "+shellQuote(tmp)+" | sha256sum --check --status")
	}
	lines = append(lines,
		"install -o "+shellQuote(stringDesired(step.Node, "owner"))+
			" -g "+shellQuote(stringDesired(step.Node, "group"))+
			" -m "+shellQuote(stringDesired(step.Node, "mode"))+
			" "+shellQuote(tmp)+" "+shellQuote(path),
		"rm -f -- "+shellQuote(tmp),
	)
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) aptSourceOriginal(ctx context.Context, step Step) (map[string]any, error) {
	original := map[string]any{}
	if step.Prior != nil && hasAPTSourceOriginal(step.Prior.Observed) {
		copyAPTSourceOriginal(original, step.Prior.Observed)
		return original, nil
	}
	current, err := p.readPathWithContent(ctx, step.Node.Host, stringDesired(step.Node, "path"))
	if err != nil {
		return nil, err
	}
	copyAPTSourceOriginal(original, aptSourceOriginalFromCurrent(current))
	return original, nil
}

func withAPTSourceOriginal(observed map[string]any, prior *corestate.Resource, current pathContentState) map[string]any {
	if observed == nil {
		observed = map[string]any{}
	}
	if prior != nil && hasAPTSourceOriginal(prior.Observed) {
		copyAPTSourceOriginal(observed, prior.Observed)
		return observed
	}
	copyAPTSourceOriginal(observed, aptSourceOriginalFromCurrent(current))
	return observed
}

func aptSourceOriginalFromCurrent(current pathContentState) map[string]any {
	return map[string]any{
		"original_exists":  current.Exists && !current.IsDir,
		"original_content": current.Content,
		"original_owner":   current.Owner,
		"original_group":   current.Group,
		"original_mode":    current.Mode,
	}
}

func hasAPTSourceOriginal(values map[string]any) bool {
	_, ok := values["original_exists"]
	return ok
}

func aptSourceStateNeedsRefresh(node graph.Node, prior *corestate.Resource) bool {
	if prior == nil {
		return false
	}
	if _, ok := prior.Observed["original_content"]; ok {
		return true
	}
	return prior.DesiredDigest != "" && prior.DesiredDigest != corestate.DesiredDigest(node.Desired)
}

func copyAPTSourceOriginal(dst map[string]any, src map[string]any) {
	for _, key := range []string{"original_exists", "original_content", "original_owner", "original_group", "original_mode"} {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func desiredSigningKeySHA(node graph.Node) (string, error) {
	if sha := stringDesired(node, "sha256"); sha != "" {
		return strings.ToLower(sha), nil
	}
	if sha := stringDesired(node, "content_sha256"); sha != "" {
		return strings.ToLower(sha), nil
	}
	if content := stringDesired(node, "content"); content != "" {
		return sha256Hex([]byte(content)), nil
	}
	if url := stringDesired(node, "url"); url != "" {
		return "", nil
	}
	return "", fmt.Errorf("%s apt signing key requires url with optional sha256 or content", node.Address)
}

func aptSourceOnDestroy(desired map[string]any) string {
	value := stringMapValue(desired, "on_destroy")
	if value == "" {
		return "keep"
	}
	return value
}
