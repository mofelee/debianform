package engine

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
)

const netplanOwnershipScript = `set -eu
export LC_ALL=C
for path in \
  /lib/netplan/*.yaml \
  /etc/netplan/*.yaml \
  /run/netplan/*.yaml \
  /run/systemd/network/*netplan* \
  /run/NetworkManager/system-connections/netplan-*; do
  [ -f "$path" ] || continue
  printf '%s\n' "$path"
done
`

func (p NativeProvider) PreflightHost(ctx context.Context, host ir.HostSpec, nodes []graph.Node) error {
	declarations := nativeNetworkdDeclarations(nodes)
	if host.Facts.System.Distribution != "ubuntu" || len(declarations) == 0 {
		return nil
	}
	if p.Runner == nil {
		return fmt.Errorf("provider runner is required")
	}
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "plan preflight",
		Action:  "inspect",
		Summary: "Netplan ownership",
	})
	result, err := p.Runner.Run(callCtx, host.Name, netplanOwnershipScript)
	if err != nil {
		return fmt.Errorf("preflight native systemd-networkd ownership for host %q: %w", host.Name, err)
	}
	ownership := nonEmptyLines(result.Stdout)
	if len(ownership) == 0 {
		return nil
	}
	sort.Strings(ownership)
	return fmt.Errorf(
		"host %q has active Netplan ownership (%s); affected native systemd-networkd declarations: %s; no provider changes were made; prepare a native-networkd target outside DebianForm",
		host.Name,
		strings.Join(ownership, ", "),
		strings.Join(declarations, ", "),
	)
}

func nativeNetworkdDeclarations(nodes []graph.Node) []string {
	declarations := []string{}
	for _, node := range nodes {
		affected := node.Kind == "networkd_netdev" || node.Kind == "networkd_network"
		if node.Kind == "file" {
			affected = pathWithinDirectory(stringDesired(node, "path"), "/etc/systemd/network")
		}
		if affected {
			declarations = append(declarations, node.Address)
		}
	}
	sort.Strings(declarations)
	return declarations
}

func pathWithinDirectory(candidate, directory string) bool {
	candidate = path.Clean(candidate)
	directory = path.Clean(directory)
	return candidate != directory && strings.HasPrefix(candidate, directory+"/")
}
