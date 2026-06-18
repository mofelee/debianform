package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
	"github.com/mofelee/debianform/internal/state"
)

// aptSourceProvider manages a deb822 apt repository definition under
// /etc/apt/sources.list.d. The structured fields are rendered into a .sources
// file whose content is then reconciled with the regular file machinery, so
// drift detection and atomic writes are shared with debian_file.
type aptSourceProvider struct{}

func (aptSourceProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Path = stringAttr(res, "path", "/etc/apt/sources.list.d/"+objectName(res)+".sources")
	d.Owner = "root"
	d.Group = "root"
	d.Mode = stringAttr(res, "mode", "0644")
	d.Ensure = stringAttr(res, "ensure", "present")
	d.Content = aptSourceContent(res)
	d.ContentSHA256 = hash(d.Content)
	return d, nil
}

func (aptSourceProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	if d.Ensure == "absent" {
		current, err := e.readPath(ctx, res.Host, d.Path)
		if err != nil {
			return Change{}, err
		}
		if current.Exists {
			return change(res, d, "delete", "remove apt source "+d.Path), nil
		}
		return noChange(res, d), nil
	}
	return e.planFile(ctx, res, d)
}

func (aptSourceProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	if change.Desired.Ensure == "absent" {
		_, err := e.runner.Run(ctx, change.Resource.Host, "rm -f -- "+sshx.ShellQuote(change.Desired.Path)+"\n")
		return err
	}
	return e.applyFile(ctx, change)
}

func (aptSourceProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	return destroyPath(ctx, e, prior)
}

// aptRepositoryProvider manages a complete APT repository: optional signing
// key, deb822 source file, and the cache refresh required after repository
// changes. It is the high-level resource users should prefer over the lower
// level debian_apt_source.
type aptRepositoryProvider struct{}

func (aptRepositoryProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Path = stringAttr(res, "path", "/etc/apt/sources.list.d/"+objectName(res)+".sources")
	d.Owner = "root"
	d.Group = "root"
	d.Mode = stringAttr(res, "mode", "0644")
	d.Ensure = stringAttr(res, "ensure", "present")

	key, err := aptRepositoryKey(res)
	if err != nil {
		return d, err
	}
	d.KeyPath = key.Path
	d.KeyURL = key.URL
	d.KeyContent = key.Content
	if d.KeyContent != "" {
		d.KeySHA256 = hash(d.KeyContent)
	}

	sourceAttrs := copyAttrs(res.Attrs)
	if d.KeyPath != "" {
		sourceAttrs["signed_by"] = d.KeyPath
	}
	sourceRes := res
	sourceRes.Attrs = sourceAttrs
	d.Content = aptSourceContent(sourceRes)
	d.ContentSHA256 = hash(d.Content)
	return d, nil
}

func (aptRepositoryProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	source, err := e.readPath(ctx, res.Host, d.Path)
	if err != nil {
		return Change{}, err
	}
	keyExists := false
	keyInSync := true
	if d.KeyPath != "" {
		key, err := e.readPath(ctx, res.Host, d.KeyPath)
		if err != nil {
			return Change{}, err
		}
		keyExists = key.Exists
		if d.KeySHA256 != "" {
			keyInSync = key.Exists && key.SHA256 == d.KeySHA256
		}
	}

	if d.Ensure == "absent" {
		if source.Exists || keyExists {
			return change(res, d, "delete", "remove apt repository "+objectName(res)), nil
		}
		return noChange(res, d), nil
	}

	sourceInSync := source.Exists &&
		source.SHA256 == d.ContentSHA256 &&
		source.Owner == d.Owner &&
		source.Group == d.Group &&
		normalizeMode(source.Mode) == normalizeMode(d.Mode)
	keyRequiredButMissing := d.KeyPath != "" && !keyExists
	if !source.Exists {
		return change(res, d, "create", "configure apt repository "+objectName(res)+" and refresh apt cache"), nil
	}
	if !sourceInSync || keyRequiredButMissing || !keyInSync {
		return change(res, d, "update", "configure apt repository "+objectName(res)+" and refresh apt cache"), nil
	}
	return noChange(res, d), nil
}

func (aptRepositoryProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	d := change.Desired
	if d.Ensure == "absent" {
		var lines []string
		lines = append(lines, "set -eu")
		lines = append(lines, "rm -f -- "+sshx.ShellQuote(d.Path))
		if d.KeyPath != "" {
			lines = append(lines, "rm -f -- "+sshx.ShellQuote(d.KeyPath))
		}
		lines = append(lines, "apt-get update")
		_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
		return err
	}

	var lines []string
	lines = append(lines, "set -eu", "export DEBIAN_FRONTEND=noninteractive")
	if d.KeyPath != "" {
		lines = append(lines, aptRepositoryKeyScript(d)...)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(d.Content))
	tmp := d.Path + ".dbf-tmp"
	lines = append(lines,
		"mkdir -p -- \"$(dirname "+sshx.ShellQuote(d.Path)+")\"",
		"base64 -d > "+sshx.ShellQuote(tmp)+" <<'__DBF_APT_SOURCE__'\n"+encoded+"\n__DBF_APT_SOURCE__",
		"install -o "+sshx.ShellQuote(d.Owner)+" -g "+sshx.ShellQuote(d.Group)+" -m "+sshx.ShellQuote(d.Mode)+" "+sshx.ShellQuote(tmp)+" "+sshx.ShellQuote(d.Path),
		"rm -f -- "+sshx.ShellQuote(tmp),
		"apt-get update",
	)
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
	return err
}

