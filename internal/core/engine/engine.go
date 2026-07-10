package engine

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	coreplan "github.com/mofelee/debianform/internal/core/plan"
	corestate "github.com/mofelee/debianform/internal/core/state"
	"github.com/mofelee/debianform/internal/core/termstyle"
)

const (
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionDestroy = "destroy"
	ActionAdopt   = "adopt"
	ActionForget  = "forget"
	ActionNoOp    = "no-op"
	ActionRun     = "run"
)

type Backend interface {
	Read(ctx context.Context, host ir.HostSpec) (corestate.State, error)
	Write(ctx context.Context, host ir.HostSpec, st corestate.State) error
	Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error)
}

type Lock interface {
	Unlock(ctx context.Context) error
}

type Provider interface {
	Plan(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error)
	Apply(ctx context.Context, step Step) (map[string]any, error)
	Destroy(ctx context.Context, step Step) error
	RunOperation(ctx context.Context, operation graph.Operation) (OperationResult, error)
}

type HostPlanner interface {
	PlanHost(ctx context.Context, host ir.HostSpec, nodes []graph.Node, priors map[string]*corestate.Resource) (map[string]ProviderPlan, error)
}

type ProviderPlan struct {
	Action    string
	Summary   string
	Observed  map[string]any
	Ownership string
}

type planRequest struct {
	node  graph.Node
	order int
}

type Observed struct {
	Exists        bool
	DesiredDigest string
	Values        map[string]any
}

type Options struct {
	Host            string
	LockTimeout     time.Duration
	Parallel        int
	PerHostParallel int
	Progress        io.Writer
	ProgressStyle   termstyle.Options
}

type Engine struct {
	Backend  Backend
	Provider Provider
	Now      func() time.Time
}

type Plan struct {
	Steps      []Step
	Operations []OperationStep
	Summary    coreplan.Summary
}

type Step struct {
	Address   string
	Host      string
	Action    string
	Summary   string
	Node      graph.Node
	Prior     *corestate.Resource
	Observed  map[string]any
	Ownership string
	Order     int
}

type OperationStep struct {
	Address          string
	Action           string
	Operation        graph.Operation
	TriggerAddresses []string
	TriggerPaths     []string
}

type OperationResult struct {
	Outputs map[string]map[string]any
}

func (e Engine) Plan(ctx context.Context, program *ir.Program, resourceGraph *graph.ResourceGraph, opts Options) (Plan, error) {
	if e.Backend == nil {
		return Plan{}, fmt.Errorf("engine backend is required")
	}
	if e.Provider == nil {
		return Plan{}, fmt.Errorf("engine provider is required")
	}
	if err := resourceGraph.Validate(); err != nil {
		return Plan{}, err
	}
	progress := newProgressLoggerWithStyle(opts.Progress, opts.ProgressStyle)
	hosts := hostsByName(program)
	selectedHosts := selectedProgramHosts(program, opts)
	parallel := boundedHostParallel(opts.Parallel, len(selectedHosts))
	stateByHost, err := e.readStates(ctx, selectedHosts, parallel, progress)
	if err != nil {
		return Plan{}, err
	}

	desired := map[string]graph.Node{}
	var nodes []planRequest
	nodesByHost := map[string][]graph.Node{}
	priorsByHost := map[string]map[string]*corestate.Resource{}
	for order, node := range resourceGraph.Nodes {
		if opts.Host != "" && node.Host != opts.Host {
			continue
		}
		desired[node.Address] = node
		host, ok := hosts[node.Host]
		if !ok {
			return Plan{}, fmt.Errorf("%s references unknown host %q", node.Address, node.Host)
		}
		st := stateByHost[host.Name]
		prior := priorResource(st, node.Address)
		nodes = append(nodes, planRequest{node: node, order: order})
		nodesByHost[host.Name] = append(nodesByHost[host.Name], node)
		if priorsByHost[host.Name] == nil {
			priorsByHost[host.Name] = map[string]*corestate.Resource{}
		}
		priorsByHost[host.Name][node.Address] = prior
	}

	providerPlans, err := e.planNodes(ctx, selectedHosts, parallel, nodes, nodesByHost, priorsByHost, progress)
	if err != nil {
		return Plan{}, err
	}

	changed := map[string]struct{}{}
	steps := []Step{}
	noOp := 0
	for _, item := range nodes {
		node := item.node
		providerPlan := providerPlans[node.Address]
		if providerPlan.Action == "" {
			providerPlan.Action = ActionNoOp
		}
		if providerPlan.Summary == "" {
			providerPlan.Summary = defaultSummary(providerPlan.Action, node.Summary)
		}
		if destroysResource(providerPlan.Action) && preventsDestroy(node.Lifecycle) {
			return Plan{}, preventDestroyError(node.Address, node.Kind, node.Lifecycle)
		}
		prior := priorsByHost[node.Host][node.Address]
		step := Step{
			Address:   node.Address,
			Host:      node.Host,
			Action:    providerPlan.Action,
			Summary:   providerPlan.Summary,
			Node:      node,
			Prior:     prior,
			Observed:  providerPlan.Observed,
			Ownership: providerPlan.Ownership,
			Order:     item.order,
		}
		if step.Action == ActionNoOp {
			noOp++
		} else {
			steps = append(steps, step)
			if triggersOperation(step.Action) {
				changed[step.Address] = struct{}{}
			}
		}
	}

	orphaned, err := orphanSteps(stateByHost, desired, opts)
	if err != nil {
		return Plan{}, err
	}
	steps = append(steps, orphaned...)
	operations := operationSteps(resourceGraph.Operations, changed, stepByAddress(steps), opts)
	sortSteps(steps)
	sortOperationSteps(operations)

	return Plan{
		Steps:      steps,
		Operations: operations,
		Summary:    summarize(steps, operations, noOp),
	}, nil
}

