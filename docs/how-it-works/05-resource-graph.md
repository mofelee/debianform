# 05. How ResourceGraph Expands Resources and Dependencies

<p align="right"><strong>English</strong> | <a href="05-resource-graph.zh.md">简体中文</a></p>

This chapter explains how `internal/core/graph` expands `ir.Program` into a
`ResourceGraph`. The graph is the common input to plan and apply. It defines
resource addresses, provider payloads, dependencies, and operation triggers.

## Data Flow

```text
ir.Program
  -> graph.Compile
  -> compileHost
  -> ResourceGraph{Nodes, Operations}
  -> Validate / Waves / ActiveWaves
```

The graph still does not connect to remote hosts. It derives desired resources
and static dependencies solely from IR.

## ResourceGraph

`ResourceGraph` contains two kinds of object:

- `Nodes`: resource nodes a provider can plan, apply, or destroy.
- `Operations`: one-time actions triggered by resource changes, such as
  refreshing the APT cache or restarting a service.

Both nodes and operations have stable addresses. An address is the shared
identity used by state, plans, dependencies, and test goldens, so it must not
change casually.

## Node

Important fields of `graph.Node` include:

- `Host`: the owning host.
- `Address`: the DebianForm-layer resource address.
- `Kind`: resource kind, such as `file`, `package`, or `systemd_unit`.
- `Summary`: short plan-facing description.
- `Source`: origin in user configuration.
- `Lifecycle`: for example, `prevent_destroy`.
- `Desired`: DebianForm-layer desired value.
- `ProviderType`: provider type.
- `ProviderAddress`: low-level provider address, primarily for debugging.
- `ProviderPayload`: the payload passed to the provider.
- `DependsOn`: graph addresses on which the node depends.

For most nodes, `Desired` and `ProviderPayload` are identical. Some resources
differ, such as a user-facing abstract resource translated into a file-like
provider payload.

## Operation

Important fields of `graph.Operation` include:

- `Host`: the explicit execution target; schedulers and providers do not parse
  it from the address.
- `Address`
- `Action`
- `Summary`
- `DependsOn`
- `TriggeredBy`
- `CommandPreview`
- `Source`

Operations do not enter state. They appear only in plans and execute during
apply when their trigger conditions are met.

`TriggeredBy` identifies the resources whose `create`, `update`, or `delete`
requires this operation. `DependsOn` controls scheduling order, such as waiting
for a file to be written before running a reload.

## Responsibilities of compileHost

`compileHost` is the main graph-expansion function. It creates nodes and
operations for every domain spec on a host.

Typical expansions include:

- Kernel module -> `kernel_module` node.
- Sysctl -> `sysctl` node, depending on the `tcp_bbr` module when necessary.
- APT repository -> signing-key node, source-file node, and APT cache-refresh
  operation.
- Package -> `package` node, depending on repositories or cache refresh when
  necessary.
- Files, secrets, systemd, nftables, and networkd -> file-like nodes.
- Group, user, membership, and authorized key -> identity and permission nodes.
- Service -> `service` node, potentially depending on a systemd unit.
- Docker -> several repository, package, daemon, service, and Compose-related
  nodes.
- Component -> artifact and domain-resource nodes with a component prefix.

`compileHost` first builds address indexes for groups, users, systemd units,
repositories, and similar resources. Later nodes use these indexes to add
dependencies.

## Address Stability

Addresses generally have these forms:

```text
host.<host>.<domain>.<kind>[<quoted-key>]
host.<host>.components.<component>.<domain>.<kind>[<quoted-key>]
```

An address must:

- Remain identical every time the same desired resource is compiled.
- Never collide with a different resource.
- Be suitable as a state key.
- Be readable and useful for locating errors.

Changing an address turns the old resource in state into an orphan, potentially
triggering destroy or forget. Do not change address formats without an explicit
compatibility migration.

## Dependencies

`DependsOn` guarantees execution order. Examples include:

- A BBR sysctl depends on the `tcp_bbr` module.
- An APT repository source depends on its signing key.
- A package depends on an APT cache refresh or repository.
- A service depends on its unit file.
- An operation depends on its triggering resource.

The graph expresses only static dependencies. The engine determines whether a
dependency actually executes in the active plan.

## Graph Validation and Scheduling

`ResourceGraph.Validate` calls `scheduleEntries` and `validateAcyclic` to:

- Check that addresses are non-empty.
- Check that addresses are unique.
- Ensure every `DependsOn` references a known address.
- Ensure every `TriggeredBy` references a known address.
- Detect dependency cycles.

`Waves` returns topological waves for the complete resource graph.

`ActiveWaves(active)` schedules only addresses that must execute in this run.
It ignores dependencies that are not active, allowing apply to run only changed
nodes and triggered operations.

## SafeParallelKind

`SafeParallelKind` marks resource kinds suitable for parallel execution on the
same host. File-like, directory, and component-artifact resources are currently
more amenable to parallelism; package, user, group, and service resources are
conservative by default.

Apply scheduling combines:

- Global `--parallel`.
- Per-host concurrency slots.
- `SafeParallelKind`.

The graph only supplies a resource-kind safety hint; the engine performs the
actual scheduling.

## Sensitive Graph JSON

`Node.MarshalJSON` clears `ProviderPayload` when the node is content-write-only
or sensitive.

This is necessary because graph data may appear in tests or debug output, while
provider payloads commonly hold real execution data such as `content` and
`source_path`. The in-memory graph and IR may require that content to execute,
but default JSON serialization must never disclose it.

## Design Boundaries

- The graph may understand resource expansion and dependencies.
- The graph must not read state or observed reality.
- The graph must not choose actions.
- The graph may generate command previews, but must not execute commands.
- Graph addresses are a stable interface and changes require compatibility
  treatment.

## Change Checklist

- New resource kind: define its address, kind, desired value, provider
  type/payload, source, and lifecycle.
- New dependency: add graph-validation and scheduling tests to prevent cycles
  and unknown references.
- New operation: verify `TriggeredBy`, `DependsOn`, plan presentation, and
  provider `RunOperation` behavior.
- Address change: assess state-orphan consequences and design a migration when
  needed.
- New sensitive payload: verify that `Node.MarshalJSON`, plan diffs, and state
  sanitization cannot leak it.
