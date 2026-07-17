# Apply SSH Debugger Requirements

<p align="right"><strong>English</strong> | <a href="apply-debugger-requirements.zh.md">简体中文</a></p>

This document defines requirements for the interactive SSH debugger in
`dbf apply`. It lets users observe and control every SSH call DebianForm makes
on remote hosts in a GDB-like workflow: step one call, run several calls,
continue execution, inspect scripts and remote output, and retry after failure.

The feature is intended for troubleshooting and learning how DebianForm applies
changes. It is not the default interaction model for routine apply.

## Background

An ordinary DebianForm `apply` first produces an online plan and asks for user
approval before modifying remote hosts. During execution, progress logs show the
current host, resource address, action, and summary, but not the complete script
for every SSH call, and users cannot pause between remote calls.

The current apply flow is approximately:

```text
parse config
  -> discover runtime facts
  -> compile program
  -> compile resource graph
  -> online plan
  -> print plan
  -> confirm
  -> lock state
  -> persist facts
  -> re-plan
  -> read state
  -> execute resource/operation waves
  -> write state after successful steps
  -> unlock state
```

Many of these phases execute remote scripts over SSH:

- Runtime fact discovery reads hostname, distribution, version, architecture,
  and codename.
- Online planning reads remote state and observed state such as file hashes,
  package status, and service status.
- Apply executes provider-generated mutation scripts such as writing files,
  installing packages, restarting services, and running component scripts.
- After successful apply steps, remote state is written back.

Users want a debug mode that shows exactly which remote command each step runs
and offers GDB-like control:

- Execute 1 remote call at a time.
- Execute 5 or 10 remote calls at a time.
- Continue to completion.
- Inspect context and retry after failure.

## Goals

- Add an interactive SSH debug mode to `dbf apply`.
- Cover every remote SSH call in the apply lifecycle, including fact discovery,
  state lock/read/write, online plan probes, resource apply, operations,
  operation-output reads, and unlock.
- Before each remote call, show its host, phase, origin, and complete remote
  script or command.
- After each remote call, show exit result, duration, stdout, and stderr.
- Support GDB-style commands: `step`, `next N`, `continue`, `list/show`,
  `retry`, `quit`, and `help`.
- Support running several calls at once, such as `next 5` and `next 10`.
- Pause after failure to allow inspection, retry, or exit.
- Write debug output to stderr, keeping stdout for ordinary plan/apply output.
- Force serial execution in debug mode so interaction and output order remain
  understandable.

## Non-Goals

- Do not turn each line within a shell script into an independently pausable step.
- Do not implement breakpoint expressions by host, resource address, or script
  pattern in the first version.
- Do not implement watchpoints or variable inspection in the first version.
- Do not allow skipping a failed remote call and continuing in the first version.
- Do not change ordinary `dbf apply` behavior.
- Do not change the existing meaning of `dbf plan --debug`.
- Do not modify the plan JSON schema, HTML plan schema, or state format.

## Terminology

### Remote Call

One debugger "step" is one `engine.Runner` call:

- `Run(ctx, host, script)`
- `RunInput(ctx, host, remoteCommand, input)`
- `RunCommand(ctx, host, remoteCommand)`

Each ultimately executes a command on a remote host over SSH. Debug mode pauses
per Runner call, not per shell-script line.

### Debug Step

One debug step corresponds to one remote call. It contains:

- Call sequence number.
- Host.
- Phase, such as `discover facts`, `plan inspect`, `state write`, or
  `apply resource`.
- Resource or operation address, when present.
- Resource action and summary, when present.
- Runner method: `Run`, `RunInput`, or `RunCommand`.
- Remote command or script.
- Stdin payload, when present.
- Execution result: exit status, duration, stdout, stderr, and error.

### Debug Session

One `dbf apply --debug` invocation is a debug session. It begins before the
first remote SSH call and ends when apply completes, fails, the user quits, or
the context is canceled.

