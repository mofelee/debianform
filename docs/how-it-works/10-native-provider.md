# 10. Native Providers and Resource Implementations

<p align="right"><strong>English</strong> | <a href="10-native-provider.zh.md">简体中文</a></p>

This chapter explains how `NativeProvider` turns graph nodes into real
observations and changes on supported managed targets. The provider is the
layer closest to the operating system.

## Data Flow

```text
graph.Node + prior state
  -> NativeProvider.Plan
  -> ProviderPlan{Action, Observed, Ownership}

engine.Step
  -> NativeProvider.Apply / Destroy
  -> observed map
```

A provider neither reads HCL nor writes state. It observes and changes remote
hosts only through `Runner`.

## Provider Interface

`Provider` defines:

- `Plan(ctx, node, prior)`: observe the remote resource and recommend an action.
- `Apply(ctx, step)`: execute create/update/delete-style actions and return
  observed state.
- `Destroy(ctx, step)`: destroy an orphaned managed resource.
- `RunOperation(ctx, operation)`: execute a graph operation.

The engine uses these results to organize the global plan and write state.

## Dispatch by Kind

Both `NativeProvider.Plan` and `Apply` dispatch on `node.Kind`. Current resource
coverage includes:

- File-like: `file`, `secret`, `systemd_unit`, `nftables_file`,
  `networkd_netdev`, and `networkd_network`.
- APT: `apt_source_file`, `apt_signing_key`, and `package`.
- Component artifacts: download, build, binary, file, archive, and CA
  certificate.
- System: directory, kernel module, and sysctl.
- Identity: group, user, group membership, and authorized key.
- Service.
- Docker package conflicts and Compose projects.

Every new kind must be considered in Plan, Apply, Destroy, and graph generation.

## Responsibilities of Plan

Provider planning begins by reading current host state. For example:

- File-like resources inspect path existence, type, SHA-256, owner, group, and
  mode.
- Packages inspect dpkg state.
- Services inspect enabled/running state.
- Groups and users inspect system databases.
- Docker Compose projects inspect Compose output or desired markers.

The provider then returns a `ProviderPlan`. Many resources ultimately follow
the Engine `Compare` rules for desired/prior/observed digests, while file-like
and similar resources compare file SHA, permissions, and write-only semantics
directly.

## File-Like Resources

`planFileLike` is one of the most important patterns:

- Ensure absent and file exists -> `delete`.
- Ensure absent and file does not exist -> no-op or absent in sync.
- File does not exist -> `create`.
- Content, owner, group, or mode differs -> `update`.
- Otherwise -> in sync.

`content_write_only` receives special treatment. The provider does not compare
plaintext by reading the remote content SHA; it relies on the prior desired
digest and observations of permissions and type. Content can therefore be
written without treating it as ordinary readable state.

`applyFileLike` writes content, owner, group, and mode. A systemd unit change is
followed by `systemctl daemon-reload`.

## Special Semantics of APT Source Files

`apt_source_file` supports `on_destroy`:

- `keep`: removing configuration or setting ensure absent favors forgetting
  state without restoring the original file.
- `restore`: deletion attempts to restore the content that existed before
  management.

Its plan/apply/destroy logic therefore handles observed original content,
owner, group, and mode beyond the behavior of an ordinary file.

## Destroy

`NativeProvider.Destroy` uses desired data from prior state to decide how to
remove a resource. Common behavior includes:

- File-like resource: remove the path.
- Directory: remove the directory while protecting an empty path and `/`.
- Package: run `apt-get remove`.
- Service: run `systemctl disable --now`.
- Docker Compose project: set state to absent and run a Compose-down style
  command.

`Destroy` applies only to orphaned managed resources. Ordinary ensure-absent
deletion usually uses `Apply`.

## RunOperation

`RunOperation` uses `operation.Host` as the remote target, then executes
`operation.CommandPreview`. It fails conservatively if Host is missing. The
address serves only as stable identity and diagnostic context, never for SSH
routing.

Consequently, a graph command preview is not merely display text; it is also
the executable command for the current native operation. New operations must
have executable, idempotent previews that contain no sensitive plaintext.

## Helper Conventions

Provider helpers perform tasks such as:

- Shell quoting.
- Reading path metadata and content.
- Writing file content.
- Computing the desired-content SHA.
- Generating package, service, and Docker commands.
- Normalizing modes.
- Extracting strings, booleans, and lists from desired maps.

Reuse these helpers when adding provider behavior instead of assembling unsafe
shell snippets independently.

## Observed Maps

Provider-returned observed data enters plan diffs and state sanitization. It
should contain enough information for later drift detection without including
sensitive plaintext that must not be persisted.

When recovery information truly must be stored, such as original APT source
content, confirm the resource's security boundary and redaction coverage.

## Design Boundaries

- A provider may execute remote commands but does not write state.
- A provider does not decide orphan policy; the engine does.
- A provider does not parse HCL or profiles.
- A provider returns actions and observations, while the engine and graph own
  global ordering and operation triggers.
- Provider commands should be idempotent to tolerate retries after a partial
  apply failure.

## Change Checklist

- New kind: update graph nodes, provider Plan/Apply/Destroy, tests, and goldens.
- New shell command: use `shellQuote` or stdin to prevent injection and secrets
  in command lines.
- New observed field: verify state sanitization and plan diffs cannot leak it.
- New operation: verify that its command preview is executable and idempotent.
- File-like logic change: focus tests on content, write-only, sensitive,
  mode/owner/group, and absent behavior.
