# DebianForm CLI

<p align="right"><strong>English</strong> | <a href="cli.zh.md">简体中文</a></p>

This document describes the primary capabilities, options, and common usage of
the `dbf` command-line tool.

`dbf` reads `.dbf.hcl` configuration files to validate, preview, apply, and
check supported Debian/Ubuntu managed targets.

New users should begin with the [quickstart](quickstart.md), which covers
preparing a root-SSH test host, writing a first configuration, `validate`,
online `plan`, `apply`, a second no-op `plan`, and `check`. See the
[operations runbook](operations-runbook.md) for stale locks, partial apply
failure, drift recovery, and common troubleshooting.

## Basic Rules

By default, `dbf` reads every `*.dbf.hcl` file in the current directory and
merges them in filename order. To read one or more explicitly selected files or
directories, repeat `-f path`:

```bash
dbf validate -f examples/bbr.dbf.hcl
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

On failure, a `dbf: ...` error is written to stderr and the command exits
nonzero. Deprecated component inputs produce warnings, but a warning alone does
not change the exit status.

## Command Overview

```text
dbf validate [-f path ...] [-var name=value] [-var-file path] [--host name]
dbf plan     [-f path ...] [-var name=value] [-var-file path] [--host name] [--format text|json] [--html file] [--debug] [--color auto|always|never] [--offline]
dbf apply    [-f path ...] [-var name=value] [-var-file path] [--host name] [--color auto|always|never] [--parallel n] [--lock-timeout duration] [--auto-approve] [--debug]
dbf check    [-f path ...] [-var name=value] [-var-file path] [--host name] [--color auto|always|never] [--lock-timeout duration]
dbf fmt      [-f path ...]
dbf variable inspect [-f path ...] [-var name=value] [-var-file path]
dbf component inspect [-f path ...] component_name
dbf version
dbf --version
dbf -version
dbf help
```

## Common Configuration Selection

Most commands that read configuration support:

| Option | Commands | Description |
| --- | --- | --- |
| `-f path` | `validate`, `plan`, `apply`, `check`, `fmt`, `variable inspect`, `component inspect` | Repeatable. A path may be a file or directory; a directory expands to its immediate `*.dbf.hcl` files. Without `-f`, read all `*.dbf.hcl` files in the current directory. |
| `--host name` | `validate`, `plan`, `apply`, `check` | Process only the named host. The command fails when it does not exist. |
| `-var name=value` | `validate`, `plan`, `apply`, `check`, `variable inspect` | Repeatable. Set a top-level `variable`. |
| `-var-file path` | `validate`, `plan`, `apply`, `check`, `variable inspect` | Repeatable. Load variable values from a `.dbfvars` or `.dbfvars.json` file. |
| `--color auto\|always\|never` | `plan`, `apply`, `check` | Control text color. JSON, HTML, and persisted logs never use ANSI. |

For a file, `-f` reads exactly that file. For a directory, it reads immediate
`*.dbf.hcl` children sorted by filename, without recursion. Multiple `-f`
options expand and parse in command-line order; the first occurrence wins for a
duplicate file.

```bash
dbf validate -f ../shared -f .
dbf plan -f ../shared/base.dbf.hcl -f ./hosts/prod.dbf.hcl --offline
```

Variable sources merge from lowest to highest precedence:

1. Environment variables `DBF_VAR_name=value`. Unknown variables are ignored so
   shell environments can be shared.
2. `debianform.dbfvars` and `debianform.dbfvars.json` in directories containing
   participating configuration files.
3. `*.auto.dbfvars` and `*.auto.dbfvars.json` in those directories, sorted by filename.
4. `-var-file path` options in command-line order.
5. `-var name=value` options in command-line order.

A later source overrides the same variable from an earlier source. With several
directories, automatic variable files load in order of each configuration
directory's first appearance, so values from later directories can override
earlier ones.

The declared type controls parsing of `-var` values. A `string` preserves the
raw string; `number`, `bool`, `list`, `map`, `object`, `tuple`, and similar types
use HCL/JSON literals. For a variable declared `sensitive = true`,
`-var token=@path` reads a file, `-var token=@-` reads stdin, and
`-var token=env:NAME` reads an environment variable. Errors and plan/state
output do not reveal sensitive sources or values.

A `host "<name>"` connects through `ssh <name>` by default and is managed as
root. Put `HostName`, `User`, `IdentityFile`, `ProxyJump`, port, and related
connection details in `~/.ssh/config`. Write an `ssh` or `state` block only when
the configuration must override the connection name, port, identity file, or
state path.

Default state paths:

```text
/var/lib/debianform/state/<host>.json
/var/lock/debianform/state/<host>.lock
```

## validate

`validate` parses local configuration, merges profiles, hosts, and components,
and validates the resulting HostSpec. It does not connect to remote hosts and
is suitable for pre-commit or CI checks.

```bash
dbf validate
dbf validate -f examples/bbr.dbf.hcl
dbf validate -f examples/bird2.dbf.hcl --host router1
dbf validate -f internal/core/testdata/fixtures/variable-cli.dbf.hcl \
  -var-file internal/core/testdata/fixtures/variable-prod.dbfvars \
  -var environment=staging
