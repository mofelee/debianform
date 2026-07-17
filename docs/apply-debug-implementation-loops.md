# `apply --debug` Implementation Loops

<p align="right"><strong>English</strong> | <a href="apply-debug-implementation-loops.zh.md">简体中文</a></p>

This document divides the
[apply SSH debugger requirements](apply-debugger-requirements.md) into
verifiable development loops. Every loop must form a mergeable closed cycle:

- The loop's capability runs independently or can be verified with a fake runner.
- Unit tests cover the new semantics.
- Documentation and help text are updated when the capability becomes visible.
- `go test ./...` passes; add `go vet ./...` where necessary.

Status convention:

- `[ ]` Incomplete
- `[x]` Complete

## Current Baseline

- [x] `Runner` provides one abstraction for remote calls: `Run`, `RunInput`, and `RunCommand`.
- [x] `apply` prints an online plan and waits for approval before real execution.
- [x] `Engine.Apply` replans while holding the lock, presents and approves the
  actual plan before writing state or mutating the host, and writes state after
  every successful resource.
- [x] `plan --debug` displays internal `provider_address` values.
- [x] Progress logs display host, action, resource address, and summary.
- [x] Ordinary plan/state output has sensitive-value redaction constraints.

## Loop 1: CLI Entry Point and Semantic Boundary (Complete)

Goal: make `dbf apply --debug` a valid entry point while preserving the existing
meaning of `plan --debug`.

Scope:

- `plan --debug` continues to display only provider addresses.
- `apply --debug` enters debug mode; this loop may initially print only a
  "debug mode enabled" placeholder.
- `check --debug` and `validate --debug` fail.
- `apply --debug --parallel 2` fails; `--parallel 1` is accepted.
- Usage/help text explains the different meanings of `--debug` for `plan` and
  `apply`.

Deferred:

- Do not intercept SSH calls.
- Do not implement an interactive prompt.
- Do not print scripts or payloads.

Code:

- [x] Adjust flag validation in `cmd/dbf/main.go` to support `--debug` for both
  `plan` and `apply`.
- [x] Add debug-mode options to the apply branch without changing normal apply.
- [x] Require `parallel <= 1` in debug mode.
- [x] Update usage to `dbf apply ... [--debug]`.

Tests:

- [x] CLI tests confirm `apply --debug` parses.
- [x] CLI tests confirm `plan --debug` still emits `provider_address`.
- [x] CLI tests confirm `check --debug` and `validate --debug` fail.
- [x] CLI tests confirm `apply --debug --parallel 2` fails.

Acceptance:

```bash
go test ./cmd/dbf
go test ./...
```

## Loop 2: DebugRunner Skeleton and Noninteractive Recording Mode (Complete)

Goal: implement a `DebugRunner` wrapper around `Runner` that initially records
and prints remote-call details noninteractively.

Scope:

- `DebugRunner` implements `Runner`.
- Intercept `Run`, `RunCommand`, and `RunInput`.
- Before every call, print sequence number, host, runner method, and remote
  command or script.
- After every call, print success/failure, elapsed time, and stdout/stderr summary.
- For `RunInput`, read input into memory before passing it to the inner runner.
- Write debug output to stderr.

Deferred:

- Do not implement prompts.
- Do not implement `step`, `next`, or `continue`.
- Do not implement retry.
- Do not integrate into the complete real apply path yet; use fake-runner unit tests.

Code:

- [x] Add `internal/core/engine/debug_runner.go` or an equivalent file.
- [x] Add `DebugSession` for sequence numbers, output writer, and result formatting.
- [x] Display short text payload/stdout/stderr in full.
- [x] Display `<empty>` for empty stdout/stderr.
- [x] Retain `RunInput` payload bytes so execution and later display are identical.

Tests:

- [x] Fake-runner tests verify forwarding of `Run`, `RunCommand`, and `RunInput`.
- [x] Tests assert host, runner, script/command, stdout, and stderr appear in output.
- [x] Tests assert the `RunInput` payload reaches the inner runner and can be displayed.
- [x] Tests assert the original error is returned and a failed result is printed.

Acceptance:

```bash
go test ./internal/core/engine -run Debug
go test ./...
```

## Loop 3: Long-Content and Binary-Output Policy (Complete)

Goal: provide readable display rules so long payloads and binary content do not
flood the terminal automatically.

Scope:

- Display short, printable UTF-8 text in full by default.
- For long text, display length, SHA-256, line count, preview, and expansion hint.
- For non-UTF-8 data or data with obvious control characters, display a binary
  summary and expansion hint by default.
