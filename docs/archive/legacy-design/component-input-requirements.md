# DebianForm Component Input Requirements

<p align="right"><strong>English</strong> | <a href="component-input-requirements.zh.md">简体中文</a></p>

This document defines target capabilities for `input` inside a `component`. It
promotes component inputs from simple parameters into a component interface
contract similar to Terraform `variable`, with particular emphasis on
structured parameters such as `list(object({...}))`.

Terraform's official `variable` block and type constraints are references, not
a compatibility target. DebianForm adapts their mature interface design to its
own components, HostSpec, plan/state, and sensitive-data model.

## Background

Current DebianForm component inputs support:

```hcl
component "app" {
  input "listen_addr" {
    type      = string
    default   = "127.0.0.1:8080"
    sensitive = false
  }
}
```

Current supported types:

```text
string
number
bool
any
list(T)
set(T)
map(T)
object({ ... })
tuple([ ... ])
optional(T)
optional(T, default)
```

`optional(...)` is valid only for object attributes. `description`, `nullable`,
strict object schemas, optional defaults, `validation`, sensitive-derived
propagation, and `deprecated` warnings are implemented. `ephemeral` remains a
future capability.

Real deployment components often need structured interfaces, for example:

- Several listeners, each with `name`, `port`, `protocol`, and `tls`.
- Several upstreams, each with `host`, `port`, and `weight`.
- Users with UID, shell, groups, and authorized keys.
- Service instances with nested environment, volumes, and health checks.

These cases require `list(object({...}))`, `map(object({...}))`, nested objects,
optional attributes, nullable values, and validation.

## Goals

- Make `input` a stable public component API.
- Support Terraform-style type-constraint syntax for primitives, collections,
  and structural types.
- Prioritize `list(object({...}))` and nested `object`.
- Support object attribute defaults and optional fields to reduce caller boilerplate.
- Support `description` for readable generated interface documentation.
- Support `validation` before component expansion.
- Support `nullable` with explicit semantics for null versus omission.
- Preserve and strengthen `sensitive` so input values and derived content do not
  leak into plans, state, or logs.
- Keep existing `type/default/sensitive` input configuration compatible.
- Report every error against a user DSL source path, including nested fields or list indexes.

## Non-Goals

- Do not implement Terraform root-module external sources such as `TF_VAR_*`,
  `.tfvars`, or CLI `-var` for component inputs.
- Do not implement Terraform's module system; inputs serve DebianForm components only.
- Do not add an `array` keyword; use `list(T)` or `tuple([...])`.
- The first phase need not reproduce all Terraform automatic conversion or
  permissive object conversion.
- The first phase does not implement complete `ephemeral` semantics until plans,
  state, and provider write-only parameters can avoid persistence.
- Input defaults cannot depend on `target`, remote runtime facts, or another input.

## Terraform Comparison

Terraform `variable` blocks support:

```hcl
variable "name" {
  type        = <TYPE>
  default     = <DEFAULT_VALUE>
  description = "<DESCRIPTION>"
  sensitive   = <true|false>
  nullable    = <true|false>
  ephemeral   = <true|false>

  validation {
    condition     = <EXPRESSION>
    error_message = "<ERROR_MESSAGE>"
  }
}
```

DebianForm selects:

| Terraform variable | DebianForm input | Phase | Description |
| --- | --- | --- | --- |
| `type` | `type` | Required | DebianForm continues to require an explicit type rather than defaulting omission to `any`. |
| `default` | `default` | Required | An input without a default is required; one with a default may be omitted. |
| `description` | `description` | Required | Documentation metadata with no compilation effect. |
| `validation` | `validation` | Required | Runs before component expansion. |
| `sensitive` | `sensitive` | Required | Expands from hiding the input value into sensitive propagation. |
| `nullable` | `nullable` | Required | Defaults to true; false forbids top-level null. |
| `ephemeral` | Reserved field | Deferred | Safe only after non-persistence and write-only provider semantics exist. |

Additional DebianForm field:

| DebianForm input | Phase | Description |
| --- | --- | --- |
| `deprecated` | Should support | Supports component API evolution; explicit caller assignment emits a warning during validate/plan. |

