package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

type NativeProvider struct {
	Runner Runner
}

func NewNativeProvider(runner Runner) NativeProvider {
	return NativeProvider{Runner: runner}
}

func (p NativeProvider) Plan(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	if p.Runner == nil {
		return ProviderPlan{}, fmt.Errorf("provider runner is required")
	}
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "plan inspect",
		Address: node.Address,
		Action:  "inspect",
		Summary: node.Summary,
	})
	switch node.Kind {
	case "system_hostname":
		return p.planSystemHostname(ctx, node, prior)
	case "system_timezone":
		return p.planSystemTimezone(ctx, node, prior)
	case "system_locale":
		return p.planSystemLocale(ctx, node, prior)
	case "file", "secret", "systemd_unit", "nftables_file", "networkd_netdev", "networkd_network":
		return p.planFileLike(ctx, node, prior)
	case "apt_source_file":
		return p.planAPTSourceFile(ctx, node, prior)
	case "apt_signing_key":
		return p.planAPTSigningKey(ctx, node, prior)
	case "component_download":
		return p.planComponentDownload(ctx, node, prior)
	case "component_build":
		return p.planComponentBuild(ctx, node, prior)
	case "component_binary":
		return p.planComponentBinary(ctx, node, prior)
	case "component_file", "component_ca_certificate":
		return p.planComponentFile(ctx, node, prior)
	case "component_archive":
		return p.planComponentArchive(ctx, node, prior)
	case "component_script_output":
		return p.planComponentScriptOutput(ctx, node, prior)
	case "directory":
		return p.planDirectory(ctx, node, prior)
	case "package":
		return p.planPackage(ctx, node, prior)
	case "docker_package_conflicts":
		return p.planDockerPackageConflicts(ctx, node, prior)
	case "kernel_module":
		return p.planKernelModule(ctx, node, prior)
	case "sysctl":
		return p.planSysctl(ctx, node, prior)
	case "group":
		return p.planGroup(ctx, node, prior)
	case "user":
		return p.planUser(ctx, node, prior)
	case "user_group_membership":
		return p.planUserGroupMembership(ctx, node, prior)
	case "ssh_authorized_key":
		return p.planAuthorizedKey(ctx, node, prior)
	case "service":
		return p.planService(ctx, node, prior)
	case "docker_compose_project":
		return p.planDockerComposeProject(ctx, node, prior)
	default:
		return ProviderPlan{}, fmt.Errorf("%s unsupported resource kind %q", node.Address, node.Kind)
	}
}

func (p NativeProvider) Apply(ctx context.Context, step Step) (map[string]any, error) {
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "apply resource",
		Address: step.Address,
		Action:  step.Action,
		Summary: step.Summary,
	})
	switch step.Node.Kind {
	case "system_hostname":
		return p.applySystemHostname(ctx, step)
	case "system_timezone":
		return p.applySystemTimezone(ctx, step)
	case "system_locale":
		return p.applySystemLocale(ctx, step)
	case "file", "secret", "nftables_file", "networkd_netdev", "networkd_network":
		return p.applyFileLike(ctx, step, false)
	case "apt_source_file":
		return p.applyAPTSourceFile(ctx, step)
	case "systemd_unit":
		return p.applyFileLike(ctx, step, true)
	case "apt_signing_key":
		return p.applyAPTSigningKey(ctx, step)
	case "component_download":
		return p.applyComponentDownload(ctx, step)
	case "component_build":
		return p.applyComponentBuild(ctx, step)
	case "component_binary":
		return p.applyComponentBinary(ctx, step)
	case "component_file", "component_ca_certificate":
		return p.applyComponentFile(ctx, step)
	case "component_archive":
		return p.applyComponentArchive(ctx, step)
	case "component_script_output":
		return p.applyComponentScriptOutput(ctx, step)
	case "directory":
		return p.applyDirectory(ctx, step)
	case "package":
		return p.applyPackage(ctx, step)
	case "docker_package_conflicts":
		return p.applyDockerPackageConflicts(ctx, step)
	case "kernel_module":
		return p.applyKernelModule(ctx, step)
	case "sysctl":
		return p.applySysctl(ctx, step)
	case "group":
		return p.applyGroup(ctx, step)
	case "user":
		return p.applyUser(ctx, step)
	case "user_group_membership":
		return p.applyUserGroupMembership(ctx, step)
	case "ssh_authorized_key":
		return p.applyAuthorizedKey(ctx, step)
	case "service":
		return p.applyService(ctx, step)
	case "docker_compose_project":
		return p.applyDockerComposeProject(ctx, step)
	default:
		return nil, fmt.Errorf("%s unsupported resource kind %q", step.Address, step.Node.Kind)
	}
}

func (p NativeProvider) Destroy(ctx context.Context, step Step) error {
	if step.Prior == nil {
		return nil
	}
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "apply resource",
		Address: step.Address,
		Action:  step.Action,
		Summary: step.Summary,
	})
	desired := step.Prior.Desired
	host := step.Prior.Host
	switch step.Prior.Kind {
	case "system_hostname", "system_timezone", "system_locale":
		return nil
	case "apt_source_file":
		return p.destroyAPTSourceFile(ctx, step)
	case "component_build":
		return p.removePath(ctx, host, stringMapValue(desired, "output_path"), false)
	case "file", "secret", "nftables_file", "networkd_netdev", "networkd_network", "apt_signing_key", "component_download", "component_binary", "component_file", "component_ca_certificate":
		return p.removePath(ctx, host, stringMapValue(desired, "path"), false)
	case "component_script_output":
		return nil
	case "component_archive":
		return p.removeDirectory(ctx, host, stringMapValue(desired, "path"))
	case "systemd_unit":
		return p.removePath(ctx, host, stringMapValue(desired, "path"), true)
	case "directory":
		path := stringMapValue(desired, "path")
		if path == "" || path == "/" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "rm -rf -- "+shellQuote(path)+"\n")
		return err
	case "package":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get remove -y "+shellQuote(name)+"\n")
		return err
	case "docker_package_conflicts":
		return nil
	case "kernel_module":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		script := "set -eu\nrm -f -- " + shellQuote(modulePath(name)) + "\nmodprobe -r " + shellQuote(name) + " 2>/dev/null || true\n"
		_, err := p.Runner.Run(ctx, host, script)
		return err
	case "sysctl":
		key := stringMapValue(desired, "key")
		if key == "" {
			return nil
		}
		return p.removePath(ctx, host, sysctlPath(key), false)
	case "group":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "if getent group "+shellQuote(name)+" >/dev/null; then groupdel "+shellQuote(name)+"; fi\n")
		return err
	case "user":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "if getent passwd "+shellQuote(name)+" >/dev/null; then userdel "+shellQuote(name)+"; fi\n")
		return err
	case "user_group_membership":
		user := stringMapValue(desired, "user")
		group := stringMapValue(desired, "group")
		if user == "" || group == "" {
			return nil
		}
		node := graph.Node{Address: step.Address, Host: host, Desired: map[string]any{"user": user, "group": group, "ensure": "absent"}}
		_, err := p.applyUserGroupMembership(ctx, Step{Node: node, Action: ActionDelete})
		return err
	case "ssh_authorized_key":
		user := stringMapValue(desired, "user")
		key := stringMapValue(desired, "key")
		if user == "" || key == "" {
			return nil
		}
		node := graph.Node{Host: host, Desired: map[string]any{"user": user, "key": key, "ensure": "absent"}}
		_, err := p.applyAuthorizedKey(ctx, Step{Node: node})
		return err
	case "service":
		name := stringMapValue(desired, "name")
		if name == "" {
			return nil
		}
		_, err := p.Runner.Run(ctx, host, "systemctl disable --now "+shellQuote(name)+" 2>/dev/null || true\n")
		return err
	case "docker_compose_project":
		node := graph.Node{Address: step.Address, Host: host, Desired: cloneMap(desired)}
		node.Desired["state"] = "absent"
		command, err := dockerComposeProjectCommand(node)
		if err != nil {
			return err
		}
		_, err = p.Runner.Run(ctx, host, command)
		return err
	default:
		return fmt.Errorf("%s unsupported prior kind %q", step.Address, step.Prior.Kind)
	}
}

func (p NativeProvider) RunOperation(ctx context.Context, operation graph.Operation) (OperationResult, error) {
	action := operation.Action
	if action == "" {
		action = ActionRun
	}
	address := operation.Address
	if current, ok := RemoteCallContextFromContext(ctx); ok && current.Address != "" {
		address = current.Address
	}
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "run operation",
		Address: address,
		Action:  action,
		Summary: operation.Summary,
	})
	if operation.ScriptPayload != nil {
		return p.runScriptOperation(ctx, operation)
	}
	if operation.CommandPreview == "" {
		return OperationResult{}, nil
	}
	host := hostFromAddress(operation.Address)
	if host == "" {
		return OperationResult{}, fmt.Errorf("cannot infer host from operation address %s", operation.Address)
	}
	_, err := p.Runner.RunCommand(ctx, host, operation.CommandPreview)
	return OperationResult{}, err
}

func (p NativeProvider) runScriptOperation(ctx context.Context, operation graph.Operation) (OperationResult, error) {
	host := hostFromAddress(operation.Address)
	if host == "" {
		return OperationResult{}, fmt.Errorf("cannot infer host from operation address %s", operation.Address)
	}
	payload := operation.ScriptPayload
	if payload == nil {
		return OperationResult{}, nil
	}
	interpreter := append([]string(nil), payload.Interpreter...)
	if len(interpreter) == 0 {
		return OperationResult{}, fmt.Errorf("%s script payload requires interpreter", operation.Address)
	}
	for i, arg := range interpreter {
		if arg == "" {
			return OperationResult{}, fmt.Errorf("%s script payload interpreter[%d] must be non-empty", operation.Address, i)
		}
	}
	script, err := scriptPayloadContent(operation.Address, payload)
	if err != nil {
		return OperationResult{}, err
	}
	remoteCommand := scriptEnvironmentPrefix(payload) + strings.Join(shellQuoteArgs(interpreter), " ")
	_, err = p.Runner.RunInput(ctx, host, remoteCommand, strings.NewReader(script))
	if err != nil {
		return OperationResult{}, err
	}
	outputs, err := p.readScriptOutputs(ctx, host, payload.Outputs)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{Outputs: outputs}, nil
}

func (p NativeProvider) readScriptOutputs(ctx context.Context, host string, outputs []graph.ScriptOutputPayload) (map[string]map[string]any, error) {
	if len(outputs) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]any, len(outputs))
	for _, output := range outputs {
		callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
			Phase:   "operation output read",
			Address: output.Address,
			Action:  "read",
			Summary: output.Path,
		})
		current, err := p.readPath(callCtx, host, output.Path)
		if err != nil {
			return nil, err
		}
		observed := current.observed()
		observed["path"] = output.Path
		if !current.Exists || current.IsDir || current.SHA256 == "" {
			return nil, fmt.Errorf("script output %s was not created as a regular file", output.Path)
		}
		out[output.Address] = observed
	}
	return out, nil
}

