# DebianForm Implementation Plan

<p align="right"><strong>English</strong> | <a href="implementation-plan.zh.md">简体中文</a></p>

This document turns the design into an executable development plan. Every loop
must form a mergeable closed cycle:

- The code path runs.
- Unit and golden tests cover the loop's semantics.
- At least one example is an acceptance input.
- Documentation is synchronized.
- `make test` passes.

Status:

- `[x]` Complete
- `[ ]` Incomplete

## Current Baseline

- [x] User requirements exist: `docs/archive/legacy-design/requirements.md`.
- [x] Intermediate-representation requirements exist: `docs/archive/legacy-design/ir-requirements.md`.
- [x] Plan JSON format documentation exists: `docs/plan-format.md`.
- [x] Design examples exist: `examples/*.dbf.hcl`.
- [x] The compiler pipeline supports HostSpec, ResourceGraph, state, plan, apply, and check.
- [x] The CLI integrates `validate`, `plan`, `apply`, `check`, and `fmt`.
- [x] Plan/apply integrates observed detection, remote state, and SSH execution.

## Loop 1a: Parsing, Merging, and HostSpec

Goal: validate `examples/bbr.dbf.hcl` through the parser/merge/HostSpec first
half of compilation, without ResourceGraph or plan.

Scope:

- `locals`
- `profile`
- `host`
- `imports`
- `ssh`
- `state`
- `system`
- `kernel`
- `packages`

Deferred:

- `apply`
- `component`
- `files`
- `secrets`
- `directories`
- `users`
- `groups`
- `systemd`
- `services`
- `apt`
- `nftables`
- ResourceGraph and plan, assigned to Loop 1b.
- HTML preview.

Note: Loop 1a permits `assert` blocks in host/profile for compatibility with the
existing BBR example but does not evaluate them. Loop 2 implements assertion
validation.

Code:

- [x] Add `internal/core/parser` for top-level and in-scope domain blocks.
- [x] Preserve `SourceRef` with at least file, line, and path.
- [x] Add `internal/core/merge` with ordered profile imports.
- [x] Implement deduplicating list append.
- [x] Implement deep map merge.
- [x] Implement later-scalar override.
- [x] Implement `force(value)`.
- [x] Implement `before(value)` and `after(value)`.
- [x] Implement `unset()` for map-key removal.
- [x] Add `internal/core/ir` with `Program`, `HostSpec`, and in-scope domain specs.
- [x] Fill defaults for `ssh`, `state`, and `system.hostname`.
- [x] Normalize `kernel.modules`.
- [x] Normalize `kernel.sysctl`.
- [x] Normalize `packages.install`.
- [x] Select a snapshot/golden framework and provide one `make update-golden` refresh path.
- [x] Route `dbf validate -f examples/bbr.dbf.hcl` through this path.

Tests:

- [x] Parser unit tests cover `host`, `profile`, nested domain blocks, and source lines.
- [x] Parser negatives cover unknown top-level blocks, wrong label count, and duplicate hosts.
- [x] Merge unit tests cover import order, list deduplication, map override, and scalar override.
- [x] Merge modifier tests cover `force`, `before`, `after`, and `unset`.
- [x] Merge negative covers profile import cycles.
- [x] HostSpec snapshot covers `examples/bbr.dbf.hcl`.

Examples:

- [x] Use `examples/bbr.dbf.hcl` as the primary golden fixture.
- [x] Add a small profile-merge example covering base packages, BBR profile, and host override.

Documentation:

- [x] Document supported and deferred scope for this loop.

Acceptance:

- [x] `dbf validate -f examples/bbr.dbf.hcl` succeeds.
- [x] The profile-merge fixture HostSpec snapshot is stable.
- [x] `make test` succeeds.

## Loop 1b: ResourceGraph and BBR Plan

Goal: compile ResourceGraph and a structured plan from Loop 1a HostSpec so
`examples/bbr.dbf.hcl` completes planning without user-authored low-level
provider resources.

This loop is a create-only preview that assumes an empty remote target and does
not read observed state. Loop 4 introduces desired/state/observed comparison and
will rewrite these plan goldens.

Code:

- [x] Add `internal/core/graph` to compile ResourceGraph from HostSpec.
- [x] Generate stable addresses such as `host.bbr1.kernel.module["tcp_bbr"]`.
- [x] Generate low-level provider payloads hidden from ordinary plans.
- [x] Derive BBR dependency: `tcp_congestion_control = "bbr"` depends on `tcp_bbr`.
- [x] Add `internal/core/plan` with a structured plan model.
- [x] Support `dbf plan -f examples/bbr.dbf.hcl`.
- [x] Support `dbf plan -f examples/bbr.dbf.hcl --format json`.

Tests:

- [x] ResourceGraph snapshot covers BBR module/sysctl addresses and dependency edges.
- [x] Plan JSON golden covers BBR create plan.
- [x] CLI smoke covers validate, plan text, and plan JSON.

Documentation:

- [x] Mark BBR validate/plan as available in README.
- [x] Add BBR plan-output example.

Acceptance:

- [x] `dbf plan -f examples/bbr.dbf.hcl` emits user addresses.
- [x] JSON plan emits `debianform.plan.alpha1`.
- [x] `make test` succeeds.

## Loop 2: Complete Merge Semantics and Error Locations

Goal: stabilize profile/host composition and report errors against user DSL,
not internal providers.

Code:

- [x] Complete `SourceRef.Path` for lists, maps, and labeled object blocks.
- [x] Carry final user sources on merge errors.
- [x] Support host overrides of profile lists, maps, and scalars.
- [x] Normalize labeled objects to maps before field-level merge.
- [x] Reject forbidden fields, such as `system.hostname` in profiles.
- [x] Detect duplicate resource identities within one host.
- [x] Detect duplicate package declarations.
- [x] Detect empty sysctl keys and kernel modules.
- [x] Reject `unset()` on a list.
- [x] Parse repeated host `assert` blocks with HCL boolean `condition`.
- [x] Evaluate assertions after profile merge/defaults and before ResourceGraph compilation.
- [x] Bind `self` to the merged host view; assertions cannot read remote runtime state.
- [x] Stop validate/plan/apply on assertion failure and report the source.
- [x] Require a non-empty string `message`.

Tests:

- [x] Merge golden covers multiple profile imports.
- [x] Merge golden covers host overrides.
- [x] Merge golden covers labeled-block merging.
- [x] Negative covers complete import-cycle path.
- [x] Negative covers invalid host-only field in profile.
- [x] Negatives cover duplicate package, empty key, and invalid modifier.
- [x] Assertion unit test covers successful `self` evaluation.
- [x] Assertion negatives cover false condition, empty message, and invalid field access.
- [x] Error-message tests cover file:line:path.

Examples:

- [x] Add `examples/profile-merge.dbf.hcl`.
- [x] Add at least one intentional invalid fixture under tests, not examples.

Documentation:

- [x] Add a labeled-block merge example.
- [x] Document invalid merge-modifier usage.

Acceptance:

- [x] Profile-merge HostSpec snapshot is stable.
- [x] All negative cases return readable source locations.
- [x] Assertion failure points to the user DSL line.
- [x] `make test` succeeds.

## Loop 3: First Everyday Domain Blocks

Goal: add local domains required for basic system configuration while retaining
user-facing plan addresses.

Scope:

- `files`
- `secrets`
- `directories`
- `groups`
- `users`
- `systemd`, raw unit files only.
- `services`

Scope note: structured systemd `service "app"` / `timer "app"` and one-to-one
serialization for `systemd.networkd`/`resolved`/`journald` are deferred.
WireGuard/networkd uses structured `systemd.networkd` to generate native
`.netdev` and `.network` files, without `wg-quick`.

Code:

- [x] Parser supports in-scope domains and labeled object blocks.
- [x] IR adds FileSpec, SecretSpec, DirectorySpec, GroupSpec, UserSpec, SystemdSpec, and ServiceSpec.
- [x] Files require exactly one of content/source.
- [x] Files support owner, group, mode, ensure, and sensitive.
- [x] Secrets support local source files.
- [x] Secrets default to root/root/0600.
- [x] Plans, logs, and state emit only secret summaries.
- [x] Directories support owner, group, mode, and ensure.
- [x] Groups support system, GID, and ensure.
- [x] Users support home, shell, group, groups, system, authorized keys, and ensure.
- [x] Systemd supports unit-file management.
- [x] Services support enabled and state.
- [x] Compile systemd daemon-reload operation.
- [x] Compile service restart/reload operation.
- [x] Derive user -> group dependency.
- [x] Derive service -> package dependency.
- [x] Derive service -> systemd unit dependency.
- [x] Detect remote path conflicts between files and secrets.

