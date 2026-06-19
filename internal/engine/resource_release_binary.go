package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
	"github.com/mofelee/debianform/internal/state"
)

type releaseBinaryProvider struct{}

func (releaseBinaryProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Path = stringAttr(res, "path", "")
	d.Owner = stringAttr(res, "owner", "root")
	d.Group = stringAttr(res, "group", "root")
	d.Mode = stringAttr(res, "mode", "0755")
	d.ArchiveFormat = stringAttr(res, "archive_format", "tar.xz")
	d.ArchiveMember = stringAttr(res, "member", "")

	if value, ok := res.Attrs["source"]; ok {
		source, err := releaseSource(value)
		if err != nil {
			return d, fmt.Errorf("%s source: %w", res.Address, err)
		}
		d.ReleaseSource = source
	}
	if value, ok := res.Attrs["sources"]; ok {
		values, ok := value.(map[string]any)
		if !ok {
			return d, fmt.Errorf("%s sources must be an object", res.Address)
		}
		d.ReleaseSources = make(map[string]ReleaseSource, len(values))
		for arch, value := range values {
			source, err := releaseSource(value)
			if err != nil {
				return d, fmt.Errorf("%s sources.%s: %w", res.Address, arch, err)
			}
			d.ReleaseSources[arch] = source
		}
	}
	return d, nil
}

func releaseSource(value any) (ReleaseSource, error) {
	values, ok := value.(map[string]any)
	if !ok {
		return ReleaseSource{}, fmt.Errorf("must be an object")
	}
	return ReleaseSource{
		URL:           config.String(values["url"]),
		ArchiveSHA256: strings.ToLower(config.String(values["archive_sha256"])),
		BinarySHA256:  strings.ToLower(config.String(values["binary_sha256"])),
	}, nil
}

func (releaseBinaryProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	selected, err := selectReleaseSource(ctx, e, res, d)
	if err != nil {
		return Change{}, err
	}
	current, err := e.readPath(ctx, res.Host, selected.Path)
	if err != nil {
		return Change{}, err
	}
	if !current.Exists {
		return change(res, selected, "create", "install release binary "+selected.Path), nil
	}
	if current.IsDir ||
		current.SHA256 != selected.ReleaseSource.BinarySHA256 ||
		current.Owner != selected.Owner ||
		current.Group != selected.Group ||
		normalizeMode(current.Mode) != normalizeMode(selected.Mode) {
		return change(res, selected, "update", "update release binary "+selected.Path), nil
	}
	return noChange(res, selected), nil
}

func selectReleaseSource(ctx context.Context, e *Engine, res config.Resource, d Desired) (Desired, error) {
	if d.ReleaseSource.URL != "" {
		return d, nil
	}
	result, err := e.runner.Run(ctx, res.Host, "dpkg --print-architecture\n")
	if err != nil {
		return d, err
	}
	arch := lastNonEmptyLine(result.Stdout)
	source, ok := d.ReleaseSources[arch]
	if !ok {
		available := make([]string, 0, len(d.ReleaseSources))
		for name := range d.ReleaseSources {
			available = append(available, name)
		}
		sort.Strings(available)
		return d, fmt.Errorf("%s has no release source for Debian architecture %q (configured: %s)", res.Address, arch, strings.Join(available, ", "))
	}
	d.ReleaseSource = source
	return d, nil
}

func (releaseBinaryProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	d := change.Desired
	if d.ArchiveFormat != "tar.xz" {
		return fmt.Errorf("%s unsupported archive format %q", change.Address, d.ArchiveFormat)
	}
	script := fmt.Sprintf(`set -eu
export DEBIAN_FRONTEND=noninteractive
if ! command -v curl >/dev/null 2>&1 || ! command -v tar >/dev/null 2>&1 || ! command -v xz >/dev/null 2>&1 || [ ! -s /etc/ssl/certs/ca-certificates.crt ]; then
  apt-get update
  apt-get install -y ca-certificates curl tar xz-utils
fi
archive=$(mktemp)
binary=$(mktemp)
cleanup() {
  rm -f -- "$archive" "$binary"
}
trap cleanup EXIT HUP INT TERM
curl --fail --location --silent --show-error --output "$archive" %s
printf '%%s  %%s\n' %s "$archive" | sha256sum -c -
tar -xJOf "$archive" %s > "$binary"
printf '%%s  %%s\n' %s "$binary" | sha256sum -c -
mkdir -p -- "$(dirname %s)"
install -o %s -g %s -m %s "$binary" %s
`,
		sshx.ShellQuote(d.ReleaseSource.URL),
		sshx.ShellQuote(d.ReleaseSource.ArchiveSHA256),
		sshx.ShellQuote(d.ArchiveMember),
		sshx.ShellQuote(d.ReleaseSource.BinarySHA256),
		sshx.ShellQuote(d.Path),
		sshx.ShellQuote(d.Owner),
		sshx.ShellQuote(d.Group),
		sshx.ShellQuote(d.Mode),
		sshx.ShellQuote(d.Path),
	)
	_, err := e.runner.Run(ctx, change.Resource.Host, script)
	return err
}

func (releaseBinaryProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	return destroyPath(ctx, e, prior)
}