func (e Engine) planNodes(ctx context.Context, hosts []ir.HostSpec, parallel int, nodes []planRequest, nodesByHost map[string][]graph.Node, priorsByHost map[string]map[string]*corestate.Resource, progress *progressLogger) (map[string]ProviderPlan, error) {
	plans := map[string]ProviderPlan{}
	if planner, ok := e.Provider.(HostPlanner); ok {
		return e.planNodesByHost(ctx, hosts, parallel, planner, nodesByHost, priorsByHost, progress)
	}
	for _, item := range nodes {
		node := item.node
		priors := priorsByHost[node.Host]
		prior := priors[node.Address]
		task := progress.Start(node.Host, "inspect", node.Address, node.Summary)
		callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
			Phase:   "plan inspect",
			Address: node.Address,
			Action:  "inspect",
			Summary: node.Summary,
		})
		providerPlan, err := e.Provider.Plan(callCtx, node, prior)
		if err != nil {
			task.Done(err)
			return nil, err
		}
		task.Done(nil)
		plans[node.Address] = providerPlan
	}
	return plans, nil
}

func (e Engine) Apply(ctx context.Context, program *ir.Program, resourceGraph *graph.ResourceGraph, opts Options) (Plan, error) {
	progress := newProgressLoggerWithStyle(opts.Progress, opts.ProgressStyle)
	hosts := hostsByName(program)
	selectedHosts := selectedProgramHosts(program, opts)
	parallel := boundedHostParallel(opts.Parallel, len(selectedHosts))
	locks, err := e.lockHosts(ctx, selectedHosts, parallel, opts, progress)
	if err != nil {
		return Plan{}, err
	}
	defer unlockHosts(ctx, locks)

	applyOpts := opts
	if applyOpts.Parallel < 1 {
		applyOpts.Parallel = parallel
	}
	if err := e.persistHostFacts(ctx, program, applyOpts); err != nil {
		return Plan{}, err
	}

	plan, err := e.Plan(ctx, program, resourceGraph, applyOpts)
	if err != nil {
		return Plan{}, err
	}
	if len(plan.Steps) == 0 && len(plan.Operations) == 0 {
		return plan, nil
	}

	states, err := e.readStates(ctx, selectedHosts, applyOpts.Parallel, progress)
	if err != nil {
		return plan, err
	}

	waves, err := executionWaves(resourceGraph, plan)
	if err != nil {
		return plan, err
	}
	if err := e.runExecutionWaves(ctx, hosts, states, waves, applyOpts); err != nil {
		return plan, err
	}
	return plan, nil
}

func (e Engine) persistHostFacts(ctx context.Context, program *ir.Program, opts Options) error {
	if program == nil {
		return nil
	}
	progress := newProgressLoggerWithStyle(opts.Progress, opts.ProgressStyle)
	hosts := selectedProgramHosts(program, opts)
	parallel := boundedHostParallel(opts.Parallel, len(hosts))
	return runHosts(ctx, hosts, parallel, func(ctx context.Context, host ir.HostSpec) error {
		if !hasHostFacts(host.Facts) {
			return nil
		}
		task := progress.Start(host.Name, "persist facts", "", "")
		st, err := e.Backend.Read(ctx, host)
		if err != nil {
			task.Done(err)
			return err
		}
		corestate.Normalize(&st, host.Name)
		facts := host.Facts
		st.Facts = &facts
		if err := e.Backend.Write(ctx, host, st); err != nil {
			task.Done(err)
			return err
		}
		task.Done(nil)
		return nil
	})
}

func hasHostFacts(facts ir.HostFacts) bool {
	return facts.System.Hostname != "" ||
		facts.System.Architecture != "" ||
		facts.System.Codename != "" ||
		facts.System.DetectedAt != ""
}

func selectedProgramHosts(program *ir.Program, opts Options) []ir.HostSpec {
	if program == nil {
		return nil
	}
	hosts := make([]ir.HostSpec, 0, len(program.Hosts))
	for _, host := range program.Hosts {
		if opts.Host != "" && host.Name != opts.Host {
			continue
		}
		hosts = append(hosts, host)
	}
	return hosts
}