Tests:

- [x] Parser unit tests cover labeled object blocks.
- [x] HostSpec snapshot covers files/secrets/directories/users/groups/systemd/services.
- [x] ResourceGraph snapshot covers semantic dependency edges.
- [x] Plan golden covers file text diff.
- [x] Negatives cover duplicate path, invalid mode, missing group reference, and absent secret source.
- [x] On introducing secrets, assert plan/log/state never contains plaintext and emits only summaries.

Examples:

- [x] Add user/group/authorized-key example.
- [x] Add systemd service example.
- [x] Extract a small test fixture from `examples/fleet.dbf.hcl`.

Documentation:

- [x] Update basic system-configuration example in README.
- [x] Document file sensitive limits.
- [x] Document that secret plaintext is never persisted.
- [x] Document service state semantics.

Acceptance:

- [x] All new examples validate.
- [x] Plans show addresses for files, secrets, users, and services.
- [x] Plan/state/logs contain no secret plaintext from this loop onward.
- [x] `make test` succeeds.

## Loop 4: State and Apply

Goal: apply kernel, package, and first everyday domain resources and write state.

Code:

- [x] Define the state-file schema.
- [x] Define desired/state/observed comparison.
- [x] Use independent state and lock paths per host.
- [x] Acquire remote locks atomically with owner/PID/token/expires_at.
- [x] Verify token on unlock to avoid deleting another process's lock.
- [x] Allow takeover of expired stale locks with an output notice.
- [x] Exclude lock files from plans and state.
- [x] Prefer user addresses as state resource keys.
- [x] Retain low-level provider addresses as debug state.
- [x] Store prior desired summaries and necessary observed summaries.
- [x] Providers read observed state by ResourceGraph.
- [x] Produce create/update/delete/destroy/no-op from desired, state, and observed.
- [x] Distinguish remote absent, existing/adoptable, and drifted resources.
- [x] When desired is absent but state exists, plan destroy or forget by ownership.
- [x] On observation failure, return diagnostics instead of assuming state equals reality.
- [x] Implement native SSH runner.
- [x] Implement native provider execution.
- [x] Implement serial single-host apply.
- [x] Add a CLI interface for parallel multi-host apply.
- [x] After failure, block dependent later nodes.
- [x] Persist successful nodes after partial apply failure to match remote progress.
- [x] `check` exits nonzero when the plan has changes.
- [x] Prevent delete/destroy/replace for `prevent_destroy` resources.

Tests:

- [x] State read/write unit tests.
- [x] Comparison tests cover the desired/state/observed matrix.
- [x] Comparison tests cover create, update, delete, destroy, forget, adopted ownership, and no-op.
- [x] Drift test covers state previously in sync while observed changed.
- [x] Observation-failure test covers explicit diagnostics.
- [x] Apply dry-run or fake-runner test.
- [x] Lock acquire/release/token/stale-takeover tests.
- [x] Idempotence: immediate plan after fake apply is no-op.
- [x] After partial failure, state contains only successful nodes.
- [x] `prevent_destroy` negative test.
- [x] Check exit-status test.

Examples:

- [x] Add BBR example to fake-runner apply tests.
- [x] Add package example to fake-runner apply tests.

Documentation:

- [x] Add state-file documentation.
- [x] Add apply/check instructions to README.
- [x] State that old state addresses are incompatible with current ones.

Acceptance:

- [x] Fake-runner apply writes state.
- [x] Plan reads observed rather than comparing only desired and state.
- [x] Plan reports create, update, delete, destroy, forget, adopted ownership, and no-op correctly.
- [x] Immediate plan after fake apply is no-op.
- [x] State matches executed results after partial failure.
- [x] `dbf check` detects drift or changes.
- [x] `make test` succeeds.

## Loop 5: Structured Plan Renderer

