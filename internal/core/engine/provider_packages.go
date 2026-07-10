package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planPackage(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "name")
	state, err := p.packageInstallState(ctx, node.Host, name)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := state.observed()
	if ensureAbsent(node) {
		if state.Installed && !state.Virtual {
			return ProviderPlan{Action: ActionDelete, Summary: "remove package " + name, Observed: observed, Ownership: ownership(prior)}, nil
		}
		if state.Virtual {
			observed["installed"] = false
			observed["satisfied_by"] = state.Package
		}
		return absentInSyncPlan(prior, "already absent package "+name, observed), nil
	}
	if !state.Installed {
		return ProviderPlan{Action: ActionCreate, Summary: "install package " + name, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if state.Virtual && state.Package != "" {
		return inSyncPlan(node, prior, "no changes for package "+name+" provided by "+state.Package, observed), nil
	}
	return inSyncPlan(node, prior, "no changes for package "+name, observed), nil
}

func (p NativeProvider) applyPackage(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "name")
	if (ensureAbsent(step.Node) || step.Action == ActionDelete) && !boolObserved(step, "installed") {
		return map[string]any{"installed": false}, nil
	}
	lines := []string{"set -eu", "export DEBIAN_FRONTEND=noninteractive"}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "apt-get remove -y "+shellQuote(name))
	} else {
		lines = append(lines,
			"if ! apt-cache policy "+shellQuote(name)+" | awk '$1 == \"Candidate:\" && $2 != \"(none)\" { found = 1 } END { exit found ? 0 : 1 }'; then",
			"  apt-get update",
			"fi",
		)
		lines = append(lines, "apt-get install -y "+shellQuote(name))
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		return map[string]any{"installed": false, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
	}
	state, err := p.packageInstallState(ctx, step.Node.Host, name)
	if err != nil {
		return nil, err
	}
	if !state.Installed {
		return nil, fmt.Errorf("%s: package %s was installed but no installed package or provider could be verified", step.Address, name)
	}
	observed := state.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

type packageInstallState struct {
	Installed bool
	Package   string
	Virtual   bool
}

func (s packageInstallState) observed() map[string]any {
	observed := map[string]any{"installed": s.Installed}
	if s.Package != "" {
		observed["package"] = s.Package
	}
	if s.Virtual {
		observed["virtual"] = true
	}
	return observed
}

func packageInstallStateScript(name string) string {
	return `set -eu
target=` + shellQuote(name) + `
if dpkg-query -W -f='${binary:Package}\t${Status}\n' "$target" 2>/dev/null | awk -F '\t' -v target="$target" '
$2 ~ /^install ok installed$/ {
  pkg = $1
  base = pkg
  sub(/:.*/, "", base)
  if (pkg == target || base == target) {
    print "package\t" pkg
  } else {
    print "provider\t" pkg
  }
  found = 1
}
END { exit found ? 0 : 1 }
'; then
  exit 0
fi
dpkg-query -W -f='${binary:Package}\t${Status}\t${Provides}\n' 2>/dev/null | awk -F '\t' -v target="$target" '
$2 ~ /^install ok installed$/ {
  n = split($3, provides, /, */)
  for (i = 1; i <= n; i++) {
    provide = provides[i]
    sub(/[[:space:]]*\(.*/, "", provide)
    sub(/[[:space:]]*:.*/, "", provide)
    gsub(/^[[:space:]]+|[[:space:]]+$/, "", provide)
    if (provide == target) {
      found = $1
    }
  }
}
END { if (found != "") print "provider\t" found }
' || true
`
}

func (p NativeProvider) packageInstallState(ctx context.Context, host, name string) (packageInstallState, error) {
	result, err := p.Runner.Run(ctx, host, packageInstallStateScript(name))
	if err != nil {
		return packageInstallState{}, err
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "package":
			provider := strings.TrimSpace(fields[1])
			if provider == "" {
				provider = name
			}
			return packageInstallState{Installed: true, Package: provider}, nil
		case "provider":
			provider := strings.TrimSpace(fields[1])
			if provider != "" {
				return packageInstallState{Installed: true, Package: provider, Virtual: true}, nil
			}
		}
	}
	return packageInstallState{Installed: false}, nil
}

func (p NativeProvider) planDockerPackageConflicts(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	packages := stringListDesired(node, "packages")
	installed, err := p.installedPackages(ctx, node.Host, packages)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := map[string]any{
		"installed": installed,
	}
	if len(installed) == 0 {
		return ProviderPlan{Action: ActionNoOp, Summary: "no docker conflict packages installed", Observed: observed, Ownership: ownership(prior)}, nil
	}
	switch stringDesired(node, "remove_conflicts") {
	case "false":
		return ProviderPlan{}, fmt.Errorf("%s: docker conflict packages are installed: %s; set docker.package.remove_conflicts = true or auto to remove them", node.Address, strings.Join(installed, ", "))
	case "true":
		return ProviderPlan{Action: ActionDelete, Summary: "remove docker conflict packages " + strings.Join(installed, ", "), Observed: observed, Ownership: ownership(prior)}, nil
	default:
		return ProviderPlan{Action: ActionDelete, Summary: "replace docker conflict packages " + strings.Join(installed, ", "), Observed: observed, Ownership: ownership(prior)}, nil
	}
}

func (p NativeProvider) applyDockerPackageConflicts(ctx context.Context, step Step) (map[string]any, error) {
	installed := stringListMapValue(step.Observed, "installed")
	if len(installed) == 0 {
		var err error
		installed, err = p.installedPackages(ctx, step.Node.Host, stringListDesired(step.Node, "packages"))
		if err != nil {
			return nil, err
		}
	}
	if len(installed) == 0 {
		return map[string]any{"installed": []string{}, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
	}
	if stringDesired(step.Node, "remove_conflicts") == "false" {
		return nil, fmt.Errorf("%s: docker conflict packages are installed: %s; set docker.package.remove_conflicts = true or auto to remove them", step.Node.Address, strings.Join(installed, ", "))
	}
	lines := []string{"set -eu", "export DEBIAN_FRONTEND=noninteractive"}
	args := []string{"apt-get", "remove", "-y"}
	args = append(args, installed...)
	lines = append(lines, strings.Join(shellQuoteArgs(args), " "))
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"installed": []string{}, "removed": installed, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) installedPackages(ctx context.Context, host string, packages []string) ([]string, error) {
	if len(packages) == 0 {
		return nil, nil
	}
	args := []string{"dpkg-query", "-W", "-f=${binary:Package}\\t${Status}\\n"}
	args = append(args, packages...)
	result, err := p.Runner.Run(ctx, host, strings.Join(shellQuoteArgs(args), " ")+" 2>/dev/null || true\n")
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || !strings.Contains(fields[1], "install ok installed") {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	var installed []string
	for _, name := range packages {
		if _, ok := seen[name]; ok {
			installed = append(installed, name)
		}
	}
	return installed, nil
}
