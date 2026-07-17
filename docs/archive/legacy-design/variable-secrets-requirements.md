# DebianForm Variables and Sensitive Data Requirements

<p align="right"><strong>English</strong> | <a href="variable-secrets-requirements.zh.md">简体中文</a></p>

This document defines top-level `variable`, sensitive-value propagation,
runtime secret injection, and their relationship to existing `secrets.file`.
It separates where a secret comes from from the resource written to the target,
instead of binding local secret-file paths as the only input method.

Terraform is the primary reference, not a compatibility target. DebianForm
adapts its variable blocks, type constraints, source precedence, and separation
of `sensitive`, `ephemeral`, and write-only provider arguments to HostSpec,
ResourceGraph, plan/state, and the SSH runner.

`variable` is a general program input, not a secret-only interface. It
parameterizes environmental differences, deployment size, feature flags,
versions, target paths, and runtime credentials across hosts, profiles, and
components. Secrets are one high-risk use case.

## Background

DebianForm already has:

- `secrets.file`: a sensitive target file sourced locally, defaulting to mode
  `0600`, with no plaintext in plan/state.
- `sensitive = true`: a mark on component inputs or selected resource content,
  redacted in plan/state and propagated to derived file/unit content.

This addresses some plaintext disclosure, but semantics remain incomplete:

- Sources are tied to local paths and cannot come directly from stdin, CI
  secrets, environment, Vault, SOPS, or another runtime source.
- `secrets.file` combines sensitive input source with target file resource.
- `sensitive` primarily controls display/state redaction; it does not mean
  absence from HostSpec/graph/plan/state.
- Apply may still disclose through provider payloads, runner commands, debug
  logs, or temporary files.
- State SHA-256/byte summaries help drift/no-op but may fingerprint low-entropy
  secrets for offline guessing.

The mature separation is:

```text
variable defines how a value enters DebianForm
sensitive defines display and propagation
ephemeral defines whether persistence is allowed
write-only defines provider-apply-only values
files.file defines the target file resource
```

## Goals

- Support top-level `variable` as external input to host/profile/component configuration.
- Use one general mechanism for ordinary configuration and secrets.
- Support `type`, `default`, `description`, `validation`, `sensitive`,
  `nullable`, `ephemeral`, `const`, and `deprecated` near Terraform semantics.
- Share structured types with component inputs, including nested objects,
  `list(object(...))`, map, set, tuple, and optional.
- Accept CLI `-var`, `-var-file`, auto files, environment, prompt/stdin, and
  local file content.
- Define stable, explainable source precedence.
- Inject secrets at runtime without requiring long-lived files beside configuration.
- Distinguish `sensitive`, `ephemeral`, and write-only semantics explicitly.
- Propagate sensitivity into `files.file.content`, `systemd.unit.content`, and similar fields.
- Keep ephemeral values out of HostSpec, ResourceGraph, plan, state, golden
  debug JSON, and ordinary logs.
- Pass write-only values only through provider apply, never desired/state/diff.
- Let `files.file` express all target-file behavior of `secrets.file`.
- Retain `secrets.file` as transitional sugar, deprecate later, and remove only after migration.
- Report sensitive-data errors against user source paths.

## Non-Goals

- Do not implement Terraform modules.
- Do not promise complete `.tfvars` compatibility, naming, or CLI behavior.
  `.dbfvars` may differ while remaining explicit.
- Do not implement HCP Terraform workspaces, variable sets, or remote runs.
- Do not promise every secret backend in the first phase.
- Do not solve target-side at-rest secrets; required files still land on disk or tmpfs.
- Do not equate `sensitive = true` with non-persistence; it controls redaction and propagation.
- Do not allow ephemeral values to affect addresses, collection keys, sorting,
  dependency structure, or deletion policy.

## Differences from Terraform Variables

Terraform variables are module parameters. Root modules receive CLI/env/tfvars/workspace
values and parent modules pass child-module inputs. DebianForm has no module system,
so its variables are program-level inputs referenced as `var.<name>` throughout one
program's hosts, profiles, and components.

Target alignment:

| Capability | Terraform | DebianForm target |
| --- | --- | --- |
| Reference | `var.name` | Use `var.name`. |
| Types | Primitive, collection, structural, `any` | Share the component input type system. |
| Defaults | Optional; required without one | Same; defaults cannot depend on runtime facts or other variables. |
| Validation | `validation` block | Same, with user source paths. |
| Redaction | `sensitive` hides CLI/UI but may enter state | Same distinction plus taint propagation into resource content. |
| Non-persistence | `ephemeral` omitted from plan/state with restricted references | Same, plus omission from HostSpec/ResourceGraph/debug JSON. |
| Write-only | Provider resource arguments | Provider apply payload separate from state/diff. |
| Early evaluation | `const` | Reserve for future imports/backend/plugin/source parsing. |
| Deprecation | `deprecated` | Same for long-term interface maintenance. |
| Sources | CLI, files, auto files, env, workspace | CLI, dbfvars, auto dbfvars, env, prompt/stdin; no workspace. |
| File input | No general `@path` CLI syntax | Add `@path`/`@-` as deployment-oriented extensions. |

