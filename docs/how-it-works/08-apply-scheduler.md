# 08. Apply Execution, Locking, and Scheduling

<p align="right"><strong>English</strong> | <a href="08-apply-scheduler.zh.md">简体中文</a></p>

This chapter explains how `Engine.Apply` executes changes. Apply is the only
core DebianForm path that mutates remote hosts and remote state.

## Data Flow

```text
program + resourceGraph
  -> Backend.Lock(each host)
  -> Engine.Plan
  -> BeforeExecute(print/review/approve locked plan)
  -> persistHostFacts
  -> Backend.Read(state)
  -> executionWaves
  -> runExecutionWaves
  -> Provider.Apply/Destroy/RunOperation
  -> Backend.Write(state)
```

The CLI first prints an unlocked preview plan for user approval. After
`Engine.Apply` locks every target host, it replans and passes the actual
execution plan through `BeforeExecute` for printing and review before any fact
or state write or provider mutation.

## The `apply --debug` Debugging Layer

For `dbf apply --debug`, the CLI creates a `DebugSession`, wraps the underlying
`SSHRunner` in a `DebugRunner`, and passes that runner to fact discovery, the
SSH backend, and the native provider. The debugger therefore sees every remote
call on the real apply path:

- Fact discovery.
- State reads and provider inspection during online planning.
- State locking, replanning, actual-plan review, and fact persistence before
  apply.
- Resource apply/destroy, operation execution, and operation-output reads.
- The state write after each successful resource.
- State-unlock cleanup on exit or failure.

`DebugRunner` does not change provider scripts or Engine scheduling semantics.
It prints context before and after remote calls and uses user input to allow a
call, allow subsequent calls, retry a failed call, or cancel an ordinary call.
Engine, backend, and provider code injects the context through
`RemoteCallContext`, including phase, address, action, summary, and cleanup flag.

In debug mode, the CLI fixes remote-call concurrency at 1 for fact discovery,
online planning, and apply so interactive prompts and multi-host output cannot
interleave. Cleanup calls are still printed but do not wait for a prompt. After
the user quits, DebianForm still makes a best-effort attempt to release state
locks. Background lease renewal is a maintenance call and bypasses the
interactive debugger so pausing at a prompt does not block renewal.

## Why Apply Replans

Reality may change between displaying a plan for approval and executing it:

- Another process may change remote state.
- A person may modify the host manually.
- A previous apply may have failed partway and then had some resources restored.

After acquiring the lock, `Engine.Apply` rereads state and observed reality so
execution uses more current information. The CLI prints this actual plan while
the lock remains held. In interactive mode, it explicitly requests approval
again if the actual plan differs from the approved preview, otherwise it reuses
the original approval. `--auto-approve` does not prompt, but still prints the
actual plan.

## Lock Ordering

`Engine.Apply` first calls `Backend.Lock` for each target host. The current SSH
backend uses a remote lock path, lock directory, and persistent guard file:

- `--lock-timeout` limits only wait time; it is not the lease TTL.
- A version 2 lock contains owner, PID, token, independent lease expiry, and an
  integrity checksum.
- A fresh holder must win the cross-version atomic bridge through exclusive
  `mkdir lock.d`, then write a token-bearing `lock.d/owner.v2` marker.
- By default, the holder renews a 2-minute lease every 30 seconds. Renewal
  failure cancels the active apply and returns the root cause.
- Acquisition, renewal, stale takeover, and unlock all revalidate token and
  lease under the same `flock` guard.
- A stale takeover uses a temporary file in the same directory and atomic `mv`;
  it cannot delete a lock subsequently published by another contender.
- After the acquisition SSH call returns, DebianForm synchronously renews and
  revalidates the token. Acquisition fails if the lock expired or was taken
  over during return latency.
- Only a partially written version 2 lease with a verifiable marker may be
  taken over after the recovery grace period. Markerless, legacy, and unknown
  records reject automatic takeover.
- Unlock verifies the token to avoid deleting another holder's lock.

Locks are host-level, not resource-level. Concurrent applies to the same host
exclude each other.

## Fact Persistence

`persistHostFacts` writes discovered host facts into state, preserving the most
recent online values for:

- hostname
- architecture
- codename
- detected_at

This occurs after the locked plan has been printed and approved but before
resource execution. Rejecting a changed actual plan writes neither facts nor
resource state. A fact write does not indicate that resources have executed; it
only saves runtime context.

## Execution Waves

`executionWaves(resourceGraph, plan)` groups active steps and operations into
dependency-ordered waves.

