package engine

import (
	"context"
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