```

Successful output resembles:

```text
configuration is valid: 1 host(s)
```

Options:

| Option | Description |
| --- | --- |
| `-f path` | Repeatable. Read an explicitly selected file or immediate `*.dbf.hcl` children of a directory. |
| `--host name` | Validate only the named host. |
| `-var name=value` | Repeatable. Set a top-level variable. |
| `-var-file path` | Repeatable. Load a variable file. |

## plan

`plan` previews configuration changes. By default, it connects to targets over
SSH, discovers runtime facts, reads remote state, and compares observed state.
Use `--offline` for a local-only preview. Online `plan` writes discovery
progress for facts, state, and observations to stderr while the plan document
remains on stdout.

```bash
dbf plan -f examples/bbr.dbf.hcl --offline
```

Example text output:

```text
Plan:
  + host.bbr1.kernel.module["tcp_bbr"]
    create kernel module tcp_bbr

Summary: 3 create, 0 update, 0 delete, 0 no-op, 0 operations
```

Emit JSON:

```bash
dbf plan -f examples/bbr.dbf.hcl --format json --offline
```

Generate a static HTML plan:

```bash
dbf plan -f examples/files-plan-preview.dbf.hcl --html plan.html --offline
```

Debug provider addresses:

```bash
dbf plan -f examples/bbr.dbf.hcl --format json --debug --offline
```

Options:

| Option | Default | Description |
| --- | --- | --- |
| `-f path` | All `*.dbf.hcl` files in current directory | Repeatable. Read an explicitly selected file or immediate `*.dbf.hcl` children of a directory. |
| `--host name` | Empty | Generate a plan for only the named host. |
| `--format text\|json` | `text` | Emit text or JSON. See [plan format](plan-format.md) for JSON. |
| `--html file` | Empty | Write a static HTML plan. Valid only for `plan` and incompatible with explicit `--format`. |
| `--debug` | `false` | Display internal provider addresses in plan output. `apply --debug` has different semantics; see apply below. |
| `--offline` | `false` | Skip SSH, state, and runtime fact discovery for a local preview. Valid only for `plan`. |
| `-var name=value` | Empty | Repeatable. Set a top-level variable. |
| `-var-file path` | Empty | Repeatable. Load a variable file. |

Notes:

- Online `plan` requires root SSH access to each configured host and a supported
  Debian target. DebianForm does not support sudo, become, or non-root
  management connections; `ssh.user` must be omitted or set to `"root"`.
- `--offline` cannot resolve expressions that depend on remote runtime facts
  unless matching `platform` facts are already declared in configuration.
- Missing parent directories for `--html` output are created automatically.

## apply

`apply` first creates an unlocked online preview plan. After approval, it
acquires the remote state lock, rereads state and observations, and prints the
locked plan that will actually execute. Only then does it write state or mutate
the remote host. The state lock prevents concurrent apply processes from
writing one host's state. During execution, current host, resource address,
action, and heartbeat for long steps go to stderr; stdout remains reserved for
plan output.

```bash
dbf apply -f examples/bbr.dbf.hcl
```

By default, when a plan has changes or operations, `apply` asks:

```text
Apply these changes? Type yes to continue:
```

If the locked plan matches the approved preview, the original approval is
reused. If it differs, apply prints the new plan and change notice and asks
again. Rejecting this second prompt releases the lock, writes neither fact nor
resource state, and invokes no provider mutation.

Skip prompts:

```bash
dbf apply -f examples/bbr.dbf.hcl --auto-approve
```

Apply several hosts concurrently:

```bash
dbf apply --parallel 4 --auto-approve
```

Without explicit `--parallel`, online SSH phases process up to four hosts at once.

Debug remote SSH calls:

```bash
dbf apply -f examples/bbr.dbf.hcl --debug --auto-approve
```

`apply --debug` enters the interactive SSH debugger, intercepting every remote
call beginning with fact discovery and writing debug information to stderr. It
displays host, phase, resource or operation address, action, summary, remote
script or command, stdin summary, stdout/stderr summaries, and execution result.
Short text displays in full by default. Long and binary payloads show length,
SHA-256, preview, and expansion hints until the user explicitly enters
`show stdin`, `show stdout`, or `show stderr`.

Debugger commands:

```text
step
next 5
continue
show
show stdin
retry
quit
help
```

After a remote-call failure, the debugger enters `(dbfdbg failed)`. Use `show`
to inspect the failure, `retry` to rerun the same call, or `quit` to cancel
apply. Cleanup calls such as state unlock are still attempted after quit.

`apply --debug` is high risk: expanded output may contain secrets, remote
scripts, stdin payloads, stdout, or stderr. Use it only for troubleshooting and
do not paste complete debug logs into public issues. Remote calls are forced
serially; combinations such as `--debug --parallel 2` fail, while
`--parallel 1` is accepted.

Options:

| Option | Default | Description |
| --- | --- | --- |
| `-f path` | All `*.dbf.hcl` files in current directory | Repeatable. Read an explicitly selected file or immediate `*.dbf.hcl` children of a directory. |
| `--host name` | Empty | Apply only the named host. |
| `--parallel n` | `4` | Maximum hosts in concurrent online SSH phases, including fact discovery, lock/state reads, inspection, and apply. Must be at least 1 and is valid only for `apply`. |
| `--lock-timeout duration` | `5m` | Maximum wait for a remote state lock. Uses Go duration syntax such as `30s`, `2m`, or `10m`. |
| `--auto-approve` | `false` | Skip interactive approval for both plans; the locked execution plan is still printed. |
| `--debug` | `false` | Enter the interactive SSH debugger. Valid only for `apply`; forces serialization and cannot combine with `--parallel` above 1. |
| `-var name=value` | Empty | Repeatable. Set a top-level variable. |
| `-var-file path` | Empty | Repeatable. Load a variable file. |

When the preview has no changes or operations, `apply` prints it and exits
without acquiring the apply lock or prompting. Within each host, execution
continues in deterministic ResourceGraph order. Progress logs contain neither
resource desired content nor remote command stdout, preserving JSON/text plan
consumption.

## check

`check` generates an online plan to detect whether remote state has diverged
from configuration. Before checking, it acquires every target host's state lock
and retains them until state reads and all provider observations finish.
`--lock-timeout` controls the maximum wait for each lock. This prevents check
from interleaving state and managed-resource reads with other DebianForm
operations that honor the same lock.

`check` is read-only: it neither writes state nor persists host facts, applies
or deletes resources, or runs operations. Runtime fact discovery needed to
parse configuration occurs before the locked period, and external processes are
not constrained by DebianForm's state lock. Discovery progress goes to stderr;
the resulting plan goes to stdout.

```bash
dbf check -f examples/bbr.dbf.hcl
dbf check -f examples/bbr.dbf.hcl --host bbr1
```

Options:

| Option | Default | Description |
| --- | --- | --- |
| `-f path` | All `*.dbf.hcl` files in current directory | Repeatable. Read an explicitly selected file or immediate `*.dbf.hcl` children of a directory. |
| `--host name` | Empty | Check only the named host. |
| `--lock-timeout duration` | `5m` | Maximum wait for a remote state lock; locks remain held through the complete check. |
| `-var name=value` | Empty | Repeatable. Set a top-level variable. |
| `-var-file path` | Empty | Repeatable. Load a variable file. |

When remote state differs from configuration and a create, update, delete,
destroy, or operation exists, `check` exits nonzero and prints:

```text
dbf: remote state does not match configuration
```

The current error retains its historical wording; semantically it means remote
state and current configuration differ.

`check` does not support `--offline`, because it must read remote facts and state.

## fmt

`fmt` formats configuration files in place with the HCL formatter.

```bash
dbf fmt
dbf fmt -f examples/bbr.dbf.hcl
```

Example output:

```text
formatted 1 file(s)
```

Options:

| Option | Description |
| --- | --- |
| `-f path` | Repeatable. Format an explicitly selected file or immediate `*.dbf.hcl` children of a directory. Without `-f`, format all `*.dbf.hcl` files in the current directory. |

`fmt` rewrites files. Rerunning it on already formatted files prints
`formatted 0 file(s)`.

## component inspect

`component inspect` emits the component's public input API as JSON. Use it to
learn required parameters, types, defaults, and descriptions.

```bash
dbf component inspect -f examples/component-inputs.dbf.hcl reverse_proxy
```

Example output structure:

```json
{
  "name": "proxy",
  "inputs": [
    {
      "name": "listeners",
      "type": "list(object({name=string,port=number}))",
      "default": [],
      "nullable": false,
      "sensitive": false,
      "deprecated": "Use endpoints instead.",
      "description": "Listeners."
    },
    {
      "name": "token",
      "type": "string",
      "default": "<sensitive>",
      "nullable": true,
      "sensitive": true
    }
  ]
}
```

Options and arguments:

| Option/argument | Description |
| --- | --- |
| `-f path` | Repeatable. Read an explicitly selected file or immediate `*.dbf.hcl` children of a directory. Without `-f`, read all `*.dbf.hcl` files in the current directory. |
| `component_name` | Required name of the component to inspect; exactly one. |

If an input has `sensitive = true` and a default, the default appears as
`"<sensitive>"` instead of plaintext.

## variable inspect

`variable inspect` emits the public API for top-level variables as JSON. Use it
to view required external variables, types, defaults,
nullable/sensitive/ephemeral/deprecated flags, and descriptions.

```bash
dbf variable inspect -f examples/variable-secret-file.dbf.hcl
```

Options:

| Option | Description |
| --- | --- |
| `-f path` | Repeatable. Read an explicitly selected file or immediate `*.dbf.hcl` children of a directory. Without `-f`, read all `*.dbf.hcl` files in the current directory. |
| `-var name=value` | Repeatable. Supply a literal for variable evaluation during inspect. |
| `-var-file path` | Repeatable. Load values from a `.dbfvars` or `.dbfvars.json` file. |

If a variable has `sensitive = true`, its default appears as `"<sensitive>"`.

## version

Display version information:

```bash
dbf version
```

`version` emits detailed build information:

```text
dbf dev
commit: unknown
built: unknown
go: go1.x
platform: linux/amd64
```

Display only a one-line version:

```bash
dbf --version
dbf -version
```

Example:

```text
dbf dev
```

## help

Display built-in help:

```bash
dbf help
dbf --help
dbf -h
```

Running `dbf` without arguments also displays help and exits successfully.

## Option Restrictions

These options have meaning only on selected commands. Invalid combinations
fail. A few meaningless combinations may parse without affecting results; use
the table below.

| Option | Commands only | Description |
| --- | --- | --- |
| `--format` | `plan` | `validate`, `apply`, and `check` do not support structured output. |
| `--html` | `plan` | Cannot combine with explicit `--format`. |
| `--debug` | `plan`, `apply` | In `plan`, show internal provider addresses; in `apply`, enter the interactive SSH debugger. |
| `--color` | `plan`, `apply`, `check` | `auto` enables color only in a TTY without `NO_COLOR` / `TERM=dumb`; `always` forces it; `never` disables it. |
| `--offline` | `plan` | Offline plan preview. |
| `--parallel` | `apply` | Control multi-host concurrency in online SSH phases. |
| `--auto-approve` | `apply` | Skip apply approval. |
| `--lock-timeout` | `apply`, `check` | Timeout while waiting for remote state locks online. |

Invalid combinations fail directly, for example:

```bash
dbf plan -f examples/bbr.dbf.hcl --parallel 2
dbf check -f examples/bbr.dbf.hcl --offline
dbf plan -f examples/bbr.dbf.hcl --html plan.html --format json
dbf apply -f examples/bbr.dbf.hcl --debug --parallel 2
```

## Common Workflows

Validate configuration locally:

```bash
dbf validate -f examples/bbr.dbf.hcl
```

Preview a runnable example locally:

```bash
dbf plan -f examples/bbr.dbf.hcl --offline
```

Emit a machine-readable plan:

```bash
dbf plan -f examples/bbr.dbf.hcl --format json --offline
```

Apply to a remote host:

```bash
dbf apply -f examples/bbr.dbf.hcl --auto-approve
```

Check for remote drift:

```bash
dbf check -f examples/bbr.dbf.hcl
```

Format current-directory configuration:

```bash
dbf fmt
```

Inspect component inputs:

```bash
dbf component inspect -f examples/component-inputs.dbf.hcl reverse_proxy
```