## User Syntax

### Basic Syntax

```hcl
component "app" {
  input "listen_addr" {
    type        = string
    description = "Address and port used by the application listener."
    default     = "127.0.0.1:8080"
    nullable    = false
  }
}
```

Rules:

- An `input` block has exactly one label.
- Input labels are unique within a component.
- `type` is required.
- `description` is optional but recommended for every public component.
- `default` is optional; without one, instances must pass a value.
- `nullable` is optional and defaults to true.
- `sensitive` is optional and defaults to false.
- `deprecated` is optional and must be a non-empty string.
- `validation` blocks may repeat.

### `list(object(...))`

```hcl
component "reverse_proxy" {
  input "listeners" {
    type = list(object({
      name     = string
      port     = number
      protocol = optional(string, "http")
      tls      = optional(bool, false)

      upstreams = list(object({
        host   = string
        port   = number
        weight = optional(number, 1)
      }))

      headers = optional(map(string), {})
    }))

    description = "Listener definitions exposed by this reverse proxy."
    default     = []
    nullable    = false

    validation {
      condition = alltrue([
        for listener in input.listeners :
        listener.port >= 1 && listener.port <= 65535
      ])
      error_message = "Each listener.port must be between 1 and 65535."
    }
  }

  files {
    file "/etc/reverse-proxy/listeners.json" {
      mode    = "0644"
      content = jsonencode(input.listeners)
    }
  }
}
```

Invocation:

```hcl
host "edge1" {
  component "proxy" {
    source = component.reverse_proxy

    inputs = {
      listeners = [
        {
          name = "public-http"
          port = 80
          upstreams = [
            { host = "127.0.0.1", port = 8080 },
          ]
        },
        {
          name     = "public-https"
          port     = 443
          protocol = "http"
          tls      = true
          upstreams = [
            { host = "127.0.0.1", port = 8443, weight = 10 },
          ]
          headers = {
            X-Forwarded-Proto = "https"
          }
        },
      ]
    }
  }
}
```

After normalization, the first item gains:

```hcl
{
  name     = "public-http"
  port     = 80
  protocol = "http"
  tls      = false
  upstreams = [
    { host = "127.0.0.1", port = 8080, weight = 1 },
  ]
  headers = {}
}
```

### `map(object(...))`

```hcl
component "users" {
  input "accounts" {
    type = map(object({
      uid                 = optional(number)
      shell               = optional(string, "/bin/bash")
      groups              = optional(list(string), [])
      ssh_authorized_keys = optional(list(string), [])
    }))

    default     = {}
    nullable    = false
    description = "Managed Unix accounts keyed by username."
  }
}
```

Invocation:

```hcl
inputs = {
  accounts = {
    deploy = {
      groups = ["docker"]
      ssh_authorized_keys = [
        "ssh-ed25519 AAAA...",
      ]
    }
  }
}
```

### Sensitive Inputs

```hcl
component "service" {
  input "environment" {
    type        = map(string)
    sensitive   = true
    description = "Environment variables containing credentials."
    default     = {}
  }
}
```

Requirements:

- Hide `input.environment` in HostSpec, plan, state, and debug output.
- Every expression derived from `input.environment` carries sensitivity.
- If it becomes `files.file.content`, plan/state stores only summaries, never plaintext.
- If a sensitive value reaches a non-sensitive field whose implementation
  cannot propagate the mark, validation must fail or warn explicitly; it cannot leak silently.

### Deprecated Inputs

```hcl
component "app" {
  input "listen_addr" {
    type       = string
    deprecated = "Use listeners instead."
    default    = "127.0.0.1:8080"
  }

  input "listeners" {
    type    = list(object({ port = number }))
    default = []
  }
}
```

An explicit caller value for `listen_addr` produces a warning in `dbf validate`
and `dbf plan` without blocking execution. Using only its default does not warn,
preventing old component defaults from polluting output.

## Type System

### Supported Type Expressions

The first phase must support:

```text
string
number
bool
any
list(T)
set(T)
map(T)
object({ name = T, other = optional(T), third = optional(T, DEFAULT) })
tuple([T1, T2, ...])
```