func (e Engine) readStates(ctx context.Context, hosts []ir.HostSpec, parallel int, progress *progressLogger) (map[string]corestate.State, error) {
	states := map[string]corestate.State{}
	var mu sync.Mutex
	err := runHosts(ctx, hosts, parallel, func(ctx context.Context, host ir.HostSpec) error {
		task := progress.Start(host.Name, "read state", "", "")
		st, err := e.Backend.Read(ctx, host)
		if err != nil {
			task.Done(err)
			return err
		}
		task.Done(nil)
		corestate.Normalize(&st, host.Name)
		mu.Lock()
		states[host.Name] = st
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return states, nil
}

func (e Engine) planNodesByHost(ctx context.Context, hosts []ir.HostSpec, parallel int, planner HostPlanner, nodesByHost map[string][]graph.Node, priorsByHost map[string]map[string]*corestate.Resource, progress *progressLogger) (map[string]ProviderPlan, error) {
	plans := map[string]ProviderPlan{}
	var mu sync.Mutex
	err := runHosts(ctx, hosts, parallel, func(ctx context.Context, host ir.HostSpec) error {
		nodes := nodesByHost[host.Name]
		if len(nodes) == 0 {
			return nil
		}
		task := progress.Start(host.Name, "inspect", fmt.Sprintf("%d resource(s)", len(nodes)), "")
		callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
			Phase:   "plan inspect",
			Action:  "inspect",
			Summary: fmt.Sprintf("%d resource(s)", len(nodes)),
		})
		hostPlans, err := planner.PlanHost(callCtx, host, nodes, priorsByHost[host.Name])
		if err != nil {
			task.Done(err)
			return err
		}
		task.Done(nil)
		mu.Lock()
		for address, plan := range hostPlans {
			plans[address] = plan
		}
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return plans, nil
}

type heldLock struct {
	host ir.HostSpec
	lock Lock
}

func (e Engine) lockHosts(ctx context.Context, hosts []ir.HostSpec, parallel int, opts Options, progress *progressLogger) ([]heldLock, error) {
	locks := make([]heldLock, 0, len(hosts))
	var mu sync.Mutex
	err := runHosts(ctx, hosts, parallel, func(ctx context.Context, host ir.HostSpec) error {
		task := progress.Start(host.Name, "lock state", "", "")
		lock, err := e.Backend.Lock(ctx, host, opts.LockTimeout)
		if err != nil {
			task.Done(err)
			return err
		}
		task.Done(nil)
		mu.Lock()
		locks = append(locks, heldLock{host: host, lock: lock})
		mu.Unlock()
		return nil
	})
	if err != nil {
		unlockHosts(ctx, locks)
		return nil, err
	}
	return locks, nil
}

func unlockHosts(ctx context.Context, locks []heldLock) error {
	var firstErr error
	for i := len(locks) - 1; i >= 0; i-- {
		if err := locks[i].lock.Unlock(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func runHosts(ctx context.Context, hosts []ir.HostSpec, parallel int, fn func(context.Context, ir.HostSpec) error) error {
	if len(hosts) == 0 {
		return nil
	}
	if parallel < 1 {
		parallel = len(hosts)
	}
	if parallel > len(hosts) {
		parallel = len(hosts)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	hostCh := make(chan ir.HostSpec)
	errCh := make(chan error, len(hosts))
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range hostCh {
				if err := fn(ctx, host); err != nil {
					errCh <- err
					cancel()
				}
			}
		}()
	}
sendHosts:
	for _, host := range hosts {
		select {
		case <-ctx.Done():
			break sendHosts
		case hostCh <- host:
		}
	}
	close(hostCh)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func Compare(node graph.Node, prior *corestate.Resource, observed Observed) ProviderPlan {
	desiredDigest := corestate.DesiredDigest(node.Desired)
	ownership := "managed"
	observedValues := observed.Values
	if observedValues == nil {
		observedValues = map[string]any{}
	}
	if observed.DesiredDigest != "" {
		observedValues = cloneMap(observedValues)
		observedValues["desired_digest"] = observed.DesiredDigest
	}
	if desiredEnsureAbsent(node) {
		if observed.Exists {
			return ProviderPlan{Action: ActionDelete, Summary: "delete " + node.Kind + " " + identity(node), Observed: observedValues, Ownership: ownership}
		}
		return ProviderPlan{Action: ActionNoOp, Summary: "already absent " + node.Kind + " " + identity(node), Observed: observedValues, Ownership: ownership}
	}
	if !observed.Exists {
		return ProviderPlan{Action: ActionCreate, Summary: "create " + node.Kind + " " + identity(node), Observed: observedValues, Ownership: ownership}
	}
	if observed.DesiredDigest == desiredDigest {
		if prior == nil {
			return ProviderPlan{Action: ActionAdopt, Summary: "adopt existing " + node.Kind + " " + identity(node), Observed: observedValues, Ownership: "adopted"}
		}
		return ProviderPlan{Action: ActionNoOp, Summary: "no changes for " + node.Kind + " " + identity(node), Observed: observedValues, Ownership: prior.Ownership}
	}
	if prior != nil && prior.DesiredDigest != "" && observed.DesiredDigest != prior.DesiredDigest {
		return ProviderPlan{Action: ActionUpdate, Summary: "repair drift for " + node.Kind + " " + identity(node), Observed: observedValues, Ownership: prior.Ownership}
	}
	return ProviderPlan{Action: ActionUpdate, Summary: "update " + node.Kind + " " + identity(node), Observed: observedValues, Ownership: ownership}
}

func (p Plan) Document(opts coreplan.Options) coreplan.Document {
	changes := make([]coreplan.Change, 0, len(p.Steps))
	for _, step := range p.Steps {
		before := any(step.Observed)
		after := any(step.Node.Desired)
		if step.Prior != nil && (step.Action == ActionDestroy || step.Action == ActionForget) {
			before = step.Prior.Desired
			after = nil
		}
		change := coreplan.Change{
			Address: step.Address,
			Action:  step.Action,
			Summary: step.Summary,
			Source:  step.Node.Source,
			Diff:    coreplan.BuildDiff(step.Action, before, after),
		}
		if diagnostic := deleteDiagnosticForStep(step); diagnostic.Behavior != "" {
			change.DeleteBehavior = diagnostic.Behavior
			change.DeleteNotes = diagnostic.Notes
			change.DeleteRisk = diagnostic.Risk
		}
		if opts.Debug {
			change.ProviderAddress = step.Node.ProviderAddress
			if change.ProviderAddress == "" && step.Prior != nil {
				change.ProviderAddress = step.Prior.ProviderAddress
			}
		}
		changes = append(changes, change)
	}
	operations := make([]coreplan.OperationNode, 0, len(p.Operations))
	for _, step := range p.Operations {
		op := step.Operation
		address := step.Address
		if address == "" {
			address = op.Address
		}
		operations = append(operations, coreplan.OperationNode{
			Address:        address,
			Action:         op.Action,
			Summary:        op.Summary,
			DependsOn:      op.DependsOn,
			TriggeredBy:    op.TriggeredBy,
			CommandPreview: op.CommandPreview,
			Source:         op.Source,
		})
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	return coreplan.Document{
		FormatVersion: coreplan.FormatVersion,
		GeneratedAt:   now().UTC().Format(time.RFC3339),
		Command: coreplan.Command{
			File: opts.CommandFile,
			Host: opts.Host,
		},
		Summary:     p.Summary,
		Changes:     changes,
		Operations:  operations,
		Diagnostics: []coreplan.Diagnostic{},
	}
}

type deleteDiagnostic struct {
	Behavior string
	Notes    []string
	Risk     string
}

func deleteDiagnosticForStep(step Step) deleteDiagnostic {
	if !deleteLikeAction(step.Action) {
		return deleteDiagnostic{}
	}
	if step.Action == ActionForget {
		return deleteDiagnostic{
			Behavior: "forget",
			Notes:    []string{"removes the resource from DebianForm state without modifying the remote resource"},
			Risk:     "low",
		}
	}

	kind, desired := stepDeleteKindAndDesired(step)
	switch kind {
	case "apt_source_file":
		path := stringMapValue(desired, "path")
		if aptSourceOnDestroy(desired) == "restore" {
			return deleteDiagnostic{
				Behavior: "restore-original",
				Notes:    compactNotes("restores the apt source file content saved when DebianForm adopted or last managed it", "path: "+path),
				Risk:     "medium",
			}
		}
		return deleteDiagnostic{
			Behavior: "forget",
			Notes:    compactNotes("keeps the remote apt source file and removes only DebianForm state", "path: "+path),
			Risk:     "low",
		}
	case "sysctl":
		key := stringMapValue(desired, "key")
		return deleteDiagnostic{
			Behavior: "remove-managed-artifact",
			Notes:    compactNotes("removes "+sysctlPath(key), "runtime sysctl value is not restored"),
			Risk:     "medium",
		}
	case "kernel_module":
		name := stringMapValue(desired, "name")
		return deleteDiagnostic{
			Behavior: "external-side-effect",
			Notes:    compactNotes("removes "+modulePath(name), "attempts to unload the kernel module with modprobe -r"),
			Risk:     "high",
		}
	case "apt_signing_key", "component_download", "component_build":
		path := deletePathForKind(kind, desired)
		return deleteDiagnostic{
			Behavior: "remove-managed-artifact",
			Notes:    compactNotes("removes DebianForm-managed artifact", "path: "+path),
			Risk:     "medium",
		}
	case "systemd_unit":
		path := stringMapValue(desired, "path")
		return deleteDiagnostic{
			Behavior: "external-side-effect",
			Notes:    compactNotes("removes systemd unit file", "path: "+path, "runs systemctl daemon-reload"),
			Risk:     "high",
		}
	case "nftables_file":
		path := stringMapValue(desired, "path")
		return deleteDiagnostic{
			Behavior: "external-side-effect",
			Notes:    compactNotes("removes nftables file", "path: "+path, "may trigger nftables validate or activate operations"),
			Risk:     "high",
		}
	case "component_ca_certificate":
		path := stringMapValue(desired, "path")
		return deleteDiagnostic{
			Behavior: "external-side-effect",
			Notes:    compactNotes("removes CA certificate file", "path: "+path, "may trigger update-ca-certificates"),
			Risk:     "high",
		}
	case "user_group_membership":
		return deleteDiagnostic{
			Behavior: "external-side-effect",
			Notes:    compactNotes("removes user from supplementary group", "existing login sessions may keep old group membership"),
			Risk:     "medium",
		}
	case "service":
		name := stringMapValue(desired, "name")
		return deleteDiagnostic{
			Behavior: "external-side-effect",
			Notes:    compactNotes("disables and stops the service when destroying the managed service resource", "service: "+name),
			Risk:     "high",
		}
	case "docker_package_conflicts", "package", "file", "secret", "directory", "group", "user", "ssh_authorized_key", "component_binary", "component_file", "component_archive", "docker_compose_project":
		return destructiveDeleteDiagnostic(kind, desired)
	case "networkd_netdev", "networkd_network":
		path := stringMapValue(desired, "path")
		return deleteDiagnostic{
			Behavior: "external-side-effect",
			Notes:    compactNotes("removes systemd-networkd file", "path: "+path),
			Risk:     "high",
		}
	default:
		return deleteDiagnostic{
			Behavior: "unknown",
			Notes:    compactNotes("provider has not declared precise delete behavior for resource kind " + kind),
			Risk:     "unknown",
		}
	}
}

func deleteLikeAction(action string) bool {
	switch action {
	case ActionDelete, ActionDestroy, ActionForget:
		return true
	default:
		return false
	}
}

func stepDeleteKindAndDesired(step Step) (string, map[string]any) {
	if step.Prior != nil && (step.Action == ActionDestroy || step.Action == ActionForget) {
		return step.Prior.Kind, step.Prior.Desired
	}
	return step.Node.Kind, step.Node.Desired
}

func deletePathForKind(kind string, desired map[string]any) string {
	if kind == "component_build" {
		return stringMapValue(desired, "output_path")
	}
	return stringMapValue(desired, "path")
}

func destructiveDeleteDiagnostic(kind string, desired map[string]any) deleteDiagnostic {
	switch kind {
	case "docker_package_conflicts":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    []string{"removes installed Docker conflict packages and does not reinstall them later"},
			Risk:     "high",
		}
	case "package":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("removes package with apt-get remove", "package: "+stringMapValue(desired, "name")),
			Risk:     "high",
		}
	case "file":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("removes file and does not restore previous content", "path: "+stringMapValue(desired, "path")),
			Risk:     "high",
		}
	case "secret":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("removes secret file and does not store or restore secret plaintext", "path: "+stringMapValue(desired, "path")),
			Risk:     "high",
		}
	case "directory", "component_archive":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("removes directory recursively and cannot restore its contents", "path: "+stringMapValue(desired, "path")),
			Risk:     "high",
		}
	case "group":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("deletes group and does not restore previous membership", "group: "+stringMapValue(desired, "name")),
			Risk:     "high",
		}
	case "user":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("deletes user and does not restore previous account state", "user: "+stringMapValue(desired, "name")),
			Risk:     "high",
		}
	case "ssh_authorized_key":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("removes authorized key from the user's authorized_keys file", "user: "+stringMapValue(desired, "user")),
			Risk:     "high",
		}
	case "component_binary", "component_file":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("removes installed component artifact", "path: "+stringMapValue(desired, "path")),
			Risk:     "high",
		}
	case "docker_compose_project":
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("runs docker compose down for the project", "project: "+stringMapValue(desired, "project"), "does not restore previous compose project state"),
			Risk:     "high",
		}
	default:
		return deleteDiagnostic{
			Behavior: "destructive",
			Notes:    compactNotes("removes remote resource managed by DebianForm", "kind: "+kind),
			Risk:     "high",
		}
	}
}