- Implement underlying formatting for `show stdin`, `show stdout`, and
  `show stderr`, initially called directly from tests.
- Render expanded binary data as a hex dump instead of writing raw bytes to the terminal.

Deferred:

- Do not parse interactive prompt commands.
- Do not wait for user input during real apply.

Code:

- [x] Add payload classification for short text, long text, and binary.
- [x] Compute SHA-256, byte length, and line count.
- [x] Generate previews, with a suggested threshold of 8 KiB or 200 lines.
- [x] Add full expansion rendering with hex dumps for binary data.

Tests:

- [x] Short text displays in full by default.
- [x] Text above the threshold displays only a summary and preview by default.
- [x] Non-UTF-8 payloads display only a binary summary by default.
- [x] `show stdin` rendering emits complete text or a complete hex dump.
- [x] stdout/stderr use the same policy.

Acceptance:

```bash
go test ./internal/core/engine -run 'Debug|Payload'
go test ./...
```

## Loop 4: Interactive Prompt and step/next/continue/show/quit (Complete)

Goal: implement GDB-style interactive control without failure retry yet.

Scope:

- Use `(dbfdbg)` as the debug prompt.
- Support `step` / `s`.
- Support `next N` / `n N`, for example `next 5` and `n 10`.
- Support `continue` / `c`.
- Support `list` / `l` / `show` to print current call details again.
- Support `show stdin`, `show stdout`, and `show stderr` to expand content.
- Support `quit` / `q` to cancel apply.
- Initially, empty Enter means `step`; afterward it repeats the previous
  execution command.

Deferred:

- Do not retry failures.
- Do not automatically allow cleanup calls; handle that in the next loop.
- Do not integrate all apply phases yet; verify with DebugRunner unit tests.

Code:

- [x] Add an input reader to `DebugSession`.
- [x] Implement command parsing and the state machine.
- [x] Let `BeforeCall` decide whether to wait for a prompt from the current mode.
- [x] Continue mode still prints call details and results without waiting for input.
- [x] Quit returns an explicit cancellation error such as
  `apply cancelled by debugger`.

Tests:

- [x] Fake stdin with `step` executes one call, then waits again before the next.
- [x] Fake stdin with `next 5` allows five consecutive calls.
- [x] Fake stdin with `continue` never waits on subsequent calls.
- [x] Fake stdin with `show stdin` expands the current payload without executing.
- [x] Fake stdin with `quit` returns cancellation without executing the current call.
- [x] An unknown command does not execute and prints help.

Acceptance:

```bash
go test ./internal/core/engine -run Debug
go test ./...
```

## Loop 5: Integrate the Complete Apply Path (Complete)

Goal: make `dbf apply --debug` wrap the real SSH runner and cover facts, online
plan, lock, replan, apply, and state writes.

Scope:

- Allow `loadOnlineProgramWithProgress` or its caller to accept a debug-wrapped runner.
- Use the debug runner for fact discovery.
- Use the debug runner for online plan.
- Use the debug runner for apply lock/read/write/replan/provider apply/operations.
- Write debug output to stderr.
- Fix fact-discovery and engine plan/apply concurrency at 1 in debug mode.
- Print a high-risk warning on startup.

Deferred:

- Do not label phase/address precisely yet; the next loop does that.
- Do not implement retry.
- Cleanup/unlock may initially prompt like ordinary calls; correct it in the next loop.

Code:

- [x] Create `DebugSession` in the CLI apply branch.
- [x] Wrap `SSHRunner` with `DebugRunner` while retaining the underlying
  `SSHRunner.Close`.
- [x] Pass `factsParallel=1` in debug mode.
- [x] Pass `coreengine.Options{Parallel: 1, PerHostParallel: 1}` in debug mode.
- [x] Leave normal apply behavior unchanged when debug is disabled.

Tests:

- [x] CLI/fake-runner tests find the debug warning on stderr for `apply --debug`.
- [x] Tests confirm stdout remains the ordinary plan/apply body in debug mode.
- [x] Tests confirm debug mode sets concurrency to 1.
- [x] Tests confirm normal apply emits no debug content.

Acceptance:

```bash
go test ./cmd/dbf
go test ./internal/core/engine
go test ./...
```

Manual verification:

```bash
dbf apply -f examples/bbr.dbf.hcl --debug --auto-approve
```

## Loop 6: Remote-Call Context Labels (Complete)

Goal: show the originating phase, resource address, action, and summary on every
debug step.

Scope:

- Label fact discovery as `discover facts`.
- Label SSH backend calls as `state lock`, `state read`, `state write`, and
  `state unlock`.
