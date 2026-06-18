package engine

import (
	"context"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
)

// hostnameProvider manages the static system hostname via hostnamectl.
type hostnameProvider struct{}

func (hostnameProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Hostname = stringAttr(res, "hostname", "")
	return d, nil
}

func (hostnameProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	result, err := e.runner.Run(ctx, res.Host, "hostnamectl --static 2>/dev/null || cat /etc/hostname 2>/dev/null || true\n")
	if err != nil {
		return Change{}, err
	}
	if lastNonEmptyLine(result.Stdout) == d.Hostname {
		return noChange(res, d), nil
	}
	return change(res, d, "update", "set hostname to "+d.Hostname), nil
}

func (hostnameProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	d := change.Desired
	_, err := e.runner.Run(ctx, change.Resource.Host, "set -eu\nhostnamectl set-hostname "+sshx.ShellQuote(d.Hostname)+"\n")
	return err
}