func compactNotes(notes ...string) []string {
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" || strings.HasSuffix(note, ": ") || strings.HasSuffix(note, ":") {
			continue
		}
		out = append(out, note)
	}
	return out
}

func priorResource(st corestate.State, address string) *corestate.Resource {
	if prior, ok := st.Resources[address]; ok {
		return &prior
	}
	return nil
}

func orphanSteps(states map[string]corestate.State, desired map[string]graph.Node, opts Options) ([]Step, error) {
	var out []Step
	for host, st := range states {
		if opts.Host != "" && host != opts.Host {
			continue
		}
		for address, prior := range st.Resources {
			if _, ok := desired[address]; ok {
				continue
			}
			action := ActionDestroy
			summary := "destroy " + prior.Kind + " " + address
			if prior.Ownership == "adopted" {
				action = ActionForget
				summary = "forget adopted " + prior.Kind + " " + address
			} else if forgetOrphan(prior) {
				action = ActionForget
				summary = "forget " + prior.Kind + " " + address
			} else if desiredStillManagesOrphan(host, prior, desired) {
				action = ActionForget
				summary = "forget shared " + prior.Kind + " " + address
			}
			if action == ActionDestroy && preventsDestroy(prior.Lifecycle) {
				return nil, preventDestroyError(address, prior.Kind, prior.Lifecycle)
			}
			priorCopy := prior
			out = append(out, Step{
				Address: address,
				Host:    host,
				Action:  action,
				Summary: summary,
				Prior:   &priorCopy,
			})
		}
	}
	return out, nil
}

