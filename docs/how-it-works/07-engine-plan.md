# 07. The Online Engine: Reading State, Observing Reality, and Computing Actions

<p align="right"><strong>English</strong> | <a href="07-engine-plan.zh.md">简体中文</a></p>

This chapter explains how `internal/core/engine.Engine.Plan` computes real
actions in online mode. This is the key layer that distinguishes desired
configuration, remote state, and actual host reality.

## Data Flow

```text
ir.Program + graph.ResourceGraph
  -> Backend.Read(host state)
  -> Provider.Plan(node, prior)
  -> Compare(desired, prior, observed)
  -> orphanSteps
  -> operationSteps
  -> engine.Plan
```

`Engine.Plan` does not make changes; it only reads state, observes reality, and
computes steps. `Engine.Plan` itself does not acquire a state lock, so ordinary
`dbf plan` remains an unlocked read. `dbf check` uses `Engine.Check`, acquiring
all target-host locks before running a complete `Engine.Plan` within the same
lock period.

## Engine Dependencies

`Engine` depends on two interfaces:

- `Backend`: reads and writes state and acquires locks.
- `Provider`: observes, applies, and destroys resources and runs operations.

An online plan needs only:

- `Backend.Read`
- `Provider.Plan`

`Engine.Check` also needs `Backend.Lock`, but still never calls write, apply,
destroy, or run-operation methods. Only apply calls those mutating interfaces.

## State, Desired, and Observed

Three kinds of information determine each resource action:

- desired: the state requested by current configuration in the graph node.
- prior: the resource state DebianForm last recorded in remote state.
- observed: the state the provider actually observes on the host.

State is not the sole source of truth. It records the desired digest, observed
value, ownership, and related data from DebianForm's last management action.
The provider must still inspect the host to identify drift and pre-existing
resources.

## Main Engine.Plan Flow

`Engine.Plan` performs these steps:

1. Ensure the backend and provider are non-nil.
2. Call `resourceGraph.Validate`.
3. Call `Backend.Read` for every target host.
4. Call `Provider.Plan(ctx, node, prior)` for every graph node.
5. Add non-no-op steps to `Steps`.
6. Record changed addresses that may trigger operations.
7. Call `orphanSteps` for resources present in state but absent from desired.
8. Call `operationSteps` to select operations based on changed addresses.
9. Sort resource and operation steps.
10. Generate the summary.

`opts.Host` filters state reads, node traversal, orphan handling, and operations.
Operation filtering compares `graph.Operation.Host` directly; it does not infer
the execution target from an address prefix.

## Engine.Check Lock Period

`Engine.Check` selects target hosts using `opts.Host` and acquires all of their
state locks before reading any state. `opts.LockTimeout` controls lock wait time.
After every lock is held, it calls `Engine.Plan`; state reads and provider
observations, whether per resource or per host, all run inside the lock period.
All locks are released only afterward.

Losing a lock lease cancels the in-progress plan. Whether the plan succeeds,
fails, or its context is canceled, the engine attempts to release every acquired
lock and returns lease or unlock errors together with any plan error. This
wrapper provides only a consistency boundary; it neither persists host facts
nor writes state. Runtime fact discovery performed by the CLI to parse
configuration occurs before `Engine.Check` and is therefore outside the lock
period. External processes that do not honor the DebianForm state lock are not
prevented from making changes.

## ProviderPlan

A provider returns `ProviderPlan`:

- `Action`
- `Summary`
- `Observed`
- `Ownership`

If the provider omits an action, the engine treats it as `no-op`. If it omits a
summary, the engine falls back to the node summary.

Most provider resources ultimately call `Compare`, although a provider may add
special-case decisions for a particular resource.

## Compare Action Semantics

The central rules of `Compare(node, prior, observed)` are:

- Desired ensure absent and observed exists -> `delete`.
- Desired ensure absent and observed does not exist -> `no-op`.
- Observed does not exist -> `create`.
- Observed digest equals desired digest and prior is absent -> `adopt`.
- Observed digest equals desired digest and prior is present -> `no-op`.
- Prior exists and observed digest differs from prior digest -> `update`, with
  a repair-drift summary.
- Any other digest mismatch -> `update`.

`adopt` means the host already contains a resource that matches desired, but
DebianForm state has no record of it. Apply writes it to state without invoking
a provider mutation.

## Orphan Handling

An orphan is a resource present in state but absent from the current desired
graph. `orphanSteps` generates one of:

- `destroy`: delete the remote resource and remove it from state by default.
- `forget`: remove it only from state, leaving the remote resource untouched.

Typical cases that select `forget` include:

- Prior ownership is `adopted`.
- An `apt_source_file` has `on_destroy = keep`.
- Another desired directory continues to manage the directory.

If an orphan would be destroyed while lifecycle `prevent_destroy` is true,
planning fails immediately.

## Operation Triggers

`operationSteps` traverses graph operations. If any address in an operation's
`TriggeredBy` set is changed in this plan, it generates an
`OperationStep{Action: run}`.

Only `create`, `update`, and `delete` trigger operations under the current
`triggersOperation` rules. `adopt`, `forget`, `destroy`, and `no-op` do not.

This distinction matters: an operation represents an action required after a
real resource write or deletion caused by configuration, not state
housekeeping.

## Plan.Document

`engine.Plan.Document` turns engine steps into `plan.Document`. It:

- Constructs before and after values for each step.
- Uses prior desired as before for destroy and forget.
- Generates diffs with `BuildDiff`.
- Carries explicit hosts and operation information on changes and operations.
- Uses the engine summary.

With `--debug`, it emits provider addresses. A provider address may also be
recovered from prior state.

## Design Boundaries

- The engine owns action semantics, not HCL parsing.
- The engine must not know the shell details of each provider resource.
- Providers observe reality; the engine organizes provider plans into a global
  plan.
- Orphan and lifecycle rules belong to the engine because they depend on the
  relationship between state and desired.

## Change Checklist

- New action: update constants, Compare, summaries, documents, apply execution,
  and plan output.
- Drift-detection change: add combinations of prior, observed, and desired.
- Orphan-policy change: add state tests and apply no-op/destroy/forget cases.
- Operation-trigger change: check APT, systemd, service, and Docker behavior.
- Host-filter change: verify consistent filtering of state reads, nodes,
  orphans, and operations.