func (aptRepositoryProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	path := priorString(prior, "path")
	keyPath := priorString(prior, "key_path")
	if path == "" && keyPath == "" {
		return nil
	}
	var lines []string
	lines = append(lines, "set -eu")
	if path != "" {
		lines = append(lines, "rm -f -- "+sshx.ShellQuote(path))
	}
	if keyPath != "" {
		lines = append(lines, "rm -f -- "+sshx.ShellQuote(keyPath))
	}
	lines = append(lines, "apt-get update")
	_, err := e.runner.Run(ctx, priorString(prior, "host"), strings.Join(lines, "\n")+"\n")
	return err
}

// aptSourceContent renders the deb822 representation of the source.
func aptSourceContent(res config.Resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Types: %s\n", stringAttr(res, "types", "deb"))
	fmt.Fprintf(&b, "URIs: %s\n", stringAttr(res, "uris", ""))
	fmt.Fprintf(&b, "Suites: %s\n", stringAttr(res, "suites", ""))
	fmt.Fprintf(&b, "Components: %s\n", stringAttr(res, "components", ""))
	if arch := stringAttr(res, "architectures", ""); arch != "" {
		fmt.Fprintf(&b, "Architectures: %s\n", arch)
	}
	if signedBy := stringAttr(res, "signed_by", ""); signedBy != "" {
		fmt.Fprintf(&b, "Signed-By: %s\n", signedBy)
	}
	return b.String()
}

type aptRepoKey struct {
	Path    string
	URL     string
	Content string
}

func aptRepositoryKey(res config.Resource) (aptRepoKey, error) {
	value, ok := res.Attrs["key"]
	if !ok {
		return aptRepoKey{}, nil
	}
	key, ok := value.(map[string]any)
	if !ok {
		return aptRepoKey{}, fmt.Errorf("%s key must be an object", res.Address)
	}
	out := aptRepoKey{
		Path: mapString(key, "path"),
		URL:  mapString(key, "url"),
	}
	if out.Path == "" {
		out.Path = "/etc/apt/keyrings/" + objectName(res) + ".asc"
	}
	out.Content = mapString(key, "content")
	if out.URL != "" && out.Content != "" {
		return out, fmt.Errorf("%s key must not set both url and content", res.Address)
	}
	if out.URL == "" && out.Content == "" {
		return out, fmt.Errorf("%s key requires url or content", res.Address)
	}
	return out, nil
}

func aptRepositoryKeyScript(d Desired) []string {
	quotedPath := sshx.ShellQuote(d.KeyPath)
	if d.KeyContent != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(d.KeyContent))
		tmp := d.KeyPath + ".dbf-tmp"
		return []string{
			"mkdir -p -- \"$(dirname " + quotedPath + ")\"",
			"base64 -d > " + sshx.ShellQuote(tmp) + " <<'__DBF_APT_KEY__'\n" + encoded + "\n__DBF_APT_KEY__",
			"install -o root -g root -m 0644 " + sshx.ShellQuote(tmp) + " " + quotedPath,
			"rm -f -- " + sshx.ShellQuote(tmp),
		}
	}
	if d.KeyURL == "" {
		return nil
	}

	tmp := d.KeyPath + ".dbf-download"
	lines := []string{}
	if strings.HasPrefix(strings.ToLower(d.KeyURL), "https://") {
		lines = append(lines, "if ! dpkg-query -W -f='${Status}' ca-certificates 2>/dev/null | grep -q 'install ok installed'; then apt-get update; apt-get install -y ca-certificates; fi")
	}
	lines = append(lines,
		"if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1 && [ ! -x /usr/lib/apt/apt-helper ]; then apt-get update; apt-get install -y curl; fi",
		"mkdir -p -- \"$(dirname "+quotedPath+")\"",
		"rm -f -- "+sshx.ShellQuote(tmp),
		"if command -v curl >/dev/null 2>&1; then curl -fsSL "+sshx.ShellQuote(d.KeyURL)+" -o "+sshx.ShellQuote(tmp)+"; elif command -v wget >/dev/null 2>&1; then wget -qO "+sshx.ShellQuote(tmp)+" "+sshx.ShellQuote(d.KeyURL)+"; else /usr/lib/apt/apt-helper download-file "+sshx.ShellQuote(d.KeyURL)+" "+sshx.ShellQuote(tmp)+"; fi",
		"install -o root -g root -m 0644 "+sshx.ShellQuote(tmp)+" "+quotedPath,
		"rm -f -- "+sshx.ShellQuote(tmp),
	)
	return lines
}

func copyAttrs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mapString(values map[string]any, name string) string {
	if value, ok := values[name]; ok {
		return config.String(value)
	}
	return ""
}