func forgetOrphan(prior corestate.Resource) bool {
	return prior.Kind == "component_script_output" ||
		prior.Kind == "system_hostname" ||
		prior.Kind == "system_timezone" ||
		prior.Kind == "system_locale" ||
		prior.Kind == "apt_source_file" && stringMapValue(prior.Desired, "on_destroy") == "keep"
}

func desiredStillManagesOrphan(host string, prior corestate.Resource, desired map[string]graph.Node) bool {
	if prior.Kind != "directory" {
		return false
	}
	for _, node := range desired {
		if node.Host != host || node.Kind != "directory" {
			continue
		}
		if sameDirectoryDesired(prior.Desired, node.Desired) {
			return true
		}
	}
	return false
}

func sameDirectoryDesired(a, b map[string]any) bool {
	if stringMapValue(a, "path") == "" {
		return false
	}
	for _, key := range []string{"path", "owner", "group", "mode", "ensure"} {
		if stringMapValue(a, key) != stringMapValue(b, key) {
			return false
		}
	}
	return true
}

func stepByAddress(steps []Step) map[string]Step {
	out := make(map[string]Step, len(steps))
	for _, step := range steps {
		out[step.Address] = step
	}
	return out
}

func operationSteps(operations []graph.Operation, changed map[string]struct{}, steps map[string]Step, opts Options) []OperationStep {
	var out []OperationStep
	for _, op := range operations {
		if opts.Host != "" && !strings.HasPrefix(op.Address, "host."+opts.Host+".") {
			continue
		}
		var triggered []string
		var triggerPaths []string
		for _, trigger := range op.TriggeredBy {
			if _, ok := changed[trigger]; ok {
				triggered = append(triggered, trigger)
				triggerPaths = append(triggerPaths, operationTriggerPath(steps[trigger]))
			}
		}
		if len(triggered) == 0 {
			continue
		}
		mode := ""
		if op.ScriptPayload != nil {
			mode = op.ScriptPayload.Mode
		}
		if mode == "each" {
			for i, trigger := range triggered {
				out = append(out, OperationStep{
					Address:          operationEachAddress(op.Address, trigger),
					Action:           ActionRun,
					Operation:        operationForTriggers(op, []string{trigger}),
					TriggerAddresses: []string{trigger},
					TriggerPaths:     []string{triggerPaths[i]},
				})
			}
			continue
		}
		out = append(out, OperationStep{
			Address:          op.Address,
			Action:           ActionRun,
			Operation:        operationForTriggers(op, triggered),
			TriggerAddresses: triggered,
			TriggerPaths:     triggerPaths,
		})
	}
	return out
}

func operationForTriggers(op graph.Operation, triggers []string) graph.Operation {
	out := op
	out.DependsOn = append([]string(nil), triggers...)
	out.TriggeredBy = append([]string(nil), triggers...)
	return out
}

func operationEachAddress(address, trigger string) string {
	return address + ".trigger[" + strconv.Quote(trigger) + "]"
}

func operationTriggerPath(step Step) string {
	if step.Node.Desired != nil {
		if path, ok := step.Node.Desired["path"].(string); ok {
			return path
		}
	}
	if step.Prior != nil && step.Prior.Desired != nil {
		if path, ok := step.Prior.Desired["path"].(string); ok {
			return path
		}
	}
	return ""
}