`T` may nest recursively.

Example:

```hcl
type = list(object({
  name = string
  tags = optional(map(string), {})
  peers = optional(list(object({
    public_key  = string
    allowed_ips = list(string)
  })), [])
}))
```

### Unsupported Type Expressions

```text
array(T)
list
map
set
object
tuple
optional(T, dynamic_default_expression)
```

Notes:

- No `array`; use `list(T)`.
- Do not support Terraform's bare `list`, `map`, or `set` shorthand. Require
  element types to reduce ambiguous interfaces.
- `optional` appears only in an object attribute's type position.
- The default in `optional(T, DEFAULT)` must be statically evaluable during
  parse/compile and convertible to `T`.

### `any`

`any` is an escape hatch, not a default.

- `type = any` permits any serializable HCL value.
- `list(any)` and `map(any)` infer element types from input.
- Accessing fields/indexes on `any` inside a component delays errors until
  expression evaluation, so public components should not prefer it.
- Values must still have JSON/HostSpec representations. Functions, unknowns,
  and unserializable values are forbidden.

### Object Field Rules

DebianForm should use strict object schemas:

- Missing required fields fail.
- Type mismatches fail.
- Undeclared fields fail by default rather than being silently discarded as in
  Terraform object conversion.
- A future permissive mode should be explicit, such as
  `additional_attributes = true`; silent discard must not be the default.

Host desired state makes silent input typos dangerous because bad
configuration could otherwise reach plan/apply.

### Optional Object Attributes

`optional(T)`:

- An omitted caller field normalizes to null.
- A supplied value must convert to `T`.

`optional(T, DEFAULT)`:

- An omitted caller field uses `DEFAULT`.
- For an explicit null:
  - Preserve null when the field type permits it.
  - A later decision may match Terraform by replacing null with the default.

The first phase uses a simple rule: only omission activates an optional default;
explicit null does not.

### Null and Nullable

`nullable` controls the top-level input value:

- `nullable = true`: caller may pass null explicitly.
- `nullable = false`: explicit null fails.
- Default true, matching Terraform.
- Nulls inside collections/objects follow nested types and optional rules;
  top-level false does not recursively forbid internal nulls.

Example:

```hcl
input "config" {
  type = object({
    path = string
    mode = optional(string)
  })
  nullable = false
}
```

Here `config = null` is invalid, while `config.mode = null` may be valid.

### Type Conversion

The target is close to Terraform cty conversion with explicit boundaries.

Required:

- Convert tuple/list literals to `list(T)`, converting every element to `T`.
- Convert object/map literals to `map(T)`, converting every element to `T`.
- Convert an object literal to `object({...})` with strict schema checks.
- Convert list/tuple to `tuple([...])` only with matching length.
- Normalize `set(T)` to deterministic order for stable HostSpec/plan output.

The first phase may omit, or support only when unambiguous:

- Terraform-style conversion among string, number, and bool.
- Permissive object/map conversion.
- Complete set/list/tuple conversion.

For every unsupported Terraform conversion, explain that DebianForm requires an
explicit type.

## Validation

### Syntax

```hcl
input "listeners" {
  type = list(object({
    name = string
    port = number
  }))

  validation {
    condition = alltrue([
      for listener in input.listeners :
      listener.port >= 1 && listener.port <= 65535
    ])
    error_message = "Each listener.port must be between 1 and 65535."
  }
}
```

Rules:

- A `validation` block has no label.
- Every validation has `condition` and `error_message`.
- `condition` evaluates to bool.
- `error_message` is a non-empty string.
- Validations may repeat and run in source order.
- Validation runs after defaults, optional defaults, type conversion, and nullable checks.
- Failure stops `validate`, `plan`, and `apply`.

### Visible Validation Context

The first phase allows only:

```text
input.<current_input_name>
```

For `input "listeners"`, this is allowed:

```hcl
input.listeners
```

These are forbidden:

```hcl
target.system.codename
input.other_input
local.some_value
file("...")
templatefile("...", {})
```

