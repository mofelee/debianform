package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

const bulkPlanPrefix = "DBF_BULK"

func (p NativeProvider) PlanHost(ctx context.Context, host ir.HostSpec, nodes []graph.Node, priors map[string]*corestate.Resource) (map[string]ProviderPlan, error) {
	if p.Runner == nil {
		return nil, fmt.Errorf("provider runner is required")
	}
	if len(nodes) == 0 {
		return map[string]ProviderPlan{}, nil
	}
	script, err := bulkPlanScript(nodes)
	if err != nil {
		return nil, err
	}
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "plan inspect",
		Action:  "inspect",
		Summary: fmt.Sprintf("%d resource(s)", len(nodes)),
	})
	result, err := p.Runner.Run(callCtx, host.Name, script)
	if err != nil {
		return nil, fmt.Errorf("bulk inspect host %s: %w", host.Name, err)
	}
	outputs, err := parseBulkPlanOutput(result.Stdout)
	if err != nil {
		return p.planHostLegacy(ctx, nodes, priors)
	}
	plans := make(map[string]ProviderPlan, len(nodes))
	for i, node := range nodes {
		runner := &bulkPlanRunner{outputs: outputs[i]}
		provider := NativeProvider{Runner: runner}
		plan, err := provider.Plan(ctx, node, priors[node.Address])
		if err != nil {
			return nil, err
		}
		if runner.pos != len(runner.outputs) {
			return nil, fmt.Errorf("%s bulk inspect returned %d unused result(s)", node.Address, len(runner.outputs)-runner.pos)
		}
		plans[node.Address] = plan
	}
	return plans, nil
}

func (p NativeProvider) planHostLegacy(ctx context.Context, nodes []graph.Node, priors map[string]*corestate.Resource) (map[string]ProviderPlan, error) {
	plans := make(map[string]ProviderPlan, len(nodes))
	for _, node := range nodes {
		plan, err := p.Plan(ctx, node, priors[node.Address])
		if err != nil {
			return nil, err
		}
		plans[node.Address] = plan
	}
	return plans, nil
}

type bulkPlanRunner struct {
	outputs []Result
	pos     int
}

func (r *bulkPlanRunner) Run(ctx context.Context, host, script string) (Result, error) {
	if r.pos >= len(r.outputs) {
		return Result{}, fmt.Errorf("bulk inspect has no result for script")
	}
	result := r.outputs[r.pos]
	r.pos++
	return result, nil
}

func (r *bulkPlanRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return Result{}, fmt.Errorf("bulk inspect does not support RunInput")
}

