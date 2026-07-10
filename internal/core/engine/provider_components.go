package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planComponentDownload(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component source " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component source "+path, observed), nil
	}
	wantSHA := strings.ToLower(stringDesired(node, "sha256"))
	if wantSHA == "" || stringDesired(node, "url") == "" {
		return ProviderPlan{}, fmt.Errorf("%s component download requires url and sha256", node.Address)
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "download component source " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component source " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for component source "+path, observed), nil
}

func (p NativeProvider) applyComponentDownload(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	url := stringDesired(step.Node, "url")
	sha := strings.ToLower(stringDesired(step.Node, "sha256"))
	if url == "" || sha == "" {
		return nil, fmt.Errorf("%s component download requires url and sha256", step.Address)
	}
	tmp := path + ".dbf-tmp"
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
		"source_url=" + shellQuote(url),
		"case \"$source_url\" in",
		"  file://*) ;;",
		"  *)",
		"    if ! command -v curl >/dev/null 2>&1; then",
		"      export DEBIAN_FRONTEND=noninteractive",
		"      apt-get update",
		"      apt-get install -y ca-certificates curl",
		"    fi",
		"    ;;",
		"esac",
		"case \"$source_url\" in",
		"  file://*) cp -- \"${source_url#file://}\" " + shellQuote(tmp) + " ;;",
		"  *) curl -fsSL \"$source_url\" -o " + shellQuote(tmp) + " ;;",
		"esac",
		"printf '%s  %s\\n' " + shellQuote(sha) + " " + shellQuote(tmp) + " | sha256sum --check --status",
		"install -o " + shellQuote(stringDesired(step.Node, "owner")) +
			" -g " + shellQuote(stringDesired(step.Node, "group")) +
			" -m " + shellQuote(stringDesired(step.Node, "mode")) +
			" " + shellQuote(tmp) + " " + shellQuote(path),
		"rm -f -- " + shellQuote(tmp),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planComponentBuild(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "output_path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component build output " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component build output "+path, observed), nil
	}
	if path == "" || stringDesired(node, "cache_path") == "" || stringDesired(node, "build_path") == "" {
		return ProviderPlan{}, fmt.Errorf("%s component build requires cache_path, build_path, and output_path", node.Address)
	}
	if len(commandMatrixDesired(node, "commands")) == 0 {
		return ProviderPlan{}, fmt.Errorf("%s component build requires commands", node.Address)
	}
	if stringDesired(node, "output") == "" {
		return ProviderPlan{}, fmt.Errorf("%s component build requires output", node.Address)
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "build component output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component build output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	desiredDigest := corestate.DesiredDigest(node.Desired)
	priorSHA := ""
	if prior != nil {
		priorSHA = stringMapValue(prior.Observed, "sha256")
	}
	if prior == nil || prior.DesiredDigest != desiredDigest || priorSHA == "" || priorSHA != current.SHA256 {
		return ProviderPlan{Action: ActionUpdate, Summary: "rebuild component output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	observed["desired_digest"] = desiredDigest
	return ProviderPlan{Action: ActionNoOp, Summary: "no changes for component build output " + path, Observed: observed, Ownership: ownership(prior)}, nil
}

func (p NativeProvider) applyComponentBuild(ctx context.Context, step Step) (map[string]any, error) {
	outputPath := stringDesired(step.Node, "output_path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, outputPath, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	if outputPath == "" {
		return nil, fmt.Errorf("%s component build requires output_path", step.Address)
	}
	lines, err := componentBuildScript(step.Node)
	if err != nil {
		return nil, err
	}
	_, err = p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, outputPath)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentBinary(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component binary "+path, observed), nil
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	desiredDigest := corestate.DesiredDigest(node.Desired)
	priorSHA := ""
	if prior != nil {
		priorSHA = stringMapValue(prior.Observed, "sha256")
	}
	if prior == nil || prior.DesiredDigest != desiredDigest || priorSHA == "" || priorSHA != current.SHA256 {
		return ProviderPlan{Action: ActionUpdate, Summary: "reinstall component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	observed["desired_digest"] = desiredDigest
	return ProviderPlan{Action: ActionNoOp, Summary: "no changes for component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
}

func (p NativeProvider) applyComponentBinary(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	cachePath := stringDesired(step.Node, "cache_path")
	if cachePath == "" {
		return nil, fmt.Errorf("%s component binary requires cache_path", step.Address)
	}
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
	}
	if format := stringDesired(step.Node, "extract_format"); format != "" {
		if format != "zip" && format != "tar.gz" && format != "tar.xz" && format != "bz2" && format != "gz" {
			return nil, fmt.Errorf("%s unsupported component binary extract format %q", step.Address, format)
		}
		lines = append(lines, componentBinaryExtractInstallScript(step.Node)...)
	} else {
		lines = append(lines,
			"install -o "+shellQuote(stringDesired(step.Node, "owner"))+
				" -g "+shellQuote(stringDesired(step.Node, "group"))+
				" -m "+shellQuote(stringDesired(step.Node, "mode"))+
				" "+shellQuote(cachePath)+" "+shellQuote(path),
		)
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, path)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentFile(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component file "+path, observed), nil
	}
	wantSHA := strings.ToLower(stringDesired(node, "source_sha256"))
	if wantSHA == "" {
		return ProviderPlan{}, fmt.Errorf("%s component file requires source_sha256", node.Address)
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install component file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for component file "+path, observed), nil
}

func (p NativeProvider) applyComponentFile(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	cachePath := stringDesired(step.Node, "cache_path")
	if cachePath == "" {
		return nil, fmt.Errorf("%s component file requires cache_path", step.Address)
	}
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
		"install -o " + shellQuote(stringDesired(step.Node, "owner")) +
			" -g " + shellQuote(stringDesired(step.Node, "group")) +
			" -m " + shellQuote(stringDesired(step.Node, "mode")) +
			" " + shellQuote(cachePath) + " " + shellQuote(path),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, path)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentArchive(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component archive "+path, observed), nil
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if !current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if prior == nil || prior.DesiredDigest != corestate.DesiredDigest(node.Desired) {
		return ProviderPlan{Action: ActionUpdate, Summary: "replace component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for component archive "+path, observed), nil
}

func (p NativeProvider) applyComponentArchive(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if path == "" || path == "/" {
		return nil, fmt.Errorf("%s refuses to manage unsafe archive path %q", step.Address, path)
	}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removeDirectory(ctx, step.Node.Host, path); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	cachePath := stringDesired(step.Node, "cache_path")
	if cachePath == "" {
		return nil, fmt.Errorf("%s component archive requires cache_path", step.Address)
	}
	if format := stringDesired(step.Node, "extract_format"); format != "tar.gz" {
		return nil, fmt.Errorf("%s unsupported component archive extract format %q", step.Address, format)
	}
	stripComponents := fmt.Sprintf("%v", step.Node.Desired["strip_components"])
	if stripComponents == "" || stripComponents == "<nil>" {
		stripComponents = "0"
	}
	lines := []string{
		"set -eu",
		"if ! command -v tar >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar gzip",
		"fi",
		"parent=$(dirname " + shellQuote(path) + ")",
		"base=$(basename " + shellQuote(path) + ")",
		"mkdir -p \"$parent\"",
		"work=$(mktemp -d)",
		"trap 'rm -rf -- \"$work\"' EXIT",
		"staging=\"$work/staging\"",
		"mkdir -p \"$staging\"",
		"tar --no-same-owner -xzf " + shellQuote(cachePath) + " -C \"$staging\" --strip-components " + shellQuote(stripComponents),
		"chown -R " + shellQuote(stringDesired(step.Node, "owner")+":"+stringDesired(step.Node, "group")) + " \"$staging\"",
		"chmod " + shellQuote(stringDesired(step.Node, "mode")) + " \"$staging\"",
		"tmp=\"$parent/.${base}.dbf-new\"",
		"old=\"$parent/.${base}.dbf-old\"",
		"rm -rf -- \"$tmp\" \"$old\"",
		"mv \"$staging\" \"$tmp\"",
		"if [ -e " + shellQuote(path) + " ]; then mv " + shellQuote(path) + " \"$old\"; fi",
		"if mv \"$tmp\" " + shellQuote(path) + "; then",
		"  rm -rf -- \"$old\"",
		"else",
		"  if [ -e \"$old\" ]; then mv \"$old\" " + shellQuote(path) + "; fi",
		"  exit 1",
		"fi",
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, path)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentScriptOutput(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if !current.Exists {
		action := ActionUpdate
		if prior == nil {
			action = ActionCreate
		}
		return ProviderPlan{Action: action, Summary: "run script to create output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir {
		return ProviderPlan{Action: ActionUpdate, Summary: "run script to repair output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if prior == nil {
		return ProviderPlan{Action: ActionUpdate, Summary: "refresh script output state " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if prior.DesiredDigest != "" && prior.DesiredDigest != corestate.DesiredDigest(node.Desired) {
		return ProviderPlan{Action: ActionUpdate, Summary: "refresh script output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	wantSHA := stringMapValue(prior.Observed, "sha256")
	if wantSHA == "" {
		return ProviderPlan{Action: ActionUpdate, Summary: "refresh script output state " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.SHA256 != wantSHA {
		return ProviderPlan{Action: ActionUpdate, Summary: "repair script output drift " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for script output "+path, observed), nil
}

func (p NativeProvider) applyComponentScriptOutput(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	observed := cloneMap(step.Observed)
	if observed == nil {
		observed = map[string]any{}
	}
	observed["path"] = path
	observed["pending_script"] = true
	return observed, nil
}

func componentBinaryExtractInstallScript(node graph.Node) []string {
	cachePath := stringDesired(node, "cache_path")
	path := stringDesired(node, "path")
	format := stringDesired(node, "extract_format")
	if format == "bz2" || format == "gz" {
		command := "bzip2"
		packageName := "bzip2"
		if format == "gz" {
			command = "gzip"
			packageName = "gzip"
		}
		return []string{
			"if ! command -v " + command + " >/dev/null 2>&1; then",
			"  export DEBIAN_FRONTEND=noninteractive",
			"  apt-get update",
			"  apt-get install -y " + packageName,
			"fi",
			"work=$(mktemp -d)",
			"trap 'rm -rf -- \"$work\"' EXIT",
			command + " -dc " + shellQuote(cachePath) + " > \"$work/binary\"",
			"install -o " + shellQuote(stringDesired(node, "owner")) +
				" -g " + shellQuote(stringDesired(node, "group")) +
				" -m " + shellQuote(stringDesired(node, "mode")) +
				" \"$work/binary\" " + shellQuote(path),
		}
	}
	include := stringDesired(node, "include")
	stripComponents := fmt.Sprintf("%v", node.Desired["strip_components"])
	if stripComponents == "" || stripComponents == "<nil>" {
		stripComponents = "0"
	}
	lines := []string{
		"if [ " + shellQuote(format) + " = 'zip' ] && ! command -v unzip >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y unzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.gz' ] && ! command -v tar >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar gzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.xz' ] && { ! command -v tar >/dev/null 2>&1 || ! command -v xz >/dev/null 2>&1; }; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar xz-utils",
		"fi",
		"work=$(mktemp -d)",
		"trap 'rm -rf -- \"$work\"' EXIT",
	}
	if format == "zip" {
		lines = append(lines, "unzip -q "+shellQuote(cachePath)+" -d \"$work\"")
	} else if format == "tar.xz" {
		lines = append(lines, "tar --no-same-owner -xJf "+shellQuote(cachePath)+" -C \"$work\"")
	} else {
		lines = append(lines, "tar --no-same-owner -xzf "+shellQuote(cachePath)+" -C \"$work\"")
	}
	lines = append(lines,
		"include="+shellQuote(include),
		"strip_components="+shellQuote(stripComponents),
		"matches=\"$work/.dbf-matches\"",
		": > \"$matches\"",
		"find \"$work\" -type f | sort > \"$work/.dbf-files\"",
		"while IFS= read -r file; do",
		"  rel=${file#\"$work\"/}",
		"  stripped=$rel",
		"  i=0",
		"  while [ \"$i\" -lt \"$strip_components\" ]; do",
		"    case \"$stripped\" in */*) stripped=${stripped#*/} ;; *) stripped= ;; esac",
		"    i=$((i + 1))",
		"  done",
		"  if [ \"$stripped\" = \"$include\" ]; then printf '%s\\n' \"$file\" >> \"$matches\"; fi",
		"done < \"$work/.dbf-files\"",
		"count=$(wc -l < \"$matches\" | tr -d ' ')",
		"if [ \"$count\" != 1 ]; then echo \"component binary include matched $count files: $include\" >&2; exit 1; fi",
		"src=$(sed -n '1p' \"$matches\")",
		"install -o "+shellQuote(stringDesired(node, "owner"))+
			" -g "+shellQuote(stringDesired(node, "group"))+
			" -m "+shellQuote(stringDesired(node, "mode"))+
			" \"$src\" "+shellQuote(path),
	)
	return lines
}

func componentBuildScript(node graph.Node) ([]string, error) {
	cachePath := stringDesired(node, "cache_path")
	buildPath := stringDesired(node, "build_path")
	outputPath := stringDesired(node, "output_path")
	output := stringDesired(node, "output")
	if cachePath == "" || buildPath == "" || outputPath == "" || output == "" {
		return nil, fmt.Errorf("%s component build requires cache_path, build_path, output_path, and output", node.Address)
	}
	commands := commandMatrixDesired(node, "commands")
	if len(commands) == 0 {
		return nil, fmt.Errorf("%s component build requires commands", node.Address)
	}
	for i, command := range commands {
		if len(command) == 0 {
			return nil, fmt.Errorf("%s component build command %d is empty", node.Address, i)
		}
	}
	format := stringDesired(node, "extract_format")
	if format != "" && format != "zip" && format != "tar.gz" && format != "tar.xz" {
		return nil, fmt.Errorf("%s unsupported component source extract format %q", node.Address, format)
	}
	stripComponents := fmt.Sprintf("%v", node.Desired["strip_components"])
	if stripComponents == "" || stripComponents == "<nil>" {
		stripComponents = "0"
	}
	sourceName := stringDesired(node, "source_name")
	if sourceName == "" {
		sourceName = "source"
	}
	lines := []string{
		"set -eu",
		"if [ " + shellQuote(format) + " = 'zip' ] && ! command -v unzip >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y unzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.gz' ] && ! command -v tar >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar gzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.xz' ] && { ! command -v tar >/dev/null 2>&1 || ! command -v xz >/dev/null 2>&1; }; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar xz-utils",
		"fi",
		"work=$(mktemp -d)",
		"trap 'rm -rf -- \"$work\"' EXIT",
		"src=\"$work/src\"",
		"mkdir -p \"$src\"",
	}
	switch format {
	case "zip":
		lines = append(lines, "unzip -q "+shellQuote(cachePath)+" -d \"$src\"")
	case "tar.gz":
		lines = append(lines, "tar --no-same-owner -xzf "+shellQuote(cachePath)+" -C \"$src\" --strip-components "+shellQuote(stripComponents))
	case "tar.xz":
		lines = append(lines, "tar --no-same-owner -xJf "+shellQuote(cachePath)+" -C \"$src\" --strip-components "+shellQuote(stripComponents))
	default:
		lines = append(lines, "cp -- "+shellQuote(cachePath)+" \"$src/"+sourceName+"\"")
	}
	lines = append(lines,
		"build_root="+shellQuote(buildPath),
		"rm -rf -- \"$build_root/work\"",
		"mkdir -p \"$build_root\"",
		"mv \"$src\" \"$build_root/work\"",
		"cd \"$build_root/work\"",
	)
	if workingDir := stringDesired(node, "working_dir"); workingDir != "" {
		lines = append(lines, "cd "+shellQuote(workingDir))
	}
	for _, command := range commands {
		lines = append(lines, shellCommandArgv(command))
	}
	lines = append(lines,
		"built="+shellQuote(output),
		"if [ ! -f \"$built\" ]; then echo \"component build output is missing: $built\" >&2; exit 1; fi",
		"mkdir -p \"$(dirname "+shellQuote(outputPath)+")\"",
		"install -o "+shellQuote(stringDesired(node, "owner"))+
			" -g "+shellQuote(stringDesired(node, "group"))+
			" -m "+shellQuote(stringDesired(node, "mode"))+
			" \"$built\" "+shellQuote(outputPath),
	)
	return lines, nil
}

func shellCommandArgv(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return "set -- " + strings.Join(quoted, " ") + "\n\"$@\""
}