Input validation describes the current input's own contract and must remain
offline, pure, stable, and reproducible. Future cross-input checks should use a
component-level assertion or validation, not hidden dependencies in one input.

### Validation Functions

Currently supported pure functions:

```text
length
contains
startswith
endswith
regex
can
alltrue
anytrue
distinct
sort
keys
values
flatten
toset
tonumber
tostring
tobool
```

May be deferred:

```text
cidrhost
cidrnetmask
cidrsubnet
jsondecode
yamldecode
```

Forbidden:

```text
file
templatefile
env
external commands
network access
```

## Input Access in Component Bodies

During expansion, the `input` object contains all normalized input values:

- Caller values take precedence.
- An omitted value uses its `default`, when present.
- Object optional fields are filled according to their rules.
- Type conversion is complete.
- Nullable checks are complete.
- Validation has passed.

Component bodies may access nested fields:

```hcl
content = input.listeners[0].upstreams[0].host
```

For-expressions may generate structures:

```hcl
content = jsonencode([
  for listener in input.listeners : {
    name = listener.name
    port = listener.port
  }
])
```

The evaluator must inject `input` as a structured cty value, not flatten it to
a string first.

## IR Requirements

### ComponentInputSpec

Recommended extension:

```go
type ComponentInputSpec struct {
    Name        string
    Type        ComponentInputTypeSpec
    TypeExpr    string
    Description string
    Default     *Value
    Sensitive   bool
    Nullable    bool
    Deprecated  string
    Validations []ComponentInputValidationSpec
    Source      SourceRef
}

type ComponentInputValidationSpec struct {
    ConditionSource SourceRef
    Message         string
    MessageSource   SourceRef
}
```

`TypeExpr` is a canonical, user-readable string such as:

```text
list(object({name=string,port=number,tls=optional(bool,false)}))
```

`ComponentInputTypeSpec` is machine-readable schema, not only a string, so docs,
error paths, and future tooling can use it.

### ComponentInputTypeSpec

Recommended structure:

```go
type ComponentInputTypeSpec struct {
    Kind       string
    Element    *ComponentInputTypeSpec
    Attributes map[string]ComponentObjectAttributeSpec
    Tuple      []ComponentInputTypeSpec
}

type ComponentObjectAttributeSpec struct {
    Type     ComponentInputTypeSpec
    Optional bool
    Default  *Value
}
```

`Kind` values:

```text
string
number
bool
any
list
set
map
object
tuple
```

### ComponentInstanceSpec

`ComponentInstanceSpec.InputValues` stores normalized values, not raw caller input.

Sensitive rules:

- A `sensitive = true` input appears as `"<sensitive>"` or is omitted in JSON HostSpec.
- If the provider needs the real value, retain it only in the in-memory compiled
  program, never plan/state JSON.
- Future ephemeral values must be omitted from HostSpec, plan, and state JSON.

### SourceRef

Nested errors need exact paths:

```text
component.reverse_proxy.input["listeners"].default[0].upstreams[1].port
host.edge1.component["proxy"].inputs["listeners"][0].upstreams[0].host
```

Example error:

```text
examples/app.dbf.hcl:42: host.edge1.component["proxy"].inputs["listeners"][0].port:
component.reverse_proxy input "listeners" expected number at .port, got string
```

## Plan/State Requirements

- Plan does not treat input metadata as resource changes.
- Component instance `input_values` exists only for explanation/debugging.
- Non-sensitive inputs may display normalized values.
- Sensitive inputs are redacted.
- File content, unit content, secret sources, and other values derived from a
  sensitive input inherit sensitivity.
- State stores no sensitive plaintext.
- When a provider currently needs content for diffs, store summaries such as
  hash, length, mode, and owner.
- `description`, `deprecated`, and type schemas are component-template metadata
  and do not belong in every host's state.

## CLI and UX Requirements

### validate

`dbf validate` checks:

- Allowed input-block attributes.
- Parseable type expressions.
- Default conversion to the declared type.
- Optional-default conversion to the field type.
- A non-null default under `nullable=false`.
- Complete validation blocks.
- Boolean validation conditions.
- Missing required instance inputs.
- Unknown instance inputs.
- Instance input conversion to declared types.
- Instance input validation.

