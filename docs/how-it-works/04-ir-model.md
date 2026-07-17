# 04. IR Data Model and Resource Boundaries

<p align="right"><strong>English</strong> | <a href="04-ir-model.zh.md">简体中文</a></p>

This chapter explains the responsibilities of `internal/core/ir`. The IR is
DebianForm's domain-layer model: it is no longer raw HCL, but it is not yet a
provider operation or state.

## Position in the Data Flow

```text
parser.Config
  -> merge.CompileWithOptions
  -> ir.Program
  -> graph.Compile
```

The parser and merge layers are upstream of the IR, and the graph layer is
downstream. The IR expresses what the user wants each host to become.

## Program

`ir.Program` is the complete compiled configuration:

- `Hosts`: desired state for each target host.
- `Variables`: variable-definition metadata.
- `Components`: component-template metadata.

`Program` contains no plan actions. Whether a resource is `create`, `update`,
or `no-op` cannot be known until the engine reads state and observed reality.

## HostSpec

`HostSpec` is the central IR structure. It contains:

- `Name`
- `Source`
- `Facts`
- `SSH`
- `State`
- `System`
- `Kernel`
- `Packages`
- `APT`
- `Files`
- `Secrets`
- `Directories`
- `Groups`
- `Users`
- `Systemd`
- `Services`
- `Nftables`
- `Docker`
- `Components`

These fields are organized by domain rather than low-level command. For
example, `APTSpec` represents repositories and source files without specifying
an `apt-get update` command. `SystemdSpec` represents unit content without
deciding when to run `daemon-reload`. Those execution details belong to the
graph and provider layers.

## SourceRef

Nearly every spec carries a `SourceRef`:

- `File`
- `Line`
- `Path`

It is used for:

- Locating compilation errors.
- Locating lifecycle errors.
- Displaying the source on a plan change.
- Stable assertions in golden tests.

When adding an IR field sourced from user configuration, preserve its source
whenever possible.

## LifecycleSpec

The central field of `LifecycleSpec` is currently `PreventDestroy`. It matters
in two places:

- The lifecycle is retained on graph nodes.
- The engine checks `prevent_destroy` when computing destroy or delete actions.

Lifecycle is resource semantics, not a provider-command detail. A provider must
not decide on its own to bypass `prevent_destroy`.

## Host Facts

In the DSL, target-platform facts are written as `platform.distribution`,
`platform.version`, `platform.architecture`, and `platform.codename`. The IR's
`HostFacts` currently continues to store detected results through the `System`
field, following the existing provider/state facts schema:

- hostname
- distribution
- version
- architecture
- codename
- detected_at

Facts may be declared by the user or injected after discovery in online mode.
Holding facts in IR lets later graph construction and component instantiation
make decisions from stable fields instead of invoking SSH throughout the code.

## Domain Specs Versus Provider Resources

An IR domain spec is not the same as a graph node:

- One `APTRepositorySpec` may expand into a signing-key file, repository source
  file, and APT cache-refresh operation.
- One `SystemdUnit` may expand into a file-like resource and trigger a daemon
  reload or service restart.
- `DockerSpec` expands into several repository, package, daemon-configuration,
  service, and Compose-plugin nodes.
- One component instance carries groups of domain resources and artifact
  resources.

This layering keeps the user DSL concise while allowing provider
implementations to share low-level resource models.

## ContentSummary

File-like resources commonly contain a `ContentSummary`. It records summary
information without exposing content directly, such as length, hash, or origin.
The graph, plan, and state layers continue enforcing the concrete redaction
policy.

The principle is that IR may carry content required for later execution, but
every path that serializes public output must pass a redaction check.

## SSHSpec and StateSpec

`SSHSpec` represents the connection target:

- `host`
- `port`
- `user`
- `identity_file`

`StateSpec` represents remote state and lock locations:

- `path`
- `lock_path`

These are host-level settings. The first online compilation phase must be able
to produce them, or DebianForm cannot connect to discover facts.

## Components in HostSpec

`HostSpec.Components` contains the output of component instances already
instantiated on the host. It retains the component name and the resource specs
produced by that instance.

The graph layer uses a component prefix when generating addresses, for example:

```text
host.<host>.components.<instance>....
```

The component instance name is therefore part of resource-address stability.

## Design Boundaries

- IR should be stable, JSON-serializable, and suitable for golden tests.
- IR must not include provider command previews, SSH commands, or remote
  observed state.
- IR may include desired content, with explicit awareness of which downstream
  outputs redact it.
- Graph-address stability depends on IR fields; change field defaults and
  sorting carefully.

## Change Checklist

- New IR field: update JSON tags, merge building, graph compilation, and goldens.
- HostSpec default change: check effects on offline plan, online plan, and state
  digests.
- New domain spec: define user semantics before deciding how graph nodes expand.
- Source-propagation change: check plan text, JSON, and error messages.
- Facts-field change: update fact discovery, state-fact persistence, and
  fact-dependent compilation logic.
