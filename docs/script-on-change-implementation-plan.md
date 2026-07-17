# `script` / `on_change` Implementation Plan

<p align="right"><strong>English</strong> | <a href="script-on-change-implementation-plan.zh.md">简体中文</a></p>

This document splits the
[`script` / `on_change` requirements](script-on-change-requirements.md) into
executable development loops. Every loop must form a mergeable closed cycle:

- The code path runs.
- Unit tests and necessary goldens cover the loop's semantics.
- At least one fixture or example verifies the capability.
- Documentation is updated.
- `make test` passes.

Status convention:

- `[x]` Complete
- `[ ]` Incomplete

## Current Baseline

- [x] Host is the final execution unit; plan/apply/check all execute by host.
- [x] Profiles can be imported by profiles or hosts and merge under existing rules.
- [x] Components support typed inputs and expand after being mounted on a host.
- [x] ResourceGraph supports operations, `TriggeredBy`, and dependency scheduling.
- [x] Plan text/JSON/HTML presents operations and `triggered_by`.
- [x] Providers can run operation commands.
- [x] Component-local `script` blocks have DSL parsing and HostSpec compilation.
- [x] `files.file` supports DSL parsing and HostSpec compilation for `on_change`.
- [x] The engine supports once/each trigger context for script `mode`.
- [x] Component `script.outputs` supports hashing generated files and retriggering on drift.

## Loop 1: DSL Parsing and IR Skeleton

Goal: allow a component to declare `script` and a `files.file` to reference a
script in that component. This loop only completes validation and compilation;
it does not generate operations.

Scope:

- Add `script "<name>" { ... }` inside `component`.
- Add `on_change = script.<name>` to `files.file`.
- Support `mode`, `interpreter`, `outputs`, `run`, `content`, and `commands` on
  scripts.
- Make `run`, `content`, and `commands` mutually exclusive and require exactly one.
- Allow only `"once"` and `"each"` for `mode`, defaulting to `"once"`.
- Default `interpreter` to `["/bin/sh", "-eu"]` and require a non-empty string list.

Deferred:

- Do not support `script` in `host` or `profile`.
- Do not generate ResourceGraph operations.
- Do not execute scripts.
- Do not pass host script references as component inputs.

Code:

- [x] Parser supports component-local and program-root `script` blocks.
- [x] Parser handles HCL traversal in `files.file.on_change`.
- [x] IR adds `ComponentScriptSpec` to `ComponentInstanceSpec`.
- [x] IR adds `OnChange` to `ManagedFile`, storing the script name and source.
- [x] merge/buildComponentSpec compiles component scripts and file on-change references.
- [x] Validation resolves `on_change` to component-local or host-scoped root declaration identity.
- [x] HostSpec JSON/goldens emit stable script metadata without leaking sensitive content.

Tests:

- [x] Parser unit tests cover component `script` and `on_change = script.reload`.
- [x] Parser/merge negative cases cover scripts in host/profile, unknown scripts, and invalid traversals.
- [x] Merge unit tests cover mutually exclusive bodies, invalid mode, and empty interpreter.
- [x] A fixture covers HostSpec for a component file referencing a script.

Documentation:

- [x] Sync implemented syntax from the requirements into the DSL Reference.

Acceptance:

- [x] `dbf validate` accepts configuration containing component script/on_change.
- [x] An unknown script reference fails at `files.file.on_change`.
- [x] `make test` succeeds.

## Loop 2: ResourceGraph Operation Generation

Goal: generate ResourceGraph operations from component scripts and show in the
plan which files trigger them.

Scope:

- Generate an operation for every referenced component script.
- Scope operation addresses to the component instance:

```text
host.<host>.components.<instance>.script["<name>"]
```

- Put file nodes referencing the script in operation `TriggeredBy`.
- Put at least the `TriggeredBy` file nodes in operation `DependsOn`.
- Use a short `CommandPreview`, such as `script reload (once)`.

Deferred:

- Do not execute real scripts.
- Do not split `each` execution; present it once using the existing operation model.

Code:

- [x] Graph compilation collects script triggers while compiling component files.
- [x] Graph generates script operations with source, summary, mode, and preview.
- [x] Graph validation confirms operation trigger addresses exist.
- [x] Plan text/JSON/HTML presents script operations and `triggered_by`.
- [x] ResourceGraph JSON does not expand full script bodies, limiting plan noise and disclosure surface.

Tests:

- [x] Graph unit tests cover component script operation addresses, dependencies, and triggers.
- [x] Plan goldens cover script operations in text and JSON.
- [x] A negative case confirms that an unreferenced script creates no operation.

Documentation:

- [x] Update plan-presentation examples in the requirements to match actual output.
- [x] Add a minimal runnable example to the DSL Reference, using `dbf plan --offline` to verify addresses.

Acceptance:

- [x] `dbf plan --offline` displays the script operation.
- [x] The operation displays only a short preview, not a full script body.
- [x] `make test` succeeds.

## Loop 3: Execution Payload and NativeProvider Execution (Implemented)