Key differences:

- Terraform variables are module APIs; DebianForm variables are program APIs.
  Components retain `input`, sharing type, validation, sensitive, and ephemeral machinery.
- Terraform `sensitive` means redaction, not ephemerality; DebianForm preserves
  that distinction.
- DebianForm has HostSpec and ResourceGraph artifacts, so ephemeral exclusions
  are broader than plan/state.
- The SSH runner makes write-only safety cover command previews, stdout/stderr,
  temporary files, and provider payloads.

## Capability Tiers

### Core

- `var.<name>` references.
- `type`, `default`, `description`, and `nullable`.
- Primitive, collection, and structural constraints.
- `-var`, `-var-file`, and environment assignment.
- Required-variable validation.
- `validation` blocks.
- Errors with source paths.

### Secure

- `sensitive` redaction and expression propagation.
- `ephemeral` propagation and persistence prohibition.
- Write-only provider payloads.
- Redacted runner channel.
- `@path`, `@-`, and prompt/stdin input.
- `content_version` or another non-sensitive version trigger.

### Ergonomic

- `.dbfvars` and `.auto.dbfvars`.
- JSON variable files.
- Environment prefix such as `DBF_VAR_name`.
- JSON complex values in CLI/environment.
- `deprecated` warnings.
- `const` early-evaluation variables.
- `dbf variable inspect` or equivalent interface listing.

## Terminology

### sensitive

- CLI, plan text/JSON, HostSpec debug, and error context contain no plaintext.
- Derived references remain sensitive by default.
- Sensitive values may enter state unless also ephemeral or write-only.
- State may retain controlled summaries of non-ephemeral sensitive content for drift/no-op.

### ephemeral

- Exists only during the current process.
- Never enters HostSpec, graph, plan, state, cache, golden fixtures, or ordinary logs.
- Cannot affect addresses, map/set keys, count-like structure, dependencies, or deletion policy.
- May enter only explicitly runtime/write-only resource arguments.

### write-only

- Available to provider apply.
- Never returned into desired, observed, state, or plan.
- Planning cannot require it to generate a non-sensitive diff.
- Updates use a non-sensitive trigger such as `content_version`, `secret_version`,
  or a controlled digest.

## User Syntax

### Top-Level Variable

```hcl
variable "wg_private_key" {
  type        = string
  description = "WireGuard private key for this host."
  sensitive   = true
  ephemeral   = true
  nullable    = false
}
```

Rules:

- Top-level block with exactly one label, unique per program.
- `type` required.
- `description` optional but recommended.
- `default` optional; external assignment required without one.
- `nullable` defaults true.
- `sensitive`, `ephemeral`, and `const` default false.
- `deprecated` is an optional non-empty string.
- `validation` may repeat.

Complete form:

```hcl
variable "name" {
  type        = <TYPE>
  default     = <DEFAULT_VALUE>
  description = "<DESCRIPTION>"
  sensitive   = <true|false>
  nullable    = <true|false>
  ephemeral   = <true|false>
  const       = <true|false>
  deprecated  = "<MESSAGE>"

  validation {
    condition     = <EXPRESSION>
    error_message = "<MESSAGE>"
  }
}
```

### Ordinary Configuration Variables

```hcl
variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "prod"
  nullable    = false

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "listeners" {
  type = list(object({
    name = string
    port = number
    tls  = optional(bool, false)
  }))

  default  = []
  nullable = false
}
```

These are not secret-special cases. They use the same type checking, defaults,
validation, and source-path error reporting as component inputs.

### External Assignment

First phase:

```bash
dbf plan  -f site.dbf.hcl -var wg_private_key=@/run/secrets/wg.key
dbf apply -f site.dbf.hcl -var wg_private_key=@/run/secrets/wg.key
dbf apply -f site.dbf.hcl -var-file prod.dbfvars
```

Semantics:

- `name=value`: literal value.
- `name=@path`: read a local file as the value; the path is not resource
  semantics and never enters HostSpec/graph/state.
- `name=@-`: read stdin.
- `-var-file`: HCL key/value, with JSON later.
- Sensitive CLI values never appear in errors, debug logs, or command previews.