## CLI Entry Point

New option:

```bash
dbf apply --debug
```

Examples:

```bash
dbf apply -f examples/bbr.dbf.hcl --debug
dbf apply -f base.dbf.hcl -f host.dbf.hcl --host web1 --debug
dbf apply --debug --auto-approve
```

`--debug` has different meanings by subcommand:

- `dbf plan --debug`: display internal provider addresses in the plan.
- `dbf apply --debug`: enter the interactive SSH debugger.

`check` and `validate` do not support `--debug`. These commands must fail:

```bash
dbf check --debug
dbf validate --debug
```

Suggested error:

```text
--debug is only supported for plan and apply
```

## Relationship to Existing `plan --debug`

Existing `dbf plan --debug` only affects plan output. It exposes internal
provider addresses for troubleshooting graph-to-provider mappings. It does not
start an interactive session or alter execution.

The new `dbf apply --debug` is an interactive remote-call debugger. The two
commands reuse the flag but interpret it by subcommand:

- `plan --debug`: enrich plan presentation.
- `apply --debug`: control every SSH call during apply.

Documentation and help text must make this explicit so users do not assume
`apply --debug` only adds provider addresses.

## High-Risk Output Policy

`apply --debug` is an explicitly high-risk debug mode. It presents remote-call
details and lets users expand complete payloads and output, which may contain
secrets.

Default display rules:

- Remote commands and shell scripts are normally short and display in full.
- A short textual `RunInput` stdin payload displays in full.
- A long or non-printable-UTF-8 stdin payload displays only a summary and a hint
  to use `show stdin`.
- Short stdout/stderr displays in full.
- Long or non-printable-UTF-8 stdout/stderr displays only a summary and a hint
  to use `show stdout` or `show stderr`.

Potential content includes:

- Secret-file content.
- Sensitive variable values.
- SSH authorized keys.
- Component script bodies.
- State JSON.
- Sensitive data in remote command output.

The session must start with a prominent warning on stderr:

```text
dbf debugger: WARNING: apply --debug can print remote scripts, stdin payloads,
stdout, and stderr. Expanded output may contain secrets. Do not paste debugger
logs into issues, CI artifacts, or shared chat without review.
```

The first version does not add a separate `--debug-sensitive` option or
environment confirmation. Selecting `apply --debug` accepts the debug-output
risk. Long and binary content still begins with summaries and expansion hints
instead of flooding the terminal automatically.

Redaction behavior for ordinary plan, ordinary apply, persisted state, and plan
JSON/HTML must remain unchanged. Only stderr from explicit `apply --debug` may
expose raw content.

## Coverage

The debugger should cover every remote SSH call in the apply command lifecycle.

### Covered Phases

| Phase | Pauses | Description |
| --- | --- | --- |
| Fact discovery | Yes | Show scripts and output used to collect hostname, distribution, version, architecture, and codename. |
| State lock | Yes | Show lock-acquisition script and output. |
| State read | Yes | Show state-read script and output. |
| State write | Yes | Show state-write script and payload. |
| Online plan inspect | Yes | Show scripts and output that inspect observed state while planning. |
| Apply resource | Yes | Show scripts that actually mutate resources. |
| Run operation | Yes | Show operation commands or component scripts. |
| Operation output read | Yes | Show scripts and output used to read script outputs. |
| State unlock | Automatic | Show the unlock call without requiring confirmation, preventing locks from remaining after quit or failure. |

`state unlock` is the sole exception. It still prints its script and result in
full, but automatically executes on the deferred cleanup path without waiting
at a prompt.

### Local Phases Not Covered

These are not remote SSH calls and do not become debug steps:

- HCL parsing.
- Variable loading.
- Component merging.
- ResourceGraph compilation.
- Plan-document rendering.
- The ordinary user-confirmation prompt for apply.

## Concurrency Rules

Debug mode requires stable remote-call order and non-interleaved output:

- Serialize remote SSH calls under `--debug`.
- Set fact-discovery concurrency to 1.
- Use Engine apply options `Parallel: 1` and `PerHostParallel: 1`.
- Fail when the user supplies both `--debug` and `--parallel N` with `N > 1`.

Suggested error:

```text
--debug cannot be combined with --parallel greater than 1
```

Accept explicit `--parallel 1`.

## Debugger Interaction

Debugger prompt:

```text
(dbfdbg)
```

Before each remote call, print its details and wait for a command.

### Supported Commands

| Command | Alias | Description |
| --- | --- | --- |
| `step` | `s` | Execute the current remote call and pause before the next one. |
| `next N` | `n N` | Execute N remote calls and then pause. N must be at least 1. |
| `next` | `n` | Equivalent to `next 1`. |
| `continue` | `c` | Stop pausing until failure or apply completion; continue printing every call and result. |
| `list` | `l` | Print current remote-call details again. |
| `show` | None | Print current call details again; after failure, also print the last stdout/stderr. |
| `show stdin` | None | Expand complete stdin; long text as text and binary data as a hex dump. |
| `show stdout` | None | Expand complete stdout from the last execution; long text as text and binary data as a hex dump. |
| `show stderr` | None | Expand complete stderr from the last execution; long text as text and binary data as a hex dump. |
| `retry` | `r` | Available only after failure; rerun the failed call. |
| `quit` | `q` | Cancel apply and run required cleanup/unlock. |
| `help` | `h` | Print debugger command help. |

Empty Enter repeats the previous execution command. At session start, it is
equivalent to `step`.

### Command-Parsing Rules

- Commands are case-insensitive.
- Ignore leading and trailing whitespace.
- `N` in `next N` must be a positive decimal integer.
- An unknown command executes no remote call, prints a help hint, and waits again.
- `retry` outside a failed state prints
  `retry is only available after a failed remote call`.
- An expansion command for nonexistent content, such as `show stdin` on a call
  without stdin, prints `current call has no stdin`.

## Call-Detail Presentation

Suggested stderr format before a remote call:

```text
dbf debugger: #12 before remote call
  phase: apply resource
  host: web1
  address: host.web1.files.file["/etc/app.conf"]
  action: update
  summary: update file /etc/app.conf
  runner: RunInput
  remote command:
----- BEGIN remote command -----
set -eu
dest='/etc/app.conf'
mkdir -p "$(dirname "$dest")"
tmp="$(mktemp "${dest}.dbf-tmp.XXXXXX")"
trap 'rm -f -- "$tmp"' EXIT
cat > "$tmp"
install -o 'root' -g 'root' -m '0644' "$tmp" "$dest"
----- END remote command -----
  stdin: text, length=128, sha256=...
----- BEGIN stdin -----
...
----- END stdin -----
(dbfdbg)
```

Field rules:

- `phase` is required.
- `host` is required.
- `address` appears with resource or operation context; otherwise omit it.
- `action` appears with a resource action.
- `summary` appears when available.
- `runner` is required and is one of `Run`, `RunInput`, or `RunCommand`.
- `remote command` or `script` is required.
- `stdin` appears only for `RunInput`.

### Long-Content and Binary Presentation

Avoid flooding the terminal automatically. For stdin, stdout, and stderr:

- Display short printable UTF-8 text in full by default.
- For long printable UTF-8 text, display a summary, first-lines preview, and
  expansion hint by default.
- For non-UTF-8 content or content with clear control characters, display a
  summary and expansion hint without writing raw bytes.
- Expand complete content only after explicit `show stdin`, `show stdout`, or
  `show stderr`.
- Use a hex dump for expanded binary content; never write raw bytes directly.

Suggested defaults:

- Treat text above 8 KiB or 200 lines as long.
- Always summarize binary content first.

Long-text summary example:

```text
  stdin: text payload, length=24576, sha256=..., 612 lines
  preview:
----- BEGIN stdin preview -----
...
----- END stdin preview -----
  hint: type "show stdin" to print the full payload
```

