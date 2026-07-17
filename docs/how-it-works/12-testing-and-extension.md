# 12. Testing Strategy and the Capability-Extension Checklist

<p align="right"><strong>English</strong> | <a href="12-testing-and-extension.zh.md">简体中文</a></p>

This chapter explains DebianForm's testing layers and the areas to update when
adding a domain block or provider resource.

## Testing Layers

Project tests fall broadly into these layers:

- Parser unit tests protect HCL, variables, expressions, and the value model.
- Merge unit tests protect profile merging, component inputs, IR compilation,
  and validation.
- Graph goldens protect expansion from IR into the resource graph.
- Plan goldens protect plan documents, text, diffs, and actions.
- Engine unit tests protect state, actions, apply, the SSH backend, and facts.
- CLI tests protect command entry points, flags, inspect, and redaction.
- Libvirt integration tests protect real behavior on Debian and Ubuntu targets.
- Source-build integration tests protect source-building components.

Do not use one testing layer as proof of a cross-layer capability. A new DSL
capability normally needs evidence at least in parser/merge, graph, and
plan/engine layers.

## Golden Data Directories

The primary directories under `internal/core/testdata` are:

- `fixtures`: input `.dbf.hcl` and variable files.
- `hostspec`: compiled IR host-spec goldens.
- `graph`: resource-graph goldens.
- `plan`: plan JSON/text goldens.
- `parser`: parser-related goldens.
- `invalid`: configuration examples expected to fail.

Goldens are architectural boundary snapshots, not a burden. Before changing a
golden, confirm that the diff represents intended semantics rather than an
accidental address or redaction change.

## CLI Tests

`cmd/dbf/*_test.go` covers:

- Command dispatch.
- Default file selection and `-f`.
- Inspect output.
- Plan output.
- The redaction matrix.

CLI tests are appropriate for user-visible behavior and combined paths,
especially variable precedence, sensitive inputs, and output formats.

## Engine and Provider Tests

`internal/core/engine/*_test.go` is appropriate for:

- The `Compare` action matrix.
- Apply state updates.
- Orphan destroy/forget.
- Lock/read/write.
- Fact discovery.
- SSH runner arguments.
- Provider command behavior through a fake runner.

Check real remote command scripts and returned observed data through a fake
runner where possible. Use libvirt when real system behavior must be proven.

## Libvirt Integration

`test/integration/libvirt` covers apply/check flows on real Debian and Ubuntu
targets. A case commonly contains:

- `*.dbf.hcl`
- `*.check.sh`
- Optional `*.drift.sh`
- Multi-host case files.

Good candidates include:

- Systemd, APT, Docker, networking, kernel, and similar behavior that a local
  fake cannot prove adequately.
- Multi-host dependencies.
- A real apply/check/drift loop.

## Steps for Adding a Domain Block

Adding a user-facing DSL domain block usually requires:

1. Parser: recognize the block or attribute and produce `parser.Value` or a
   dedicated structure.
2. IR: add a spec type and `HostSpec` field.
3. Merge: build the spec from raw values, assigning defaults, source,
   lifecycle, and summary.
4. Validate: check required values, enumerations, paths, and references.
5. Graph: expand the spec into nodes and operations with stable addresses.
6. Provider: when a new kind is required, implement Plan/Apply/Destroy.
7. Plan/state: confirm diff, sanitization, and digest behavior is sufficient.
8. Docs: update user documentation, the support matrix, and relevant chapters
   in this series.
9. Tests: add parser/merge/graph/plan/engine/CLI/integration coverage.

If the new block is only syntactic sugar, expand it into an existing provider
kind in merge or graph where possible instead of adding a needless provider.

## Steps for Adding a Provider Resource

Adding a provider resource kind usually requires:

1. Graph: generate a node and define `Kind`, `ProviderType`, `Desired`, and
   `ProviderPayload`.
2. Provider: register it in Plan/Apply/Destroy switches.
3. Provider plan: define observations and action decisions.
4. Provider apply: implement idempotent create/update/delete commands.
5. Provider destroy: implement removal of orphaned managed resources.
6. State: confirm desired/observed sanitization and digests.
7. Scheduling: update `SafeParallelKind` when appropriate.
8. Operations: when reload/restart/update is required, generate an operation
   and implement RunOperation.
9. Tests: add fake-runner unit tests, graph goldens, plan goldens, and libvirt
   coverage when necessary.

## Steps for Adding a Component Capability

Component capabilities span the parser, merge, graph, and provider layers.
Check:

- Component-template syntax.
- Input types, defaults, validation, and sensitivity.
- Artifact source/extract/build/install fields.
- Fact dependencies, such as architecture.
- Component instance address prefixes.
- Provider behavior for source builds or binary installation.
- Redaction, especially for sensitive component inputs.

Components readily affect addresses and state, so scrutinize golden diffs when
adding fields.

## Test Selection Guidance

- Pure syntax error: parser test.
- Type normalization or default: merge test plus hostspec golden.
- Resource expansion or dependency: graph golden plus scheduling test.
- Plan presentation: plan golden.
- Action decision: engine/provider unit test.
- Remote shell command: provider fake-runner test.
- Real system effect: libvirt integration.
- Secret risk: redaction matrix.

## Definition of Done

A capability is not complete merely because the plan looks correct. Completion
requires at least:

- Validate detects invalid configuration.
- Offline plan produces a reasonable resource shape or explains explicitly why
  runtime facts are required.
- Online plan distinguishes create, update, no-op, drift, and adopt correctly.
- Apply executes idempotently.
- Check detects drift.
- State does not leak sensitive content.
- Docs and tests cover user-visible behavior and maintainer boundaries.

## Change Checklist

- Did this change an address, digest, JSON format, or state schema?
- Does it affect sensitive, ephemeral, or content-write-only behavior?
- Does it require a new operation or dependency?
- Is host-filter, multi-host, or concurrency coverage needed?
- Does distribution-specific real behavior need libvirt verification on the
  corresponding target?
- Were the support matrix, CLI documentation, and this tutorial series updated?