func (r *bulkPlanRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

func bulkPlanScript(nodes []graph.Node) (string, error) {
	var b strings.Builder
	b.WriteString(`set -u
dbf_emit_script() {
  dbf_idx=$1
  dbf_call=$2
  dbf_out=$(mktemp)
  dbf_err=$(mktemp)
  if sh -s >"$dbf_out" 2>"$dbf_err"; then
    printf '` + bulkPlanPrefix + `\t%s\t%s\tOK\t' "$dbf_idx" "$dbf_call"
    base64 < "$dbf_out" | tr -d '\n'
    printf '\n'
  else
    dbf_status=$?
    cat "$dbf_err" >&2
    rm -f -- "$dbf_out" "$dbf_err"
    exit "$dbf_status"
  fi
  rm -f -- "$dbf_out" "$dbf_err"
}
`)
	for i, node := range nodes {
		scripts, err := bulkPlanNodeScripts(node)
		if err != nil {
			return "", err
		}
		for call, script := range scripts {
			fmt.Fprintf(&b, "dbf_emit_script %d %d <<'__DBF_BULK_%d_%d__'\n", i, call, i, call)
			b.WriteString(strings.TrimRight(script, "\n"))
			fmt.Fprintf(&b, "\n__DBF_BULK_%d_%d__\n", i, call)
		}
	}
	return b.String(), nil
}

func bulkPlanNodeScripts(node graph.Node) ([]string, error) {
	switch node.Kind {
	case "system_hostname":
		return []string{systemHostnameReadScript()}, nil
	case "system_timezone":
		return []string{systemTimezoneReadScript(stringDesired(node, "timezone"))}, nil
	case "system_locale":
		return []string{systemLocaleReadScript(stringDesired(node, "locale"))}, nil
	case "file", "secret", "systemd_unit", "nftables_file", "networkd_netdev", "networkd_network":
		return []string{bulkReadPathScript(stringDesired(node, "path"))}, nil
	case "apt_source_file":
		return []string{bulkReadPathWithContentScript(stringDesired(node, "path"))}, nil
	case "apt_signing_key", "component_download", "component_binary", "component_file", "component_ca_certificate", "component_script_output", "directory":
		return []string{bulkReadPathScript(stringDesired(node, "path"))}, nil
	case "component_build":
		return []string{bulkReadPathScript(stringDesired(node, "output_path"))}, nil
	case "component_archive":
		return []string{bulkReadPathScript(stringDesired(node, "path"))}, nil
	case "package":
		return []string{bulkPackageInstallStateScript(stringDesired(node, "name"))}, nil
	case "docker_package_conflicts":
		return []string{bulkInstalledPackagesScript(stringListDesired(node, "packages"))}, nil
	case "kernel_module":
		return []string{bulkKernelModuleScript(stringDesired(node, "name"))}, nil
	case "sysctl":
		return []string{bulkSysctlScript(stringDesired(node, "key"), stringDesired(node, "value"))}, nil
	case "group":
		return []string{"getent group " + shellQuote(stringDesired(node, "name")) + " || true\n"}, nil
	case "user":
		return []string{bulkReadUserScript(stringDesired(node, "name"))}, nil
	case "user_group_membership":
		return []string{bulkReadUserScript(stringDesired(node, "user"))}, nil
	case "ssh_authorized_key":
		script, err := bulkAuthorizedKeyScript(node)
		if err != nil {
			return nil, err
		}
		return []string{script}, nil
	case "service":
		name := stringDesired(node, "unit")
		return []string{"printf 'enabled='; systemctl is-enabled " + shellQuote(name) + " 2>/dev/null || true\nprintf 'active='; systemctl is-active " + shellQuote(name) + " 2>/dev/null || true\n"}, nil
	case "docker_compose_project":
		return []string{dockerComposeProjectServicesCommand(node), dockerComposeProjectPSCommand(node)}, nil
	default:
		return nil, fmt.Errorf("%s unsupported resource kind %q", node.Address, node.Kind)
	}
}

func bulkReadPathScript(path string) string {
	if path == "" {
		return ":\n"
	}
	q := shellQuote(path)
	return fmt.Sprintf(`set -eu
if [ ! -e %s ]; then
  echo missing
  exit 0
fi
if [ -d %s ]; then echo dir; else echo file; fi
stat -c '%%U' %s
stat -c '%%G' %s
stat -c '%%a' %s
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, q, q, q, q, q, q, q)
}

func bulkReadPathWithContentScript(path string) string {
	if path == "" {
		return ":\n"
	}
	q := shellQuote(path)
	return fmt.Sprintf(`set -eu
if [ ! -e %s ]; then
  echo missing
  exit 0
fi
if [ -d %s ]; then echo dir; else echo file; fi
stat -c '%%U' %s
stat -c '%%G' %s
stat -c '%%a' %s
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
if [ -f %s ]; then base64 < %s | tr -d '\n'; echo; else echo ''; fi
`, q, q, q, q, q, q, q, q, q)
}

func bulkPackageInstallStateScript(name string) string {
	return packageInstallStateScript(name)
}

func bulkInstalledPackagesScript(packages []string) string {
	if len(packages) == 0 {
		return ":\n"
	}
	args := []string{"dpkg-query", "-W", "-f=${binary:Package}\\t${Status}\\n"}
	args = append(args, packages...)
	return strings.Join(shellQuoteArgs(args), " ") + " 2>/dev/null || true\n"
}

func bulkKernelModuleScript(name string) string {
	path := modulePath(name)
	return fmt.Sprintf(`module=%s
kmod=$(printf '%%s' "$module" | tr '-' '_')
if awk -v m="$kmod" '$1 == m { found = 1 } END { exit found ? 0 : 1 }' /proc/modules; then echo loaded; else echo missing; fi
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(name), shellQuote(path), shellQuote(path))
}

func bulkSysctlScript(key, value string) string {
	path := sysctlPath(key)
	return fmt.Sprintf(`sysctl -n %s 2>/dev/null || true
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(key), shellQuote(path), shellQuote(path))
}

func bulkReadUserScript(name string) string {
	quoted := shellQuote(name)
	return "if getent passwd " + quoted + " >/dev/null 2>&1; then\n" +
		"  getent passwd " + quoted + "\n" +
		"  id -gn " + quoted + "\n" +
		"  id -nG " + quoted + "\n" +
		"else\n" +
		"  echo __ABSENT__\n" +
		"fi\n"
}

func bulkAuthorizedKeyScript(node graph.Node) (string, error) {
	keytype, keyblob, err := splitAuthorizedKey(stringDesired(node, "key"))
	if err != nil {
		return "", fmt.Errorf("%s %w", node.Address, err)
	}
	return authorizedKeyPreamble(node.Desired) +
		"if [ -n \"$home\" ] && [ -f \"$file\" ] && awk -v t=" + shellQuote(keytype) +
		" -v b=" + shellQuote(keyblob) +
		" '($1==t && $2==b){f=1} END{exit f?0:1}' \"$file\"; then echo present; else echo absent; fi\n", nil
}

func parseBulkPlanOutput(output string) (map[int][]Result, error) {
	out := map[int][]Result{}
	records := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 5)
		if len(fields) != 5 || fields[0] != bulkPlanPrefix {
			return nil, fmt.Errorf("invalid bulk inspect output")
		}
		idx, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid bulk inspect index %q", fields[1])
		}
		call, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("invalid bulk inspect call %q", fields[2])
		}
		if fields[3] != "OK" {
			return nil, fmt.Errorf("bulk inspect call failed")
		}
		data, err := base64.StdEncoding.DecodeString(fields[4])
		if err != nil {
			return nil, err
		}
		for len(out[idx]) <= call {
			out[idx] = append(out[idx], Result{})
		}
		out[idx][call] = Result{Stdout: string(data)}
		records++
	}
	if records == 0 {
		return nil, fmt.Errorf("empty bulk inspect output")
	}
	return out, nil
}