- Label provider planning as `plan inspect`.
- Label resource apply/destroy as `apply resource`.
- Label operations as `run operation`.
- Label operation-output reads as `operation output read`.
- Without context, display `phase: remote call`.

Deferred:

- Do not alter provider script content.
- Do not split shell scripts into line-level steps.

Code:

- [x] Add `RemoteCallContext` and `WithRemoteCallContext` / getter.
- [x] Inject context at fact, backend, engine execute, and provider plan/apply entry points.
- [x] Have `DebugRunner` read and print phase/address/action/summary from context.
- [x] Mark state unlock as cleanup for automatic allowance in the next loop.

Tests:

- [x] Tests with injected context display phase/address/action/summary.
- [x] Tests without context fall back to `remote call`.
- [x] Tests cover state read/write/lock/unlock phases.
- [x] Tests cover apply-resource and operation phases.

Acceptance:

```bash
go test ./internal/core/engine -run 'Debug|RemoteCallContext'
go test ./...
```

## Loop 7: Automatic Cleanup and Failure Retry (Complete)

Goal: complete failure and exit paths so quitting does not leave a state lock,
and support retrying failed calls.

Scope:

- Print cleanup calls such as `state unlock` in full without prompting.
- After quit, execute no more ordinary calls but still attempt cleanup.
- Enter `(dbfdbg failed)` after a remote-call failure.
- At the failed prompt, support `show`, `show stdin`, `show stdout`,
  `show stderr`, `retry`, `quit`, and `help`.
- `retry` reruns the same remote call with the same script/stdin payload.
- Disallow `step`, `next`, and `continue` while failed.

Deferred:

- Do not support skip.
- Do not support breakpoints.

Code:

- [x] `DebugRunner` retains the last call and result after a failure.
- [x] Enter a retry loop after failure.
- [x] `RunInput` retry reuses the in-memory payload.
- [x] Cleanup bypasses the prompt while still printing details and results.
- [x] Cleanup can execute after quit or cancellation.

Tests:

- [x] A fake runner fails once and succeeds on retry.
- [x] Fake-runner retry receives the same stdin payload.
- [x] `step`, `next`, and `continue` at the failed prompt do not execute.
- [x] Ordinary calls do not execute after quit, but cleanup does.
- [x] Cleanup never waits for a prompt.

Acceptance:

```bash
go test ./internal/core/engine -run Debug
go test ./...
```

## Loop 8: Documentation, Real-Scenario Acceptance, and Completion (Complete)

Goal: deliver the capability as a complete user feature by updating user docs
and running at least one end-to-end verification.

Scope:

- Update CLI documentation.
- Update the operations runbook.
- Update the apply-scheduler how-it-works chapter.
- Add a real or fake integration scenario for `apply --debug`.
- Confirm debug mode preserves existing default sensitive-value redaction tests.

Deferred:

- Do not implement breakpoints.
- Do not implement line-level shell debugging.
- Do not implement file logging.

Code:

- [x] Document `apply --debug` in `docs/cli.md`.
- [x] Add debug-apply troubleshooting to `docs/operations-runbook.md`.
- [x] Add the debug runner's position in apply to
  `docs/how-it-works/08-apply-scheduler.md`.
- [x] If useful, add a short entry point in README or the user manual.

Tests:

- [x] `make test`.
- [x] `go vet ./...`.
- [x] When the environment permits, run one libvirt case with
  `--debug --auto-approve` and `continue`.
- [x] Manually verify long text and binary payloads do not flood the terminal
  automatically and `show stdin` expands them.

Acceptance:

```bash
make test
go vet ./...
test -z "$(gofmt -l $(git ls-files '*.go'))"
```

Optional real verification:

```bash
dbf apply -f examples/bbr.dbf.hcl --debug --auto-approve
```

At the prompt, enter:

```text
next 5
continue
```

Expected:

- The debugger presents remote calls starting with fact discovery.
- After `next 5`, it pauses before the sixth remote call.
- After `continue`, it no longer pauses.
- No state lock remains after apply succeeds.

## Future Enhancement Candidates

These capabilities are excluded from the first-version loops:

- Breakpoints by host, phase, address, or command substring.
- `until phase/address` to continue to a selected phase or resource.
- `set print stdin off` to suppress stdin display for the session.
- `set print stdout limit N` to adjust the long-output threshold dynamically.
- `save log path` to write debug output to a file.
- `skip` to continue after a failed call, requiring very explicit state-risk warnings.
- Line-level shell debugging, which requires structured provider-script refactoring.