Goal: make plans a stable machine interface with readable terminal and HTML preview.

Scope:

- Text diff.
- Sensitive diff.
- JSON renderer.
- Terminal tree renderer.
- Static HTML renderer.

Code:

- [x] Emit top-level `format_version = "debianform.plan.alpha1"`.
- [x] DiffNode supports object, map, set, list, scalar, text, and sensitive.
- [x] Text content supports line-level hunks.
- [x] Sensitive content emits summaries only.
- [x] JSON renderer has stable field order.
- [x] Terminal renderer displays source locations and field-level diffs.
- [x] Terminal renderer retains `+`, `-`, and `~` without color.
- [x] HTML renderer generates a standalone static file.
- [x] HTML preview supports action filtering.
- [x] HTML preview supports host filtering.
- [x] HTML preview supports field-path search.
- [x] Debug mode emits provider addresses.

Tests:

- [x] Plan JSON golden covers create/update/delete/no_op/run.
- [x] Text-diff golden covers ordinary file content.
- [x] Sensitive-diff golden covers secret files.
- [x] Terminal renderer snapshot.
- [x] HTML renderer smoke test.
- [x] Assert plan/state/logs contain no secret plaintext.

Examples:

- [x] Add a small files/secrets preview fixture as the primary fixture.
- [x] Add placeholder-secret guidance without committing real secrets.
- [x] Gitignore the example secrets directory.

Documentation:

- [x] Update actual fields in `docs/plan-format.md`.
- [x] Add `plan --format json` to README.
- [x] Add `plan --html plan.html` to README.

Acceptance:

- [x] JSON plan for `examples/files-plan-preview.dbf.hcl` leaks no secret plaintext.
- [x] HTML plan generates a static file.
- [x] `make test` succeeds.

## Loop 6: APT and Components

Goal: support reusable deployment units and explicit APT repository relationships.

Scope:

- `apt.repository`
- `packages.package`
- Component inputs.
- Architecture-selectable component sources.
- Binary artifacts.
- Archive/file/CA-certificate artifacts.

Code:

- [x] Parser supports `apt.repository`.
- [x] Parser supports `packages.package`.
- [x] Parser supports `component`.
- [x] Parser supports host `components`.
- [x] IR adds APTSpec.
- [x] IR adds ComponentInstanceSpec.
- [x] IR adds ComponentTemplateSpec.
- [x] Component inputs support string, number, bool, list(string), and map(string).
- [x] Component instances reject missing and unknown inputs.
- [x] Implement read-only `input.<name>` evaluation and template rendering inside components.
- [x] Implement read-only component `target` context, such as
  `target.system.codename`, without allowing host mutation.
- [x] Select exactly one source by host architecture.
- [x] Require SHA-256 for remote URL sources.
- [x] Repository signing keys accept exactly one of URL/content.
- [x] URL signing keys require SHA-256 and provider download verification.
- [x] Repository emits deb822 source payload.
- [x] Package repositories reference same-host repositories.
- [x] Generate host-scoped `apt.cache_refresh` operation.
- [x] Coalesce several repository changes into one APT refresh per host.
- [x] Compile binary artifacts into download, verify, and install nodes.
- [x] Compile archive/file artifacts into download, verify, and install nodes.
- [x] CA certificates generate `update-ca-certificates` operation.
- [x] Reject remote-identity collisions between components and host/profile.

Tests:

- [x] APT repository HostSpec snapshot.
- [x] APT ResourceGraph snapshot covers cache-refresh aggregation.
- [x] APT plan JSON golden covers cache-refresh operation.
- [x] Component parser unit tests.
- [x] Component architecture-selection tests.
- [x] Component `input.<name>` rendering tests.
- [x] Component `target` context test, with BIRD2 suites selecting `target.system.codename`.
- [x] Component input-validation negatives.
- [x] Artifact SHA-256 negative.
- [x] Identity-collision negative.

Examples:

- [x] Add `examples/apt-repository.dbf.hcl` to goldens.
- [x] Add `examples/bird2.dbf.hcl` to goldens.
- [x] Add `examples/component-binary.dbf.hcl` to goldens.

Documentation:

- [x] Add a basic component example to README.
- [x] Document APT repository/package dependency rules.
- [x] Document architecture source-selection rules.

Acceptance:

- [x] The same component mounts on several hosts.
- [x] Different architectures select matching sources.
- [x] Validation fails if a unique source cannot be selected.
- [x] `make test` succeeds.

## Loop 7: nftables

Goal: use native nftables files as the primary path with validation, activation,
and text diffs.

Code:

- [x] Parser supports `nftables.enable`.
- [x] Parser supports `nftables.main`.
- [x] Parser supports `nftables.file "<label>"`.
- [x] IR adds NftablesSpec.
- [x] Main defaults to `/etc/nftables.conf`.
- [x] File defaults to `/etc/nftables.d/<label>.nft`.
- [x] Require exactly one of content/source.
- [x] Detect final path conflicts.
- [x] Compile nftables file resources.
- [x] Compile `nftables.validate` operation.
- [x] Compile `nftables.activate` operation.
- [x] Coalesce several nftables file changes into one validate/activate per host.
- [x] Plan displays line-level nftables content diffs.

Tests:

- [x] HostSpec snapshot covers main and snippet.
- [x] ResourceGraph snapshot covers validation/activation aggregation.
- [x] Text-diff golden covers port changes.
- [x] Negatives cover duplicate path and simultaneous content/source.

Examples:

- [x] Add `examples/nftables.dbf.hcl` to goldens.
- [x] Use `examples/plan-preview.dbf.hcl` as a complete nftables + secret preview fixture.

Documentation:

- [x] Add a short nftables example to README.
- [x] State that nftables does not provide a generic firewall abstraction.

Acceptance:

- [x] Nftables examples validate and plan.
- [x] Plan displays validate and activate operations.
- [x] `make test` succeeds.

## Loop 8: Scheduler Enhancements

Goal: advance from conservative serial execution to ResourceGraph DAG-wave scheduling.

Code:

- [x] Implement ResourceGraph topological sorting.
- [x] Implement DAG wave calculation.
- [x] Validate unique resource addresses.
- [x] Validate dependency-address existence.
- [x] Detect cycles and report their path.
- [x] Implement global concurrency limits.
- [x] Implement per-host concurrency limits.
- [x] Add safe-parallel markers by resource type.
- [x] A failed node blocks dependent nodes.
- [x] Do not implement user `depends_on` in the first version. Dependencies are
  compiler-derived; reserve an escape hatch for a separate design.

Tests:

- [x] Wave-calculation unit tests.
- [x] Missing-dependency negative.
- [x] Cycle-detection negative.
- [x] Concurrency-limit tests.
- [x] Failure-propagation tests.

Documentation:

- [x] Add a wave example to scheduling semantics.
- [x] Add `--parallel` to CLI docs.

Acceptance:

- [x] Multi-host plan/apply presentation remains deterministically ordered.
- [x] Single-host execution may remain conservative and serial by default.
- [x] `make test` succeeds.

## Loop 9: Complete the Main Version

Goal: make the current implementation the primary DebianForm version.

Code:

- [x] Switch the default CLI path to current.
- [x] Implement `dbf fmt` for canonical DSL formatting.
- [x] Remove old experimental entry points and code paths.
- [x] Remove temporary feature gates no longer needed; retain only internal
  compile options for validation/formatting/runtime-fact bootstrap.
- [x] Use user terminology consistently in errors.
- [x] Include current docs and examples in release builds.

Tests:

- [x] Validate every first-version example except design-only fixtures.
- [x] Small golden examples cover parser, HostSpec, ResourceGraph, and plan.
- [x] `dbf fmt` idempotence test.
- [x] Use `fleet` only as a composition stress test.

Examples:

- [x] BBR example.
- [x] Nginx or systemd service example.
- [x] User/group/authorized-key example.
- [x] Multi-host/profile example.
- [x] APT repository example.
- [x] BIRD2 component example.
- [x] Binary component example.
- [x] Nftables example.
- [x] Plan-preview example.

Documentation:

- [x] Make current syntax the README entry point.
- [x] State that the old experimental format is retired.
- [x] Explain design fixtures versus runnable examples.
- [x] Add common-error guidance.

Acceptance:

- [x] All first-version acceptance criteria are met.
- [x] `make test` succeeds.
- [x] All current commands in README can be copied and run.

## Overall First-Version Acceptance Criteria

- [x] Configure BBR on one host with `host`.
- [x] Reuse BBR and base packages through `profile`.
- [x] Hosts override profile maps and lists.
- [x] `force([])` clears inherited lists.
- [x] The same component mounts on several hosts and selects one source by architecture.
- [x] Validation rejects remote-identity collisions between component and host/profile.
- [x] Plans emit user-level addresses.
- [x] Apply executes kernel modules, sysctls, and packages correctly.
- [x] The next plan after apply is no-op.
- [x] Each host uses independent state and locking for concurrency safety.
- [x] Validate detects import cycles, field conflicts, and graph cycles.
- [x] Assertions block invalid combinations before graph compilation and report sources.
- [x] Basic system configuration needs no user-authored low-level provider resources.

## Later Enhancement: Component Input 2.0

The following loops divide
[`component input` requirements](component-input-requirements.md) into mergeable
cycles. They enhance the current first version without changing the first-version
acceptance result above.

General principles:

- [x] Every loop preserves existing `type/default/sensitive` input compatibility.
- [x] Every loop has parser tests, merge/compiler tests, and at least one golden.
- [x] Complex input capabilities must pass `dbf validate` before plan/apply integration.
- [x] The new type system cannot expose sensitive data in HostSpec, plan, or state JSON.
- [x] `make test` must pass.

## Loop 10: Component Input Type System and Normalization

Goal: replace string-only component input types with structured schemas and
support `list(object({...}))`, `map(object({...}))`, `optional(...)`,
`nullable`, and `description`. Do not implement validation blocks or change
sensitive propagation in this loop.

Scope:

- `description`
- `nullable`
- `any`
- `list(T)`
- `set(T)`
- `map(T)`
- `object({ ... })`
- `tuple([ ... ])`
- `optional(T)`
- `optional(T, default)`
- Strict object schemas.
- Component-instance input normalization.

Deferred:

- Input `validation` blocks.
- Sensitive-mark propagation.
- `deprecated`.
- `ephemeral`.
- Terraform root-module variable sources.
- `array(T)` alias.
- Permissive object conversion and silent discarding of extra fields.

Code:

- [x] Add component input type parser based on the HCL AST.
- [x] Add machine-readable `ComponentInputTypeSpec`, not only a canonical string.
- [x] Add `TypeSpec`, `TypeExpr`, `Description`, and `Nullable` to parser.ComponentInput.
- [x] Add those fields to ir.ComponentInputSpec.
- [x] Parse `description` and `nullable`.
- [x] Preserve old `Type string` JSON/debug compatibility as canonical type expression.
- [x] Normalize defaults through type conversion and optional defaults.
- [x] Normalize instance values through type conversion and optional defaults.
- [x] Reject top-level null under `nullable = false`.
- [x] Reject objects missing required attributes.
- [x] Reject extra object attributes by default.
- [x] Reject tuples of the wrong length.
- [x] Deterministically order `set(T)` output for stable HostSpec/goldens.
- [x] Reject `array(T)` explicitly and suggest `list(T)`.
- [x] Inject normalized structured `input` during component expansion.
- [x] Add `jsonencode` to expression evaluation for structured inputs in file content.
- [x] Report error paths down to nested fields and list indexes.

Tests:

- [x] Parser tests cover primitive, `any`, `list(T)`, `set(T)`, `map(T)`, and tuple.
- [x] Parser tests cover `object({ name = string, enabled = optional(bool, true) })`.
- [x] Parser tests cover `list(object({ ... }))`.
- [x] Parser negatives cover `array(string)`, bare list/map, and optional outside object.
- [x] Merge tests cover default normalization.
- [x] Merge tests cover instance-input normalization.
- [x] Merge negative covers missing required object field.
- [x] Merge negative covers extra object field.
- [x] Merge negative covers nested type errors and a source path with an index.
- [x] HostSpec golden covers normalized `list(object(...))`.
- [x] ResourceGraph/plan golden covers structured-input generated file content.

Examples:

- [x] Add `examples/component-inputs.dbf.hcl`.
- [x] Example component uses `list(object(...))` to generate JSON configuration.
- [x] Example covers filled optional defaults.
- [x] Example enters runnable-example validation tests.

Documentation:

- [x] Add a short component input 2.0 example to README.
- [x] Mark this scope implemented in `component-input-requirements.md`.
- [x] Update supported component input types in `requirements.md`.
- [x] Update ComponentInputSpec in `ir-requirements.md`.

Acceptance:

- [x] `dbf validate -f examples/component-inputs.dbf.hcl` succeeds.
- [x] Offline JSON plan succeeds.
- [x] HostSpec normalized `list(object(...))` is deterministically ordered.
- [x] `make test` succeeds.

## Loop 11: Component Input Validation Blocks

Goal: support Terraform-like `validation` blocks that verify a component's
input contract before expansion.

Scope:

- Repeated `validation` blocks within input.
- `condition`.
- `error_message`.
- Read-only `input.<current_input_name>` validation context.
- Initial pure-function set.
- Validation source locations.

Deferred:

- Cross-input validation.
- Access to `target`.
- Access to `local`.
- `file`, `templatefile`, network, or external commands.
- Provider runtime checks.

Code:

- [x] Add `Validations []ComponentInputValidation` to parser.ComponentInput.
- [x] Add validation metadata to ir.ComponentInputSpec.
- [x] Allow nested validation blocks in input parsing.
- [x] Require validation blocks to have no labels.
- [x] Require `condition` and `error_message`.
- [x] Require a non-empty string error message.
- [x] Run validation after defaults, conversion, optional defaults, and nullable checks.
- [x] Require a boolean condition.
- [x] Restrict validation to the current input.
- [x] Forbid other inputs, `target`, `local`, and `path`.
- [x] Add pure functions `length`, `contains`, `startswith`, and `endswith`.
- [x] Add collection functions `alltrue`, `anytrue`, `distinct`, `sort`, `keys`, and `values`.
- [x] Add conversions `tonumber`, `tostring`, and `tobool`.
- [x] Add `regex` and `can`.
- [x] Block validate/plan/apply on validation failure and report validation source.

Tests:

- [x] Parser tests cover repeated validation blocks.
- [x] Parser negatives cover labels, missing condition/message, and empty message.
- [x] Merge test covers validation success.
- [x] Merge test covers validation failure.
- [x] Merge test covers non-boolean condition.
- [x] Merge negative covers access to another input.
- [x] Merge negatives cover `target`, `local`, and `path`.
- [x] Function tests cover `alltrue`, `anytrue`, `regex`, and `can`.
- [x] CLI smoke verifies validation-failure source path.

Examples:

- [x] Add port-range validation to `examples/component-inputs.dbf.hcl`.
- [x] Add an invalid validation-failure fixture.

Documentation:

- [x] Show one short validation in README.
- [x] Record the implemented function set in component input requirements.
- [x] Add a validation-failure common-error example.

Acceptance:

- [x] Valid input validation passes.
- [x] Invalid port reports `component.<name>.input["..."].validation[0]`.
- [x] Validation cannot access `target.system.codename`.
- [x] `make test` succeeds.

## Loop 12: Component Input Sensitive Propagation

Goal: promote `sensitive = true` from redacting only `input_values` into
expression-level propagation so complex and derived values cannot leak into
HostSpec, plan, state, or logs.

Scope:

- Sensitive cty marks.
- Sensitive metadata on `parser.Value`.
- Derived expression results remain sensitive.
- HostSpec JSON redaction.
- Plan JSON redaction or summaries.
- No plaintext in state JSON.
- Sensitive summaries for file/unit/secret payloads.

Deferred:

- `ephemeral`.
- Provider write-only parameters.
- Semantic secret detection for every third-party format.

Code:

