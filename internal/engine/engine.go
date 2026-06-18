package engine

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
	"github.com/mofelee/debianform/internal/state"
)

type Engine struct {
	cfg     *config.Config
	runner  Runner
	backend *state.SSHBackend
}

// Runner executes shell scripts on a remote host. *sshx.Runner is the
// production implementation; tests substitute a fake so plan/apply logic
// can be exercised without a live SSH connection.
type Runner interface {
	Run(ctx context.Context, host, script string) (sshx.Result, error)
	RunCommand(ctx context.Context, host, remoteCommand string) (sshx.Result, error)
}

type Options struct {
	Host        string
	LockTimeout time.Duration
}

type Plan struct {
	Changes  []Change
	Handlers []HandlerRun
}

type Change struct {
	Address  string
	Action   string
	Summary  string
	Resource config.Resource
	Desired  Desired
	// Prior holds the recorded state for a "destroy" change, whose resource is
	// no longer present in configuration.
	Prior state.ResourceState
}

type HandlerRun struct {
	Address string
	Host    string
	Command string
	Reasons []string
}

type Desired struct {
	Name          string
	Key           string
	Value         string
	Path          string
	Content       string
	ContentSHA256 string
	Owner         string
	Group         string
	Mode          string
	Ensure        string
	Version       string
	UpdateCache   bool
	Enabled       *bool
	ServiceState  string
	Activate      bool
	Persist       bool
	ApplyRuntime  bool
	Validate      bool
	GID           string
	System        bool
	UID           string
	Home          string
	Shell         string
	Groups        []string
	User          string
	PublicKey     string
	Hostname      string
}

func New(cfg *config.Config, runner Runner, backend *state.SSHBackend) *Engine {
	return &Engine{cfg: cfg, runner: runner, backend: backend}
}

func (e *Engine) Plan(ctx context.Context, opts Options) (Plan, error) {
	prior, err := e.backend.Read(ctx)
	if err != nil {
		return Plan{}, err
	}
	resources, err := e.resources(opts)
	if err != nil {
		return Plan{}, err
	}
	managed := make(map[string]struct{}, len(resources))
	changes := make([]Change, 0, len(resources))
	for _, res := range resources {
		managed[res.Address] = struct{}{}
		change, err := e.planResource(ctx, res)
		if err != nil {
			return Plan{}, err
		}
		if change.Action != "no-op" {
			changes = append(changes, change)
		}
	}
	changes = append(changes, orphanDestroys(prior, managed, opts)...)
	return Plan{Changes: changes, Handlers: e.handlerRuns(changes)}, nil
}

// orphanDestroys returns destroy changes for resources that are recorded in
// state but no longer present in configuration. They are ordered by their
// recorded apply position, descending, so dependents are destroyed before
// their dependencies (the reverse of how they were created).
func orphanDestroys(prior state.State, managed map[string]struct{}, opts Options) []Change {
	var addrs []string
	for addr, rs := range prior.Resources {
		if priorString(rs, "type") == "handler" {
			continue
		}
		if _, ok := managed[addr]; ok {
			continue
		}
		if opts.Host != "" && priorString(rs, "host") != opts.Host {
			continue
		}
		if _, ok := providers[priorString(rs, "type")]; !ok {
			continue
		}
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		oi, oj := priorOrder(prior.Resources[addrs[i]]), priorOrder(prior.Resources[addrs[j]])
		if oi != oj {
			return oi > oj
		}
		return addrs[i] > addrs[j]
	})
	changes := make([]Change, 0, len(addrs))
	for _, addr := range addrs {
		rs := prior.Resources[addr]
		changes = append(changes, Change{Address: addr, Action: "destroy", Summary: destroySummary(rs), Prior: rs})
	}
	return changes
}

func destroySummary(rs state.ResourceState) string {
	resType := priorString(rs, "type")
	id := priorString(rs, "name")
	if id == "" {
		id = priorString(rs, "path")
	}
	if id == "" {
		id = priorString(rs, "user")
	}
	if id != "" {
		return "destroy " + resType + " " + id
	}
	return "destroy " + resType
}

