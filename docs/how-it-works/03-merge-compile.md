# 03. How Profiles, Hosts, and Components Compile into IR

<p align="right"><strong>English</strong> | <a href="03-merge-compile.zh.md">简体中文</a></p>

This chapter explains how `internal/core/merge` compiles the parser's raw
configuration output into `internal/core/ir.Program`. This stage is the boundary
where DebianForm moves from a configuration syntax tree into its domain model.

## Data Flow

```text
parser.Config
  -> merge.CompileWithOptions
  -> variables/component templates
  -> profiles + host body merge
  -> host facts injection
  -> component instantiation
  -> validate + assert
  -> ir.Program
```

The main entry point is `merge.CompileWithOptions`. It accepts `CompileOptions`
that control host filtering, fact injection, whether components are skipped,
and whether only runtime templates are validated.

## CompileOptions

Important options include:

- `HostFilter`: compile only the named host.
- `HostFacts`: facts discovered in online mode, injected by host name.
- `SkipComponents`: used by the first online compilation phase to avoid
  instantiating components before facts have been discovered.
- `ValidateRuntimeTemplates`: used by `validate` to check component templates
  without real instantiation.
- `Warnings`: collects non-fatal warnings, such as deprecated capabilities.

These options allow the same compilation logic to serve `validate`, offline
plan, online plan, and apply.

## Program Top-Level Structure

`ir.Program` contains:

- `Hosts []HostSpec`
- `Variables map[string]VariableSpec`
- `Components map[string]ComponentTemplateSpec`

`Variables` and `Components` are public metadata used by inspect commands and
retained as necessary context for later stages. `Hosts` is the primary input
expanded into graph nodes.

## Profile Merging

The compile stage resolves the relationship between hosts and profiles. The
approximate process is:

1. Create an empty map value for the host.
2. Resolve each profile in the host's `Imports` order.
3. Overlay each profile body on the current raw value.
4. Overlay the host's own body last.
5. Collect profile and host assertions.

`resolveProfile` uses cache and visiting markers to avoid repeated work and
detect import cycles.

The merge function is `Merge(base, overlay)`. It understands parser modifiers:

- Default map merging.
- `force()` replaces the whole value.
- `before()` and `after()` control list merge order.
- `unset()` removes or clears the corresponding value.

A profile is therefore not copied mechanically; it uses deliberate override
semantics.

## Building HostSpec

`buildHostSpec` turns the merged `parser.Value` into a strongly typed
`ir.HostSpec`. It handles:

- Default SSH host/user/state paths.
- System, kernel, packages, APT, files, secrets, and directories.
- Groups, users, systemd, services, nftables, and Docker.
- Lifecycle, source, content-summary, and related metadata.

This stage normalizes convenient DSL forms into explicit IR. Examples include
block labels, default owners/groups/modes, and default `ensure` values.

## Runtime Facts

Online mode first compiles a basic program used to construct SSH runners and
discover facts. The second compilation injects them through
`CompileOptions.HostFacts`.

`applyHostFacts` writes facts into `HostSpec.Facts` and related system fields.
Configuration that depends on runtime facts, such as selecting a component
artifact or APT suite by architecture, fails during compilation or graph
construction when facts are unavailable offline.

## Component Templates and Instantiation

The parser retains a component's template body, input definitions, and artifact
definitions. The merge layer performs two kinds of work:

1. `componentTemplateSpecs` compiles component-template metadata into
   `ir.ComponentTemplateSpec`.
2. `instantiateComponents` turns a host's component instances and inputs into
   resource specs under that host.

Inputs pass through:

- Type normalization.
- Required/default/nullability validation.
- Sensitive-mark propagation.
- Validation-block checks.
- Deprecated warnings.

Users, groups, files, systemd units, component artifacts, and other resources
produced by component instantiation are attached to `HostSpec.Components`. The
graph layer expands both host-owned and component-owned resources.

## Public Metadata for Variables and Component Inputs

`variableSpecs` and `componentTemplateSpecs` convert variable and component
input definitions into IR metadata:

- Type, type expression, and type spec.
- Description and default.
- Sensitive, nullable, ephemeral, const, and deprecated properties.
- Validations.
- Source.

If a default is sensitive, the CLI layer further replaces it with
`"<sensitive>"` in inspect output.

## Assertions

Assertion logic lives in `internal/core/merge/assert.go`. After collecting
profile and host assertions, compilation runs `evaluateAssertions` following
HostSpec construction and fact/component processing.

Assertions operate on the normalized host spec, not raw HCL. They can therefore
validate final semantics, such as the result after profile imports and host
overrides.

## Validation

`validateHostSpec` guards the IR boundary. It checks that HostSpec satisfies
domain constraints such as required fields, paths, duplicate identities,
`ensure` values, and references.

The parser guarantees only syntax and local shape. Cross-resource and
cross-block semantics belong here or in graph validation.

## Design Boundaries

- The merge layer may understand DebianForm domain semantics.
- The merge layer must not read remote state or execute commands.
- Its IR output should express stable user intent, not provider shell details.
- Component instantiation belongs in merge because graph construction must see
  the host's complete resource set.

## Change Checklist

- New DSL field: update parser reading, merge building, IR types, validation,
  and goldens.
- New profile merge semantics: add `merge.Merge` unit tests and verify source
  and modifier behavior.
- New component input type or validation: add component inspect, merge
  validation, and sensitive cases.
- New runtime-fact-dependent capability: verify offline errors, online fact
  injection, and state-fact persistence.
- Assertion-context change: add positive and negative tests to ensure assertions
  never see an unnormalized structure.
