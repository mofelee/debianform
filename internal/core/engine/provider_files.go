package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planFileLike(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove " + node.Kind + " " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent "+node.Kind+" "+path, observed), nil
	}
	wantSHA, err := desiredContentSHA(node)
	if err != nil {
		return ProviderPlan{}, err
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "write file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if fileContentWriteOnly(node) {
		delete(observed, "sha256")
		desiredDigest := corestate.DesiredDigest(node.Desired)
		if current.IsDir ||
			current.Owner != stringDesired(node, "owner") ||
			current.Group != stringDesired(node, "group") ||
			normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
			return ProviderPlan{Action: ActionUpdate, Summary: "update file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		if prior == nil || prior.DesiredDigest != desiredDigest {
			return ProviderPlan{Action: ActionUpdate, Summary: "update file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return inSyncPlan(node, prior, "no changes for file "+path, observed), nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for file "+path, observed), nil
}

func (p NativeProvider) applyFileLike(ctx context.Context, step Step, daemonReload bool) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, daemonReload); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	content, err := desiredContent(step.Node)
	if err != nil {
		return nil, err
	}
	if err := p.writePathContent(ctx, step.Node.Host, path, content, stringDesired(step.Node, "owner"), stringDesired(step.Node, "group"), stringDesired(step.Node, "mode")); err != nil {
		return nil, err
	}
	if daemonReload {
		_, err = p.Runner.Run(ctx, step.Node.Host, "systemctl daemon-reload\n")
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planDirectory(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove directory " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent directory "+path, observed), nil
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "create directory " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if !current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update directory " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for directory "+path, observed), nil
}

func (p NativeProvider) applyDirectory(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if path == "" || path == "/" {
		return nil, fmt.Errorf("%s refuses to manage unsafe directory path %q", step.Address, path)
	}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		_, err := p.Runner.Run(ctx, step.Node.Host, "rm -rf -- "+shellQuote(path)+"\n")
		return map[string]any{"exists": false}, err
	}
	lines := []string{
		"set -eu",
		"mkdir -p -- " + shellQuote(path),
		"chown " + shellQuote(stringDesired(step.Node, "owner")+":"+stringDesired(step.Node, "group")) + " -- " + shellQuote(path),
		"chmod " + shellQuote(stringDesired(step.Node, "mode")) + " -- " + shellQuote(path),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) removePath(ctx context.Context, host, path string, daemonReload bool) error {
	if path == "" {
		return nil
	}
	script := "set -eu\nrm -f -- " + shellQuote(path) + "\n"
	if daemonReload {
		script += "systemctl daemon-reload\n"
	}
	_, err := p.Runner.Run(ctx, host, script)
	return err
}

func (p NativeProvider) removeDirectory(ctx context.Context, host, path string) error {
	if path == "" || path == "/" {
		return nil
	}
	_, err := p.Runner.Run(ctx, host, "set -eu\nrm -rf -- "+shellQuote(path)+"\n")
	return err
}

type pathState struct {
	Exists bool
	IsDir  bool
	Owner  string
	Group  string
	Mode   string
	SHA256 string
}

func (s pathState) observed() map[string]any {
	return map[string]any{
		"exists": s.Exists,
		"is_dir": s.IsDir,
		"owner":  s.Owner,
		"group":  s.Group,
		"mode":   displayMode(s.Mode),
		"sha256": s.SHA256,
	}
}

func (p NativeProvider) readPath(ctx context.Context, host, path string) (pathState, error) {
	if path == "" {
		return pathState{}, nil
	}
	script := fmt.Sprintf(`set -eu
if [ ! -e %s ]; then
  echo missing
  exit 0
fi
if [ -d %s ]; then echo dir; else echo file; fi
stat -c '%%U' %s
stat -c '%%G' %s
stat -c '%%a' %s
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, host, script)
	if err != nil {
		return pathState{}, err
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "missing" {
		return pathState{}, nil
	}
	st := pathState{Exists: true, IsDir: lines[0] == "dir"}
	if len(lines) > 1 {
		st.Owner = lines[1]
	}
	if len(lines) > 2 {
		st.Group = lines[2]
	}
	if len(lines) > 3 {
		st.Mode = lines[3]
	}
	if len(lines) > 4 {
		st.SHA256 = lines[4]
	}
	return st, nil
}

type pathContentState struct {
	pathState
	Content string
}

func (p NativeProvider) readPathWithContent(ctx context.Context, host, path string) (pathContentState, error) {
	if path == "" {
		return pathContentState{}, nil
	}
	script := fmt.Sprintf(`set -eu
if [ ! -e %s ]; then
  echo missing
  exit 0
fi
if [ -d %s ]; then echo dir; else echo file; fi
stat -c '%%U' %s
stat -c '%%G' %s
stat -c '%%a' %s
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
if [ -f %s ]; then base64 < %s | tr -d '\n'; echo; else echo ''; fi
`, shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, host, script)
	if err != nil {
		return pathContentState{}, err
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "missing" {
		return pathContentState{}, nil
	}
	st := pathContentState{pathState: pathState{Exists: true, IsDir: lines[0] == "dir"}}
	if len(lines) > 1 {
		st.Owner = lines[1]
	}
	if len(lines) > 2 {
		st.Group = lines[2]
	}
	if len(lines) > 3 {
		st.Mode = lines[3]
	}
	if len(lines) > 4 {
		st.SHA256 = lines[4]
	}
	if len(lines) > 5 && lines[5] != "" {
		data, err := base64.StdEncoding.DecodeString(lines[5])
		if err != nil {
			return pathContentState{}, err
		}
		st.Content = string(data)
	}
	return st, nil
}

func (p NativeProvider) writePathContent(ctx context.Context, host, path string, content []byte, owner, group, mode string) error {
	script := strings.Join([]string{
		"set -eu",
		"dest=" + shellQuote(path),
		"mkdir -p \"$(dirname \"$dest\")\"",
		"tmp=\"$(mktemp \"${dest}.dbf-tmp.XXXXXX\")\"",
		"trap 'rm -f -- \"$tmp\"' EXIT",
		"cat > \"$tmp\"",
		"install -o " + shellQuote(owner) +
			" -g " + shellQuote(group) +
			" -m " + shellQuote(mode) +
			" \"$tmp\" \"$dest\"",
	}, "\n") + "\n"
	_, err := p.Runner.RunInput(ctx, host, script, bytes.NewReader(content))
	if err != nil {
		return redactPayloadError(err)
	}
	return nil
}

func desiredContent(node graph.Node) ([]byte, error) {
	payload := providerDesired(node)
	if content, ok := payload["content"].(string); ok {
		return []byte(content), nil
	}
	if source, ok := payload["source_path"].(string); ok && source != "" {
		return os.ReadFile(source)
	}
	return nil, fmt.Errorf("%s requires content or source_path", node.Address)
}

func desiredContentSHA(node graph.Node) (string, error) {
	if sha := stringDesired(node, "content_sha256"); sha != "" {
		return strings.ToLower(sha), nil
	}
	if fileContentWriteOnly(node) {
		return "", nil
	}
	content, err := desiredContent(node)
	if err != nil {
		return "", err
	}
	return sha256Hex(content), nil
}

func fileContentWriteOnly(node graph.Node) bool {
	value, _ := node.Desired["content_write_only"].(bool)
	return value
}

func providerDesired(node graph.Node) map[string]any {
	if len(node.ProviderPayload) > 0 {
		return node.ProviderPayload
	}
	return node.Desired
}