func summarize(steps []Step, operations []OperationStep, noOp int) coreplan.Summary {
	var summary coreplan.Summary
	for _, step := range steps {
		switch step.Action {
		case ActionCreate:
			summary.Create++
		case ActionUpdate, ActionAdopt:
			summary.Update++
		case ActionDelete, ActionDestroy, ActionForget:
			summary.Delete++
		case ActionNoOp:
			summary.NoOp++
		}
	}
	summary.NoOp += noOp
	summary.Operations = len(operations)
	return summary
}

func resourceStateForStep(step Step, observed map[string]any, updatedAt string) corestate.Resource {
	ownership := step.Ownership
	if ownership == "" {
		ownership = "managed"
	}
	sanitizedObserved := corestate.SanitizeObserved(observed)
	if sensitive, _ := step.Node.Desired["sensitive"].(bool); sensitive {
		delete(sanitizedObserved, "original_content")
	}
	return corestate.Resource{
		Host:            step.Host,
		Kind:            step.Node.Kind,
		ProviderType:    step.Node.ProviderType,
		ProviderAddress: step.Node.ProviderAddress,
		Ownership:       ownership,
		Lifecycle:       cloneLifecycle(step.Node.Lifecycle),
		Desired:         corestate.SanitizeDesired(step.Node.Desired),
		DesiredDigest:   corestate.DesiredDigest(step.Node.Desired),
		Observed:        sanitizedObserved,
		UpdatedAt:       updatedAt,
		Order:           step.Order,
	}
}

func providerNode(node graph.Node) graph.Node {
	if len(node.ProviderPayload) == 0 {
		return node
	}
	if fileContentWriteOnly(node) {
		return node
	}
	out := node
	out.Desired = node.ProviderPayload
	return out
}

func providerStep(step Step) Step {
	step.Node = providerNode(step.Node)
	return step
}

func destroysResource(action string) bool {
	switch action {
	case ActionDelete, ActionDestroy:
		return true
	default:
		return false
	}
}

func preventsDestroy(lifecycle *ir.LifecycleSpec) bool {
	return lifecycle != nil && lifecycle.PreventDestroy
}

func cloneLifecycle(lifecycle *ir.LifecycleSpec) *ir.LifecycleSpec {
	if lifecycle == nil {
		return nil
	}
	copy := *lifecycle
	return &copy
}

func preventDestroyError(address, kind string, lifecycle *ir.LifecycleSpec) error {
	if lifecycle != nil && lifecycle.Source.File != "" {
		return fmt.Errorf("%s:%d:%s: %s %s is protected by lifecycle.prevent_destroy", lifecycle.Source.File, lifecycle.Source.Line, lifecycle.Source.Path, kind, address)
	}
	return fmt.Errorf("%s %s is protected by lifecycle.prevent_destroy", kind, address)
}

func hostsByName(program *ir.Program) map[string]ir.HostSpec {
	out := map[string]ir.HostSpec{}
	if program == nil {
		return out
	}
	for _, host := range program.Hosts {
		out[host.Name] = host
	}
	return out
}

func defaultSummary(action, fallback string) string {
	if fallback == "" {
		return action
	}
	return fallback
}

func triggersOperation(action string) bool {
	switch action {
	case ActionCreate, ActionUpdate, ActionDelete:
		return true
	default:
		return false
	}
}

func sortSteps(steps []Step) {
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].Order != steps[j].Order {
			return steps[i].Order < steps[j].Order
		}
		return steps[i].Address < steps[j].Address
	})
}

func sortOperationSteps(steps []OperationStep) {
	sort.SliceStable(steps, func(i, j int) bool {
		left := steps[i].Address
		if left == "" {
			left = steps[i].Operation.Address
		}
		right := steps[j].Address
		if right == "" {
			right = steps[j].Operation.Address
		}
		return left < right
	})
}

func desiredEnsureAbsent(node graph.Node) bool {
	if ensure, ok := node.Desired["ensure"].(string); ok {
		return ensure == "absent"
	}
	return false
}

