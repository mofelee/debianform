package engine

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/mofelee/debianform/internal/v1/config"
	"github.com/mofelee/debianform/internal/v1/sshx"
	"github.com/mofelee/debianform/internal/v1/state"
)

type systemdUnitProvider struct{}

func (systemdUnitProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	d.Path = stringAttr(res, "path", "")
	if d.Path == "" {
		d.Path = "/etc/systemd/system/" + d.Name
	}
	content, err := contentAttr(res)
	if err != nil {
		return d, err
	}
	d.Content = content
	d.ContentSHA256 = hash(content)
	d.Owner = stringAttr(res, "owner", "root")
	d.Group = stringAttr(res, "group", "root")
	d.Mode = stringAttr(res, "mode", "0644")
	return d, nil
}

func (systemdUnitProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	return e.planFile(ctx, res, d)
}

func (systemdUnitProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	d := change.Desired
	encoded := base64.StdEncoding.EncodeToString([]byte(d.Content))
	tmp := d.Path + ".dbf-tmp"
	backup := d.Path + ".dbf-rollback"
	script := fmt.Sprintf(`set -eu
mkdir -p "$(dirname %s)"
had_old=false
if [ -e %s ]; then
  rm -f -- %s
  cp -a -- %s %s
  had_old=true
fi
base64 -d > %s <<'__DBF_SYSTEMD_UNIT__'
%s
__DBF_SYSTEMD_UNIT__
install -o %s -g %s -m %s %s %s
rm -f -- %s
if ! systemctl daemon-reload; then
  if [ "$had_old" = true ]; then
    mv -f -- %s %s
  else
    rm -f -- %s
  fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  exit 1
fi
rm -f -- %s
`,
		sshx.ShellQuote(d.Path),
		sshx.ShellQuote(d.Path),
		sshx.ShellQuote(backup),
		sshx.ShellQuote(d.Path),
		sshx.ShellQuote(backup),
		sshx.ShellQuote(tmp),
		encoded,
		sshx.ShellQuote(d.Owner),
		sshx.ShellQuote(d.Group),
		sshx.ShellQuote(d.Mode),
		sshx.ShellQuote(tmp),
		sshx.ShellQuote(d.Path),
		sshx.ShellQuote(tmp),
		sshx.ShellQuote(backup),
		sshx.ShellQuote(d.Path),
		sshx.ShellQuote(d.Path),
		sshx.ShellQuote(backup),
	)
	_, err := e.runner.Run(ctx, change.Resource.Host, script)
	return err
}

func (systemdUnitProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	path := priorString(prior, "path")
	if path == "" {
		return nil
	}
	script := "set -eu\nrm -f -- " + sshx.ShellQuote(path) + "\nsystemctl daemon-reload\n"
	_, err := e.runner.Run(ctx, priorString(prior, "host"), script)
	return err
}