Later sources may include:

```bash
dbf apply -var-file secrets.dbfvars
dbf apply -var wg_private_key=env:WG_PRIVATE_KEY
dbf apply -var wg_private_key=cmd:pass-show-wireguard
```

All sources obey identical sensitive/ephemeral semantics.

Recommended precedence, high to low:

1. CLI `-var`, later occurrences win.
2. CLI `-var-file`, later occurrences win.
3. `*.auto.dbfvars[.json]` in lexical order.
4. `debianform.dbfvars[.json]`.
5. `DBF_VAR_<name>` environment.
6. Variable `default`.

Unknown variables:

- Unknown CLI `-var` is an error.
- Unknown variable-file keys should warn or error; prefer error initially.
- Unknown environment variables may be ignored to avoid CI pollution.

### Writing a Sensitive File from a Variable

```hcl
variable "wg_private_key" {
  type      = string
  sensitive = true
  ephemeral = true
  nullable  = false
}

host "wg-a" {
  files {
    file "/etc/wireguard/private.key" {
      content = var.wg_private_key
      owner   = "root"
      group   = "systemd-network"
      mode    = "0640"
    }
  }
}
```

- Sensitive content marks the file sensitive automatically.
- Ephemeral content never enters desired/state/plan.
- The target file is still written; do not claim the secret is never at rest.

### Explicit Version Trigger

Without persistent plaintext or summaries, ephemeral/write-only content cannot
be compared from state. Supply a non-sensitive version:

```hcl
variable "app_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "app_token_version" {
  type     = string
  nullable = false
}

files {
  file "/etc/app/token" {
    content         = var.app_token
    content_version = var.app_token_version
    mode            = "0600"
  }
}
```

- `content_version` is non-sensitive and may enter desired/state/diff.
- A version change triggers update.
- Without a version, plan may conservatively require update/replace, but cannot
  promise a subsequent no-op.

## Relationship to `secrets.file`

Current syntax:

```hcl
secrets {
  file "/etc/app/token" {
    source = "secrets/app-token"
    owner  = "root"
    group  = "root"
    mode   = "0600"
  }
}
```

Target equivalence:

```hcl
variable "app_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

files {
  file "/etc/app/token" {
    content = var.app_token
    owner   = "root"
    group   = "root"
    mode    = "0600"
  }
}
```

Migration:

- Retain `secrets.file` unchanged initially.
- Compile it into sensitive file sugar using the common file provider path.
- The current compatibility layer retains
  `host.<host>.secrets.file["<path>"]` while sharing file-like write, safety,
  and state-redaction logic. Prefer variable + file for new configuration.
- Later add a deprecation warning.
- Remove only after files support sensitive/ephemeral/write-only fully.

Short-term value: convenient local-file deployment, no immediate example/test
breakage, and compatibility while new configuration moves to variables.

Migration example:

```hcl
variable "app_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "app_token_version" {
  type = string
}

files {
  file "/etc/app/token" {
    content         = var.app_token
    content_version = var.app_token_version
    owner           = "root"
    group           = "root"
    mode            = "0600"
  }
}
```

Original source maps to:

```bash
dbf plan -f app.dbf.hcl -var app_token=@secrets/app-token -var app_token_version=2026-06-23
```

## Compilation and IR Requirements

### Parser

- Parse top-level `variable` with all fields and validations.
- Permit `var.<name>` in host/profile/component expressions.
- Fail unassigned required variables.
- Convert and validate external values against the type.

### Value

Values carry `Sensitive`, `Ephemeral`, and a `WriteOnly` or provider-only mark.
Expression evaluation propagates sensitive and ephemeral; write-only values may
enter only fields that explicitly support them.

### HostSpec

- Contains no ephemeral plaintext.
- May retain normalized non-sensitive variables.
- Redacts non-ephemeral sensitive values in debug JSON.
- Expresses runtime-only resource fields without fake empty or
  `"<sensitive>"` values.

### ResourceGraph

- `Node.Desired` stores only persistable desired data.
- `Node.ProviderPayload` or a runtime payload may carry apply values but is
  omitted/redacted in JSON.
- Distinguish plan-visible desired, provider apply payload, and persisted state desired.

## Plan and State Requirements

### Plan

- Sensitive diffs contain no plaintext.
- Ephemeral values never enter plan JSON.
- Write-only fields show `<write-only>` or only version changes.
- Without a persistent version, say no-op cannot be determined reliably rather
  than fabricating a precise diff.

### State

- Store no ephemeral or write-only plaintext.
- Sensitive non-ephemeral content may retain documented drift/no-op summaries.
- Prefer explicit `content_version` for low-entropy secrets instead of
  long-lived guessable SHA-256.
