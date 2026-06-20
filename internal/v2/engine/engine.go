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
	Host        string
	LockTimeout time.Duration
	Parallel    int
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
	if opts.Host == "" && opts.Parallel > 1 && len(program.Hosts) > 1 {
		return e.applyHostsParallel(ctx, program, resourceGraph, opts)
	}

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

	activeSteps := activeStepMap(plan)
	for _, item := range executionOrder(resourceGraph, plan) {
		switch item.kind {
		case "resource":
			step := activeSteps[item.address]
			host, ok := hosts[step.Host]
			if !ok {
				return plan, fmt.Errorf("%s references unknown host %q", step.Address, step.Host)
			}
			st := states[host.Name]
			if err := e.applyStep(ctx, &st, step); err != nil {
				return plan, err
			}
			if err := e.Backend.Write(ctx, host, st); err != nil {
				return plan, err
			}
			states[host.Name] = st
		case "operation":
			op := item.operation
			if err := e.Provider.RunOperation(ctx, op); err != nil {
				return plan, fmt.Errorf("%s failed: %w", op.Address, err)
			}
		}
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

func (e Engine) applyHostsParallel(ctx context.Context, program *ir.Program, resourceGraph *graph.ResourceGraph, opts Options) (Plan, error) {
	parallel := opts.Parallel
	if parallel > len(program.Hosts) {
		parallel = len(program.Hosts)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		index int
		host  string
		plan  Plan
		err   error
	}
	jobs := make(chan int)
	results := make(chan result, len(program.Hosts))
	var workers sync.WaitGroup
	for range parallel {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				host := program.Hosts[index]
				plan, err := e.Apply(ctx, program, resourceGraph, Options{
					Host:        host.Name,
					LockTimeout: opts.LockTimeout,
					Parallel:    1,
				})
				results <- result{index: index, host: host.Name, plan: plan, err: err}
				if err != nil {
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index := range program.Hosts {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	plans := make([]Plan, len(program.Hosts))
	completed := make([]bool, len(program.Hosts))
	var firstErr error
	for item := range results {
		if item.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("host %s apply failed: %w", item.host, item.err)
		}
		plans[item.index] = item.plan
		completed[item.index] = item.err == nil
	}
	if firstErr != nil {
		return combinePlans(plans, completed), firstErr
	}
	return combinePlans(plans, completed), nil
}

func combinePlans(plans []Plan, included []bool) Plan {
	var combined Plan
	for index, plan := range plans {
		if !included[index] {
			continue
		}
		combined.Steps = append(combined.Steps, plan.Steps...)
		combined.Operations = append(combined.Operations, plan.Operations...)
		combined.Summary.Create += plan.Summary.Create
		combined.Summary.Update += plan.Summary.Update
		combined.Summary.Delete += plan.Summary.Delete
		combined.Summary.NoOp += plan.Summary.NoOp
		combined.Summary.Operations += plan.Summary.Operations
	}
	return combined
}

func (e Engine) applyStep(ctx context.Context, st *v2state.State, step Step) error {
	now := e.now().UTC().Format(time.RFC3339)
	switch step.Action {
	case ActionCreate, ActionUpdate, ActionDelete:
		observed, err := e.Provider.Apply(ctx, step)
		if err != nil {
			return fmt.Errorf("%s failed: %w", step.Address, err)
		}
		if step.Action == ActionDelete {
			delete(st.Resources, step.Address)
			return nil
		}
		if observed == nil {
			observed = step.Observed
		}
		st.Resources[step.Address] = resourceStateForStep(step, observed, now)
	case ActionAdopt:
		st.Resources[step.Address] = resourceStateForStep(step, step.Observed, now)
	case ActionDestroy:
		if err := e.Provider.Destroy(ctx, step); err != nil {
			return fmt.Errorf("%s failed: %w", step.Address, err)
		}
		delete(st.Resources, step.Address)
	case ActionForget:
		delete(st.Resources, step.Address)
	case ActionNoOp:
		return nil
	default:
		return fmt.Errorf("%s has unsupported action %q", step.Address, step.Action)
	}
	return nil
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
	for _, key := range []string{"name", "path", "key", "user"} {
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
	kind      string
	address   string
	operation graph.Operation
}

func activeStepMap(plan Plan) map[string]Step {
	out := map[string]Step{}
	for _, step := range plan.Steps {
		out[step.Address] = step
	}
	return out
}

func executionOrder(resourceGraph *graph.ResourceGraph, plan Plan) []executionItem {
	active := activeStepMap(plan)
	activeOps := map[string]graph.Operation{}
	for _, step := range plan.Operations {
		activeOps[step.Operation.Address] = step.Operation
	}

	nodes := map[string]graph.Node{}
	for _, node := range resourceGraph.Nodes {
		nodes[node.Address] = node
	}
	operations := map[string]graph.Operation{}
	for _, op := range resourceGraph.Operations {
		operations[op.Address] = op
	}

	visited := map[string]bool{}
	var out []executionItem
	var visit func(string)
	visit = func(address string) {
		if visited[address] {
			return
		}
		visited[address] = true
		if node, ok := nodes[address]; ok {
			for _, dep := range node.DependsOn {
				if _, activeDep := active[dep]; activeDep {
					visit(dep)
				}
				if _, activeDep := activeOps[dep]; activeDep {
					visit(dep)
				}
			}
			if _, ok := active[address]; ok {
				out = append(out, executionItem{kind: "resource", address: address})
			}
			return
		}
		if _, ok := active[address]; ok {
			out = append(out, executionItem{kind: "resource", address: address})
			return
		}
		if op, ok := operations[address]; ok {
			for _, dep := range op.DependsOn {
				if _, activeDep := active[dep]; activeDep {
					visit(dep)
				}
				if _, activeDep := activeOps[dep]; activeDep {
					visit(dep)
				}
			}
			if _, ok := activeOps[address]; ok {
				out = append(out, executionItem{kind: "operation", address: address, operation: op})
			}
		}
	}
	for _, step := range plan.Steps {
		visit(step.Address)
	}
	for _, step := range plan.Operations {
		visit(step.Operation.Address)
	}
	return out
}