Binary summary example:

```text
  stdin: binary payload, length=128, sha256=...
  hint: type "show stdin" to print a full hex dump
```

Binary expansion example:

```text
(dbfdbg) show stdin
----- BEGIN stdin hex -----
00000000  ...
----- END stdin hex -----
```

## Execution-Result Presentation

Suggested stderr format after a remote call succeeds:

```text
dbf debugger: #12 remote call succeeded in 348ms
  stdout:
----- BEGIN stdout -----
...
----- END stdout -----
  stderr:
----- BEGIN stderr -----
...
----- END stderr -----
```

On failure:

```text
dbf debugger: #12 remote call failed in 348ms
  error: remote command on web1 failed: exit status 1: ...
  stdout:
----- BEGIN stdout -----
...
----- END stdout -----
  stderr:
----- BEGIN stderr -----
...
----- END stderr -----
(dbfdbg failed)
```

Show empty stdout/stderr explicitly to avoid ambiguity:

```text
  stdout: <empty>
  stderr: <empty>
```

Long or binary stdout/stderr follows the same rule as stdin: show a summary and
hint first, then expand after `show stdout` or `show stderr`.

## Failure Handling

The debugger must pause after a remote-call failure.

Allowed in failed state:

- `show`: print call details and failed output again.
- `show stdin` / `show stdout` / `show stderr`: expand complete payload or output.
- `retry` / `r`: execute the same failed call again.
- `quit` / `q`: exit apply.
- `help` / `h`: display help.

Disallowed in failed state:

- `step`
- `next`
- `continue`

A failed call may already have changed the remote host partially. Skipping it
and continuing can make remote state and DebianForm state harder to reason about.
The first version provides no skip semantics.

If retry succeeds:

- Print the successful result.
- Leave failed state.
- Continue according to the control mode before retry:
  - When the user entered `retry`, pause before the next remote call.
  - A future extension may restore continue mode after failure; the first
    version does not require it.

## User Cancellation

After the user enters `quit`:

- Do not execute the current unexecuted remote call.
- Return a nonzero apply error.
- Do not roll back successful remote changes.
- Make a best-effort attempt to unlock acquired state locks.
- Print unlock scripts and results to stderr without waiting for a prompt.

Suggested error:

```text
apply cancelled by debugger
```

After Ctrl-C:

- Reuse existing context-cancellation behavior as far as possible.
- Registered deferred cleanup should still run.
- Unlock should still be attempted.

## Relationship to Apply Confirmation

`dbf apply --debug` retains the existing apply confirmation:

```text
Apply these changes? Type yes to continue:
```

unless the user supplied `--auto-approve`.

Because the debugger covers fact discovery and online plan probes, it begins
pausing before the initial plan is printed. This is intentional: users want to
see what remote information was collected.

Flow:

```text
dbf apply --debug
  -> debugger starts before facts discovery SSH call
  -> facts/probes are stepped
  -> unlocked preview plan is printed
  -> normal apply confirmation
  -> lock/re-plan are stepped
  -> locked execution plan is printed and re-confirmed if changed
  -> apply/state writes are stepped
```

## Runner Design Recommendation

Add a debugging wrapper around the existing `Runner`:

```go
type DebugRunner struct {
    Inner Runner
    Session *DebugSession
}
```

It implements the same interface:

```go
Run(ctx context.Context, host, script string) (Result, error)
RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error)
RunCommand(ctx context.Context, host, remoteCommand string) (Result, error)
```

`DebugRunner`:

- Constructs the debug step.
- Passes it to `DebugSession.BeforeCall` before execution.
- Invokes the inner runner.
- Passes the result to `DebugSession.AfterCall`.
- Enters a retry loop after failure.

`RunInput` must read `input` completely into memory:

- To display the payload.
- To execute the actual call.
- To retry after failure.

