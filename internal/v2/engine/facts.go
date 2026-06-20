package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mofelee/debianform/internal/v2/ir"
)

func DiscoverProgramFacts(ctx context.Context, runner Runner, program *ir.Program, now func() time.Time) (map[string]ir.HostFacts, error) {
	facts := map[string]ir.HostFacts{}
	if program == nil {
		return facts, nil
	}
	for _, host := range program.Hosts {
		hostFacts, err := DiscoverHostFacts(ctx, runner, host, now)
		if err != nil {
			return nil, err
		}
		facts[host.Name] = hostFacts
	}
	return facts, nil
}

func DiscoverHostFacts(ctx context.Context, runner Runner, host ir.HostSpec, now func() time.Time) (ir.HostFacts, error) {
	if runner == nil {
		return ir.HostFacts{}, fmt.Errorf("v2 facts discovery runner is required")
	}
	script := `set -eu
host_name="$(hostname 2>/dev/null || true)"
arch="$(dpkg --print-architecture 2>/dev/null || true)"
if [ -z "$arch" ]; then
  machine="$(uname -m 2>/dev/null || true)"
  case "$machine" in
    x86_64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    armv7l|armhf) arch="armhf" ;;
    *) arch="$machine" ;;
  esac
fi
codename=""
if [ -r /etc/os-release ]; then
  codename="$(
    . /etc/os-release
    printf '%s' "${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}"
  )"
fi
if [ -z "$codename" ] && command -v lsb_release >/dev/null 2>&1; then
  codename="$(lsb_release -sc 2>/dev/null || true)"
fi
printf 'hostname=%s\n' "$host_name"
printf 'architecture=%s\n' "$arch"
printf 'codename=%s\n' "$codename"
`
	result, err := runner.Run(ctx, host.Name, script)
	if err != nil {
		return ir.HostFacts{}, fmt.Errorf("discover host facts for %s: %w", host.Name, err)
	}
	values := parseFactLines(result.Stdout)
	arch := normalizeDebianArchitecture(values["architecture"])
	if arch == "" {
		return ir.HostFacts{}, fmt.Errorf("discover host facts for %s: architecture is empty", host.Name)
	}
	codename := values["codename"]
	if codename == "" {
		return ir.HostFacts{}, fmt.Errorf("discover host facts for %s: codename is empty", host.Name)
	}
	if now == nil {
		now = time.Now
	}
	return ir.HostFacts{
		System: ir.SystemFacts{
			Hostname:     values["hostname"],
			Architecture: arch,
			Codename:     codename,
			DetectedAt:   now().UTC().Format(time.RFC3339),
		},
	}, nil
}

func parseFactLines(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

func normalizeDebianArchitecture(value string) string {
	switch strings.TrimSpace(value) {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return strings.TrimSpace(value)
	}
}
