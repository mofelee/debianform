package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mofelee/debianform/internal/v2/graph"
	"github.com/mofelee/debianform/internal/v2/ir"
	v2plan "github.com/mofelee/debianform/internal/v2/plan"
	v2state "github.com/mofelee/debianform/internal/v2/state"
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
	Read(ctx context.Context, host ir.HostSpec) (v2state.State, error)
	Write(ctx context.Context, host ir.HostSpec, st v2state.State) error
	Lock(ctx context.Context, host ir.HostSpec, timeout time.Duration) (Lock, error)
}

type Lock interface {
	Unlock(ctx context.Context) error
}

type Provider interface {
	Plan(ctx context.Context, node graph.Node, prior *v2state.Resource) (ProviderPlan, error)
	Apply(ctx context.Context, step Step) (map[string]any, error)
	Destroy(ctx context.Context, step Step) error
	RunOperation(ctx context.Context, operation graph.Operation) error
}

type ProviderPlan struct {
	Action    string
	Summary   string
	Observed  map[string]any
	Ownership string
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
}

type Engine struct {
	Backend  Backend
	Provider Provider
	Now      func() time.Time
}

type Plan struct {
	Steps      []Step
	Operations []OperationStep
	Summary    v2plan.Summary
}

type Step struct {
	Address   string
	Host      string
	Action    string
	Summary   string
	Node      graph.Node
	Prior     *v2state.Resource
	Observed  map[string]any
	Ownership string
	Order     int
}

type OperationStep struct {
	Action    string
	Operation graph.Operation
}