- [x] Mark injected cty values for sensitive inputs.
- [x] Preserve sensitive marks in `ctyToValue`.
- [x] Add sensitive metadata to `parser.Value` or equivalent wrapper.
- [x] Aggregate sensitive marks after expression evaluation.
- [x] Have `parserValueToAny` emit `"<sensitive>"`.
- [x] Redact sensitive component `input_values` in HostSpec JSON.
- [x] Treat file content derived from sensitive values as sensitive automatically.
- [x] Treat systemd unit content derived from sensitive values as sensitive automatically.
- [x] Treat service environment derived from sensitive values as sensitive automatically.
- [x] Plan text/JSON emits only hash, length, and similar summaries.
- [x] State JSON stores no sensitive plaintext.
- [x] If a resource cannot carry a sensitive derived value safely, fail
  compilation. Current implementation connects only summary-capable fields such
  as file and systemd unit and exposes no unsafe field.

Tests:

- [x] Unit test covers sensitive input redaction.
- [x] Unit test covers redaction of a sensitive object's fields.
- [x] Unit test covers sensitive-derived file content.
- [x] Unit test covers sensitive-derived systemd unit content.
- [x] Plan golden contains no sensitive plaintext.
- [x] State unit test contains no sensitive plaintext.
- [x] Negative for sensitive-derived value in an unsupported non-sensitive
  field. No such field is currently exposed; enforce via positive file/unit
  coverage and JSON leak assertions.

Examples:

- [x] Add a sensitive `environment` input to `examples/component-inputs.dbf.hcl`.
- [x] Its plan golden contains no secret value.

Documentation:

- [x] Document sensitive input behavior in README.
- [x] Document that sensitive-derived content is not persisted in state.
- [x] Document sensitive summaries/redaction in plan format.
- [x] Mark sensitive propagation implemented in component input requirements.

Acceptance:

- [x] Offline JSON plan for the component-input example contains no sensitive plaintext.
- [x] State serialization contains no sensitive plaintext.
- [x] `make test` succeeds.

## Loop 13: Component Input API Evolution and User Completion

Goal: complete user-facing component input 2.0 with deprecation warnings,
interface inspection, docs, examples, and integration coverage.

Scope:

- `deprecated`.
- Warning aggregation/output.
- Component input inspection.
- README/docs completion.
- Libvirt integration.

Deferred:

- `ephemeral`.
- Terraform `.tfvars`/environment/CLI variables.
- `const`.
- Component registry/package manager.

Code:

- [x] Parse `deprecated`.
- [x] Add `Deprecated` to parser.ComponentInput and ir.ComponentInputSpec.
- [x] Warn when a caller explicitly passes a deprecated input.
- [x] Do not warn when only its default is used.
- [x] Aggregate CLI warnings consistently in validate, plan, and apply.
- [x] Warnings do not change exit status.
- [x] Add `dbf component inspect -f <file> <component>` with name, type,
  default, nullable, sensitive, deprecated, and description.
- [x] Redact sensitive defaults in inspect.
- [x] Return a clear error for unknown components.
- [x] Include new docs and examples in release builds.

Tests:

- [x] Parser test covers `deprecated`.
- [x] Merge test warns for an explicitly passed deprecated input.
- [x] Merge test does not warn when using its default.
- [x] CLI smoke covers validate warnings.
- [x] CLI smoke covers plan warnings.
- [x] CLI smoke covers `dbf component inspect`.
- [x] Inspect golden covers complex `list(object(...))` type presentation.
- [x] Libvirt integration verifies component input 2.0 writes a real file.

Examples:

- [x] Add `examples/component-inputs.dbf.hcl` to the README example list.
- [x] Add `test/integration/libvirt/cases/component-inputs`.
- [x] Integration verifies file content after optional defaults.
- [x] Integration verifies sensitive values are absent from remote state.

Documentation:

- [x] Add the complete component input 2.0 entry point to README.
- [x] Add object-field type errors and validation failures to common errors.
- [x] Update implementation status in component input requirements.
- [x] Mark completed items in this implementation plan.

Acceptance:

- [x] `dbf component inspect -f examples/component-inputs.dbf.hcl reverse_proxy`
  emits the complete input API.
- [x] Component-input example validates with expected warnings.
- [x] `make test` succeeds.
- [x] `component-inputs` Debian 13 amd64 VM flow succeeds.
  - 2026-06-23: on disposable VM `dbf-test-component-inputs`, ran `validate`,
    `apply --auto-approve`, `check`, and `1.check.sh`/`2.check.sh`. A CI failure
    caused by state-address matching without JSON-escaped quotes was corrected.
