package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

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