- Omit `source_path` for sensitive/ephemeral sources by default.

## Provider and Runner Requirements

- Provider APIs receive write-only payloads explicitly.
- They never return write-only values through ProviderPlan, Observed, state, or debug logs.
- SSH runner supports redacted commands/payloads: no secret in preview, logs, or errors.
- Prefer stdin, SFTP/SCP, or controlled temporary files over shell command interpolation.
- Temporary files use strict permissions and best-effort cleanup on failure.

## Validation Rules

- Ephemeral values cannot enter addresses, keys, labels, dependencies,
  lifecycle, owner/group/path, or structural fields.
- Write-only values cannot enter ordinary desired fields compared during plan.
- `files.file.content` accepts sensitive/ephemeral values.
- File path, owner, group, mode, and ensure reject ephemeral values.
- Files and secrets support explicit `path`; otherwise block label is target
  path, allowing static labels plus dynamic paths in components.
- Sensitive values taint non-sensitive output fields automatically.
- Explicit `sensitive = false` cannot override propagated sensitivity.

## Implementation Loops

These mergeable loops follow `implementation-plan.md`. Each is independently
committable/testable and preserves existing examples and integration tests.

Status:

- `[x]` Complete
- `[ ]` Incomplete

General rules, not task counters:

- Every loop closes code, tests, fixtures/examples, docs, and acceptance.
- Advance parser, CLI, HostSpec, graph, plan/state, provider, and runner
  boundaries separately unless the loop explicitly spans them.
- Update only directly affected goldens.
- Every new syntax has a test, fixture, or CLI smoke.
- Security-boundary loops add negative plaintext-leak assertions.
- `make test` passes; record unavailable integration coverage.

### Current Baseline

- [x] `secrets.file` exists and plan/state exclude secret plaintext.
- [x] Component inputs propagate sensitive marks into derived file/unit content.
- [x] Plan/state has sensitive summaries or redaction.
- [x] Top-level `variable` is implemented.
- [x] External sources, ephemeral propagation, write-only payloads, and the
  redacted runner channel are complete through Loops 3-10.

### Loop 0: Baseline and Regression Guards

Goal: freeze current secret/sensitive behavior before variable work.

Scope: `secrets.file`, sensitive files, sensitive-derived component files/units/
service environments, and leak assertions across HostSpec, plans, state, logs.

Deferred: top-level variables, CLI sources, ephemeral, write-only payloads, and runner changes.

Code:

- [x] Consolidate fixtures into reusable leak-assertion helpers.
- [x] Define currently permitted non-secret metadata such as `source_path` in
  non-state debug output.
- [x] Record TODOs for metadata disclosure not closed immediately and assign later loops.

Tests:

- [x] `secrets.file` plaintext is absent from state and plans.
- [x] Sensitive file plaintext is absent from HostSpec, plans, and state.
- [x] Sensitive-derived component file/unit content is absent from HostSpec, plans, and state.
- [x] Ordinary non-sensitive files retain readable text diffs.

Examples/docs:

- [x] Preserve existing example syntax.
- [x] Record baseline/TODOs without new user syntax.

Acceptance:

- [x] User syntax and plan/state formats do not change.
- [x] `make test` passes.

Implementation record:

- Added `internal/core/testassert.NoSecretLeak` across HostSpec, graph desired,
  plan text/JSON/HTML, and state JSON.
- Reused foundation/files/component fixtures and added
  `sensitive-service-environment.dbf.hcl` for structured service environment propagation.
- Allowed summaries such as `content_sha256`/`content_bytes`; ordinary files keep text diffs.
- TODO: ProviderPayload may carry sensitive apply data; close under write-only payload loops.
- TODO: native apply scripts may contain base64 content; close under redacted runner channel.

### Loop 1: Parse Top-Level Variable Declarations

Goal: expose declarations to parser/IR without external assignment or `var` references.

Scope: variable label, type, default, description, nullable, sensitive,
ephemeral, const, deprecated, and validation metadata.

Deferred: evaluation, external sources, validation execution, const behavior,
and sensitive/ephemeral propagation.

Code:

- [x] Add `Program.Variables map[string]VariableSpec` or equivalent.
- [x] Parse exactly one variable label.
- [x] Require unique labels per program.
- [x] Reuse component input type, normalization, and validation parsers.
- [x] Normalize defaults by type but store only metadata, not evaluator context.
- [x] Show non-sensitive metadata in debug output and redact sensitive defaults.

Tests:

- [x] Parser covers primitive, list(object), map, set, tuple, and optional attributes.
- [x] Negatives cover duplicates, label count, unknown fields, and invalid types.
- [x] Default normalization covers optional defaults, nullable, and mismatch.
- [x] Sensitive defaults do not appear in JSON/debug output.

Examples/docs:

- [x] Add a declaration-only fixture.
- [x] State that declarations cannot yet be referenced.

Acceptance:

- [x] `dbf validate` accepts an unreferenced declaration-only configuration.
- [x] Existing component input behavior/goldens do not change.
- [x] `make test` passes.

Implementation record: parser.Config and ir.Program gained complete declaration
metadata; defaults reuse structured normalization, sensitive defaults render as
`<sensitive>`, and `variable-declarations.dbf.hcl` is the acceptance fixture.

### Loop 2: Variable Evaluation and `var.<name>`

Goal: let hosts/profiles/components reference defaults as minimal program inputs,
without CLI overrides.

Scope: `var` namespace, required checks, default evaluation/source paths, and
stable ordinary output.

Deferred: external sources, validation, runtime sources, sensitive taint, and ephemeral limits.

Code:

- [x] Add read-only `var` namespace.
- [x] Resolve final values from defaults; error when required and absent.
- [x] Permit only declared variables.
- [x] Forbid defaults from reading `var`, `input`, `target`, `path`, or runtime facts.
- [x] Use one normalized variable value across host/profile/component expansion.
- [x] Point errors to declarations or references.

Tests:

- [x] `files.file.content = var.message` produces expected HostSpec.
- [x] `system.hostname = var.hostname` works.
- [x] Component bodies read variables.
- [x] Unknown references fail.
- [x] Missing required values fail.
- [x] Defaults depending on runtime facts/input/other variables fail.

Examples/docs:

- [x] Add default-only ordinary file/hostname fixture.
- [x] Explain program API versus component input.

Acceptance:

- [x] Default-only ordinary variables expand in hosts/profiles/components.
- [x] Validate and offline plan succeed on the fixture.
- [x] `make test` passes.

Implementation record: parsing now resolves locals and declarations before
top-level blocks and injects normalized defaults into a read-only namespace.
Defaults may read `local.*` and constants but not other dynamic namespaces.
`variable-defaults.dbf.hcl` covers all three consumer scopes. Validation and
sensitive propagation remain later loops.

### Loop 3: CLI `-var` Literals

Goal: override ordinary configuration through CLI literals, without files,
stdin, or secret backends.

Scope: repeated `-var` for validate/plan/apply and type conversion.

Deferred: variable files, auto files, environment, runtime sources, prompt, and backends.

Code:

- [x] Support repeated `-var name=value`.
- [x] Make `-var` override defaults.
- [x] Later repeated values win.
- [x] Reject undeclared CLI variables.
- [x] Parse string/number/bool by declared type; complex values may require JSON.
- [x] Keep sensitive CLI values out of errors/debug logs.

Tests:

- [x] String, number, bool, list, and object assignments succeed.
- [x] CLI overrides defaults.
- [x] Type mismatch reports the variable without the value.
- [x] Later duplicate wins.
- [x] Undeclared variable fails.
- [x] Sensitive value is absent from errors/snapshots.

Examples/docs:

- [x] Add CLI smoke for `-var env=prod`.
- [x] Document complex-value encoding.

Acceptance:

- [x] Validate/plan/apply accept ordinary `-var env=prod`.
- [x] `make test` passes.

Implementation record: all three commands accept repeatable values; strings
remain raw, primitive values use HCL literals, and list/map/object/tuple/set/any
may use JSON such as `ports=[80,443]`. Added `variable-cli.dbf.hcl` and smokes for
overrides, errors, and redaction.

### Loop 4: Variable Files, Auto Files, and Environment

Goal: make variables usable beyond CLI strings.

Scope: repeated `-var-file`, HCL/JSON dbfvars, auto/default files,
`DBF_VAR_<name>`, and precedence.

Deferred: runtime `@` sources, prompts, command sources, workspaces, and secret backends.

Code:

- [x] Support repeated `-var-file`.
- [x] Support top-level HCL attributes.
- [x] Support top-level JSON objects.
- [x] Auto-load `*.auto.dbfvars[.json]` and `debianform.dbfvars[.json]`.
- [x] Support `DBF_VAR_<name>`.
- [x] Fix precedence: CLI > explicit files > auto/default files > env > default.
- [x] Reject unknown CLI/file values and ignore unknown environment values.

Tests:

- [x] Parse string/list/object from files.
- [x] Later explicit file wins.
- [x] Auto files load lexically.
- [x] Environment is below files/CLI.
- [x] Unknown file key fails.
- [x] Unknown environment value is ignored.
- [x] Sensitive values are absent from errors, HostSpec, plans, and state.