This increases memory use, but apply payloads are normally configuration files,
state JSON, or script text, which is acceptable for the first version. Support
for very large payloads may later add a size threshold and temporary-file
strategy.

## Debug-Context Labels

Runner calls need contextual labels so users know their origin.

Use a context helper in the engine package:

```go
type RemoteCallContext struct {
    Phase   string
    Host    string
    Address string
    Action  string
    Summary string
    Cleanup bool
}
```

Inject it before the call:

```go
ctx = WithRemoteCallContext(ctx, RemoteCallContext{
    Phase: "apply resource",
    Host: step.Host,
    Address: step.Address,
    Action: step.Action,
    Summary: step.Summary,
})
```

The debug runner reads this context. Without one, it still displays:

```text
phase: remote call
```

Primary labeling sites:

- Fact discovery: `discover facts`.
- SSH backend read: `state read`.
- SSH backend write: `state write`.
- SSH backend lock: `state lock`.
- SSH backend unlock: `state unlock`, with `Cleanup: true`.
- Provider plan: `plan inspect`.
- Resource apply/destroy: `apply resource`.
- Operation execution: `run operation`.
- Operation output read: `operation output read`.

If provider apply first executes a mutation script and then an observation
script, both SSH calls should appear in the debugger. Both may use phase
`apply resource` and retain the resource summary. For extra clarity, the second
summary may add `verify observed state`.

## Output Streams

All debugger content goes to stderr:

- Risk warning.
- Call details.
- Prompts.
- Stdout/stderr result blocks.
- Debugger help.
- Failure context.

Ordinary plan/apply content remains on stdout.

This preserves current script consumption of stdout and allows separate files:

```bash
dbf apply --debug > apply.out 2> debugger.log
```

## State and Lock Semantics

The debugger must not change DebianForm state semantics:

- State is still written immediately after each successful resource.
- Completed resources are not rolled back after failure.
- A later plan/apply still uses observed state to continue convergence.
- State locks remain host-level mutexes.

After debugger quit or failure, successfully written state remains. Resources
that did not complete successfully do not receive successful state entries.

Unlock must be attempted even after the user exits the debugger; deliberately
retaining the lock is forbidden.

## Example Session

### Single-Step Execution

```text
$ dbf apply -f examples/bbr.dbf.hcl --debug
dbf debugger: WARNING: apply --debug can print remote scripts, stdin payloads, stdout, and stderr. Expanded output may contain secrets.

dbf debugger: #1 before remote call
  phase: discover facts
  host: bbr1
  runner: Run
  script:
----- BEGIN script -----
set -eu
host_name="$(hostname 2>/dev/null || true)"
...
----- END script -----
(dbfdbg) step
dbf debugger: #1 remote call succeeded in 112ms
  stdout:
----- BEGIN stdout -----
hostname=bbr1
architecture=amd64
codename=trixie
----- END stdout -----
  stderr: <empty>

dbf debugger: #2 before remote call
  phase: state read
  host: bbr1
  runner: Run
  script:
...
(dbfdbg)
```

### Execute Five Steps

```text
(dbfdbg) next 5
dbf debugger: #2 remote call succeeded in 23ms
...
dbf debugger: #3 remote call succeeded in 31ms
...
dbf debugger: #4 remote call succeeded in 28ms
...
dbf debugger: #5 remote call succeeded in 44ms
...
dbf debugger: #6 remote call succeeded in 51ms
...

dbf debugger: #7 before remote call
  phase: apply resource
  host: bbr1
  address: host.bbr1.kernel.module["tcp_bbr"]
...
(dbfdbg)
```

### Retry After Failure

```text
dbf debugger: #12 remote call failed in 348ms
  error: remote command on web1 failed: exit status 100: ...
  stdout: <empty>
  stderr:
----- BEGIN stderr -----
E: Could not get lock /var/lib/dpkg/lock-frontend
----- END stderr -----
(dbfdbg failed) show
...
(dbfdbg failed) retry
dbf debugger: #12 remote call succeeded in 2.1s
...
dbf debugger: #13 before remote call
...
```

