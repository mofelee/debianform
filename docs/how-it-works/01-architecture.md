# 01. Overall Architecture and Command Lifecycle

<p align="right"><strong>English</strong> | <a href="01-architecture.zh.md">简体中文</a></p>

This chapter explains the main `dbf` command path, from accepting arguments
through parsing configuration, compiling IR, generating a resource graph, and
either producing a plan or applying changes. Later chapters examine the parser,
merge, graph, plan, engine, and provider layers individually.

## The Model in One Sentence

DebianForm's main pipeline turns user declarations into an observable,
comparable, and executable resource graph:

```text
CLI flags
  -> parser.Config
  -> ir.Program
  -> graph.ResourceGraph
  -> engine.Plan or plan.Document
  -> provider/backend executes against remote hosts
```

`plan --offline` does not connect to hosts and treats the resource graph as
entirely pending creation. Online `plan`, `apply`, and `check` use SSH to read
facts, remote state, and observed state before computing actual actions.

## Command Entry Point

The main entry point is `cmd/dbf/main.go`:

- `main` only calls `run(os.Args[1:])`, prints errors to stderr, and exits.
- `run` dispatches on the first argument to `version`, `fmt`, `component`,
  `variable`, or a configuration command.
- The configuration commands `validate`, `plan`, `apply`, and `check` all enter
  through `runConfigCommand`.

This structure keeps command dispatch in the CLI layer. The parser, merge,
graph, and engine layers do not interpret command-line arguments directly.

## Shared Preparation for Configuration Commands

`runConfigCommand` does three things:

1. Defines and parses flags.
2. Uses `configFiles` to decide which `.dbf.hcl` files to read.
3. Calls `runConfigWorkflow` to execute the actual workflow.

The `configFiles` rules are straightforward:

- If one or more `-f` options are supplied, read those sources in command-line
  order.
- A `-f` source may be a file or directory. A directory expands to its immediate
  `*.dbf.hcl` files, sorted by filename, without recursion.
- Without `-f`, find all `*.dbf.hcl` files in the current working directory and
  sort them by filename.
- Return an error if no configuration files are found.

Variable inputs are also collected in the CLI layer, but their types are
normalized in the parser. The CLI supports:

- `DBF_VAR_` environment variables.
- Default variable files: `debianform.dbfvars` and `debianform.dbfvars.json`.
- Automatic variable files: `*.auto.dbfvars` and `*.auto.dbfvars.json`.
- Explicit `-var-file` options.
- Explicit `-var name=value` options.

The entry point is `parseConfigWithExternalValues`. It first parses variable
declarations with `SkipTopLevel`, collects external values, and then passes
those values back to the parser for a complete parse.

## `validate`

The purpose of `validate` is to confirm that local configuration compiles into
valid IR. It does not connect to hosts or read state.

Data flow:

```text
files + vars
  -> parseConfigWithExternalValues
  -> compileValidationProgram
  -> merge.CompileWithOptions(ValidateRuntimeTemplates: true)
```

`ValidateRuntimeTemplates` means that runtime templates such as components do
not instantiate real remote artifacts, but their structure, inputs, and
assertions are validated as far as possible. This supports fast local failure.

`validate` does not accept `--format`. On success, it prints only the host count.

## `plan --offline`

An offline plan does not connect to a target. It builds a resource graph using
only facts and static information declared in configuration, then renders every
node as `create`.

Data flow:

```text
parser.Config
  -> merge.CompileWithOptions
  -> graph.Compile
  -> plan.New
  -> PrintText/PrintJSON/PrintHTML
```

This path does not call `engine.Plan`, so it neither reads state nor knows
whether resources already exist on the target.

If the configuration depends on runtime facts, for example selecting a
component artifact by architecture without declaring `platform.architecture`,
the offline plan fails and asks the user to use an online plan or declare the
facts explicitly.

## Online `plan`

An online plan first connects to the host to discover facts, then recompiles
the program.

Data flow:

```text
parser.Config
  -> compileProgram(SkipComponents: true)
  -> SSHRunner
  -> DiscoverProgramFacts
  -> compileProgram(HostFacts: facts)
  -> graph.Compile
  -> engine.Plan
  -> engine.Plan.Document
```

This involves an important two-phase compilation:

1. The first phase uses `SkipComponents` to compile basic host and SSH/state
   configuration, which identifies the hosts to connect to.
2. After discovering facts, the second phase injects them through the compile
   options and instantiates components and resources that depend on those facts.

The resulting `engine.Plan` reads remote state and asks providers to observe
the actual host. Its actions have online semantics: `create`, `update`,
`delete`, `adopt`, `forget`, `destroy`, `no-op`, and so on.

## `check`

`check` shares the initial path with online `plan` and also prints plan text.
The difference is:

- If `engine.Plan` contains any resource or operation step, `check` returns an
  error.
- If there are no changes, `check` succeeds.

Therefore, `check` detects drift without making changes.

## `apply`

`apply` first generates an online plan and prints it for user approval. After
approval, it does not execute that previously printed object directly; it calls
`engine.Apply`. Inside `Engine.Apply`:

1. Acquire the state lock for the target host.
2. Call `Engine.Plan` again.
3. While still holding the lock, print the actual execution plan. In interactive
   mode, ask for approval again if it differs from the approved preview.
4. After approval, persist the discovered facts.
5. Split the resource graph into dependency-based execution waves.
6. Ask providers to execute resource steps and operations.
7. Write state after every successful resource step.

Replanning ensures that apply acts on the latest state and observed reality
after taking the lock. The actual execution plan is displayed and approved
before any state write or provider mutation, so the user never approves only
an outdated preview while new actions execute silently.

## `fmt`

`fmt` is a special command. It first calls `loadProgram` to confirm that the
configuration parses and compiles, then rewrites the input with
`hclwrite.Format`. Formatting is therefore not a pure text operation;
semantically invalid configuration is not formatted.

## Inspect Commands

Both `component inspect` and `variable inspect` produce machine-readable output:

- `component inspect` parses configuration, compiles component-template input
  definitions, and emits JSON.
- `variable inspect` supports `AllowMissingVariables` and `SkipTopLevel` to list
  variable definitions and defaults.

Sensitive defaults appear as `"<sensitive>"` to prevent disclosure in inspect
output.

## Design Boundaries

- The CLI layer owns flags, file selection, output formats, and user approval.
- The parser layer owns HCL, variables, locals, expressions, and the value model.
- The merge layer owns profile/component/host merging and IR validation.
- The graph layer expands IR into resource nodes and dependencies.
- The plan layer owns only plan documents and presentation.
- The engine layer owns online state, observations, actions, and apply scheduling.
- The provider/backend layer interacts with remote hosts.

When adding a feature, do not cross these boundaries. For example, a provider
must not parse HCL, the parser must not know the systemd reload command, and the
plan layer must not decide whether a resource exists.

## Change Checklist

- New CLI flag: ensure it is consumed only in `cmd/dbf/main.go` and passed to
  the appropriate internal layer.
- New command: update `run`, usage text, tests, and documentation.
- Plan/apply behavior change: consider offline plan, online plan, check, and
  apply paths.
- Variable-input change: add CLI tests, parser variable tests, and sensitive
  error cases.
- Output-format change: add text, JSON, or HTML goldens/assertions and verify
  that sensitive values cannot leak.