Examples/docs:

- [x] Add production variable-file fixture.
- [x] Document precedence and unknown-variable handling.

Acceptance:

- [x] Ordinary deployment parameters can come entirely from files/environment.
- [x] Validate/plan/apply behave consistently across sources.
- [x] `make test` passes.

Implementation record: validate/plan/apply/check support repeated files. Auto
loading is default HCL, default JSON, then lexical auto HCL/JSON from the config
directory. Low-to-high order is env, defaults, auto, explicit files, CLI.
Unknown CLI/files fail; unknown env is ignored. Sensitive propagation completes
in Loop 6.

### Loop 5: Validation, Nullable, Deprecated, and Inspect

Goal: expose a stable documented interface rather than a raw map.

Scope: validation, nullable, deprecated warning aggregation, and
`dbf variable inspect`.

Deferred: ephemeral, write-only, const behavior, runtime secret sources, and backends.

Code:

- [x] Run validation after final assignment and conversion.
- [x] Reuse component input pure functions and source errors.
- [x] Enforce `nullable = false`.
- [x] Warn for explicit deprecated assignment, not defaults.
- [x] Aggregate warnings consistently across validate/plan/apply.
- [x] Add variable inspect with name/type/default/nullable/sensitive/ephemeral/description/deprecated.
- [x] Redact sensitive defaults in inspect.

Tests:

- [x] Validation success and failure.
- [x] Non-boolean condition fails.
- [x] Validation cannot read target/input/path or forbidden namespaces.
- [x] Nullable false rejects null.
- [x] Explicit deprecation warns; default does not.
- [x] Inspect golden covers complex type, redaction, and message.

Examples/docs:

- [x] Add environment validation.
- [x] Add inspect-output example.

Acceptance:

- [x] Variables can be documented and inspected as public configuration APIs.
- [x] Validation failure reports its variable path.
- [x] `make test` passes.

Implementation record: validation uses the component pure functions and only
the current `var.<name>`. Nullable applies to every source. Deprecation depends
on external explicit assignment. Inspect accepts CLI/file values and emits:

```json
{
  "variables": [
    {
      "name": "environment",
      "type": "string",
      "default": "prod",
      "nullable": false,
      "sensitive": false,
      "ephemeral": false,
      "deprecated": "Use deployment_environment instead.",
      "description": "Deployment environment."
    },
    {
      "name": "token",
      "type": "string",
      "default": "<sensitive>",
      "nullable": true,
      "sensitive": true,
      "ephemeral": false
    }
  ]
}
```

### Loop 6: Sensitive Propagation into Resource Content

Goal: propagate sensitive taint from variable sources through expressions and
resource payloads. Sensitive still means redaction, not non-persistence.

Scope: variable marks, expression aggregation, file content, systemd unit
content, structured service environment, and CLI/inspect/error/debug redaction.

Deferred: ephemeral, write-only payloads, runtime sources, runner changes, and
low-entropy digest policy.

Code:

- [x] Inject variables with a Sensitive mark.
- [x] Aggregate through `jsonencode`, templates, maps, lists, and objects.
- [x] Mark files sensitive automatically.
- [x] Mark raw/structured systemd content/environment sensitive automatically.
- [x] Explicit `sensitive = false` cannot override propagation.
- [x] CLI/inspect/errors contain no plaintext.

Tests:

- [x] Sensitive variable file content is absent from plan/state/HostSpec.
- [x] `jsonencode`, template, map/list/object derivations remain sensitive.
- [x] Explicit false cannot override it.
- [x] Ordinary variables retain readable text diffs.
- [x] Errors and warnings contain no plaintext.

Examples/docs:

- [x] Add sensitive-derived file fixture.
- [x] Explain that sensitive is not ephemeral and target data is still written.

Acceptance:

- [x] Sensitive values are safe for redaction and summary presentation.
- [x] This loop adds no non-persistence semantics.
- [x] `make test` passes.

Implementation record: normalized values from every source carry cty marks;
merge recognizes them across direct content, JSON, templates, raw units, and
structured environments. Added `sensitive-variable-files.dbf.hcl`. Target files
still land on disk/tmpfs.

### Loop 7: `@path`, `@-`, and Runtime Input

Goal: inject secrets at runtime without long-lived files beside configuration.

Scope: `-var name=@path`, `@-`, optional `env:NAME`, stdin/prompt paths, and
sensitive source-path redaction.

Deferred: command sources, secret backends, ephemeral, write-only payloads, and runner changes.

Code:

- [x] Read `@path` file content as the value.
- [x] Read `@-` from stdin.
- [x] Optionally read `env:ENV_NAME`.
- [x] Keep sensitive paths/values out of every serialized/debug/error boundary.
- [x] File/permission/stdin errors reveal no partial secret.

Tests:

- [x] `@path` reaches target file content.
- [x] `@-` injects from test stdin.
- [x] If implemented, env source covers present, missing, and empty.
- [x] Missing-file error follows source redaction.
- [x] Plan/state contains neither plaintext nor local path.

Examples/docs:

- [x] Add variable + file + `-var secret=@path` smoke/fixture.
- [x] Explain that `@path` is an input source, not target resource source path.

Acceptance:

- [x] Variable + file can replace simple `secrets.file` sources.
- [x] `make test` passes.

Implementation record: all three runtime sources are implemented and feed
normal conversion/propagation. Sensitive errors use `<sensitive-source>`.
Command/SOPS/age/Vault backends, ephemeral, and write-only remain later.

### Loop 8: Ephemeral Propagation and Structural Restrictions

Goal: implement non-persistence and reject ephemeral influence on graph structure.

Scope: Ephemeral marks, expression aggregation, serialization boundaries,
field allowlists, and structural restrictions.

Deferred: complete provider payload separation, runner redaction, secrets migration, and backends.

Code:

- [x] Add and propagate `Ephemeral` marks.
- [x] Forbid plaintext in HostSpec/graph/plan/state/golden JSON.
- [x] Permit ephemeral values only in explicit runtime/write-only fields.
- [x] Reject them from labels, paths, owner/group/mode/ensure, lifecycle,
  map/set keys, dependencies, and structural fields.
- [x] Report both the reference and destination field.

Tests:

- [x] Ephemeral variable reaches the defined runtime boundary in file content.
- [x] Ephemeral file path fails.
- [x] Ephemeral map/set key fails.
- [x] Ephemeral dependency/lifecycle fails.
- [x] Every serialization/golden excludes plaintext.

Examples/docs:

- [x] Add minimal ephemeral-content fixture.
- [x] List allowed and forbidden fields.

Acceptance:

- [x] Compilation artifacts honor ephemeral safety.
- [x] Unsupported fields fail closed.
- [x] `make test` passes.

Implementation record: parser.Value and cty conversions propagate Ephemeral.
First-version content fields include file, raw/structured unit, APT source/key,
and nftables; this loop reuses sensitive redaction while Loop 9 separates
payloads fully. Scalar/list structural reads reject by default, including set
elements. The DSL had no user `depends_on`, but future support must reuse these
restrictions. Added `ephemeral-variable-content.dbf.hcl` to all no-leak paths.

### Loop 9: Write-Only Provider Payload and Version Triggers

Goal: remove ephemeral content from desired/state/diff, pass it only to apply,
and trigger updates through non-sensitive versions.

Scope: desired/payload separation, write-only file content, content version,
provider/state redaction, and no-op/drift semantics.

Deferred: runner channel changes, secrets sugar migration, and a general write-only registry.

Code:

- [x] Separate persisted desired, plan-visible desired, and provider apply payload.
- [x] Support write-only `files.file.content`.
- [x] Give provider apply access to content.
- [x] Return no plaintext in plan, observed, state, or provider plan.
- [x] Use `content_version` or equivalent for updates.
- [x] Fix missing-version behavior as error or conservative update, never false no-op.

Tests:

- [x] Write-only content is absent from `Node.Desired`.
- [x] Apply writes it to the target.
- [x] State excludes it.
- [x] Observed/provider plan excludes it.
- [x] Missing-version behavior is fixed and tested.
- [x] Version change updates; unchanged version follows fixed no-op/conservative behavior.

Examples/docs:

- [x] Add sensitive+ephemeral file content with content version.
- [x] Explain explicit versions for low-entropy secrets instead of persisted digests.

Acceptance:

- [x] Recommended secret syntax has explainable plan/apply behavior.
- [x] Plan/state/provider boundaries contain no write-only plaintext.
- [x] `make test` passes.

Implementation record: ephemeral file content requires `content_version` at
compile time. Only ProviderPayload carries content; desired/graph/plan/state
contains non-sensitive metadata and no digest/summary. Engine preserves
plan-visible desired for state while provider consumes the payload. The fixture
uses the recommended form. Runner encoding remains for Loop 10.

### Loop 10: Redacted Runner Channel

Goal: close disclosure through SSH previews, stdout/stderr, wrapped errors, and
temporary files.

Scope: runner API, previews, stdin/SCP/SFTP or controlled temp files,
stdout/stderr, errors, permissions, and cleanup.

Deferred: new backends, secrets deprecation, and cross-host secret distribution.