## Testing Requirements

### DebugRunner Unit Tests

Cover:

- `step` executes only one remote call at a time.
- Initial empty Enter is equivalent to `step`.
- `next 5` executes five consecutive calls, then pauses again.
- `continue` no longer waits for prompts but still prints every call and result.
- `list` / `show` print details again without executing.
- `RunInput` prints the remote command and short textual stdin in full by default.
- Long stdin displays a summary, preview, and `show stdin` hint by default.
- Non-UTF-8 stdin displays a binary summary by default; `show stdin` expands a
  hex dump.
- Long stdout/stderr displays a summary and expansion hint by default.
- `show stdout` / `show stderr` expands complete output.
- A failure enters the failed prompt.
- `retry` reruns the same call after failure.
- `step` / `next` / `continue` are disallowed after failure.
- `quit` returns an explicit cancellation error.
- Cleanup/unlock calls do not wait for a prompt.

### CLI Unit Tests

Cover:

- The `apply --debug` flag parses and enters debug mode.
- `plan --debug` retains provider-address output semantics and does not enter
  debug mode.
- `check --debug` fails.
- `validate --debug` fails.
- `apply --debug --parallel 2` fails.
- `apply --debug --parallel 1` is accepted.
- Usage text contains `--debug`.
- Debug output goes to stderr, not stdout.

### Integration-Test Recommendations

Use a fake runner for unit or light integration tests without real libvirt:

- The fake runner records call order.
- The fake runner returns predetermined stdout/stderr.
- Fake stdin simulates `step`, `next 2`, `continue`, `retry`, and `quit`.

A later real libvirt test can add a small case:

- Run `dbf apply --debug --auto-approve`.
- Feed `next 100` or `continue` through stdin.
- Assert command success.
- Assert stderr contains fact discovery, state lock, resource apply, and state write.

## Documentation Requirements

Implementation must update:

- `docs/cli.md`
- `docs/operations-runbook.md`
- `docs/how-it-works/08-apply-scheduler.md`
- CLI `usage()` output

Documentation must explain:

- `apply --debug` is high risk; expanding payloads or output may disclose secrets.
- Long text and binary payloads display only summaries and expansion hints by default.
- One debugger step is one SSH call, not one line of a shell script.
- `next N` executes several remote calls at once.
- Debug mode forces serialization and is unsuitable for performance tests.
- The difference between `plan --debug` and `apply --debug`.

## Acceptance Criteria

This command:

```bash
dbf apply -f examples/bbr.dbf.hcl --debug
```

- Pauses before the first fact-discovery SSH call.
- Displays the complete fact-discovery script.
- After `step`, executes the call and displays stdout/stderr.

This command:

```text
(dbfdbg) next 5
```

- Executes five remote calls in succession.
- Pauses before the next call after the fifth completes.

This command:

```text
(dbfdbg) continue
```

- Stops pausing.
- Continues printing each remote-call detail and execution result.

After a remote-call failure:

- The debugger pauses at the failed prompt.
- `show` displays the script and output again.
- `retry` reruns the same remote call.
- `quit` exits apply and makes a best-effort unlock attempt.

Ordinary commands are unaffected:

```bash
dbf apply -f examples/bbr.dbf.hcl
dbf plan -f examples/bbr.dbf.hcl --debug
```

- `apply` does not enter the debugger.
- `plan --debug` continues to affect only plan output.

## Future Extensions

After the first version, consider:

- Breakpoints by host, phase, address, or command substring.
- `until phase/address` to continue until a selected phase or resource.
- `set print stdin off` to suppress stdin printing for the session.
- `set print stdout limit N` to limit large-output display.
- `save log path` to write debug output to a file.
- `skip` to continue after an explicit failed-call skip, with very prominent
  state-risk labeling.
- Refactoring provider scripts for shell-line-level debugging.
