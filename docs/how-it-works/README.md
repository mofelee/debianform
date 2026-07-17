# How DebianForm Works

<p align="right"><strong>English</strong> | <a href="README.zh.md">简体中文</a></p>

This directory contains a tutorial series for future DebianForm developers. It
does not repeat the user manual. Instead, it explains the internal path from
reading `.dbf.hcl` through planning changes, applying them, and writing state,
along with the responsibility boundary of each package.

Read the series in the following order. Each tutorial should cover:

- User-visible behavior: the problem this stage solves.
- Core code entry points: where to begin reading.
- Data structures: inputs, outputs, and important intermediate representations.
- Design constraints: boundaries that must not be crossed casually.
- Change guidance: which areas and tests to update when adding a capability.

## Series Contents

### Task List

- [x] [01. Overall Architecture and Command Lifecycle](01-architecture.md)
- [x] [02. HCL Parsing, Variables, and the Value Model](02-parser-values.md)
- [x] [03. How Profiles, Hosts, and Components Compile into IR](03-merge-compile.md)
- [x] [04. IR Data Model and Resource Boundaries](04-ir-model.md)
- [x] [05. How ResourceGraph Expands Resources and Dependencies](05-resource-graph.md)
- [x] [06. Plan Documents, Diffs, and Output Formats](06-plan-output.md)
- [x] [07. The Online Engine: Reading State, Observing Reality, and Computing Actions](07-engine-plan.md)
- [x] [08. Apply Execution, Locking, and Scheduling](08-apply-scheduler.md)
- [x] [09. Backends, SSH, and Remote State](09-backend-state.md)
- [x] [10. Native Providers and Resource Implementations](10-native-provider.md)
- [x] [11. Secrets, Sensitive Values, and the Redaction Pipeline](11-redaction.md)
- [x] [12. Testing Strategy and the Capability-Extension Checklist](12-testing-and-extension.md)

When adding a chapter to this series, add it to the task list above before
writing the chapter file.

### [01. Overall Architecture and Command Lifecycle](01-architecture.md)

Starting from `cmd/dbf/main.go`, this chapter explains how `validate`, `plan`,
`apply`, `check`, `fmt`, `component inspect`, and `variable inspect` are
dispatched. It focuses on the branch between offline and online plans and the
end-to-end data flow of a typical command:

```text
CLI flags -> parser.Config -> ir.Program -> graph.ResourceGraph -> engine.Plan -> plan.Document
```

Code entry points to cover:

- `run`
- `runConfigCommand`
- `runConfigWorkflow`
- `parseConfigWithExternalValues`

### [02. HCL Parsing, Variables, and the Value Model](02-parser-values.md)

This chapter explains how `internal/core/parser` reads multiple `.dbf.hcl`
files, processes `locals`, `variable`, and top-level blocks in phases, and
applies variable-source precedence. It focuses on how `.dbfvars`,
`.auto.dbfvars`, `DBF_VAR_` environment variables, `-var`, and `-var-file`
enter the parsing process.

Code entry points to cover:

- `parser.ParseFilesWithOptions`
- `parseLocals`
- `parseVariables`
- `parseTopLevel`
- `resolveVariableValues`
- `ParseVariableFile`

### [03. How Profiles, Hosts, and Components Compile into IR](03-merge-compile.md)

This chapter explains how `internal/core/merge` combines parsed configuration
into `internal/core/ir.Program`. It focuses on the relationship among profile
imports, host overrides, component templates, component input validation,
assertions, and runtime facts during compilation.

Code entry points to cover:

- `merge.CompileWithOptions`
- `resolveProfile`
- `Merge`
- `buildHostSpec`
- `instantiateComponents`
- `validateHostSpec`
- `evaluateAssertions`

### [04. IR Data Model and Resource Boundaries](04-ir-model.md)

Using `internal/core/ir/types.go`, this chapter explains the responsibilities
of `Program`, `HostSpec`, and the domain specs. It emphasizes that the IR is a
domain-layer structure, not a direct representation of provider operations. It
should express user intent and stable semantics, not remote-command details.

Code entry points to cover:

- `ir.Program`
- `ir.HostSpec`
- `ir.SourceRef`
- `ir.LifecycleSpec`
- Domain specs such as `APTSpec`, `FileSpec`, `SystemdSpec`, and `DockerSpec`

### [05. How ResourceGraph Expands Resources and Dependencies](05-resource-graph.md)