It handles two kinds of address:

- Active steps and operations that remain in the graph are topologically
  sorted by `ResourceGraph.ActiveWaves`.
- State-orphan steps, which are absent from the current graph, are placed in an
  initial orphan wave.

Apply can therefore destroy or forget orphaned resources and still execute
normal changes in current graph-dependency order.

## Meaning of ActiveWaves

`ActiveWaves` schedules only addresses that execute in the current run.
Unchanged dependencies are not executed, but if one active node depends on
another active node, their order is preserved.

For example:

- When a repository source and APT cache refresh are both active, the source
  runs first.
- When a package is active but its repository is unchanged, the package does
  not wait for a repository step that will not execute.

## Concurrency Control

`runExecutionWaves` uses two semaphore levels:

- Global concurrency: `opts.Parallel`, corresponding to CLI `--parallel`.
- Per-host concurrency: `opts.PerHostParallel`, currently 1 by default.

Each execution item also uses `SafeParallelKind` to choose how many host slots
it occupies:

- A safe-parallel resource occupies 1 host slot.
- A resource not safe for parallel execution occupies all slots for that host,
  effectively serializing it within the host.

Both limits use weighted semaphores. An item not safe for parallel execution
atomically acquires the host's full capacity, so it cannot deadlock with another
waiter when each holds only part of the slots. When the context is canceled, an
unsuccessful request occupies and leaks no capacity.

With the current per-host default of 1, one host remains effectively serial;
global concurrency primarily benefits multiple hosts.

## Failure Propagation

Runnable items in a wave execute concurrently. After execution:

- Failed addresses are recorded in `failed`.
- In later waves, items depending on failed addresses are marked blocked and do
  not execute.
- `runExecutionWaves` returns the first error.

Successfully applied resources are not rolled back. DebianForm relies on state
and observations so the next `plan/apply` can continue convergence.

When apply exits, it gives each host's state unlock an independent short-timeout
context that preserves values from the original call but not its canceled state
or deadline. Even if the caller is canceled or one unlock times out or fails,
DebianForm attempts to release every other acquired lock. The apply error, lost
lease, and every host-qualified unlock error are returned together through the
error chain. An unlock failure prevents a fully successful result even when the
main flow succeeded. If acquisition fails partway through multiple hosts, all
already acquired locks are released, and acquisition and rollback-cleanup
errors are combined.

## Resource-Step Execution

`executeResourceStep` chooses the provider call by action:

- `create`, `update`, `delete` -> `Provider.Apply`
- `destroy` -> `Provider.Destroy`
- `adopt` -> write state without changing the remote resource
- `forget` -> remove only state
- `no-op` -> do nothing

After success, state is updated as follows:

- `create`, `update`, and `adopt` write resource state.
- `delete`, `destroy`, and `forget` remove resource state.
- `no-op` does not write.

`Backend.Write` runs immediately after each successful resource, not once at
the end of apply. State after a partial failure therefore reflects completed
progress as closely as possible.

## Provider Payload

Before execution, `providerStep` turns a node into a provider node. Normally,
the provider uses `ProviderPayload` as desired.

Content-write-only resources are the exception: execution retains the original
node so write-only content never enters persistable desired data.

## Operation Execution

`Provider.RunOperation` executes operations. Neither success nor failure writes
state because an operation is not a resource.

The graph generation logic and provider implementation jointly guarantee
operation idempotence and safety. APT cache refreshes, daemon reloads, and
service restarts should all be safe to repeat.

## State Contents

`resourceStateForStep` writes:

- host
- kind
- provider type/address
- ownership
- lifecycle
- sanitized desired
- desired digest
- sanitized observed
- updated_at
- order

The desired digest is computed from unredacted desired, while state stores the
sanitized desired. This supports change comparison without persisting plaintext
content.

## Design Boundaries

- Apply does not roll back; a later plan/apply converges after failure.
- A host-level lock does not replace idempotence inside provider commands.
- State records progress but is not the sole source of remote reality.
- Provider actions must be repeatable after a partially successful apply.

## Change Checklist

- New action: update `executeResourceStep` and state-update logic.
- New operation: verify dependencies, triggers, provider `RunOperation`, and
  failure behavior.
- Concurrency change: add scheduling/engine tests, especially for resources not
  safe to parallelize on one host.
- State-write change: verify sanitization, digest, serial, `updated_at`, and
  partial-failure semantics.
- Lock change: add SSH backend tests and verify stale-lock and token-mismatch
  behavior.
