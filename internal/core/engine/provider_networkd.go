package engine

import (
	"context"
	"strings"
)

func (p NativeProvider) destroyNetworkdNetDev(ctx context.Context, step Step) error {
	desired := step.Prior.Desired
	path := stringMapValue(desired, "path")
	if path == "" {
		return nil
	}

	name := stringMapValue(desired, "name")
	quotedPath := shellQuote(path)
	lines := []string{
		"set -eu",
		"netdev_name=" + shellQuote(name),
	}
	if name == "" {
		lines = append(lines,
			"if [ -f "+quotedPath+" ]; then",
			"  netdev_name=\"$(sed -n '/^[[:space:]]*\\[NetDev\\][[:space:]]*$/,/^[[:space:]]*\\[/ { s/^[[:space:]]*Name[[:space:]]*=[[:space:]]*//p; }' "+quotedPath+" | head -n 1)\"",
			"fi",
		)
	}
	lines = append(lines,
		"rm -f -- "+quotedPath,
		"systemctl start systemd-networkd.service",
		"networkctl reload",
		"if [ -n \"$netdev_name\" ]; then",
		"  ip link delete dev \"$netdev_name\" 2>/dev/null || true",
		"fi",
	)
	_, err := p.Runner.Run(ctx, step.Host, strings.Join(lines, "\n")+"\n")
	return err
}