### plan/apply

`dbf plan` and `dbf apply` reuse all input checks from validate. Every input
error fails before ResourceGraph generation.

### Inspect or Documentation Output

A later command may provide:

```bash
dbf component inspect -f app.dbf.hcl reverse_proxy
```

Output:

```text
component.reverse_proxy inputs

listeners
  type: list(object({ ... }))
  default: []
  nullable: false
  sensitive: false
  description: Listener definitions exposed by this reverse proxy.
```

This is not mandatory in the first phase, but `description` and type schema must
support it.

## Implementation Recommendations

### Parsing Type Expressions

Do not parse types through string concatenation. Use the HCL AST:

- `hcl.AbsTraversalForExpr` for primitives and `any`.
- `hclsyntax.FunctionCallExpr` for `list(T)`, `map(T)`, `set(T)`,
  `object({...})`, `tuple([...])`, and `optional(...)`.
- Require an object constructor argument for `object`.
- Require a tuple/list constructor argument for `tuple`.
- Permit `optional` only in an object attribute type position.

### Internal Value Model

Existing `parser.Value` represents string/bool/number/list/map/null but lacks:

- Explicit type.
- Object schema normalized field defaults.
- Sensitive marks.
- Distinction between set and tuple.

Recommendation:

- Use `cty.Type` / `cty.Value` for checking and conversion.
- Retain `parser.Value` as a stable HostSpec/IR JSON representation.
- Add `Sensitive bool` to `parser.Value`, or wrap it to retain marks.
- Preserve information required for redaction and summaries when converting cty
  back into `parser.Value`.

### Conversion Order

For each component instance:

```text
1. Collect caller inputs.
2. Check unknown inputs.
3. For every declared input:
   a. Use the caller value when supplied.
   b. Otherwise use the default when present.
   c. Otherwise report required.
   d. Reject top-level null when nullable=false.
   e. Convert to the type and fill object optional defaults.
   f. Run validation blocks.
   g. Mark sensitivity.
4. Construct the normalized input object.
5. Expand the component body using that object.
```

### Sensitive Propagation

Like Terraform, DebianForm should keep expressions sensitive when they refer to
sensitive variables.

Current implementation:

- With `input.foo.sensitive = true`, `input.foo` is a marked sensitive cty value.
- If any expression input is sensitive, the result remains sensitive.
- `parser.Value` retains the sensitive mark.
- File and systemd unit content becomes sensitive automatically from that mark.
- HostSpec JSON, plan JSON/text, and state JSON choose summaries or redaction
  from the mark.

## Error-Message Requirements

Errors contain:

- File.
- Line number.
- DebianForm source path.
- Component template name.
- Input name.
- Nested field path.
- Expected type.
- Actual type.

Example:

```text
examples/proxy.dbf.hcl:37: host.edge1.component["proxy"].inputs["listeners"][0].upstreams[0].port:
component.reverse_proxy input "listeners" expected number at .upstreams[0].port, got string
```

Validation failure:

```text
examples/proxy.dbf.hcl:51: component.reverse_proxy.input["listeners"].validation[0]:
validation failed for input "listeners": Each listener.port must be between 1 and 65535.
```

Deprecated warning:

```text
warning: examples/app.dbf.hcl:20: host.web1.component["app"].inputs["listen_addr"]:
component.app input "listen_addr" is deprecated: Use listeners instead.
```

## Compatibility

Existing configuration remains valid:

```hcl
input "repo_uri" {
  type = string
}

input "packages" {
  type    = list(string)
  default = []
}

input "labels" {
  type    = map(string)
  default = {}
}
```

New fields do not change old input behavior:

```hcl
input "repo_uri" {
  type        = string
  description = "APT repository URI."
}
```

Avoid breaking changes:

- Continue requiring `type`.
- Preserve `list(string)` and `map(string)` semantics.
- Keep at least `input_values` redaction for `sensitive = true`.
- Keep old error paths recognizable where possible.

Potential behavior changes:

- Terraform-style primitive conversion could turn `default = 123` for
  `type = string` from failure into `"123"`. Remain strict in the first phase
  to avoid an implicit change.
- Strict object schemas reject extra fields. This affects only the new type
  capability, not old configuration.

## Testing Requirements

### Parser Unit Tests

- Support `description`, `nullable`, and `deprecated`.
- Support repeated `validation` blocks.
- Reject unknown input attributes.
- Reject nested blocks other than `validation`.
- Parse primitives, `any`, `list(T)`, `set(T)`, and `map(T)`.
- Parse `object({ name = string, enabled = optional(bool, true) })`.
- Parse `list(object({ ... }))`.
- Reject `array(string)`.
- Reject bare `list`, `map`, and `set`.
- Reject `optional(...)` outside an object.

### Merge/Compiler Unit Tests

- Missing required input fails.
- Unknown input fails.
- Default is used and normalized.
- `nullable=false` rejects null.
- `nullable=true` accepts top-level null.
- Missing required object field fails.
- Extra object field fails.
- Omitted optional field fills null or its default.
- Nested type error in `list(object(...))` reports the index.
- Validation succeeds.
- Validation fails.
- Non-boolean validation condition fails.
- Explicit deprecated input warns.
- Sensitive input is redacted in HostSpec JSON.

### Golden Tests

- Add runnable fixture `examples/component-inputs.dbf.hcl`.
- HostSpec golden covers normalized `list(object(...))`.
- Plan golden confirms no sensitive input leak.

### Integration Tests

The first phase need not cover every type with libvirt, but at least one
component should generate a real file from `list(object(...))`, with VM
verification of normalized content.

## Phased Plan

### Phase 1: Types and Description (Implemented)

- Add a type parser for primitives, `any`, list, set, map, object, tuple, and optional.
- Add `description`.
- Add `nullable`.
- Add strict object validation.
- Add optional object attributes.
- Keep strict primitive typing by default without string/number/bool auto-conversion.
- Update IR, HostSpec goldens, and parser/merge tests.

### Phase 2: Validation (Implemented)

- Support input `validation` blocks.
- Inject `input.<current>` validation context.
- Add the pure-function set.
- Run validation before component-body expansion.
- Add error paths and source locations.

### Phase 3: Sensitive Propagation (Implemented)

- Add sensitive marks to cty/parser.Value.
- Propagate marks through expression evaluation.
- Have file/unit/service/environment provider payloads respect sensitivity.
- Emit only summaries or redaction in plan/state.

### Phase 4: Deprecated and Tooling (Implemented)

- Support `deprecated` warnings.
- Add `dbf component inspect`.
- Present the component input API in README and examples.

### Phase 5: Ephemeral Evaluation

Implement only after:

- Plan JSON does not store ephemeral values.
- State JSON does not store ephemeral values.
- Providers support write-only or runtime-only parameters.
- Expressions referencing ephemeral inputs remain marked ephemeral.
- Ephemeral-derived values cannot enter resources needing plaintext in diffs/state.

## Acceptance Criteria

This configuration validates and produces normalized structured input in HostSpec:

```hcl
component "demo" {
  input "listeners" {
    type = list(object({
      name = string
      port = number
      tls  = optional(bool, false)
      tags = optional(map(string), {})
    }))

    description = "Listeners exposed by demo."
    default     = []
    nullable    = false
  }
}
```

This invocation is valid:

```hcl
inputs = {
  listeners = [
    {
      name = "http"
      port = 80
    },
  ]
}
```

Normalized result includes:

```hcl
listeners = [
  {
    name = "http"
    port = 80
    tls  = false
    tags = {}
  },
]
```

This invocation fails and points to `.listeners[0].port`:

```hcl
inputs = {
  listeners = [
    {
      name = "http"
      port = "eighty"
    },
  ]
}
```

This type fails with guidance to use `list(T)`:

```hcl
input "ports" {
  type = array(number)
}
```

## References

- Terraform variable block reference:
  <https://developer.hashicorp.com/terraform/language/block/variable>
- Terraform type constraints:
  <https://developer.hashicorp.com/terraform/language/expressions/type-constraints>