func priorString(rs state.ResourceState, key string) string {
	if v, ok := rs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func priorOrder(rs state.ResourceState) float64 {
	switch n := rs["order"].(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func (e *Engine) Apply(ctx context.Context, opts Options) (Plan, error) {
	lock, err := e.backend.Lock(ctx, opts.LockTimeout)
	if err != nil {
		return Plan{}, err
	}
	defer lock.Unlock()

	st, err := e.backend.Read(ctx)
	if err != nil {
		return Plan{}, err
	}

	plan, err := e.Plan(ctx, opts)
	if err != nil {
		return Plan{}, err
	}
	for _, change := range plan.Changes {
		if err := e.applyChange(ctx, change); err != nil {
			return plan, err
		}
	}
	handlers := e.handlerRuns(plan.Changes)
	for _, handler := range handlers {
		if err := e.runHandler(ctx, handler); err != nil {
			return plan, err
		}
		st.Resources[handler.Address] = stateForHandler(handler)
	}
	plan.Handlers = handlers

	// Drop resources that were deleted or destroyed from state.
	removed := make(map[string]struct{})
	for _, change := range plan.Changes {
		if change.Action == "delete" || change.Action == "destroy" {
			removed[change.Address] = struct{}{}
			delete(st.Resources, change.Address)
		}
	}

	resources, err := e.resources(opts)
	if err != nil {
		return plan, err
	}
	for i, res := range resources {
		if _, gone := removed[res.Address]; gone {
			continue
		}
		desired, err := desiredFor(res)
		if err != nil {
			return plan, err
		}
		rs := stateForResource(res, desired)
		rs["order"] = i
		st.Resources[res.Address] = rs
	}

	// Prune state for handlers no longer declared in configuration.
	declared := make(map[string]struct{}, len(e.cfg.Handlers))
	for _, handler := range e.cfg.Handlers {
		declared[handler.Address] = struct{}{}
	}
	for addr, rs := range st.Resources {
		if priorString(rs, "type") == "handler" {
			if _, ok := declared[addr]; !ok {
				delete(st.Resources, addr)
			}
		}
	}

	if err := e.backend.Write(ctx, st); err != nil {
		return plan, err
	}
	return plan, nil
}

func (p Plan) HasChanges() bool {
	return len(p.Changes) > 0 || len(p.Handlers) > 0
}

func PrintPlan(w io.Writer, plan Plan) {
	if len(plan.Changes) == 0 {
		fmt.Fprintln(w, "No changes.")
		return
	}
	fmt.Fprintln(w, "Plan:")
	for _, change := range plan.Changes {
		symbol := "~"
		switch change.Action {
		case "create":
			symbol = "+"
		case "delete", "destroy":
			symbol = "-"
		}
		fmt.Fprintf(w, "  %s %s %s\n", symbol, change.Address, change.Summary)
		for _, notify := range change.Resource.Notify {
			fmt.Fprintf(w, "    notifies %s\n", notify)
		}
	}
	for _, handler := range plan.Handlers {
		fmt.Fprintf(w, "  ! %s run handler on %s\n", handler.Address, handler.Host)
	}
}

func (e *Engine) resources(opts Options) ([]config.Resource, error) {
	resources := make([]config.Resource, 0, len(e.cfg.Resources))
	for _, res := range e.cfg.Resources {
		if opts.Host != "" && res.Host != opts.Host {
			continue
		}
		resources = append(resources, res)
	}
	return topoSort(resources)
}

func (e *Engine) handlerRuns(changes []Change) []HandlerRun {
	handlers := make(map[string]config.Handler, len(e.cfg.Handlers))
	order := make(map[string]int, len(e.cfg.Handlers))
	for i, handler := range e.cfg.Handlers {
		handlers[handler.Address] = handler
		order[handler.Address] = i
	}
	reasons := map[string][]string{}
	for _, change := range changes {
		for _, target := range change.Resource.Notify {
			if _, ok := handlers[target]; !ok {
				continue
			}
			reasons[target] = append(reasons[target], change.Address)
		}
	}
	addresses := make([]string, 0, len(reasons))
	for address := range reasons {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return order[addresses[i]] < order[addresses[j]]
	})
	out := make([]HandlerRun, 0, len(addresses))
	for _, address := range addresses {
		handler := handlers[address]
		out = append(out, HandlerRun{
			Address: handler.Address,
			Host:    handler.Host,
			Command: handler.Command,
			Reasons: reasons[address],
		})
	}
	return out
}

func (e *Engine) runHandler(ctx context.Context, handler HandlerRun) error {
	_, err := e.runner.RunCommand(ctx, handler.Host, handler.Command)
	if err != nil {
		return fmt.Errorf("%s failed: %w", handler.Address, err)
	}
	return nil
}

func topoSort(resources []config.Resource) ([]config.Resource, error) {
	byAddress := map[string]config.Resource{}
	for _, res := range resources {
		byAddress[res.Address] = res
	}
	visited := map[string]int{}
	var out []config.Resource
	var visit func(config.Resource) error
	visit = func(res config.Resource) error {
		switch visited[res.Address] {
		case 1:
			return fmt.Errorf("dependency cycle at %s", res.Address)
		case 2:
			return nil
		}
		visited[res.Address] = 1
		for _, dep := range res.DependsOn {
			for _, match := range dependencyMatches(dep, byAddress) {
				if err := visit(match); err != nil {
					return err
				}
			}
		}
		visited[res.Address] = 2
		out = append(out, res)
		return nil
	}
	for _, res := range resources {
		if err := visit(res); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func dependencyMatches(dep string, byAddress map[string]config.Resource) []config.Resource {
	if res, ok := byAddress[dep]; ok {
		return []config.Resource{res}
	}
	prefix := dep + "["
	var out []config.Resource
	for addr, res := range byAddress {
		if strings.HasPrefix(addr, prefix) {
			out = append(out, res)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func (e *Engine) planResource(ctx context.Context, res config.Resource) (Change, error) {
	p, err := lookupProvider(res.Type)
	if err != nil {
		return Change{}, err
	}
	desired, err := p.Desired(res)
	if err != nil {
		return Change{}, err
	}
	return p.Plan(ctx, e, res, desired)
}

func (e *Engine) applyChange(ctx context.Context, change Change) error {
	if change.Action == "destroy" {
		p, err := lookupProvider(priorString(change.Prior, "type"))
		if err != nil {
			return err
		}
		return p.Destroy(ctx, e, change.Prior)
	}
	p, err := lookupProvider(change.Resource.Type)
	if err != nil {
		return err
	}
	return p.Apply(ctx, e, change)
}

func contentAttr(res config.Resource) (string, error) {
	if value, ok := res.Attrs["content"]; ok {
		return config.String(value), nil
	}
	if value, ok := res.Attrs["source"]; ok {
		path := config.String(value)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("%s read source %s: %w", res.Address, path, err)
		}
		return string(data), nil
	}
	return "", nil
}

func objectName(res config.Resource) string {
	if value, ok := res.Attrs["name"]; ok {
		return config.String(value)
	}
	if res.Key != nil {
		return *res.Key
	}
	return res.Name
}

func stringAttr(res config.Resource, name, def string) string {
	if value, ok := res.Attrs[name]; ok {
		return config.String(value)
	}
	return def
}

func boolAttr(res config.Resource, name string, def bool) bool {
	if value, ok := res.Attrs[name]; ok {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return def
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}

func stateFor(change Change) state.ResourceState {
	return stateForResource(change.Resource, change.Desired)
}

func stateForResource(res config.Resource, desired Desired) state.ResourceState {
	out := state.ResourceState{
		"type":       res.Type,
		"host":       res.Host,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if desired.Name != "" {
		out["name"] = desired.Name
	}
	if desired.Key != "" {
		out["key"] = desired.Key
	}
	if desired.Value != "" {
		out["value"] = desired.Value
	}
	if desired.Path != "" {
		out["path"] = desired.Path
	}
	if desired.ContentSHA256 != "" {
		out["content_sha256"] = desired.ContentSHA256
	}
	if desired.Version != "" {
		out["version"] = desired.Version
	}
	if desired.User != "" {
		out["user"] = desired.User
	}
	if desired.PublicKey != "" {
		out["public_key"] = desired.PublicKey
	}
	return out
}

func stateForHandler(handler HandlerRun) state.ResourceState {
	return state.ResourceState{
		"type":           "handler",
		"host":           handler.Host,
		"command_sha256": hash(handler.Command),
		"reasons":        handler.Reasons,
		"updated_at":     time.Now().UTC().Format(time.RFC3339),
	}
}

func (e *Engine) planPackage(ctx context.Context, res config.Resource, d Desired) (Change, error) {
	script := fmt.Sprintf(`dpkg-query -W -f='${Status}\t${Version}\n' %s 2>/dev/null || true`, sshx.ShellQuote(d.Name))
	result, err := e.runner.Run(ctx, res.Host, script)
	if err != nil {
		return Change{}, err
	}
	installed := strings.Contains(result.Stdout, "install ok installed")
	version := ""
	fields := strings.Split(strings.TrimSpace(result.Stdout), "\t")
	if len(fields) > 1 {
		version = fields[1]
	}
	if d.Ensure == "absent" {
		if installed {
			return change(res, d, "delete", "remove package "+d.Name), nil
		}
		return noChange(res, d), nil
	}
	if !installed {
		return change(res, d, "create", "install package "+d.Name), nil
	}
	if d.Version != "" && version != d.Version {
		return change(res, d, "update", fmt.Sprintf("install package %s version %s", d.Name, d.Version)), nil
	}
	return noChange(res, d), nil
}

func (e *Engine) applyPackage(ctx context.Context, change Change) error {
	d := change.Desired
	var lines []string
	lines = append(lines, "set -eu", "export DEBIAN_FRONTEND=noninteractive")
	if d.UpdateCache {
		lines = append(lines, "apt-get update")
	}
	if d.Ensure == "absent" {
		lines = append(lines, "apt-get remove -y "+sshx.ShellQuote(d.Name))
	} else {
		pkg := d.Name
		if d.Version != "" {
			pkg += "=" + d.Version
		}
		lines = append(lines, "apt-get install -y "+sshx.ShellQuote(pkg))
	}
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
	return err
}

func (e *Engine) planFile(ctx context.Context, res config.Resource, d Desired) (Change, error) {
	current, err := e.readPath(ctx, res.Host, d.Path)
	if err != nil {
		return Change{}, err
	}
	if !current.Exists {
		return change(res, d, "create", "write file "+d.Path), nil
	}
	if current.SHA256 != d.ContentSHA256 || current.Owner != d.Owner || current.Group != d.Group || normalizeMode(current.Mode) != normalizeMode(d.Mode) {
		return change(res, d, "update", "update file "+d.Path), nil
	}
	if res.Type == "debian_networkd_file" && d.Activate {
		return change(res, d, "update", "reload systemd-networkd after "+d.Path), nil
	}
	return noChange(res, d), nil
}

func (e *Engine) applyFile(ctx context.Context, change Change) error {
	d := change.Desired
	encoded := base64.StdEncoding.EncodeToString([]byte(d.Content))
	tmp := d.Path + ".dbf-tmp"
	backup := boolAttr(change.Resource, "backup", false)
	var backupLine string
	if backup {
		backupLine = fmt.Sprintf(`[ ! -e %s ] || cp -a %s %s.$(date +%%Y%%m%%d%%H%%M%%S).bak`, sshx.ShellQuote(d.Path), sshx.ShellQuote(d.Path), sshx.ShellQuote(d.Path))
	}
	script := fmt.Sprintf(`set -eu
mkdir -p "$(dirname %s)"
%s
base64 -d > %s <<'__DBF_FILE__'
%s
__DBF_FILE__
install -o %s -g %s -m %s %s %s
rm -f %s
`, sshx.ShellQuote(d.Path), backupLine, sshx.ShellQuote(tmp), encoded, sshx.ShellQuote(d.Owner), sshx.ShellQuote(d.Group), sshx.ShellQuote(d.Mode), sshx.ShellQuote(tmp), sshx.ShellQuote(d.Path), sshx.ShellQuote(tmp))
	if change.Resource.Type == "debian_networkd_file" && d.Activate {
		script += "systemctl reload-or-restart systemd-networkd\n"
	}
	_, err := e.runner.Run(ctx, change.Resource.Host, script)
	return err
}

func (e *Engine) applyNftablesFile(ctx context.Context, change Change) error {
	d := change.Desired
	encoded := base64.StdEncoding.EncodeToString([]byte(d.Content))
	tmp := d.Path + ".dbf-tmp"
	var validateLine string
	if d.Validate {
		validateLine = "nft -c -f " + sshx.ShellQuote(tmp)
	}
	var activateLine string
	if d.Activate {
		activateLine = "nft -f " + sshx.ShellQuote(d.Path)
	}
	script := fmt.Sprintf(`set -eu
mkdir -p "$(dirname %s)"
base64 -d > %s <<'__DBF_NFT__'
%s
__DBF_NFT__
%s
install -o %s -g %s -m %s %s %s
rm -f %s
%s
`, sshx.ShellQuote(d.Path), sshx.ShellQuote(tmp), encoded, validateLine, sshx.ShellQuote(d.Owner), sshx.ShellQuote(d.Group), sshx.ShellQuote(d.Mode), sshx.ShellQuote(tmp), sshx.ShellQuote(d.Path), sshx.ShellQuote(tmp), activateLine)
	_, err := e.runner.Run(ctx, change.Resource.Host, script)
	return err
}

func (e *Engine) planDirectory(ctx context.Context, res config.Resource, d Desired) (Change, error) {
	current, err := e.readPath(ctx, res.Host, d.Path)
	if err != nil {
		return Change{}, err
	}
	if d.Ensure == "absent" {
		if current.Exists {
			return change(res, d, "delete", "remove directory "+d.Path), nil
		}
		return noChange(res, d), nil
	}
	if !current.Exists {
		return change(res, d, "create", "create directory "+d.Path), nil
	}
	if !current.IsDir {
		return change(res, d, "update", "replace non-directory "+d.Path), nil
	}
	if d.Owner != "" && current.Owner != d.Owner || d.Group != "" && current.Group != d.Group || d.Mode != "" && normalizeMode(current.Mode) != normalizeMode(d.Mode) {
		return change(res, d, "update", "update directory "+d.Path), nil
	}
	return noChange(res, d), nil
}

func (e *Engine) applyDirectory(ctx context.Context, change Change) error {
	d := change.Desired
	if d.Path == "" || d.Path == "/" {
		return fmt.Errorf("%s refuses to manage unsafe directory path %q", change.Address, d.Path)
	}
	if d.Ensure == "absent" {
		_, err := e.runner.Run(ctx, change.Resource.Host, "rm -rf -- "+sshx.ShellQuote(d.Path)+"\n")
		return err
	}
	var lines []string
	lines = append(lines, "set -eu")
	lines = append(lines, "mkdir -p -- "+sshx.ShellQuote(d.Path))
	if d.Owner != "" || d.Group != "" {
		owner := d.Owner
		group := d.Group
		if owner == "" {
			owner = "root"
		}
		if group == "" {
			group = "root"
		}
		lines = append(lines, "chown "+sshx.ShellQuote(owner+":"+group)+" -- "+sshx.ShellQuote(d.Path))
	}
	if d.Mode != "" {
		lines = append(lines, "chmod "+sshx.ShellQuote(d.Mode)+" -- "+sshx.ShellQuote(d.Path))
	}
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
	return err
}

func (e *Engine) planService(ctx context.Context, res config.Resource, d Desired) (Change, error) {
	script := fmt.Sprintf(`printf 'enabled='; systemctl is-enabled %s 2>/dev/null || true
printf 'active='; systemctl is-active %s 2>/dev/null || true
`, sshx.ShellQuote(d.Name), sshx.ShellQuote(d.Name))
	result, err := e.runner.Run(ctx, res.Host, script)
	if err != nil {
		return Change{}, err
	}
	enabled := strings.Contains(result.Stdout, "enabled=enabled")
	active := strings.Contains(result.Stdout, "active=active")
	var changes []string
	if d.Enabled != nil && enabled != *d.Enabled {
		if *d.Enabled {
			changes = append(changes, "enable")
		} else {
			changes = append(changes, "disable")
		}
	}
	switch d.ServiceState {
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
	if len(changes) == 0 {
		return noChange(res, d), nil
	}
	return change(res, d, "update", strings.Join(changes, " ")+" service "+d.Name), nil
}

func (e *Engine) applyService(ctx context.Context, change Change) error {
	d := change.Desired
	var lines []string
	lines = append(lines, "set -eu")
	if d.Enabled != nil {
		if *d.Enabled {
			lines = append(lines, "systemctl enable "+sshx.ShellQuote(d.Name))
		} else {
			lines = append(lines, "systemctl disable "+sshx.ShellQuote(d.Name))
		}
	}
	switch d.ServiceState {
	case "running":
		lines = append(lines, "systemctl start "+sshx.ShellQuote(d.Name))
	case "stopped":
		lines = append(lines, "systemctl stop "+sshx.ShellQuote(d.Name))
	case "restarted":
		lines = append(lines, "systemctl restart "+sshx.ShellQuote(d.Name))
	case "reloaded":
		lines = append(lines, "systemctl reload "+sshx.ShellQuote(d.Name))
	}
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
	return err
}

func (e *Engine) planKernelModule(ctx context.Context, res config.Resource, d Desired) (Change, error) {
	script := fmt.Sprintf(`module=%s
kmod=$(printf '%%s' "$module" | tr '-' '_')
if awk -v m="$kmod" '$1 == m { found = 1 } END { exit found ? 0 : 1 }' /proc/modules; then
  echo loaded
else
  echo missing
fi
`, sshx.ShellQuote(d.Name))
	result, err := e.runner.Run(ctx, res.Host, script)
	if err != nil {
		return Change{}, err
	}
	loaded := strings.Contains(result.Stdout, "loaded")
	persisted := true
	persistedExists := false
	if d.Persist {
		current, err := e.readPath(ctx, res.Host, d.Path)
		if err != nil {
			return Change{}, err
		}
		persistedExists = current.Exists
		persisted = current.Exists && current.SHA256 == d.ContentSHA256
	}
	if d.Ensure == "absent" {
		if loaded || persistedExists {
			return change(res, d, "delete", "unload kernel module "+d.Name+" and remove "+d.Path), nil
		}
		return noChange(res, d), nil
	}
	if !loaded || !persisted {
		summary := "modprobe " + d.Name
		if d.Persist {
			summary += " and write " + d.Path
		}
		return change(res, d, "update", summary), nil
	}
	return noChange(res, d), nil
}

func (e *Engine) applyKernelModule(ctx context.Context, change Change) error {
	d := change.Desired
	var lines []string
	lines = append(lines, "set -eu")
	if d.Ensure == "absent" {
		if d.Persist {
			lines = append(lines, "rm -f -- "+sshx.ShellQuote(d.Path))
		}
		lines = append(lines, "modprobe -r "+sshx.ShellQuote(d.Name)+" 2>/dev/null || true")
	} else {
		lines = append(lines, "modprobe "+sshx.ShellQuote(d.Name))
		if d.Persist {
			lines = append(lines,
				"mkdir -p -- \"$(dirname "+sshx.ShellQuote(d.Path)+")\"",
				"printf '%s\\n' "+sshx.ShellQuote(d.Name)+" > "+sshx.ShellQuote(d.Path),
				"chown root:root "+sshx.ShellQuote(d.Path),
				"chmod 0644 "+sshx.ShellQuote(d.Path),
			)
		}
	}
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
	return err
}

func (e *Engine) planSysctl(ctx context.Context, res config.Resource, d Desired) (Change, error) {
	runtimeOK := true
	if d.ApplyRuntime {
		script := fmt.Sprintf(`sysctl -n %s 2>/dev/null || true`, sshx.ShellQuote(d.Key))
		result, err := e.runner.Run(ctx, res.Host, script)
		if err != nil {
			return Change{}, err
		}
		current := lastNonEmptyLine(result.Stdout)
		runtimeOK = current == d.Value
	}
	persisted := true
	if d.Persist {
		current, err := e.readPath(ctx, res.Host, d.Path)
		if err != nil {
			return Change{}, err
		}
		persisted = current.Exists && current.SHA256 == d.ContentSHA256
	}
	if !runtimeOK || !persisted {
		summary := "sysctl -w " + d.Key + "=" + d.Value
		if d.Persist {
			summary += " and write " + d.Path
		}
		return change(res, d, "update", summary), nil
	}
	return noChange(res, d), nil
}

func (e *Engine) applySysctl(ctx context.Context, change Change) error {
	d := change.Desired
	var lines []string
	lines = append(lines, "set -eu")
	if d.ApplyRuntime {
		lines = append(lines, "sysctl -w "+sshx.ShellQuote(d.Key+"="+d.Value))
	}
	if d.Persist {
		lines = append(lines,
			"mkdir -p -- \"$(dirname "+sshx.ShellQuote(d.Path)+")\"",
			"printf '%s\\n' "+sshx.ShellQuote(d.Key+" = "+d.Value)+" > "+sshx.ShellQuote(d.Path),
			"chown root:root "+sshx.ShellQuote(d.Path),
			"chmod 0644 "+sshx.ShellQuote(d.Path),
		)
	}
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
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

func (e *Engine) readPath(ctx context.Context, host, path string) (pathState, error) {
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
`, sshx.ShellQuote(path), sshx.ShellQuote(path), sshx.ShellQuote(path), sshx.ShellQuote(path), sshx.ShellQuote(path), sshx.ShellQuote(path), sshx.ShellQuote(path))
	result, err := e.runner.Run(ctx, host, script)
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

func change(res config.Resource, d Desired, action, summary string) Change {
	return Change{Address: res.Address, Action: action, Summary: summary, Resource: res, Desired: d}
}

func noChange(res config.Resource, d Desired) Change {
	return Change{Address: res.Address, Action: "no-op", Resource: res, Desired: d}
}

func normalizeMode(mode string) string {
	return strings.TrimLeft(mode, "0")
}

func lastNonEmptyLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}