func identity(node graph.Node) string {
	for _, key := range []string{"name", "path", "output_path", "key", "user"} {
		if value, ok := node.Desired[key].(string); ok && value != "" {
			return value
		}
	}
	return node.Address
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type executionItem struct {
	kind         string
	address      string
	host         string
	safeParallel bool
	dependsOn    []string
	step         Step
	operation    OperationStep
}

func activeStepMap(plan Plan) map[string]Step {
	out := map[string]Step{}
	for _, step := range plan.Steps {
		out[step.Address] = step
	}
	return out
}

func activeOperationMap(plan Plan) map[string]OperationStep {
	out := map[string]OperationStep{}
	for _, step := range plan.Operations {
		address := step.Address
		if address == "" {
			address = step.Operation.Address
		}
		step.Address = address
		out[address] = step
	}
	return out
}

func executionWaves(resourceGraph *graph.ResourceGraph, plan Plan) ([][]executionItem, error) {
	active := activeStepMap(plan)
	activeOps := activeOperationMap(plan)

	known := map[string]bool{}
	for _, node := range resourceGraph.Nodes {
		known[node.Address] = true
	}
	for _, op := range resourceGraph.Operations {
		known[op.Address] = true
	}

	activeAddresses := map[string]bool{}
	orphanSteps := []Step{}
	for _, step := range plan.Steps {
		if known[step.Address] {
			activeAddresses[step.Address] = true
		} else {
			orphanSteps = append(orphanSteps, step)
		}
	}
	aliases := map[string]string{}
	aliasDependsOn := map[string][]string{}
	for address, step := range activeOps {
		activeAddresses[address] = true
		if address != step.Operation.Address {
			aliases[address] = step.Operation.Address
			aliasDependsOn[address] = append([]string(nil), step.Operation.DependsOn...)
		}
	}

	scheduled, err := resourceGraph.ActiveWavesWithAliases(activeAddresses, aliases, aliasDependsOn)
	if err != nil {
		return nil, err
	}

	waves := make([][]executionItem, 0, len(scheduled)+1)
	if len(orphanSteps) > 0 {
		sortSteps(orphanSteps)
		wave := make([]executionItem, 0, len(orphanSteps))
		for _, step := range orphanSteps {
			wave = append(wave, executionItem{
				kind:    "resource",
				address: step.Address,
				host:    step.Host,
				step:    step,
			})
		}
		waves = append(waves, wave)
	}
	for _, scheduledWave := range scheduled {
		wave := make([]executionItem, 0, len(scheduledWave))
		for _, item := range scheduledWave {
			if item.Operation {
				opStep := activeOps[item.Address]
				wave = append(wave, executionItem{
					kind:      "operation",
					address:   item.Address,
					host:      item.Host,
					dependsOn: item.DependsOn,
					operation: opStep,
				})
				continue
			}
			step := active[item.Address]
			wave = append(wave, executionItem{
				kind:         "resource",
				address:      item.Address,
				host:         step.Host,
				safeParallel: item.SafeParallel,
				dependsOn:    item.DependsOn,
				step:         step,
			})
		}
		waves = append(waves, wave)
	}
	return waves, nil
}

func (e Engine) runExecutionWaves(ctx context.Context, hosts map[string]ir.HostSpec, states map[string]corestate.State, waves [][]executionItem, opts Options) error {
	parallel := opts.Parallel
	if parallel < 1 {
		parallel = 1
	}
	perHostParallel := opts.PerHostParallel
	if perHostParallel < 1 {
		perHostParallel = 1
	}
	progress := newProgressLoggerWithStyle(opts.Progress, opts.ProgressStyle)

	globalSem := make(chan struct{}, parallel)
	hostSems := map[string]chan struct{}{}
	statesLock := &sync.Mutex{}
	stateLocks := map[string]*sync.Mutex{}
	for name := range hosts {
		hostSems[name] = make(chan struct{}, perHostParallel)
		stateLocks[name] = &sync.Mutex{}
	}

	failed := map[string]error{}
	blocked := map[string]string{}
	var firstErr error
	for _, wave := range waves {
		runnable := make([]executionItem, 0, len(wave))
		for _, item := range wave {
			if dep := blockedDependency(item.dependsOn, failed, blocked); dep != "" {
				blocked[item.address] = dep
				continue
			}
			runnable = append(runnable, item)
		}
		results := runExecutionWave(ctx, e, hosts, states, statesLock, stateLocks, globalSem, hostSems, perHostParallel, progress, runnable)
		for _, item := range runnable {
			if err := results[item.address]; err != nil {
				failed[item.address] = err
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

func runExecutionWave(ctx context.Context, e Engine, hosts map[string]ir.HostSpec, states map[string]corestate.State, statesLock *sync.Mutex, stateLocks map[string]*sync.Mutex, globalSem chan struct{}, hostSems map[string]chan struct{}, perHostParallel int, progress *progressLogger, wave []executionItem) map[string]error {
	type result struct {
		address string
		err     error
	}
	results := make(chan result, len(wave))
	var wg sync.WaitGroup
	for _, item := range wave {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			releaseGlobal, err := acquire(ctx, globalSem, 1)
			if err != nil {
				results <- result{address: item.address, err: err}
				return
			}
			defer releaseGlobal()

			hostSlots := 0
			if item.host != "" {
				hostSlots = 1
				if !item.safeParallel {
					hostSlots = perHostParallel
				}
			}
			releaseHost := func() {}
			if hostSlots > 0 {
				hostSem, ok := hostSems[item.host]
				if !ok {
					results <- result{address: item.address, err: fmt.Errorf("%s references unknown host %q", item.address, item.host)}
					return
				}
				releaseHost, err = acquire(ctx, hostSem, hostSlots)
				if err != nil {
					results <- result{address: item.address, err: err}
					return
				}
				defer releaseHost()
			}

			results <- result{
				address: item.address,
				err:     e.executeItem(ctx, hosts, states, statesLock, stateLocks, progress, item),
			}
		}()
	}
	wg.Wait()
	close(results)

	out := map[string]error{}
	for item := range results {
		out[item.address] = item.err
	}
	return out
}

func (e Engine) executeItem(ctx context.Context, hosts map[string]ir.HostSpec, states map[string]corestate.State, statesLock *sync.Mutex, stateLocks map[string]*sync.Mutex, progress *progressLogger, item executionItem) error {
	switch item.kind {
	case "resource":
		task := progress.Start(item.step.Host, item.step.Action, item.step.Address, item.step.Summary)
		callCtx := WithRemoteCallContext(ctx, RemoteCallContext{onFailurePrompt: task.stopHeartbeat})
		err := e.executeResourceStep(callCtx, hosts, states, statesLock, stateLocks, item.step)
		task.Done(err)
		return err
	case "operation":
		operation := operationWithStepContext(item.operation)
		task := progress.Start(item.host, "run", item.address, operation.Summary)
		action := operation.Action
		if action == "" {
			action = ActionRun
		}
		callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
			Phase:           "run operation",
			Address:         item.address,
			Action:          action,
			Summary:         operation.Summary,
			onFailurePrompt: task.stopHeartbeat,
		})
		result, err := e.Provider.RunOperation(callCtx, operation)
		task.Done(err)
		if err != nil {
			return fmt.Errorf("%s failed: %w", item.address, err)
		}
		if err := e.recordOperationOutputs(callCtx, hosts, states, statesLock, stateLocks, item.operation, result); err != nil {
			return fmt.Errorf("%s failed: %w", item.address, err)
		}
		return nil
	default:
		return fmt.Errorf("%s has unsupported execution item kind %q", item.address, item.kind)
	}
}

func (e Engine) recordOperationOutputs(ctx context.Context, hosts map[string]ir.HostSpec, states map[string]corestate.State, statesLock *sync.Mutex, stateLocks map[string]*sync.Mutex, step OperationStep, result OperationResult) error {
	if len(result.Outputs) == 0 {
		return nil
	}
	hostName := hostFromAddress(step.Operation.Address)
	host, ok := hosts[hostName]
	if !ok {
		return fmt.Errorf("%s references unknown host %q", step.Address, hostName)
	}
	now := e.now().UTC().Format(time.RFC3339)
	lock := stateLocks[host.Name]
	lock.Lock()
	defer lock.Unlock()

	statesLock.Lock()
	st := states[host.Name]
	statesLock.Unlock()
	for address, observed := range result.Outputs {
		if observed == nil {
			observed = map[string]any{}
		}
		node, ok := scriptOutputNodeForOperation(step.Operation, address)
		if !ok {
			node = graph.Node{
				Host:            host.Name,
				Address:         address,
				Kind:            "component_script_output",
				Desired:         map[string]any{"path": stringMapValue(observed, "path")},
				ProviderType:    "component_script_output",
				ProviderAddress: "component_script_output." + address,
			}
		}
		st.Resources[address] = resourceStateForStep(Step{
			Address:   address,
			Host:      host.Name,
			Action:    ActionUpdate,
			Node:      node,
			Observed:  observed,
			Ownership: "managed",
		}, observed, now)
	}
	if err := e.Backend.Write(ctx, host, st); err != nil {
		return err
	}
	statesLock.Lock()
	states[host.Name] = st
	statesLock.Unlock()
	return nil
}

func scriptOutputNodeForOperation(operation graph.Operation, address string) (graph.Node, bool) {
	payload := operation.ScriptPayload
	if payload == nil {
		return graph.Node{}, false
	}
	for _, output := range payload.Outputs {
		if output.Address != address {
			continue
		}
		desired := map[string]any{
			"path":      output.Path,
			"component": payload.ComponentName,
			"script":    payload.Name,
		}
		if output.ScriptDigest != "" {
			desired["script_digest"] = output.ScriptDigest
		}
		hostName := hostFromAddress(operation.Address)
		return graph.Node{
			Host:            hostName,
			Address:         address,
			Kind:            "component_script_output",
			Source:          operation.Source,
			Desired:         desired,
			ProviderType:    "component_script_output",
			ProviderAddress: output.ProviderAddress,
		}, true
	}
	return graph.Node{}, false
}

func operationWithStepContext(step OperationStep) graph.Operation {
	operation := step.Operation
	if operation.ScriptPayload != nil {
		payload := *operation.ScriptPayload
		payload.TriggerAddresses = append([]string(nil), step.TriggerAddresses...)
		payload.TriggerPaths = append([]string(nil), step.TriggerPaths...)
		operation.ScriptPayload = &payload
	}
	return operation
}

func (e Engine) executeResourceStep(ctx context.Context, hosts map[string]ir.HostSpec, states map[string]corestate.State, statesLock *sync.Mutex, stateLocks map[string]*sync.Mutex, step Step) error {
	host, ok := hosts[step.Host]
	if !ok {
		return fmt.Errorf("%s references unknown host %q", step.Address, step.Host)
	}

	now := e.now().UTC().Format(time.RFC3339)
	callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "apply resource",
		Address: step.Address,
		Action:  step.Action,
		Summary: step.Summary,
	})
	var observed map[string]any
	switch step.Action {
	case ActionCreate, ActionUpdate, ActionDelete:
		result, err := e.Provider.Apply(callCtx, providerStep(step))
		if err != nil {
			return fmt.Errorf("%s failed: %w", step.Address, err)
		}
		observed = result
	case ActionDestroy:
		if err := e.Provider.Destroy(callCtx, step); err != nil {
			return fmt.Errorf("%s failed: %w", step.Address, err)
		}
	case ActionAdopt, ActionForget, ActionNoOp:
	default:
		return fmt.Errorf("%s has unsupported action %q", step.Address, step.Action)
	}

	lock := stateLocks[host.Name]
	lock.Lock()
	defer lock.Unlock()

	statesLock.Lock()
	st := states[host.Name]
	statesLock.Unlock()
	switch step.Action {
	case ActionCreate, ActionUpdate:
		if step.Node.Kind == "component_script_output" {
			return nil
		}
		if observed == nil {
			observed = step.Observed
		}
		st.Resources[step.Address] = resourceStateForStep(step, observed, now)
	case ActionDelete, ActionDestroy, ActionForget:
		delete(st.Resources, step.Address)
	case ActionAdopt:
		st.Resources[step.Address] = resourceStateForStep(step, step.Observed, now)
	case ActionNoOp:
		return nil
	}
	if err := e.Backend.Write(callCtx, host, st); err != nil {
		return err
	}
	statesLock.Lock()
	states[host.Name] = st
	statesLock.Unlock()
	return nil
}

func blockedDependency(deps []string, failed map[string]error, blocked map[string]string) string {
	for _, dep := range deps {
		if _, ok := failed[dep]; ok {
			return dep
		}
		if blockedBy, ok := blocked[dep]; ok {
			return blockedBy
		}
	}
	return ""
}

func acquire(ctx context.Context, sem chan struct{}, slots int) (func(), error) {
	acquired := 0
	for acquired < slots {
		select {
		case sem <- struct{}{}:
			acquired++
		case <-ctx.Done():
			for acquired > 0 {
				<-sem
				acquired--
			}
			return nil, ctx.Err()
		}
	}
	return func() {
		for ; acquired > 0; acquired-- {
			<-sem
		}
	}, nil
}