func scriptEnvironmentPrefix(payload *graph.ScriptPayload) string {
	if payload == nil {
		return ""
	}
	env := map[string]string{
		"DBF_SCRIPT_NAME":       payload.Name,
		"DBF_COMPONENT_NAME":    payload.ComponentName,
		"DBF_TRIGGER_ADDRESS":   firstString(payload.TriggerAddresses),
		"DBF_TRIGGER_PATH":      firstString(payload.TriggerPaths),
		"DBF_TRIGGER_ADDRESSES": strings.Join(payload.TriggerAddresses, "\n"),
		"DBF_TRIGGER_PATHS":     strings.Join(payload.TriggerPaths, "\n"),
	}
	keys := []string{
		"DBF_SCRIPT_NAME",
		"DBF_COMPONENT_NAME",
		"DBF_TRIGGER_ADDRESS",
		"DBF_TRIGGER_PATH",
		"DBF_TRIGGER_ADDRESSES",
		"DBF_TRIGGER_PATHS",
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+shellQuote(env[key]))
	}
	return strings.Join(parts, " ") + " "
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

type systemHostnameState struct {
	Hostname string
}

func (s systemHostnameState) observed() map[string]any {
	return map[string]any{"hostname": s.Hostname}
}

func (p NativeProvider) planSystemHostname(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	hostname := stringDesired(node, "hostname")
	if hostname == "" {
		return ProviderPlan{}, fmt.Errorf("%s system_hostname requires hostname", node.Address)
	}
	current, err := p.readSystemHostname(ctx, node.Host)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if current.Hostname != hostname {
		return ProviderPlan{Action: ActionUpdate, Summary: systemHostnameUpdateSummary(current.Hostname, hostname), Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for system hostname "+hostname, observed), nil
}

func (p NativeProvider) applySystemHostname(ctx context.Context, step Step) (map[string]any, error) {
	hostname := stringDesired(step.Node, "hostname")
	if hostname == "" {
		return nil, fmt.Errorf("%s system_hostname requires hostname", step.Address)
	}
	if _, err := p.Runner.Run(ctx, step.Node.Host, systemHostnameApplyScript(hostname)); err != nil {
		return nil, err
	}
	current, err := p.readSystemHostname(ctx, step.Node.Host)
	if err != nil {
		return nil, err
	}
	if current.Hostname != hostname {
		return nil, fmt.Errorf("%s set system hostname to %q but observed %q", step.Address, hostname, current.Hostname)
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) readSystemHostname(ctx context.Context, host string) (systemHostnameState, error) {
	result, err := p.Runner.Run(ctx, host, systemHostnameReadScript())
	if err != nil {
		return systemHostnameState{}, err
	}
	return systemHostnameState{Hostname: parseSystemHostname(result.Stdout)}, nil
}

func systemHostnameReadScript() string {
	return `set -eu
if command -v hostnamectl >/dev/null 2>&1; then
  if hostnamectl --static 2>/dev/null; then
    exit 0
  fi
fi
if [ -r /etc/hostname ]; then
  sed -n '1p' /etc/hostname
else
  printf '\n'
fi
`
}

func systemHostnameApplyScript(hostname string) string {
	return strings.Join([]string{
		"set -eu",
		"name=" + shellQuote(hostname),
		"if command -v hostnamectl >/dev/null 2>&1; then",
		`  hostnamectl set-hostname "$name"`,
		"else",
		`  printf '%s\n' "$name" > /etc/hostname`,
		"  if command -v hostname >/dev/null 2>&1; then hostname \"$name\"; fi",
		"fi",
	}, "\n") + "\n"
}

func parseSystemHostname(stdout string) string {
	line, _, _ := strings.Cut(strings.TrimRight(stdout, "\n"), "\n")
	return strings.TrimSpace(line)
}

func systemHostnameUpdateSummary(current string, desired string) string {
	if current == "" {
		return "set system hostname to " + desired
	}
	return "change system hostname from " + current + " to " + desired
}

type systemTimezoneState struct {
	Timezone   string
	ZoneExists bool
}

func (s systemTimezoneState) observed() map[string]any {
	return map[string]any{
		"timezone":    s.Timezone,
		"zone_exists": s.ZoneExists,
	}
}

func (p NativeProvider) planSystemTimezone(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	timezone := stringDesired(node, "timezone")
	if timezone == "" {
		return ProviderPlan{}, fmt.Errorf("%s system_timezone requires timezone", node.Address)
	}
	current, err := p.readSystemTimezone(ctx, node.Host, timezone)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if !current.ZoneExists {
		return ProviderPlan{}, fmt.Errorf("%s system_timezone requires /usr/share/zoneinfo/%s on target", node.Address, timezone)
	}
	if current.Timezone != timezone {
		return ProviderPlan{Action: systemSettingAction(prior, current.Timezone), Summary: systemTimezoneUpdateSummary(current.Timezone, timezone), Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for system timezone "+timezone, observed), nil
}

func (p NativeProvider) applySystemTimezone(ctx context.Context, step Step) (map[string]any, error) {
	timezone := stringDesired(step.Node, "timezone")
	if timezone == "" {
		return nil, fmt.Errorf("%s system_timezone requires timezone", step.Address)
	}
	if _, err := p.Runner.Run(ctx, step.Node.Host, systemTimezoneApplyScript(timezone)); err != nil {
		return nil, err
	}
	current, err := p.readSystemTimezone(ctx, step.Node.Host, timezone)
	if err != nil {
		return nil, err
	}
	if !current.ZoneExists {
		return nil, fmt.Errorf("%s system timezone zoneinfo %q is not available after apply", step.Address, timezone)
	}
	if current.Timezone != timezone {
		return nil, fmt.Errorf("%s set system timezone to %q but observed %q", step.Address, timezone, current.Timezone)
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) readSystemTimezone(ctx context.Context, host string, timezone string) (systemTimezoneState, error) {
	result, err := p.Runner.Run(ctx, host, systemTimezoneReadScript(timezone))
	if err != nil {
		return systemTimezoneState{}, err
	}
	values := parseFactLines(result.Stdout)
	return systemTimezoneState{
		Timezone:   values["timezone"],
		ZoneExists: values["zone_exists"] == "true",
	}, nil
}

func systemTimezoneReadScript(timezone string) string {
	return strings.Join([]string{
		"set -eu",
		"want=" + shellQuote(timezone),
		`tz=""`,
		`if command -v timedatectl >/dev/null 2>&1; then`,
		`  tz="$(timedatectl show -p Timezone --value 2>/dev/null || true)"`,
		`fi`,
		`if [ -z "$tz" ] && [ -L /etc/localtime ]; then`,
		`  target="$(readlink -f /etc/localtime 2>/dev/null || true)"`,
		`  case "$target" in`,
		`    /usr/share/zoneinfo/*) tz="${target#/usr/share/zoneinfo/}" ;;`,
		`  esac`,
		`fi`,
		`if [ -z "$tz" ] && [ -r /etc/timezone ]; then`,
		`  tz="$(sed -n '1p' /etc/timezone | tr -d '[:space:]')"`,
		`fi`,
		`zone_exists=false`,
		`if [ -f "/usr/share/zoneinfo/$want" ]; then zone_exists=true; fi`,
		`printf 'timezone=%s\n' "$tz"`,
		`printf 'zone_exists=%s\n' "$zone_exists"`,
	}, "\n") + "\n"
}

func systemTimezoneApplyScript(timezone string) string {
	return strings.Join([]string{
		"set -eu",
		"tz=" + shellQuote(timezone),
		`zone="/usr/share/zoneinfo/$tz"`,
		`if [ ! -f "$zone" ]; then`,
		`  printf 'timezone %s is not available at %s\n' "$tz" "$zone" >&2`,
		`  exit 1`,
		`fi`,
		`if command -v timedatectl >/dev/null 2>&1 && timedatectl set-timezone "$tz" 2>/dev/null; then`,
		`  exit 0`,
		`fi`,
		`ln -sfn "$zone" /etc/localtime`,
		`printf '%s\n' "$tz" > /etc/timezone`,
	}, "\n") + "\n"
}

func systemTimezoneUpdateSummary(current string, desired string) string {
	if current == "" {
		return "set system timezone to " + desired
	}
	return "change system timezone from " + current + " to " + desired
}

type systemLocaleState struct {
	Locale    string
	Available bool
}

func (s systemLocaleState) observed() map[string]any {
	return map[string]any{
		"locale":    s.Locale,
		"available": s.Available,
	}
}

func (p NativeProvider) planSystemLocale(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	locale := stringDesired(node, "locale")
	if locale == "" {
		return ProviderPlan{}, fmt.Errorf("%s system_locale requires locale", node.Address)
	}
	current, err := p.readSystemLocale(ctx, node.Host, locale)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if current.Locale != locale || !current.Available {
		return ProviderPlan{Action: systemSettingAction(prior, current.Locale), Summary: systemLocaleUpdateSummary(current.Locale, locale, current.Available), Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for system locale "+locale, observed), nil
}

func (p NativeProvider) applySystemLocale(ctx context.Context, step Step) (map[string]any, error) {
	locale := stringDesired(step.Node, "locale")
	if locale == "" {
		return nil, fmt.Errorf("%s system_locale requires locale", step.Address)
	}
	if _, err := p.Runner.Run(ctx, step.Node.Host, systemLocaleApplyScript(locale)); err != nil {
		return nil, err
	}
	current, err := p.readSystemLocale(ctx, step.Node.Host, locale)
	if err != nil {
		return nil, err
	}
	if current.Locale != locale {
		return nil, fmt.Errorf("%s set system locale to %q but observed %q", step.Address, locale, current.Locale)
	}
	if !current.Available {
		return nil, fmt.Errorf("%s system locale %q is not available after apply", step.Address, locale)
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) readSystemLocale(ctx context.Context, host string, locale string) (systemLocaleState, error) {
	result, err := p.Runner.Run(ctx, host, systemLocaleReadScript(locale))
	if err != nil {
		return systemLocaleState{}, err
	}
	values := parseFactLines(result.Stdout)
	return systemLocaleState{
		Locale:    values["locale"],
		Available: values["available"] == "true",
	}, nil
}

func systemLocaleReadScript(locale string) string {
	return strings.Join([]string{
		"set -eu",
		"want=" + shellQuote(locale),
		`normalize_locale() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '-'; }`,
		`current=""`,
		`if [ -r /etc/default/locale ]; then`,
		`  current="$(sed -n 's/^LANG=//p' /etc/default/locale | tail -n 1)"`,
		`  case "$current" in \"*\") current="${current#\"}"; current="${current%\"}" ;; esac`,
		`  case "$current" in \'*\') current="${current#\'}"; current="${current%\'}" ;; esac`,
		`fi`,
		`available=false`,
		`case "$want" in`,
		`  C|POSIX|C.UTF-8|C.utf8) available=true ;;`,
		`  *)`,
		`    if command -v locale >/dev/null 2>&1; then`,
		`      want_norm="$(normalize_locale "$want")"`,
		`      if locale -a 2>/dev/null | awk -v want="$want_norm" '`,
		`        {`,
		`          item = tolower($0)`,
		`          gsub(/-/, "", item)`,
		`          if (item == want) found = 1`,
		`        }`,
		`        END { exit found ? 0 : 1 }`,
		`      '; then`,
		`        available=true`,
		`      fi`,
		`    fi`,
		`    ;;`,
		`esac`,
		`printf 'locale=%s\n' "$current"`,
		`printf 'available=%s\n' "$available"`,
	}, "\n") + "\n"
}

func systemLocaleApplyScript(locale string) string {
	return strings.Join([]string{
		"set -eu",
		"loc=" + shellQuote(locale),
		`case "$loc" in`,
		`  C|POSIX|C.UTF-8|C.utf8) needs_generation=false ;;`,
		`  *) needs_generation=true ;;`,
		`esac`,
		`if [ "$needs_generation" = true ]; then`,
		`  export DEBIAN_FRONTEND=noninteractive`,
		`  if ! command -v locale-gen >/dev/null 2>&1 || [ ! -e /etc/locale.gen ]; then`,
		`    apt-get update`,
		`    apt-get install -y locales`,
		`  fi`,
		`  [ -e /etc/locale.gen ] || : > /etc/locale.gen`,
		`  charmap="${loc#*.}"`,
		`  case "$charmap" in "$loc") charmap="UTF-8" ;; *@*) charmap="${charmap%@*}" ;; esac`,
		`  case "$(printf '%s' "$charmap" | tr '[:lower:]' '[:upper:]' | tr -d '-')" in UTF8) charmap="UTF-8" ;; esac`,
		`  tmp="$(mktemp)"`,
		`  awk -v loc="$loc" -v charmap="$charmap" '`,
		`    BEGIN { found = 0 }`,
		`    {`,
		`      stripped = $0`,
		`      sub(/^[[:space:]#]+/, "", stripped)`,
		`      count = split(stripped, fields, /[[:space:]]+/)`,
		`      if (count > 0 && fields[1] == loc) {`,
		`        print loc " " charmap`,
		`        found = 1`,
		`        next`,
		`      }`,
		`      print $0`,
		`    }`,
		`    END { if (!found) print loc " " charmap }`,
		`  ' /etc/locale.gen > "$tmp"`,
		`  cat "$tmp" > /etc/locale.gen`,
		`  rm -f -- "$tmp"`,
		`  locale-gen "$loc"`,
		`fi`,
		`mkdir -p /etc/default`,
		`tmp="$(mktemp)"`,
		`if [ -f /etc/default/locale ]; then`,
		`  awk -v loc="$loc" '`,
		`    BEGIN { done = 0 }`,
		`    /^LANG=/ { if (!done) { print "LANG=\"" loc "\""; done = 1 }; next }`,
		`    { print $0 }`,
		`    END { if (!done) print "LANG=\"" loc "\"" }`,
		`  ' /etc/default/locale > "$tmp"`,
		`else`,
		`  printf 'LANG="%s"\n' "$loc" > "$tmp"`,
		`fi`,
		`install -m 0644 "$tmp" /etc/default/locale`,
		`rm -f -- "$tmp"`,
	}, "\n") + "\n"
}

func systemLocaleUpdateSummary(current string, desired string, available bool) string {
	if current == "" {
		return "set system locale to " + desired
	}
	if current == desired && !available {
		return "generate system locale " + desired
	}
	return "change system locale from " + current + " to " + desired
}

func systemSettingAction(prior *corestate.Resource, current string) string {
	if prior == nil && current == "" {
		return ActionCreate
	}
	return ActionUpdate
}

func scriptPayloadContent(address string, payload *graph.ScriptPayload) (string, error) {
	if payload == nil {
		return "", nil
	}
	switch payload.Kind {
	case "run":
		return payload.Run + "\n", nil
	case "content":
		return payload.Content, nil
	case "commands":
		return scriptPayloadCommands(address, payload.Commands)
	default:
		return "", fmt.Errorf("%s script payload kind must be run, content, or commands", address)
	}
}

func scriptPayloadCommands(address string, commands [][]string) (string, error) {
	if len(commands) == 0 {
		return "", fmt.Errorf("%s script payload commands must contain at least one command", address)
	}
	lines := make([]string, 0, len(commands))
	for i, command := range commands {
		if len(command) == 0 {
			return "", fmt.Errorf("%s script payload command[%d] must contain at least one argument", address, i)
		}
		for j, arg := range command {
			if arg == "" {
				return "", fmt.Errorf("%s script payload command[%d][%d] must be non-empty", address, i, j)
			}
		}
		lines = append(lines, strings.Join(shellQuoteArgs(command), " "))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func (p NativeProvider) planFileLike(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove " + node.Kind + " " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent "+node.Kind+" "+path, observed), nil
	}
	wantSHA, err := desiredContentSHA(node)
	if err != nil {
		return ProviderPlan{}, err
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "write file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if fileContentWriteOnly(node) {
		delete(observed, "sha256")
		desiredDigest := corestate.DesiredDigest(node.Desired)
		if current.IsDir ||
			current.Owner != stringDesired(node, "owner") ||
			current.Group != stringDesired(node, "group") ||
			normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
			return ProviderPlan{Action: ActionUpdate, Summary: "update file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		if prior == nil || prior.DesiredDigest != desiredDigest {
			return ProviderPlan{Action: ActionUpdate, Summary: "update file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return inSyncPlan(node, prior, "no changes for file "+path, observed), nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for file "+path, observed), nil
}

func (p NativeProvider) applyFileLike(ctx context.Context, step Step, daemonReload bool) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, daemonReload); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	content, err := desiredContent(step.Node)
	if err != nil {
		return nil, err
	}
	if err := p.writePathContent(ctx, step.Node.Host, path, content, stringDesired(step.Node, "owner"), stringDesired(step.Node, "group"), stringDesired(step.Node, "mode")); err != nil {
		return nil, err
	}
	if daemonReload {
		_, err = p.Runner.Run(ctx, step.Node.Host, "systemctl daemon-reload\n")
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planAPTSourceFile(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPathWithContent(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	onDestroy := stringDesired(node, "on_destroy")
	if onDestroy == "" {
		onDestroy = "keep"
	}
	if ensureAbsent(node) {
		if prior == nil {
			return ProviderPlan{Action: ActionNoOp, Summary: "already unmanaged apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		if onDestroy == "keep" {
			return ProviderPlan{Action: ActionForget, Summary: "forget apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return ProviderPlan{Action: ActionDelete, Summary: "restore apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	wantSHA, err := desiredContentSHA(node)
	if err != nil {
		return ProviderPlan{}, err
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "write apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update apt source file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for apt source file "+path, withAPTSourceOriginal(observed, prior, current)), nil
}

func (p NativeProvider) applyAPTSourceFile(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if aptSourceOnDestroy(step.Node.Desired) == "restore" {
			if err := p.restoreAPTSourceFile(ctx, step.Node.Host, path, step.Prior); err != nil {
				return nil, err
			}
		}
		return map[string]any{"exists": false}, nil
	}
	original, err := p.aptSourceOriginal(ctx, step)
	if err != nil {
		return nil, err
	}
	content, err := desiredContent(step.Node)
	if err != nil {
		return nil, err
	}
	if err := p.writePathContent(ctx, step.Node.Host, path, content, stringDesired(step.Node, "owner"), stringDesired(step.Node, "group"), stringDesired(step.Node, "mode")); err != nil {
		return nil, err
	}
	observed := map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}
	copyAPTSourceOriginal(observed, original)
	return observed, nil
}

func (p NativeProvider) destroyAPTSourceFile(ctx context.Context, step Step) error {
	if step.Prior == nil {
		return nil
	}
	if aptSourceOnDestroy(step.Prior.Desired) != "restore" {
		return nil
	}
	if err := p.restoreAPTSourceFile(ctx, step.Prior.Host, stringMapValue(step.Prior.Desired, "path"), step.Prior); err != nil {
		return err
	}
	_, err := p.Runner.Run(ctx, step.Prior.Host, "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get update\n")
	return err
}

func (p NativeProvider) restoreAPTSourceFile(ctx context.Context, host, path string, prior *corestate.Resource) error {
	if prior == nil {
		return nil
	}
	if !boolMapValue(prior.Observed, "original_exists") {
		return p.removePath(ctx, host, path, false)
	}
	content := stringMapValue(prior.Observed, "original_content")
	owner := stringMapValue(prior.Observed, "original_owner")
	if owner == "" {
		owner = "root"
	}
	group := stringMapValue(prior.Observed, "original_group")
	if group == "" {
		group = "root"
	}
	mode := stringMapValue(prior.Observed, "original_mode")
	if mode == "" {
		mode = "0644"
	}
	return p.writePathContent(ctx, host, path, []byte(content), owner, group, mode)
}

func (p NativeProvider) planAPTSigningKey(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent apt signing key "+path, observed), nil
	}
	wantSHA, err := desiredSigningKeySHA(node)
	if err != nil {
		return ProviderPlan{}, err
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		(wantSHA != "" && current.SHA256 != wantSHA) ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if wantSHA == "" && prior != nil && prior.DesiredDigest != "" && prior.DesiredDigest != corestate.DesiredDigest(node.Desired) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update apt signing key " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for apt signing key "+path, observed), nil
}

func (p NativeProvider) applyAPTSigningKey(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	if stringDesired(step.Node, "content") != "" {
		return p.applyFileLike(ctx, step, false)
	}
	url := stringDesired(step.Node, "url")
	sha := stringDesired(step.Node, "sha256")
	if url == "" {
		return nil, fmt.Errorf("%s apt signing key requires url or content", step.Address)
	}
	tmp := path + ".dbf-tmp"
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
		"if ! command -v curl >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y ca-certificates curl",
		"fi",
		"curl -fsSL " + shellQuote(url) + " -o " + shellQuote(tmp),
	}
	if sha != "" {
		lines = append(lines, "printf '%s  %s\\n' "+shellQuote(sha)+" "+shellQuote(tmp)+" | sha256sum --check --status")
	}
	lines = append(lines,
		"install -o "+shellQuote(stringDesired(step.Node, "owner"))+
			" -g "+shellQuote(stringDesired(step.Node, "group"))+
			" -m "+shellQuote(stringDesired(step.Node, "mode"))+
			" "+shellQuote(tmp)+" "+shellQuote(path),
		"rm -f -- "+shellQuote(tmp),
	)
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planComponentDownload(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component source " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component source "+path, observed), nil
	}
	wantSHA := strings.ToLower(stringDesired(node, "sha256"))
	if wantSHA == "" || stringDesired(node, "url") == "" {
		return ProviderPlan{}, fmt.Errorf("%s component download requires url and sha256", node.Address)
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "download component source " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component source " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for component source "+path, observed), nil
}

func (p NativeProvider) applyComponentDownload(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	url := stringDesired(step.Node, "url")
	sha := strings.ToLower(stringDesired(step.Node, "sha256"))
	if url == "" || sha == "" {
		return nil, fmt.Errorf("%s component download requires url and sha256", step.Address)
	}
	tmp := path + ".dbf-tmp"
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
		"source_url=" + shellQuote(url),
		"case \"$source_url\" in",
		"  file://*) ;;",
		"  *)",
		"    if ! command -v curl >/dev/null 2>&1; then",
		"      export DEBIAN_FRONTEND=noninteractive",
		"      apt-get update",
		"      apt-get install -y ca-certificates curl",
		"    fi",
		"    ;;",
		"esac",
		"case \"$source_url\" in",
		"  file://*) cp -- \"${source_url#file://}\" " + shellQuote(tmp) + " ;;",
		"  *) curl -fsSL \"$source_url\" -o " + shellQuote(tmp) + " ;;",
		"esac",
		"printf '%s  %s\\n' " + shellQuote(sha) + " " + shellQuote(tmp) + " | sha256sum --check --status",
		"install -o " + shellQuote(stringDesired(step.Node, "owner")) +
			" -g " + shellQuote(stringDesired(step.Node, "group")) +
			" -m " + shellQuote(stringDesired(step.Node, "mode")) +
			" " + shellQuote(tmp) + " " + shellQuote(path),
		"rm -f -- " + shellQuote(tmp),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planComponentBuild(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "output_path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component build output " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component build output "+path, observed), nil
	}
	if path == "" || stringDesired(node, "cache_path") == "" || stringDesired(node, "build_path") == "" {
		return ProviderPlan{}, fmt.Errorf("%s component build requires cache_path, build_path, and output_path", node.Address)
	}
	if len(commandMatrixDesired(node, "commands")) == 0 {
		return ProviderPlan{}, fmt.Errorf("%s component build requires commands", node.Address)
	}
	if stringDesired(node, "output") == "" {
		return ProviderPlan{}, fmt.Errorf("%s component build requires output", node.Address)
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "build component output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component build output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	desiredDigest := corestate.DesiredDigest(node.Desired)
	priorSHA := ""
	if prior != nil {
		priorSHA = stringMapValue(prior.Observed, "sha256")
	}
	if prior == nil || prior.DesiredDigest != desiredDigest || priorSHA == "" || priorSHA != current.SHA256 {
		return ProviderPlan{Action: ActionUpdate, Summary: "rebuild component output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	observed["desired_digest"] = desiredDigest
	return ProviderPlan{Action: ActionNoOp, Summary: "no changes for component build output " + path, Observed: observed, Ownership: ownership(prior)}, nil
}

func (p NativeProvider) applyComponentBuild(ctx context.Context, step Step) (map[string]any, error) {
	outputPath := stringDesired(step.Node, "output_path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, outputPath, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	if outputPath == "" {
		return nil, fmt.Errorf("%s component build requires output_path", step.Address)
	}
	lines, err := componentBuildScript(step.Node)
	if err != nil {
		return nil, err
	}
	_, err = p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, outputPath)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentBinary(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component binary "+path, observed), nil
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	desiredDigest := corestate.DesiredDigest(node.Desired)
	priorSHA := ""
	if prior != nil {
		priorSHA = stringMapValue(prior.Observed, "sha256")
	}
	if prior == nil || prior.DesiredDigest != desiredDigest || priorSHA == "" || priorSHA != current.SHA256 {
		return ProviderPlan{Action: ActionUpdate, Summary: "reinstall component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	observed["desired_digest"] = desiredDigest
	return ProviderPlan{Action: ActionNoOp, Summary: "no changes for component binary " + path, Observed: observed, Ownership: ownership(prior)}, nil
}

func (p NativeProvider) applyComponentBinary(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	cachePath := stringDesired(step.Node, "cache_path")
	if cachePath == "" {
		return nil, fmt.Errorf("%s component binary requires cache_path", step.Address)
	}
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
	}
	if format := stringDesired(step.Node, "extract_format"); format != "" {
		if format != "zip" && format != "tar.gz" && format != "tar.xz" && format != "bz2" && format != "gz" {
			return nil, fmt.Errorf("%s unsupported component binary extract format %q", step.Address, format)
		}
		lines = append(lines, componentBinaryExtractInstallScript(step.Node)...)
	} else {
		lines = append(lines,
			"install -o "+shellQuote(stringDesired(step.Node, "owner"))+
				" -g "+shellQuote(stringDesired(step.Node, "group"))+
				" -m "+shellQuote(stringDesired(step.Node, "mode"))+
				" "+shellQuote(cachePath)+" "+shellQuote(path),
		)
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, path)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentFile(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component file " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component file "+path, observed), nil
	}
	wantSHA := strings.ToLower(stringDesired(node, "source_sha256"))
	if wantSHA == "" {
		return ProviderPlan{}, fmt.Errorf("%s component file requires source_sha256", node.Address)
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install component file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir ||
		current.SHA256 != wantSHA ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component file " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for component file "+path, observed), nil
}

func (p NativeProvider) applyComponentFile(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	cachePath := stringDesired(step.Node, "cache_path")
	if cachePath == "" {
		return nil, fmt.Errorf("%s component file requires cache_path", step.Address)
	}
	lines := []string{
		"set -eu",
		"mkdir -p \"$(dirname " + shellQuote(path) + ")\"",
		"install -o " + shellQuote(stringDesired(step.Node, "owner")) +
			" -g " + shellQuote(stringDesired(step.Node, "group")) +
			" -m " + shellQuote(stringDesired(step.Node, "mode")) +
			" " + shellQuote(cachePath) + " " + shellQuote(path),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, path)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentArchive(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent component archive "+path, observed), nil
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "install component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if !current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if prior == nil || prior.DesiredDigest != corestate.DesiredDigest(node.Desired) {
		return ProviderPlan{Action: ActionUpdate, Summary: "replace component archive " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for component archive "+path, observed), nil
}

func (p NativeProvider) applyComponentArchive(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if path == "" || path == "/" {
		return nil, fmt.Errorf("%s refuses to manage unsafe archive path %q", step.Address, path)
	}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removeDirectory(ctx, step.Node.Host, path); err != nil {
			return nil, err
		}
		return map[string]any{"exists": false}, nil
	}
	cachePath := stringDesired(step.Node, "cache_path")
	if cachePath == "" {
		return nil, fmt.Errorf("%s component archive requires cache_path", step.Address)
	}
	if format := stringDesired(step.Node, "extract_format"); format != "tar.gz" {
		return nil, fmt.Errorf("%s unsupported component archive extract format %q", step.Address, format)
	}
	stripComponents := fmt.Sprintf("%v", step.Node.Desired["strip_components"])
	if stripComponents == "" || stripComponents == "<nil>" {
		stripComponents = "0"
	}
	lines := []string{
		"set -eu",
		"if ! command -v tar >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar gzip",
		"fi",
		"parent=$(dirname " + shellQuote(path) + ")",
		"base=$(basename " + shellQuote(path) + ")",
		"mkdir -p \"$parent\"",
		"work=$(mktemp -d)",
		"trap 'rm -rf -- \"$work\"' EXIT",
		"staging=\"$work/staging\"",
		"mkdir -p \"$staging\"",
		"tar --no-same-owner -xzf " + shellQuote(cachePath) + " -C \"$staging\" --strip-components " + shellQuote(stripComponents),
		"chown -R " + shellQuote(stringDesired(step.Node, "owner")+":"+stringDesired(step.Node, "group")) + " \"$staging\"",
		"chmod " + shellQuote(stringDesired(step.Node, "mode")) + " \"$staging\"",
		"tmp=\"$parent/.${base}.dbf-new\"",
		"old=\"$parent/.${base}.dbf-old\"",
		"rm -rf -- \"$tmp\" \"$old\"",
		"mv \"$staging\" \"$tmp\"",
		"if [ -e " + shellQuote(path) + " ]; then mv " + shellQuote(path) + " \"$old\"; fi",
		"if mv \"$tmp\" " + shellQuote(path) + "; then",
		"  rm -rf -- \"$old\"",
		"else",
		"  if [ -e \"$old\" ]; then mv \"$old\" " + shellQuote(path) + "; fi",
		"  exit 1",
		"fi",
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	current, err := p.readPath(ctx, step.Node.Host, path)
	if err != nil {
		return nil, err
	}
	observed := current.observed()
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) planComponentScriptOutput(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if !current.Exists {
		action := ActionUpdate
		if prior == nil {
			action = ActionCreate
		}
		return ProviderPlan{Action: action, Summary: "run script to create output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.IsDir {
		return ProviderPlan{Action: ActionUpdate, Summary: "run script to repair output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if prior == nil {
		return ProviderPlan{Action: ActionUpdate, Summary: "refresh script output state " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if prior.DesiredDigest != "" && prior.DesiredDigest != corestate.DesiredDigest(node.Desired) {
		return ProviderPlan{Action: ActionUpdate, Summary: "refresh script output " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	wantSHA := stringMapValue(prior.Observed, "sha256")
	if wantSHA == "" {
		return ProviderPlan{Action: ActionUpdate, Summary: "refresh script output state " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if current.SHA256 != wantSHA {
		return ProviderPlan{Action: ActionUpdate, Summary: "repair script output drift " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for script output "+path, observed), nil
}

func (p NativeProvider) applyComponentScriptOutput(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	observed := cloneMap(step.Observed)
	if observed == nil {
		observed = map[string]any{}
	}
	observed["path"] = path
	observed["pending_script"] = true
	return observed, nil
}

func componentBinaryExtractInstallScript(node graph.Node) []string {
	cachePath := stringDesired(node, "cache_path")
	path := stringDesired(node, "path")
	format := stringDesired(node, "extract_format")
	if format == "bz2" || format == "gz" {
		command := "bzip2"
		packageName := "bzip2"
		if format == "gz" {
			command = "gzip"
			packageName = "gzip"
		}
		return []string{
			"if ! command -v " + command + " >/dev/null 2>&1; then",
			"  export DEBIAN_FRONTEND=noninteractive",
			"  apt-get update",
			"  apt-get install -y " + packageName,
			"fi",
			"work=$(mktemp -d)",
			"trap 'rm -rf -- \"$work\"' EXIT",
			command + " -dc " + shellQuote(cachePath) + " > \"$work/binary\"",
			"install -o " + shellQuote(stringDesired(node, "owner")) +
				" -g " + shellQuote(stringDesired(node, "group")) +
				" -m " + shellQuote(stringDesired(node, "mode")) +
				" \"$work/binary\" " + shellQuote(path),
		}
	}
	include := stringDesired(node, "include")
	stripComponents := fmt.Sprintf("%v", node.Desired["strip_components"])
	if stripComponents == "" || stripComponents == "<nil>" {
		stripComponents = "0"
	}
	lines := []string{
		"if [ " + shellQuote(format) + " = 'zip' ] && ! command -v unzip >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y unzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.gz' ] && ! command -v tar >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar gzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.xz' ] && { ! command -v tar >/dev/null 2>&1 || ! command -v xz >/dev/null 2>&1; }; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar xz-utils",
		"fi",
		"work=$(mktemp -d)",
		"trap 'rm -rf -- \"$work\"' EXIT",
	}
	if format == "zip" {
		lines = append(lines, "unzip -q "+shellQuote(cachePath)+" -d \"$work\"")
	} else if format == "tar.xz" {
		lines = append(lines, "tar --no-same-owner -xJf "+shellQuote(cachePath)+" -C \"$work\"")
	} else {
		lines = append(lines, "tar --no-same-owner -xzf "+shellQuote(cachePath)+" -C \"$work\"")
	}
	lines = append(lines,
		"include="+shellQuote(include),
		"strip_components="+shellQuote(stripComponents),
		"matches=\"$work/.dbf-matches\"",
		": > \"$matches\"",
		"find \"$work\" -type f | sort > \"$work/.dbf-files\"",
		"while IFS= read -r file; do",
		"  rel=${file#\"$work\"/}",
		"  stripped=$rel",
		"  i=0",
		"  while [ \"$i\" -lt \"$strip_components\" ]; do",
		"    case \"$stripped\" in */*) stripped=${stripped#*/} ;; *) stripped= ;; esac",
		"    i=$((i + 1))",
		"  done",
		"  if [ \"$stripped\" = \"$include\" ]; then printf '%s\\n' \"$file\" >> \"$matches\"; fi",
		"done < \"$work/.dbf-files\"",
		"count=$(wc -l < \"$matches\" | tr -d ' ')",
		"if [ \"$count\" != 1 ]; then echo \"component binary include matched $count files: $include\" >&2; exit 1; fi",
		"src=$(sed -n '1p' \"$matches\")",
		"install -o "+shellQuote(stringDesired(node, "owner"))+
			" -g "+shellQuote(stringDesired(node, "group"))+
			" -m "+shellQuote(stringDesired(node, "mode"))+
			" \"$src\" "+shellQuote(path),
	)
	return lines
}

func componentBuildScript(node graph.Node) ([]string, error) {
	cachePath := stringDesired(node, "cache_path")
	buildPath := stringDesired(node, "build_path")
	outputPath := stringDesired(node, "output_path")
	output := stringDesired(node, "output")
	if cachePath == "" || buildPath == "" || outputPath == "" || output == "" {
		return nil, fmt.Errorf("%s component build requires cache_path, build_path, output_path, and output", node.Address)
	}
	commands := commandMatrixDesired(node, "commands")
	if len(commands) == 0 {
		return nil, fmt.Errorf("%s component build requires commands", node.Address)
	}
	for i, command := range commands {
		if len(command) == 0 {
			return nil, fmt.Errorf("%s component build command %d is empty", node.Address, i)
		}
	}
	format := stringDesired(node, "extract_format")
	if format != "" && format != "zip" && format != "tar.gz" && format != "tar.xz" {
		return nil, fmt.Errorf("%s unsupported component source extract format %q", node.Address, format)
	}
	stripComponents := fmt.Sprintf("%v", node.Desired["strip_components"])
	if stripComponents == "" || stripComponents == "<nil>" {
		stripComponents = "0"
	}
	sourceName := stringDesired(node, "source_name")
	if sourceName == "" {
		sourceName = "source"
	}
	lines := []string{
		"set -eu",
		"if [ " + shellQuote(format) + " = 'zip' ] && ! command -v unzip >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y unzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.gz' ] && ! command -v tar >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar gzip",
		"fi",
		"if [ " + shellQuote(format) + " = 'tar.xz' ] && { ! command -v tar >/dev/null 2>&1 || ! command -v xz >/dev/null 2>&1; }; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		"  apt-get update",
		"  apt-get install -y tar xz-utils",
		"fi",
		"work=$(mktemp -d)",
		"trap 'rm -rf -- \"$work\"' EXIT",
		"src=\"$work/src\"",
		"mkdir -p \"$src\"",
	}
	switch format {
	case "zip":
		lines = append(lines, "unzip -q "+shellQuote(cachePath)+" -d \"$src\"")
	case "tar.gz":
		lines = append(lines, "tar --no-same-owner -xzf "+shellQuote(cachePath)+" -C \"$src\" --strip-components "+shellQuote(stripComponents))
	case "tar.xz":
		lines = append(lines, "tar --no-same-owner -xJf "+shellQuote(cachePath)+" -C \"$src\" --strip-components "+shellQuote(stripComponents))
	default:
		lines = append(lines, "cp -- "+shellQuote(cachePath)+" \"$src/"+sourceName+"\"")
	}
	lines = append(lines,
		"build_root="+shellQuote(buildPath),
		"rm -rf -- \"$build_root/work\"",
		"mkdir -p \"$build_root\"",
		"mv \"$src\" \"$build_root/work\"",
		"cd \"$build_root/work\"",
	)
	if workingDir := stringDesired(node, "working_dir"); workingDir != "" {
		lines = append(lines, "cd "+shellQuote(workingDir))
	}
	for _, command := range commands {
		lines = append(lines, shellCommandArgv(command))
	}
	lines = append(lines,
		"built="+shellQuote(output),
		"if [ ! -f \"$built\" ]; then echo \"component build output is missing: $built\" >&2; exit 1; fi",
		"mkdir -p \"$(dirname "+shellQuote(outputPath)+")\"",
		"install -o "+shellQuote(stringDesired(node, "owner"))+
			" -g "+shellQuote(stringDesired(node, "group"))+
			" -m "+shellQuote(stringDesired(node, "mode"))+
			" \"$built\" "+shellQuote(outputPath),
	)
	return lines, nil
}

func shellCommandArgv(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return "set -- " + strings.Join(quoted, " ") + "\n\"$@\""
}

func (p NativeProvider) planDirectory(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	path := stringDesired(node, "path")
	current, err := p.readPath(ctx, node.Host, path)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := current.observed()
	if ensureAbsent(node) {
		if current.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove directory " + path, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent directory "+path, observed), nil
	}
	if !current.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "create directory " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if !current.IsDir ||
		current.Owner != stringDesired(node, "owner") ||
		current.Group != stringDesired(node, "group") ||
		normalizeMode(current.Mode) != normalizeMode(stringDesired(node, "mode")) {
		return ProviderPlan{Action: ActionUpdate, Summary: "update directory " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for directory "+path, observed), nil
}

func (p NativeProvider) applyDirectory(ctx context.Context, step Step) (map[string]any, error) {
	path := stringDesired(step.Node, "path")
	if path == "" || path == "/" {
		return nil, fmt.Errorf("%s refuses to manage unsafe directory path %q", step.Address, path)
	}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		_, err := p.Runner.Run(ctx, step.Node.Host, "rm -rf -- "+shellQuote(path)+"\n")
		return map[string]any{"exists": false}, err
	}
	lines := []string{
		"set -eu",
		"mkdir -p -- " + shellQuote(path),
		"chown " + shellQuote(stringDesired(step.Node, "owner")+":"+stringDesired(step.Node, "group")) + " -- " + shellQuote(path),
		"chmod " + shellQuote(stringDesired(step.Node, "mode")) + " -- " + shellQuote(path),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

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

func (p NativeProvider) planKernelModule(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "name")
	path := modulePath(name)
	script := fmt.Sprintf(`module=%s
kmod=$(printf '%%s' "$module" | tr '-' '_')
if awk -v m="$kmod" '$1 == m { found = 1 } END { exit found ? 0 : 1 }' /proc/modules; then echo loaded; else echo missing; fi
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(name), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, node.Host, script)
	if err != nil {
		return ProviderPlan{}, err
	}
	lines := nonEmptyLines(result.Stdout)
	loaded := len(lines) > 0 && lines[0] == "loaded"
	persisted := len(lines) > 1 && lines[1] == sha256Hex([]byte(name+"\n"))
	observed := map[string]any{"loaded": loaded, "persisted": persisted}
	if ensureAbsent(node) {
		if loaded || persisted {
			return ProviderPlan{Action: ActionDelete, Summary: "unload kernel module " + name, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent kernel module "+name, observed), nil
	}
	if !loaded || !persisted {
		return ProviderPlan{Action: ActionUpdate, Summary: "modprobe " + name + " and write " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for kernel module "+name, observed), nil
}

func (p NativeProvider) applyKernelModule(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "name")
	path := modulePath(name)
	var lines []string
	lines = append(lines, "set -eu")
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "rm -f -- "+shellQuote(path), "modprobe -r "+shellQuote(name)+" 2>/dev/null || true")
	} else {
		lines = append(lines,
			"modprobe "+shellQuote(name),
			"mkdir -p -- \"$(dirname "+shellQuote(path)+")\"",
			"printf '%s\\n' "+shellQuote(name)+" > "+shellQuote(path),
			"chown root:root "+shellQuote(path),
			"chmod 0644 "+shellQuote(path),
		)
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"loaded": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planSysctl(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	key := stringDesired(node, "key")
	value := stringDesired(node, "value")
	path := sysctlPath(key)
	script := fmt.Sprintf(`sysctl -n %s 2>/dev/null || true
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(key), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, node.Host, script)
	if err != nil {
		return ProviderPlan{}, err
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	current := ""
	if len(lines) > 0 {
		current = strings.TrimSpace(lines[0])
	}
	persisted := len(lines) > 1 && strings.TrimSpace(lines[1]) == sha256Hex([]byte(key+" = "+value+"\n"))
	observed := map[string]any{"value": current, "persisted": persisted}
	if ensureAbsent(node) {
		if persisted {
			return ProviderPlan{Action: ActionDelete, Summary: "remove sysctl " + key, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent sysctl "+key, observed), nil
	}
	if current != value || !persisted {
		return ProviderPlan{Action: ActionUpdate, Summary: "sysctl -w " + key + "=" + value + " and write " + path, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for sysctl "+key, observed), nil
}

func (p NativeProvider) applySysctl(ctx context.Context, step Step) (map[string]any, error) {
	key := stringDesired(step.Node, "key")
	value := stringDesired(step.Node, "value")
	path := sysctlPath(key)
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		if err := p.removePath(ctx, step.Node.Host, path, false); err != nil {
			return nil, err
		}
		return map[string]any{"persisted": false, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
	}
	lines := []string{
		"set -eu",
		"sysctl -w " + shellQuote(key+"="+value),
		"mkdir -p -- \"$(dirname " + shellQuote(path) + ")\"",
		"printf '%s\\n' " + shellQuote(key+" = "+value) + " > " + shellQuote(path),
		"chown root:root " + shellQuote(path),
		"chmod 0644 " + shellQuote(path),
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"value": value, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planGroup(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "name")
	result, err := p.Runner.Run(ctx, node.Host, "getent group "+shellQuote(name)+" || true\n")
	if err != nil {
		return ProviderPlan{}, err
	}
	line := strings.TrimSpace(result.Stdout)
	exists := line != ""
	gid := ""
	if fields := strings.Split(line, ":"); len(fields) >= 3 {
		gid = fields[2]
	}
	observed := map[string]any{"exists": exists, "gid": gid}
	if ensureAbsent(node) {
		if exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove group " + name, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent group "+name, observed), nil
	}
	wantGID := stringDesired(node, "gid")
	if !exists {
		return ProviderPlan{Action: ActionCreate, Summary: "create group " + name, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if wantGID != "" && gid != wantGID {
		return ProviderPlan{Action: ActionUpdate, Summary: "set group " + name + " gid to " + wantGID, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for group "+name, observed), nil
}

func (p NativeProvider) applyGroup(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "name")
	quoted := shellQuote(name)
	lines := []string{"set -eu"}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "if getent group "+quoted+" >/dev/null; then groupdel "+quoted+"; fi")
	} else {
		add := "groupadd"
		if boolDesired(step.Node, "system") {
			add += " -r"
		}
		if gid := stringDesired(step.Node, "gid"); gid != "" {
			add += " -g " + shellQuote(gid)
		}
		add += " " + quoted
		mod := ":"
		if gid := stringDesired(step.Node, "gid"); gid != "" {
			mod = "groupmod -g " + shellQuote(gid) + " " + quoted
		}
		lines = append(lines, "if getent group "+quoted+" >/dev/null; then "+mod+"; else "+add+"; fi")
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planUser(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "name")
	cur, err := p.readUser(ctx, node.Host, name)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := map[string]any{"exists": cur.exists, "uid": cur.uid, "group": cur.primaryGroup, "home": cur.home, "shell": cur.shell, "groups": cur.groups}
	if ensureAbsent(node) {
		if cur.exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove user " + name, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent user "+name, observed), nil
	}
	if !cur.exists {
		return ProviderPlan{Action: ActionCreate, Summary: "create user " + name, Observed: observed, Ownership: ownership(prior)}, nil
	}
	var reasons []string
	if want := stringDesired(node, "uid"); want != "" && cur.uid != want {
		reasons = append(reasons, "uid")
	}
	if want := stringDesired(node, "group"); want != "" && want != cur.gidNum && want != cur.primaryGroup {
		reasons = append(reasons, "primary group")
	}
	if want := stringDesired(node, "home"); want != "" && cur.home != want {
		reasons = append(reasons, "home")
	}
	if want := stringDesired(node, "shell"); want != "" && cur.shell != want {
		reasons = append(reasons, "shell")
	}
	if want := stringListDesired(node, "groups"); len(want) > 0 && !sameStringSet(want, cur.groups) {
		reasons = append(reasons, "groups")
	}
	if len(reasons) > 0 {
		return ProviderPlan{Action: ActionUpdate, Summary: "update user " + name + " (" + strings.Join(reasons, ", ") + ")", Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for user "+name, observed), nil
}

func (p NativeProvider) applyUser(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "name")
	quoted := shellQuote(name)
	lines := []string{"set -eu"}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "if getent passwd "+quoted+" >/dev/null; then userdel "+quoted+"; fi")
	} else {
		flags := userFlags(step.Node)
		add := "useradd"
		if boolDesired(step.Node, "system") {
			add += " -r"
		} else {
			add += " -m"
		}
		add += flags + " " + quoted
		mod := ":"
		if flags != "" {
			mod = "usermod" + flags + " " + quoted
		}
		lines = append(lines, "if getent passwd "+quoted+" >/dev/null; then "+mod+"; else "+add+"; fi")
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planUserGroupMembership(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	user := stringDesired(node, "user")
	group := stringDesired(node, "group")
	cur, err := p.readUser(ctx, node.Host, user)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := map[string]any{
		"exists":        cur.exists,
		"user":          user,
		"group":         group,
		"primary_group": cur.primaryGroup,
		"present":       cur.exists && userInGroup(cur, group),
		"groups":        cur.groups,
	}
	if ensureAbsent(node) {
		if cur.exists && stringSliceContains(cur.groups, group) {
			return ProviderPlan{Action: ActionDelete, Summary: "remove user " + user + " from group " + group, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent user group membership "+user+":"+group, observed), nil
	}
	if !cur.exists {
		if membershipDependsOnManagedUser(node, user) {
			return ProviderPlan{Action: ActionCreate, Summary: "add user " + user + " to group " + group + " after creating user", Observed: observed, Ownership: ownership(prior)}, nil
		}
		return ProviderPlan{}, fmt.Errorf("%s: user %q does not exist; declare users.user[%q] or create it before applying group membership %q", node.Address, user, user, group)
	}
	if userInGroup(cur, group) {
		return inSyncPlan(node, prior, "no changes for user group membership "+user+":"+group, observed), nil
	}
	return ProviderPlan{
		Action:    ActionUpdate,
		Summary:   "add user " + user + " to group " + group + " (log out and back in for group session to refresh)",
		Observed:  observed,
		Ownership: ownership(prior),
	}, nil
}

func membershipDependsOnManagedUser(node graph.Node, user string) bool {
	needle := `.users.user["` + user + `"]`
	for _, dep := range node.DependsOn {
		if strings.Contains(dep, needle) {
			return true
		}
	}
	return false
}

func (p NativeProvider) applyUserGroupMembership(ctx context.Context, step Step) (map[string]any, error) {
	user := stringDesired(step.Node, "user")
	group := stringDesired(step.Node, "group")
	lines := []string{"set -eu"}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "if getent passwd "+shellQuote(user)+" >/dev/null && [ \"$(id -gn "+shellQuote(user)+")\" != "+shellQuote(group)+" ] && id -nG "+shellQuote(user)+" | tr ' ' '\\n' | grep -Fx "+shellQuote(group)+" >/dev/null; then gpasswd -d "+shellQuote(user)+" "+shellQuote(group)+"; fi")
	} else {
		lines = append(lines,
			"getent passwd "+shellQuote(user)+" >/dev/null || { echo "+shellQuote("debianform: user "+user+" does not exist; create it before applying group membership "+group)+" >&2; exit 1; }",
			"getent group "+shellQuote(group)+" >/dev/null || { echo "+shellQuote("debianform: group "+group+" does not exist; create it before applying membership for user "+user)+" >&2; exit 1; }",
			"usermod -aG "+shellQuote(group)+" "+shellQuote(user),
			"echo "+shellQuote("debianform: user "+user+" must log out and back in for "+group+" group membership to affect existing sessions"),
		)
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		return map[string]any{"exists": true, "present": false, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
	}
	return map[string]any{"exists": true, "present": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planAuthorizedKey(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	keytype, keyblob, err := splitAuthorizedKey(stringDesired(node, "key"))
	if err != nil {
		return ProviderPlan{}, fmt.Errorf("%s %w", node.Address, err)
	}
	script := authorizedKeyPreamble(node.Desired) +
		"if [ -n \"$home\" ] && [ -f \"$file\" ] && awk -v t=" + shellQuote(keytype) +
		" -v b=" + shellQuote(keyblob) +
		" '($1==t && $2==b){f=1} END{exit f?0:1}' \"$file\"; then echo present; else echo absent; fi\n"
	result, err := p.Runner.Run(ctx, node.Host, script)
	if err != nil {
		return ProviderPlan{}, err
	}
	present := strings.Contains(result.Stdout, "present")
	observed := map[string]any{"present": present}
	if ensureAbsent(node) {
		if present {
			return ProviderPlan{Action: ActionDelete, Summary: "remove authorized key for " + stringDesired(node, "user"), Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent authorized key", observed), nil
	}
	if !present {
		return ProviderPlan{Action: ActionCreate, Summary: "add authorized key for " + stringDesired(node, "user"), Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for authorized key", observed), nil
}

func (p NativeProvider) applyAuthorizedKey(ctx context.Context, step Step) (map[string]any, error) {
	key := stringDesired(step.Node, "key")
	keytype, keyblob, err := splitAuthorizedKey(key)
	if err != nil {
		return nil, err
	}
	match := "awk -v t=" + shellQuote(keytype) + " -v b=" + shellQuote(keyblob)
	var body string
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		body = "if [ -z \"$home\" ]; then exit 0; fi\n" +
			"if [ -f \"$file\" ]; then\n" +
			"  tmp=$(mktemp)\n" +
			"  " + match + " '!($1==t && $2==b)' \"$file\" > \"$tmp\"\n" +
			"  cat \"$tmp\" > \"$file\"\n" +
			"  rm -f \"$tmp\"\n" +
			"fi\n"
	} else {
		body = "dir=$(dirname \"$file\")\n" +
			"group=$(id -gn \"$user\")\n" +
			"mkdir -p \"$dir\"\n" +
			"chmod 0700 \"$dir\"\n" +
			"if ! { [ -f \"$file\" ] && " + match + " '($1==t && $2==b){f=1} END{exit f?0:1}' \"$file\"; }; then\n" +
			"  printf '%s\\n' " + shellQuote(key) + " >> \"$file\"\n" +
			"fi\n" +
			"chmod 0600 \"$file\"\n" +
			"chown \"$user\":\"$group\" \"$dir\" \"$file\"\n"
	}
	_, err = p.Runner.Run(ctx, step.Node.Host, "set -eu\n"+authorizedKeyPreamble(step.Node.Desired)+body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"present": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planService(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "unit")
	result, err := p.Runner.Run(ctx, node.Host, "printf 'enabled='; systemctl is-enabled "+shellQuote(name)+" 2>/dev/null || true\nprintf 'active='; systemctl is-active "+shellQuote(name)+" 2>/dev/null || true\n")
	if err != nil {
		return ProviderPlan{}, err
	}
	enabled := strings.Contains(result.Stdout, "enabled=enabled")
	active := strings.Contains(result.Stdout, "active=active")
	observed := map[string]any{"enabled": enabled, "active": active}
	var changes []string
	if want, ok := node.Desired["enabled"].(bool); ok && enabled != want {
		if want {
			changes = append(changes, "enable")
		} else {
			changes = append(changes, "disable")
		}
	}
	switch stringDesired(node, "state") {
	case "running":
		if !active {
			changes = append(changes, "start")
		}
	case "stopped":
		if active {
			changes = append(changes, "stop")
		}
	case "restarted":
		changes = append(changes, "restart")
	case "reloaded":
		changes = append(changes, "reload")
	}
	if len(changes) > 0 {
		return ProviderPlan{Action: ActionUpdate, Summary: strings.Join(changes, " ") + " service " + name, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for service "+name, observed), nil
}

func (p NativeProvider) applyService(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "unit")
	lines := []string{"set -eu"}
	if enabled, ok := step.Node.Desired["enabled"].(bool); ok {
		if enabled {
			lines = append(lines, "systemctl enable "+shellQuote(name))
		} else {
			lines = append(lines, "systemctl disable "+shellQuote(name))
		}
	}
	switch stringDesired(step.Node, "state") {
	case "running":
		lines = append(lines, "systemctl start "+shellQuote(name))
	case "stopped":
		lines = append(lines, "systemctl stop "+shellQuote(name))
	case "restarted":
		lines = append(lines, "systemctl restart "+shellQuote(name))
	case "reloaded":
		lines = append(lines, "systemctl reload "+shellQuote(name))
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planDockerComposeProject(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	observed, err := p.readDockerComposeProject(ctx, node)
	if err != nil {
		return ProviderPlan{}, err
	}
	want := stringDesired(node, "state")
	actual, _ := observed["state"].(string)
	project := stringDesired(node, "project")
	orphanCount := intMapValue(observed, "orphan_count")
	if want == "absent" {
		if actual == "absent" {
			return absentInSyncPlan(prior, "already absent docker compose project "+project, observed), nil
		}
		return ProviderPlan{Action: ActionDelete, Summary: "remove docker compose project " + project, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if actual != want {
		return ProviderPlan{Action: ActionUpdate, Summary: dockerComposeDriftSummary(project, want, actual, orphanCount), Observed: observed, Ownership: ownership(prior)}, nil
	}
	if orphanCount > 0 {
		return ProviderPlan{Action: ActionUpdate, Summary: dockerComposeOrphanSummary(project, orphanCount, boolDesired(node, "remove_orphans")), Observed: observed, Ownership: ownership(prior)}, nil
	}
	desiredDigest := corestate.DesiredDigest(node.Desired)
	if prior != nil && prior.DesiredDigest != "" && prior.DesiredDigest != desiredDigest {
		observed = cloneMap(observed)
		observed["desired_digest"] = prior.DesiredDigest
		if oldProject := stringMapValue(prior.Desired, "project"); oldProject != "" && oldProject != project {
			return ProviderPlan{Action: ActionUpdate, Summary: "replace docker compose project " + oldProject + " with " + project, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return ProviderPlan{Action: ActionUpdate, Summary: "update docker compose project " + project, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for docker compose project "+project, observed), nil
}

func (p NativeProvider) applyDockerComposeProject(ctx context.Context, step Step) (map[string]any, error) {
	if oldProject := dockerComposePriorProject(step); oldProject != "" && oldProject != stringDesired(step.Node, "project") {
		command := dockerComposeProjectCommandForProject(step.Node, oldProject, "down")
		if _, err := p.Runner.Run(ctx, step.Node.Host, command); err != nil {
			return nil, err
		}
	}
	command, err := dockerComposeProjectCommand(step.Node)
	if err != nil {
		return nil, err
	}
	_, err = p.Runner.Run(ctx, step.Node.Host, command)
	if err != nil {
		return nil, err
	}
	observed, err := p.readDockerComposeProject(ctx, step.Node)
	if err != nil {
		return nil, err
	}
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) readDockerComposeProject(ctx context.Context, node graph.Node) (map[string]any, error) {
	expectedServices := []string{}
	servicesResult, err := p.Runner.Run(ctx, node.Host, dockerComposeProjectServicesCommand(node))
	if err != nil {
		return nil, err
	}
	expectedServices = dockerComposeConfigServices(servicesResult.Stdout)
	psResult, err := p.Runner.Run(ctx, node.Host, dockerComposeProjectPSCommand(node))
	if err != nil {
		return nil, err
	}
	return summarizeDockerComposePS(psResult.Stdout, expectedServices), nil
}

func dockerComposeProjectServicesCommand(node graph.Node) string {
	args := dockerComposeBaseArgs(node)
	args = append(args, "config", "--services")
	return strings.Join(shellQuoteArgs(args), " ") + " 2>/dev/null || true\n"
}

func dockerComposeProjectPSCommand(node graph.Node) string {
	args := dockerComposeBaseArgs(node)
	args = append(args, "ps", "--all", "--format", "json")
	return strings.Join(shellQuoteArgs(args), " ") + " 2>/dev/null || true\n"
}

func dockerComposeProjectCommand(node graph.Node) (string, error) {
	args := dockerComposeBaseArgs(node)
	switch stringDesired(node, "state") {
	case "running":
		args = append(args, "up", "-d")
		args = append(args, dockerComposePullArgs(stringDesired(node, "pull"))...)
		args = append(args, dockerComposeRecreateArgs(stringDesired(node, "recreate"))...)
		if boolDesired(node, "remove_orphans") {
			args = append(args, "--remove-orphans")
		}
	case "stopped":
		args = append(args, "stop")
	case "absent":
		args = append(args, "down")
		if boolDesired(node, "remove_orphans") {
			args = append(args, "--remove-orphans")
		}
	default:
		return "", fmt.Errorf("%s unsupported docker compose state %q", node.Address, stringDesired(node, "state"))
	}
	return strings.Join(shellQuoteArgs(args), " ") + "\n", nil
}

func dockerComposeProjectCommandForProject(node graph.Node, project string, command ...string) string {
	args := []string{"docker", "compose", "-p", project}
	for _, file := range stringListDesired(node, "files") {
		args = append(args, "-f", file)
	}
	args = append(args, command...)
	return strings.Join(shellQuoteArgs(args), " ") + "\n"
}

func dockerComposeBaseArgs(node graph.Node) []string {
	args := []string{"docker", "compose", "-p", stringDesired(node, "project")}
	for _, file := range stringListDesired(node, "files") {
		args = append(args, "-f", file)
	}
	return args
}

func dockerComposePullArgs(value string) []string {
	switch value {
	case "never":
		return []string{"--pull", "never"}
	case "always":
		return []string{"--pull", "always"}
	default:
		return []string{"--pull", "missing"}
	}
}

func dockerComposeRecreateArgs(value string) []string {
	switch value {
	case "always":
		return []string{"--force-recreate"}
	case "never":
		return []string{"--no-recreate"}
	default:
		return nil
	}
}

func shellQuoteArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, shellQuote(arg))
	}
	return out
}

func summarizeDockerComposePS(stdout string, expectedServices []string) map[string]any {
	total := 0
	running := 0
	stopped := 0
	actualServices := []string{}
	for _, container := range dockerComposePSContainers(stdout) {
		total++
		if strings.EqualFold(container.State, "running") {
			running++
		} else {
			stopped++
		}
		if container.Service != "" {
			actualServices = append(actualServices, container.Service)
		}
	}
	expectedServices = dedupeStringValues(expectedServices)
	actualServices = dedupeStringValues(actualServices)
	state := dockerComposeProjectObservedState(total, running)
	orphanServices := dockerComposeOrphanServices(actualServices, expectedServices)
	return map[string]any{
		"exists":   total > 0,
		"state":    state,
		"services": map[string]any{"total": total, "running": running, "stopped": stopped, "expected": expectedServices, "actual": actualServices},
		"containers": map[string]any{
			"total": total,
		},
		"orphan_count":    len(orphanServices),
		"orphan_services": orphanServices,
	}
}

func dockerComposeProjectObservedState(total, running int) string {
	switch {
	case total == 0:
		return "absent"
	case running == total:
		return "running"
	case running == 0:
		return "stopped"
	default:
		return "degraded"
	}
}

type dockerComposeContainer struct {
	Service string
	State   string
	Name    string
}

func dockerComposePSContainers(stdout string) []dockerComposeContainer {
	text := strings.TrimSpace(stdout)
	if text == "" || text == "[]" {
		return nil
	}
	var array []map[string]any
	if err := json.Unmarshal([]byte(text), &array); err == nil {
		containers := make([]dockerComposeContainer, 0, len(array))
		for _, item := range array {
			if container := dockerComposePSContainer(item); container.State != "" {
				containers = append(containers, container)
			}
		}
		return containers
	}
	var containers []dockerComposeContainer
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "[]" {
			continue
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err == nil {
			if container := dockerComposePSContainer(item); container.State != "" {
				containers = append(containers, container)
			}
		}
	}
	return containers
}

func dockerComposePSContainer(item map[string]any) dockerComposeContainer {
	container := dockerComposeContainer{}
	for _, key := range []string{"State", "state"} {
		if state, ok := item[key].(string); ok {
			container.State = state
			break
		}
	}
	for _, key := range []string{"Service", "service"} {
		if service, ok := item[key].(string); ok {
			container.Service = service
			break
		}
	}
	for _, key := range []string{"Name", "name"} {
		if name, ok := item[key].(string); ok {
			container.Name = name
			break
		}
	}
	return container
}

func dockerComposeConfigServices(stdout string) []string {
	var services []string
	for _, line := range strings.Split(stdout, "\n") {
		service := strings.TrimSpace(line)
		if service == "" {
			continue
		}
		services = append(services, service)
	}
	return dedupeStringValues(services)
}

func dockerComposeOrphanServices(actual, expected []string) []string {
	if len(actual) == 0 || len(expected) == 0 {
		return nil
	}
	expectedSet := map[string]struct{}{}
	for _, service := range expected {
		expectedSet[service] = struct{}{}
	}
	var out []string
	for _, service := range dedupeStringValues(actual) {
		if _, ok := expectedSet[service]; !ok {
			out = append(out, service)
		}
	}
	return out
}

func dockerComposePriorProject(step Step) string {
	if step.Prior == nil {
		return ""
	}
	return stringMapValue(step.Prior.Desired, "project")
}

func dockerComposeDriftSummary(project, want, actual string, orphanCount int) string {
	summary := "converge docker compose project " + project + " from " + actual + " to " + want
	if orphanCount > 0 {
		summary += " and inspect " + pluralize(orphanCount, "orphan service")
	}
	return summary
}

func dockerComposeOrphanSummary(project string, orphanCount int, removeOrphans bool) string {
	if removeOrphans {
		return "remove " + pluralize(orphanCount, "orphan service") + " from docker compose project " + project
	}
	return "docker compose project " + project + " has " + pluralize(orphanCount, "orphan service") + "; set remove_orphans = true to clean them"
}

func pluralize(count int, singular string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func dedupeStringValues(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (p NativeProvider) removePath(ctx context.Context, host, path string, daemonReload bool) error {
	if path == "" {
		return nil
	}
	script := "set -eu\nrm -f -- " + shellQuote(path) + "\n"
	if daemonReload {
		script += "systemctl daemon-reload\n"
	}
	_, err := p.Runner.Run(ctx, host, script)
	return err
}

func (p NativeProvider) removeDirectory(ctx context.Context, host, path string) error {
	if path == "" || path == "/" {
		return nil
	}
	_, err := p.Runner.Run(ctx, host, "set -eu\nrm -rf -- "+shellQuote(path)+"\n")
	return err
}

type pathState struct {
	Exists bool
	IsDir  bool
	Owner  string
	Group  string
	Mode   string
	SHA256 string
}

func (s pathState) observed() map[string]any {
	return map[string]any{
		"exists": s.Exists,
		"is_dir": s.IsDir,
		"owner":  s.Owner,
		"group":  s.Group,
		"mode":   displayMode(s.Mode),
		"sha256": s.SHA256,
	}
}

func (p NativeProvider) readPath(ctx context.Context, host, path string) (pathState, error) {
	if path == "" {
		return pathState{}, nil
	}
	script := fmt.Sprintf(`set -eu
if [ ! -e %s ]; then
  echo missing
  exit 0
fi
if [ -d %s ]; then echo dir; else echo file; fi
stat -c '%%U' %s
stat -c '%%G' %s
stat -c '%%a' %s
if [ -f %s ]; then sha256sum %s | awk '{print $1}'; else echo ''; fi
`, shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, host, script)
	if err != nil {
		return pathState{}, err
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "missing" {
		return pathState{}, nil
	}
	st := pathState{Exists: true, IsDir: lines[0] == "dir"}
	if len(lines) > 1 {
		st.Owner = lines[1]
	}
	if len(lines) > 2 {
		st.Group = lines[2]
	}
	if len(lines) > 3 {
		st.Mode = lines[3]
	}
	if len(lines) > 4 {
		st.SHA256 = lines[4]
	}
	return st, nil
}

type pathContentState struct {
	pathState
	Content string
}

func (p NativeProvider) readPathWithContent(ctx context.Context, host, path string) (pathContentState, error) {
	if path == "" {
		return pathContentState{}, nil
	}
	script := fmt.Sprintf(`set -eu
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
`, shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path), shellQuote(path))
	result, err := p.Runner.Run(ctx, host, script)
	if err != nil {
		return pathContentState{}, err
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "missing" {
		return pathContentState{}, nil
	}
	st := pathContentState{pathState: pathState{Exists: true, IsDir: lines[0] == "dir"}}
	if len(lines) > 1 {
		st.Owner = lines[1]
	}
	if len(lines) > 2 {
		st.Group = lines[2]
	}
	if len(lines) > 3 {
		st.Mode = lines[3]
	}
	if len(lines) > 4 {
		st.SHA256 = lines[4]
	}
	if len(lines) > 5 && lines[5] != "" {
		data, err := base64.StdEncoding.DecodeString(lines[5])
		if err != nil {
			return pathContentState{}, err
		}
		st.Content = string(data)
	}
	return st, nil
}

func (p NativeProvider) writePathContent(ctx context.Context, host, path string, content []byte, owner, group, mode string) error {
	script := strings.Join([]string{
		"set -eu",
		"dest=" + shellQuote(path),
		"mkdir -p \"$(dirname \"$dest\")\"",
		"tmp=\"$(mktemp \"${dest}.dbf-tmp.XXXXXX\")\"",
		"trap 'rm -f -- \"$tmp\"' EXIT",
		"cat > \"$tmp\"",
		"install -o " + shellQuote(owner) +
			" -g " + shellQuote(group) +
			" -m " + shellQuote(mode) +
			" \"$tmp\" \"$dest\"",
	}, "\n") + "\n"
	_, err := p.Runner.RunInput(ctx, host, script, bytes.NewReader(content))
	if err != nil {
		return redactPayloadError(err)
	}
	return nil
}

type redactedPayloadError struct {
	err error
}

func (e redactedPayloadError) Error() string {
	return "redacted payload command failed: <redacted>"
}

func (e redactedPayloadError) Unwrap() error {
	return e.err
}

func redactPayloadError(err error) error {
	if err == nil {
		return nil
	}
	return redactedPayloadError{err: err}
}

func (p NativeProvider) aptSourceOriginal(ctx context.Context, step Step) (map[string]any, error) {
	original := map[string]any{}
	if step.Prior != nil && hasAPTSourceOriginal(step.Prior.Observed) {
		copyAPTSourceOriginal(original, step.Prior.Observed)
		return original, nil
	}
	current, err := p.readPathWithContent(ctx, step.Node.Host, stringDesired(step.Node, "path"))
	if err != nil {
		return nil, err
	}
	copyAPTSourceOriginal(original, aptSourceOriginalFromCurrent(current))
	return original, nil
}

func withAPTSourceOriginal(observed map[string]any, prior *corestate.Resource, current pathContentState) map[string]any {
	if observed == nil {
		observed = map[string]any{}
	}
	if prior != nil && hasAPTSourceOriginal(prior.Observed) {
		copyAPTSourceOriginal(observed, prior.Observed)
		return observed
	}
	copyAPTSourceOriginal(observed, aptSourceOriginalFromCurrent(current))
	return observed
}

func aptSourceOriginalFromCurrent(current pathContentState) map[string]any {
	return map[string]any{
		"original_exists":  current.Exists && !current.IsDir,
		"original_content": current.Content,
		"original_owner":   current.Owner,
		"original_group":   current.Group,
		"original_mode":    current.Mode,
	}
}

func hasAPTSourceOriginal(values map[string]any) bool {
	_, ok := values["original_exists"]
	return ok
}

func copyAPTSourceOriginal(dst map[string]any, src map[string]any) {
	for _, key := range []string{"original_exists", "original_content", "original_owner", "original_group", "original_mode"} {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

type userState struct {
	exists       bool
	uid          string
	gidNum       string
	primaryGroup string
	home         string
	shell        string
	groups       []string
}

func (p NativeProvider) readUser(ctx context.Context, host, name string) (userState, error) {
	quoted := shellQuote(name)
	script := "if getent passwd " + quoted + " >/dev/null 2>&1; then\n" +
		"  getent passwd " + quoted + "\n" +
		"  id -gn " + quoted + "\n" +
		"  id -nG " + quoted + "\n" +
		"else\n" +
		"  echo __ABSENT__\n" +
		"fi\n"
	result, err := p.Runner.Run(ctx, host, script)
	if err != nil {
		return userState{}, err
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "__ABSENT__" || strings.TrimSpace(lines[0]) == "" {
		return userState{}, nil
	}
	st := userState{exists: true}
	fields := strings.Split(lines[0], ":")
	if len(fields) >= 7 {
		st.uid = fields[2]
		st.gidNum = fields[3]
		st.home = fields[5]
		st.shell = fields[6]
	}
	if len(lines) > 1 {
		st.primaryGroup = strings.TrimSpace(lines[1])
	}
	if len(lines) > 2 {
		for _, group := range strings.Fields(lines[2]) {
			if group != st.primaryGroup {
				st.groups = append(st.groups, group)
			}
		}
	}
	return st, nil
}

func desiredContent(node graph.Node) ([]byte, error) {
	payload := providerDesired(node)
	if content, ok := payload["content"].(string); ok {
		return []byte(content), nil
	}
	if source, ok := payload["source_path"].(string); ok && source != "" {
		return os.ReadFile(source)
	}
	return nil, fmt.Errorf("%s requires content or source_path", node.Address)
}

func desiredContentSHA(node graph.Node) (string, error) {
	if sha := stringDesired(node, "content_sha256"); sha != "" {
		return strings.ToLower(sha), nil
	}
	if fileContentWriteOnly(node) {
		return "", nil
	}
	content, err := desiredContent(node)
	if err != nil {
		return "", err
	}
	return sha256Hex(content), nil
}

func fileContentWriteOnly(node graph.Node) bool {
	value, _ := node.Desired["content_write_only"].(bool)
	return value
}

func providerDesired(node graph.Node) map[string]any {
	if len(node.ProviderPayload) > 0 {
		return node.ProviderPayload
	}
	return node.Desired
}

func desiredSigningKeySHA(node graph.Node) (string, error) {
	if sha := stringDesired(node, "sha256"); sha != "" {
		return strings.ToLower(sha), nil
	}
	if content := stringDesired(node, "content"); content != "" {
		return sha256Hex([]byte(content)), nil
	}
	if url := stringDesired(node, "url"); url != "" {
		return "", nil
	}
	return "", fmt.Errorf("%s apt signing key requires url with optional sha256 or content", node.Address)
}

func inSyncPlan(node graph.Node, prior *corestate.Resource, summary string, observed map[string]any) ProviderPlan {
	if observed == nil {
		observed = map[string]any{}
	}
	observed = cloneMap(observed)
	observed["desired_digest"] = corestate.DesiredDigest(node.Desired)
	if prior == nil {
		return ProviderPlan{Action: ActionAdopt, Summary: "adopt existing " + node.Kind + " " + identity(node), Observed: observed, Ownership: "adopted"}
	}
	return ProviderPlan{Action: ActionNoOp, Summary: summary, Observed: observed, Ownership: ownership(prior)}
}

func absentInSyncPlan(prior *corestate.Resource, summary string, observed map[string]any) ProviderPlan {
	if observed == nil {
		observed = map[string]any{}
	}
	if prior != nil {
		return ProviderPlan{Action: ActionDelete, Summary: summary, Observed: cloneMap(observed), Ownership: ownership(prior)}
	}
	return ProviderPlan{Action: ActionNoOp, Summary: summary, Observed: cloneMap(observed), Ownership: ownership(prior)}
}

func ownership(prior *corestate.Resource) string {
	if prior != nil && prior.Ownership != "" {
		return prior.Ownership
	}
	return "managed"
}

func ensureAbsent(node graph.Node) bool {
	return stringDesired(node, "ensure") == "absent"
}

func stringDesired(node graph.Node, name string) string {
	return stringMapValue(node.Desired, name)
}

func stringMapValue(values map[string]any, name string) string {
	if value, ok := values[name]; ok {
		switch v := value.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		}
	}
	return ""
}

func boolMapValue(values map[string]any, name string) bool {
	value, _ := values[name].(bool)
	return value
}

func intMapValue(values map[string]any, name string) int {
	switch value := values[name].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func aptSourceOnDestroy(desired map[string]any) string {
	value := stringMapValue(desired, "on_destroy")
	if value == "" {
		return "keep"
	}
	return value
}

func boolDesired(node graph.Node, name string) bool {
	value, _ := node.Desired[name].(bool)
	return value
}

func boolObserved(step Step, name string) bool {
	value, _ := step.Observed[name].(bool)
	return value
}

func stringListDesired(node graph.Node, name string) []string {
	value, ok := node.Desired[name]
	if !ok || value == nil {
		return nil
	}
	return stringListAnyValue(value)
}

func stringListMapValue(values map[string]any, name string) []string {
	value, ok := values[name]
	if !ok || value == nil {
		return nil
	}
	return stringListAnyValue(value)
}

func stringListAnyValue(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func commandMatrixDesired(node graph.Node, name string) [][]string {
	value, ok := node.Desired[name]
	if !ok || value == nil {
		return nil
	}
	switch v := value.(type) {
	case [][]string:
		out := make([][]string, 0, len(v))
		for _, command := range v {
			out = append(out, append([]string(nil), command...))
		}
		return out
	case []any:
		out := make([][]string, 0, len(v))
		for _, rawCommand := range v {
			switch command := rawCommand.(type) {
			case []string:
				out = append(out, append([]string(nil), command...))
			case []any:
				args := make([]string, 0, len(command))
				for _, rawArg := range command {
					if arg, ok := rawArg.(string); ok {
						args = append(args, arg)
					}
				}
				out = append(out, args)
			}
		}
		return out
	default:
		return nil
	}
}

func userFlags(node graph.Node) string {
	var flags string
	if uid := stringDesired(node, "uid"); uid != "" {
		flags += " -u " + shellQuote(uid)
	}
	if group := stringDesired(node, "group"); group != "" {
		flags += " -g " + shellQuote(group)
	}
	if groups := stringListDesired(node, "groups"); len(groups) > 0 {
		flags += " -G " + shellQuote(strings.Join(groups, ","))
	}
	if home := stringDesired(node, "home"); home != "" {
		flags += " -d " + shellQuote(home)
	}
	if shell := stringDesired(node, "shell"); shell != "" {
		flags += " -s " + shellQuote(shell)
	}
	return flags
}

func authorizedKeyPreamble(desired map[string]any) string {
	preamble := "user=" + shellQuote(stringMapValue(desired, "user")) + "\n" +
		"home=$(getent passwd \"$user\" | cut -d: -f6) || home=\n"
	if path := stringMapValue(desired, "path"); path != "" {
		preamble += "file=" + shellQuote(path) + "\n"
	} else {
		preamble += "file=\"$home/.ssh/authorized_keys\"\n"
	}
	return preamble
}

func splitAuthorizedKey(key string) (keytype, keyblob string, err error) {
	fields := strings.Fields(strings.TrimSpace(key))
	if len(fields) < 2 {
		return "", "", fmt.Errorf("authorized key must contain a type and body")
	}
	return fields[0], fields[1], nil
}

func hostFromAddress(address string) string {
	if !strings.HasPrefix(address, "host.") {
		return ""
	}
	rest := strings.TrimPrefix(address, "host.")
	if idx := strings.Index(rest, "."); idx >= 0 {
		return rest[:idx]
	}
	return ""
}

func modulePath(name string) string {
	return "/etc/modules-load.d/dbf-" + safeName(name) + ".conf"
}

func sysctlPath(key string) string {
	return "/etc/sysctl.d/99-dbf-" + safeName(key) + ".conf"
}

func safeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "resource"
	}
	return strings.ToLower(out)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeMode(mode string) string {
	return strings.TrimLeft(mode, "0")
}

func displayMode(mode string) string {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= 4 {
		return trimmed
	}
	return strings.Repeat("0", 4-len(trimmed)) + trimmed
}

func nonEmptyLines(output string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func userInGroup(user userState, group string) bool {
	return user.primaryGroup == group || stringSliceContains(user.groups, group)
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
