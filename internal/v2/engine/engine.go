package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

	steps = append(steps, orphanSteps(stateByHost, desired, opts)...)
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
		changes = append(changes, v2plan.Change{
			Address: step.Address,
			Action:  step.Action,
			Summary: step.Summary,
			Source:  step.Node.Source,
			Diff: v2plan.DiffNode{
				Path:      []string{},
				Kind:      "object",
				Action:    step.Action,
				Sensitive: isSensitive(step.Node.Desired),
				Before:    v2state.SanitizeObserved(step.Observed),
				After:     v2state.SanitizeDesired(step.Node.Desired),
			},
		})
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

func orphanSteps(states map[string]v2state.State, desired map[string]graph.Node, opts Options) []Step {
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
	return out
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
		Desired:         v2state.SanitizeDesired(step.Node.Desired),
		DesiredDigest:   v2state.DesiredDigest(step.Node.Desired),
		Observed:        v2state.SanitizeObserved(observed),
		UpdatedAt:       updatedAt,
		Order:           step.Order,
	}
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

func isSensitive(values map[string]any) bool {
	sensitive, _ := values["sensitive"].(bool)
	return sensitive
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