Code:

- [x] Runner accepts a redacted payload or dedicated secret payload type.
- [x] Prefer stdin/SFTP/SCP/strict temp files rather than shell interpolation.
- [x] Command previews contain no secret.
- [x] Stdout/stderr and wrapped errors contain no secret.
- [x] Temporary files use strict permissions and best-effort cleanup.
- [x] Preserve dependency order for systemd/nftables follow-up operations.

Tests:

- [x] Fake-runner preview contains no secret.
- [x] Provider error contains no secret.
- [x] Stdout/stderr redaction covers success and failure.
- [x] File writing still succeeds.
- [x] Follow-up operations still work.

Examples/docs:

- [x] Document runner-level redaction.
- [x] Debug examples show only redacted previews.

Acceptance:

- [x] Write-only secrets stay out of logs/previews during execution.
- [x] `make test` passes; record real SSH coverage status.

Implementation record: Runner gained `RunInput`, and SSH connects input to
stdin. File-like writes use `writePathContent`, with only path/metadata and
mktemp/trap cleanup in the command. Failures wrap a fixed redacted error.
Systemd reload and nftables operations remain dependency-driven. Fake/local
runner coverage exists; no new real SSH environment was added.

### Loop 11: Make `secrets.file` Syntax Sugar

Goal: migrate the old syntax to the common file/sensitive/write-only pipeline
without breaking state addresses.

Scope: secrets/files, address compatibility, path conflicts, and state compatibility.

Deferred: default deprecation warning, deletion, and new secret backends.

Code:

- [x] Compile secrets as sensitive files or through shared helpers.
- [x] Retain original resource addresses.
- [x] Continue rejecting path conflicts with files.
- [x] Prefer variable + file in new examples.
- [x] Mark secrets as a compatibility layer.

Tests:

- [x] Old secret examples retain compatible plan/state behavior.
- [x] Secret/file path conflict still fails.
- [x] State addresses do not change unexpectedly.
- [x] Secrets reuse new leak assertions.

Examples/docs:

- [x] Show new syntax while documenting old compatibility.
- [x] Add recommended examples without deleting old fixtures.

Acceptance:

- [x] New and old secret files share one safety path.
- [x] Existing state/examples remain valid.
- [x] `make test` passes.

Implementation record: file and secret graph compilation shares one helper,
while secrets retain kind/address and map to file providers. Explicit paths use
resolved targets without requiring migration. Existing conflict validation and
state/leak tests remain. Added `variable-secret-file.dbf.hcl` with the recommended form.

### Loop 12: `secrets.file` Deprecation Evaluation and User Completion

Goal: after migration stabilizes, decide whether to warn on `secrets.file`;
deletion is not a default goal.

Scope: warnings, compatibility/suppression window, docs/examples, and migration notes.

Deferred: deletion, forced state migration, and disabling old syntax.

Code:

- [x] Add controllable deprecation warning.
- [x] Warning does not change exit status.
- [x] Provide suppression if needed or retain at least one minor compatibility cycle.
- [x] Recommend variable + file in docs/examples.

Tests:

- [x] Secrets emit a warning.
- [x] Warning does not change validate/plan/apply exit status.
- [x] If suppression exists, test enabled/disabled behavior.
- [x] Existing integration remains runnable.

Examples/docs:

- [x] README defaults to the new form.
- [x] Migration maps `source` to `-var @path` clearly.
- [x] Document final compatibility-layer status.

Acceptance:

- [x] `secrets.file` may continue as a compatibility layer.
- [x] Deletion needs a separate decision and loop.
- [x] `make test` passes.

Implementation record: merge emits an `ir.Warning` on compilation; all commands
retain exit semantics. `SuppressSecretFileDeprecationWarning` supports phased
migration. CLI and merge tests cover warning sources and suppression. Syntax is
not removed.

## Recommended Implementation Order

For general variables first:

```text
Loop 0 -> 1 -> 2 -> 3 -> 4 -> 5
```

To replace secret-file sources:

```text
Loop 6 -> 7
```

For Terraform-like ephemeral/write-only safety:

```text
Loop 8 -> 9 -> 10
```

Then compatibility syntax:

```text
Loop 11 -> 12
```

Do not skip Loop 6 before ephemeral/write-only work: without propagation and
leak tests, later safety changes cannot be evaluated reliably.

## Open Questions

- Provide `.dbfvars`, or only CLI/stdin?
- Embed SOPS/age, or expose command/stdin integration for external tools?
- Persist sensitive SHA-256 by default, or require explicit opt-in?
- Use one `content_version` field or per-resource versions?
- Without a write-only version, always plan update or fail?