This chapter explains how `internal/core/graph` expands a host's IR into
plannable and executable resource nodes and operations. It focuses on address
naming, provider payloads, dependencies, operation triggers, and preventing
sensitive content from leaking into graph JSON.

Code entry points to cover:

- `graph.Compile`
- `compileHost`
- `ResourceGraph.Validate`
- `Node.MarshalJSON`
- Domain node builders

### [06. Plan Documents, Diffs, and Output Formats](06-plan-output.md)

This chapter explains how `internal/core/plan` renders an offline graph or an
online engine plan as human-readable text, JSON, and HTML. It focuses on the
`plan.Document` format version, summary counts, diff trees, sensitive-content
summaries, and the `--debug` provider address.

Code entry points to cover:

- `plan.New`
- `engine.Plan.Document`
- `BuildDiff`
- `PrintText`
- `PrintJSON`
- `PrintHTML`

### [07. The Online Engine: Reading State, Observing Reality, and Computing Actions](07-engine-plan.md)

This chapter explains how `internal/core/engine` reads remote state in online
mode, asks providers to observe actual state, and compares desired state, prior
state, and observed reality to determine actions. It focuses on the semantics
of `create`, `update`, `delete`, `adopt`, `forget`, `destroy`, and `no-op`.

Code entry points to cover:

- `Engine.Plan`
- `Compare`
- `orphanSteps`
- `operationSteps`
- `Plan.Document`

### [08. Apply Execution, Locking, and Scheduling](08-apply-scheduler.md)

This chapter explains why apply regenerates an online plan, how it acquires a
lock for each host, how resource and operation steps run in dependency waves,
and how state is updated after execution. It focuses on `--parallel`, per-host
concurrency, partial-execution behavior after failure, and the idempotence
expected from repeated applies.

Code entry points to cover:

- `Engine.Apply`
- `executionWaves`
- `runExecutionWaves`
- `applyStep`
- `runOperation`
- `persistHostFacts`

### [09. Backends, SSH, and Remote State](09-backend-state.md)

This chapter explains how the backend abstraction and SSH implementation in
`internal/core/engine` read and write remote state, acquire locks, execute
commands, and upload content. It also covers the state format, normalization,
desired digest, and ownership in `internal/core/state`.

Code entry points to cover:

- `Backend`
- `NewSSHBackend`
- `Runner`
- `state.State`
- `state.Resource`
- `state.Normalize`
- `state.DesiredDigest`

### [10. Native Providers and Resource Implementations](10-native-provider.md)

This chapter explains how a native provider turns a graph node's provider
payload into real operations on a supported managed target. It focuses on the
plan/apply/destroy boundaries, observed values, desired digests, and low-level
command previews for each provider type.

Code entry points to cover:

- `Provider`
- `NewNativeProvider`
- `Provider.Plan`
- `Provider.Apply`
- `Provider.Destroy`
- `Provider.RunOperation`

### [11. Secrets, Sensitive Values, and the Redaction Pipeline](11-redaction.md)

This chapter explains the complete redaction path for sensitive data through
the parser, IR, graph, plan, state, HTML/JSON output, and test assertions. It
focuses on the differences among `content_write_only`, secret files, sensitive
variables, and sensitive component inputs, and identifies structures that must
never serialize plaintext.

Code entry points to cover:

- `Node.MarshalJSON`
- `plan.BuildDiff`
- `cmd/dbf/redaction_matrix_test.go`
- `internal/core/testassert/secrets.go`
- Compilation logic for sensitive variables and component inputs

### [12. Testing Strategy and the Capability-Extension Checklist](12-testing-and-extension.md)

This chapter explains the boundaries protected by unit tests, golden tests,
CLI tests, source-build tests, and libvirt integration tests. It concludes with
the code and test checklists for adding a domain block or provider resource.

Paths to cover:

- `cmd/dbf/*_test.go`
- `internal/core/parser/*_test.go`
- `internal/core/merge/*_test.go`
- `internal/core/graph/*_test.go`
- `internal/core/plan/*_test.go`
- `internal/core/engine/*_test.go`
- `internal/core/testdata/**`
- `test/integration/libvirt/**`

## Writing Conventions

- Use a two-digit sequence number and a concise topic slug in filenames, such
  as `01-architecture.md`.
- Begin each chapter with a data-flow diagram before discussing code entry points.
- Refer to code by stable function names and package paths instead of copying
  large source excerpts.
- Explain design rationale to maintainers, rather than teaching end users how
  to run commands.
- End each chapter with a checklist for changes to that stage.