func (e Engine) Plan(ctx context.Context, program *ir.Program, resourceGraph *graph.ResourceGraph, opts Options) (Plan, error) {
	if e.Backend == nil {
		return Plan{}, fmt.Errorf("v2 engine backend is required")
	}
	if e.Provider == nil {
		return Plan{}, fmt.Errorf("v2 engine provider is required")
	}
	if err := resourceGraph.Validate(); err != nil {
		return Plan{}, err
	}
	hosts := hostsByName(program)
	stateByHost := map[string]v2state.State{}
	for _, host := range program.Hosts {
		if opts.Host != "" && host.Name != opts.Host {
			continue
		}
		st, err := e.Backend.Read(ctx, host)
		if err != nil {
			return Plan{}, err
		}
		v2state.Normalize(&st, host.Name)
		stateByHost[host.Name] = st
	}

	desired := map[string]graph.Node{}
	changed := map[string]struct{}{}
	steps := []Step{}
	noOp := 0
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
		providerPlan, err := e.Provider.Plan(ctx, node, prior)
		if err != nil {
			return Plan{}, err
		}
		if providerPlan.Action == "" {
			providerPlan.Action = ActionNoOp
		}
		if providerPlan.Summary == "" {
			providerPlan.Summary = defaultSummary(providerPlan.Action, node.Summary)
		}
		if destroysResource(providerPlan.Action) && preventsDestroy(node.Lifecycle) {
			return Plan{}, preventDestroyError(node.Address, node.Kind, node.Lifecycle)
		}
		step := Step{
			Address:   node.Address,
			Host:      node.Host,
			Action:    providerPlan.Action,
			Summary:   providerPlan.Summary,
			Node:      node,
			Prior:     prior,
			Observed:  providerPlan.Observed,
			Ownership: providerPlan.Ownership,
			Order:     order,
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
	operations := operationSteps(resourceGraph.Operations, changed, opts)
	sortSteps(steps)
	sortOperationSteps(operations)

	return Plan{
		Steps:      steps,
		Operations: operations,
		Summary:    summarize(steps, operations, noOp),
	}, nil
}

func (e Engine) Apply(ctx context.Context, program *ir.Program, resourceGraph *graph.ResourceGraph, opts Options) (Plan, error) {
	hosts := hostsByName(program)
	for _, host := range program.Hosts {
		if opts.Host != "" && host.Name != opts.Host {
			continue
		}
		lock, err := e.Backend.Lock(ctx, host, opts.LockTimeout)
		if err != nil {
			return Plan{}, err
		}
		defer lock.Unlock(ctx)
	}
	if err := e.persistHostFacts(ctx, program, opts); err != nil {
		return Plan{}, err
	}

	plan, err := e.Plan(ctx, program, resourceGraph, opts)
	if err != nil {
		return Plan{}, err
	}
	if len(plan.Steps) == 0 && len(plan.Operations) == 0 {
		return plan, nil
	}

	states := map[string]v2state.State{}
	for name, host := range hosts {
		if opts.Host != "" && name != opts.Host {
			continue
		}
		st, err := e.Backend.Read(ctx, host)
		if err != nil {
			return plan, err
		}
		v2state.Normalize(&st, host.Name)
		states[name] = st
	}

	waves, err := executionWaves(resourceGraph, plan)
	if err != nil {
		return plan, err
	}
	if err := e.runExecutionWaves(ctx, hosts, states, waves, opts); err != nil {
		return plan, err
	}
	return plan, nil
}

func (e Engine) persistHostFacts(ctx context.Context, program *ir.Program, opts Options) error {
	if program == nil {
		return nil
	}
	for _, host := range program.Hosts {
		if opts.Host != "" && host.Name != opts.Host {
			continue
		}
		if !hasHostFacts(host.Facts) {
			continue
		}
		st, err := e.Backend.Read(ctx, host)
		if err != nil {
			return err
		}
		v2state.Normalize(&st, host.Name)
		facts := host.Facts
		st.Facts = &facts
		if err := e.Backend.Write(ctx, host, st); err != nil {
			return err
		}
	}
	return nil
}

func hasHostFacts(facts ir.HostFacts) bool {
	return facts.System.Hostname != "" ||
		facts.System.Architecture != "" ||
		facts.System.Codename != "" ||
		facts.System.DetectedAt != ""
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func Compare(node graph.Node, prior *v2state.Resource, observed Observed) ProviderPlan {
	desiredDigest := v2state.DesiredDigest(node.Desired)
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

func (p Plan) Document(opts v2plan.Options) v2plan.Document {
	changes := make([]v2plan.Change, 0, len(p.Steps))
	for _, step := range p.Steps {
		before := any(step.Observed)
		after := any(step.Node.Desired)
		if step.Prior != nil && (step.Action == ActionDestroy || step.Action == ActionForget) {
			before = step.Prior.Desired
			after = nil
		}
		change := v2plan.Change{
			Address: step.Address,
			Action:  step.Action,
			Summary: step.Summary,
			Source:  step.Node.Source,
			Diff:    v2plan.BuildDiff(step.Action, before, after),
		}
		if opts.Debug {
			change.ProviderAddress = step.Node.ProviderAddress
			if change.ProviderAddress == "" && step.Prior != nil {
				change.ProviderAddress = step.Prior.ProviderAddress
			}
		}
		changes = append(changes, change)
	}
	operations := make([]v2plan.OperationNode, 0, len(p.Operations))
	for _, step := range p.Operations {
		op := step.Operation
		operations = append(operations, v2plan.OperationNode{
			Address:        op.Address,
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
	return v2plan.Document{
		FormatVersion: v2plan.FormatVersion,
		GeneratedAt:   now().UTC().Format(time.RFC3339),
		Command: v2plan.Command{
			File: opts.CommandFile,
			Host: opts.Host,
		},
		Summary:     p.Summary,
		Changes:     changes,
		Operations:  operations,
		Diagnostics: []v2plan.Diagnostic{},
	}
}

func priorResource(st v2state.State, address string) *v2state.Resource {
	if prior, ok := st.Resources[address]; ok {
		return &prior
	}
	return nil
}

func orphanSteps(states map[string]v2state.State, desired map[string]graph.Node, opts Options) ([]Step, error) {
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

func forgetOrphan(prior v2state.Resource) bool {
	return prior.Kind == "apt_source_file" && stringMapValue(prior.Desired, "on_destroy") == "keep"
}

func desiredStillManagesOrphan(host string, prior v2state.Resource, desired map[string]graph.Node) bool {
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

func operationSteps(operations []graph.Operation, changed map[string]struct{}, opts Options) []OperationStep {
	var out []OperationStep
	for _, op := range operations {
		if opts.Host != "" && !strings.HasPrefix(op.Address, "host."+opts.Host+".") {
			continue
		}
		for _, trigger := range op.TriggeredBy {
			if _, ok := changed[trigger]; ok {
				out = append(out, OperationStep{Action: ActionRun, Operation: op})
				break
			}
		}
	}
	return out
}

func summarize(steps []Step, operations []OperationStep, noOp int) v2plan.Summary {
	var summary v2plan.Summary
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

func resourceStateForStep(step Step, observed map[string]any, updatedAt string) v2state.Resource {
	ownership := step.Ownership
	if ownership == "" {
		ownership = "managed"
	}
	return v2state.Resource{
		Host:            step.Host,
		Kind:            step.Node.Kind,
		ProviderType:    step.Node.ProviderType,
		ProviderAddress: step.Node.ProviderAddress,
		Ownership:       ownership,
		Lifecycle:       cloneLifecycle(step.Node.Lifecycle),
		Desired:         v2state.SanitizeDesired(step.Node.Desired),
		DesiredDigest:   v2state.DesiredDigest(step.Node.Desired),
		Observed:        v2state.SanitizeObserved(observed),
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
		return steps[i].Operation.Address < steps[j].Operation.Address
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
	operation    graph.Operation
}

func activeStepMap(plan Plan) map[string]Step {
	out := map[string]Step{}
	for _, step := range plan.Steps {
		out[step.Address] = step
	}
	return out
}

func activeOperationMap(plan Plan) map[string]graph.Operation {
	out := map[string]graph.Operation{}
	for _, step := range plan.Operations {
		out[step.Operation.Address] = step.Operation
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
	for address := range activeOps {
		activeAddresses[address] = true
	}

	scheduled, err := resourceGraph.ActiveWaves(activeAddresses)
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
				op := activeOps[item.Address]
				wave = append(wave, executionItem{
					kind:      "operation",
					address:   item.Address,
					host:      item.Host,
					dependsOn: item.DependsOn,
					operation: op,
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

func (e Engine) runExecutionWaves(ctx context.Context, hosts map[string]ir.HostSpec, states map[string]v2state.State, waves [][]executionItem, opts Options) error {
	parallel := opts.Parallel
	if parallel < 1 {
		parallel = 1
	}
	perHostParallel := opts.PerHostParallel
	if perHostParallel < 1 {
		perHostParallel = 1
	}

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
		results := runExecutionWave(ctx, e, hosts, states, statesLock, stateLocks, globalSem, hostSems, perHostParallel, runnable)
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

func runExecutionWave(ctx context.Context, e Engine, hosts map[string]ir.HostSpec, states map[string]v2state.State, statesLock *sync.Mutex, stateLocks map[string]*sync.Mutex, globalSem chan struct{}, hostSems map[string]chan struct{}, perHostParallel int, wave []executionItem) map[string]error {
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
				err:     e.executeItem(ctx, hosts, states, statesLock, stateLocks, item),
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

func (e Engine) executeItem(ctx context.Context, hosts map[string]ir.HostSpec, states map[string]v2state.State, statesLock *sync.Mutex, stateLocks map[string]*sync.Mutex, item executionItem) error {
	switch item.kind {
	case "resource":
		return e.executeResourceStep(ctx, hosts, states, statesLock, stateLocks, item.step)
	case "operation":
		if err := e.Provider.RunOperation(ctx, item.operation); err != nil {
			return fmt.Errorf("%s failed: %w", item.operation.Address, err)
		}
		return nil
	default:
		return fmt.Errorf("%s has unsupported execution item kind %q", item.address, item.kind)
	}
}

func (e Engine) executeResourceStep(ctx context.Context, hosts map[string]ir.HostSpec, states map[string]v2state.State, statesLock *sync.Mutex, stateLocks map[string]*sync.Mutex, step Step) error {
	host, ok := hosts[step.Host]
	if !ok {
		return fmt.Errorf("%s references unknown host %q", step.Address, step.Host)
	}

	now := e.now().UTC().Format(time.RFC3339)
	var observed map[string]any
	switch step.Action {
	case ActionCreate, ActionUpdate, ActionDelete:
		result, err := e.Provider.Apply(ctx, providerStep(step))
		if err != nil {
			return fmt.Errorf("%s failed: %w", step.Address, err)
		}
		observed = result
	case ActionDestroy:
		if err := e.Provider.Destroy(ctx, step); err != nil {
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
	if err := e.Backend.Write(ctx, host, st); err != nil {
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