Goal: execute script operations during apply while keeping plan presentation concise.

Scope:

- Add an internal execution payload to ResourceGraph operations containing the
  interpreter, script content, and mode.
- Use the payload in provider execution instead of putting the full script in
  `CommandPreview`.
- Wrap `run` as executable script text.
- Pass `content` unchanged to the interpreter.
- Safely assemble `commands` into script text.
- Fail apply when the script exits nonzero.

Deferred:

- Do not split `each`; continue running once.
- Do not support JSON stdin.

Code:

- [x] graph.Operation has a script payload not expanded in ordinary plan output.
- [x] Operation JSON redaction/omission prevents script text from polluting plan JSON.
- [x] NativeProvider.RunOperation supports script payloads.
- [x] SSH runner passes script text to custom interpreters using `RunInput` or an equivalent mechanism.
- [x] Commands mode uses existing shell-quoting rules to build safe script text.

Tests:

- [x] NativeProvider unit tests cover run/content/commands bodies.
- [x] Failure tests cover a nonzero exit.
- [x] Redaction tests ensure script payloads never enter operation previews or plans.
- [x] Engine unit tests confirm a triggered script operation invokes the provider.

Documentation:

- [x] State explicitly that execution payloads are not part of the plan public interface.

Acceptance:

- [x] Apply executes the script after file changes.
- [x] Plan text/JSON/HTML omits full script content.
- [x] `make test` succeeds.

## Loop 4: once/each Trigger Semantics and Environment Variables (Implemented)

Goal: implement final `mode = "once"` and `mode = "each"` semantics and inject
trigger context into scripts.

Scope:

- `once`: if several files trigger one script during an apply, run it once.
- `each`: run the script once for every file that actually changed.
- Inject environment variables:
  - `DBF_SCRIPT_NAME`
  - `DBF_COMPONENT_NAME`
  - `DBF_TRIGGER_ADDRESS`
  - `DBF_TRIGGER_PATH`
  - `DBF_TRIGGER_ADDRESSES`
  - `DBF_TRIGGER_PATHS`
- Online plan and apply choose operation steps from the actual changed set.

Code:

- [x] engine.OperationStep carries actual trigger addresses and paths.
- [x] operationSteps splits `each` into one step per actual trigger.
- [x] Execution-item addresses remain unique across `each` steps to prevent result overwrites.
- [x] NativeProvider injects environment variables before executing a script.
- [x] Multi-trigger lists in `once` use newline separators.
- [x] The scheduler keeps scripts after their triggering files.

Tests:

- [x] Engine unit tests cover one once script triggered by two files running once.
- [x] Engine unit tests cover one each script triggered by two files running twice.
- [x] NativeProvider unit tests cover environment-variable contents.
- [x] Plan JSON/text covers stable addresses or presentation for split each steps.

Documentation:

- [x] DSL Reference documents once/each semantics and environment variables.
- [x] The user manual or an example documents an appropriate use for each.

Acceptance:

- [x] once/each matches the requirements.
- [x] `make test` succeeds.

## Loop 5: Examples, Documentation, and Integration Acceptance (Implemented)

Goal: finish the capability as a user-readable, verifiable, maintainable delivery.

Scope:

- Add a small component example in which a configuration-file change reloads a service.
- Add a libvirt integration case proving first apply, second no-op, and reload
  after a configuration change on real Debian 13.
- Update the support matrix, DSL Reference, and README or user-manual entry point.
- State clearly that `script` / `on_change` is implemented rather than a design goal.

Code:

- [x] Add `examples/component-script-on-change.dbf.hcl`.
- [x] Add `test/integration/libvirt/cases/script-on-change/`.
- [x] Update inspect output if needed so relevant inputs appear in a component's public API.

Tests:

- [x] `dbf validate -f examples/component-script-on-change.dbf.hcl` succeeds.
- [x] `dbf plan -f examples/component-script-on-change.dbf.hcl --offline` emits the expected operation.
- [x] `make test` succeeds.
- [x] `make test-integration-layout` succeeds.
- [x] The libvirt case passes on Debian 13 amd64.

Documentation:

- [x] Move DSL Reference wording from design goal to implemented syntax.
- [x] Optionally add a short README example or link to the example.
- [x] Mark implemented portions in `docs/script-on-change-requirements.md`, retaining future questions.
- [x] Move the link in `docs/README.md` from design requirements to an appropriate everyday-reference or DSL section.

Acceptance:

- [x] Users can encapsulate reload/restart after file changes within a component.
- [x] Hosts only mount the component and pass inputs; they need not know internal script details.
- [x] Plan/apply/check output follows the existing operation style.
- [x] `make test` and relevant integration checks pass.

## Future Extensions

- Coalesce similar operations across components, such as one nginx reload
  triggered by several components.
- Provide JSON stdin trigger context instead of newline-separated environment
  variable lists.
- Add finer failure policies such as warn-only or retry; exclude them from the
  first version.
- Enrich script inspect output for component-author debugging.
