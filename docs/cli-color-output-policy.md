# DebianForm CLI Color Output and Logging Policy

<p align="right"><strong>English</strong> | <a href="cli-color-output-policy.zh.md">简体中文</a></p>

This document records the requirements discussion for CLI and log color. The
conclusion is that color is appropriate for human-facing terminal output,
especially risk that users must scan quickly in `plan` and `apply`. It is not
appropriate in structured output, persisted logs, or default CI output.

## Background

DebianForm output serves two kinds of use case:

- People read `plan`, `apply`, and errors in a terminal.
- Programs, CI, and audit workflows consume JSON, log files, and command output.

Color helps users identify dangerous actions quickly, but ANSI escape sequences
pollute machine-readable output and log files. Color must therefore remain a
presentation-layer feature and never become semantics.

## Goals

- Make create, update, delete, warning, error, and high-risk actions easier to
  distinguish in human-readable terminal output.
- Color deletion notices by risk category.
- Keep JSON, state, persisted logs, and default CI output free of ANSI escape
  sequences.
- Support `NO_COLOR` and an explicit color-control option.
- Preserve complete semantics in environments without color.

## Non-Goals

- Do not color every log line uniformly.
- Do not put color in plan JSON, state, debug logs, or remote log files.
- Do not make color the sole indicator of behavior.
- Do not create new disclosure risks for secrets, sensitive values, or command
  previews.

## Policy by Output Type

| Output location | Colored by default | Rationale | Requirement |
| --- | --- | --- | --- |
| `dbf plan` text | Yes, TTY only | Users need to scan change types and deletion risks quickly. | Also display a textual category such as `delete behavior: destructive`. |
| Pre-execution plan in `dbf apply` | Yes, TTY only | Must match `plan`; risk matters most before execution. | Use the same color rules as `plan`. |
| Progress during `dbf apply` | Sparingly, TTY only | Excess color reduces readability. | Color only warnings, errors, dangerous deletion, and notes requiring attention. |
| Post-execution `dbf apply` summary | Yes, TTY only | Helps review what actually ran. | Retain the deletion-behavior legend if deletions occurred. |
| `dbf check` text | May be colored, TTY only | Color helps scan drift, errors, and warnings. | Drift types must have textual descriptions. |
| `dbf validate` text | May be colored, TTY only | Warnings and errors can use color. | Successful output needs little color. |
| `--format json` | No | Machine-readable output cannot contain ANSI. | Express risk and classification through structured fields. |
| HTML plan | Yes, with CSS | HTML is a presentation format. | Use CSS classes, never ANSI. |
| Persisted log files | No | Supports searching, auditing, and copying. | Forbid ANSI by default; any future support must be explicit. |
| Non-TTY CI output | No | Defaults should be stable and grep-friendly. | An explicit option may force color. |
| Debug log | No | Debug logs prioritize diagnostics and redaction. | Add no ANSI and disclose no sensitive content. |

## Color-Control Interface

Provide one consistent option:

```text
--color=auto|always|never
```

Semantics:

- `auto`: the default. Enable color only when stdout/stderr is a TTY and color
  has not been disabled.
- `always`: force color, for example when a user intentionally retains color in
  CI logs.
- `never`: force color off.

Environment variables:

- In `auto` mode, a present and non-empty `NO_COLOR` disables color.
- In `auto` mode, `TERM=dumb` disables color.
- If `CLICOLOR` or `CLICOLOR_FORCE` is supported later, the CLI manual must
  define its precedence relative to `--color`.

Recommended precedence:

1. Explicit `--color=never` disables color.
2. Explicit `--color=always` enables color.
3. `--color=auto` honors `NO_COLOR`, `TERM=dumb`, and TTY detection.

## Color Semantics

Color only assists scanning and must always accompany textual semantics.

| Semantic category | Suggested color | Example |
| --- | --- | --- |
| create / add | Green | Creating a resource or successfully adopting one. |
| update / change | Yellow | Content or attribute changes. |
| delete / destroy | Classified by deletion behavior | Do not use one uniform red. |
| forget | Gray | Stop managing without changing the remote resource. |
| restore-original | Blue | Restore original content retained in state. |
| remove-managed-artifact | Yellow | Delete a file or artifact written by DebianForm. |
| destructive | Red | May delete data, accounts, packages, or directories, or stop services. |
| external-side-effect | Purple | Triggers an extra reload, restart, activation, update, or similar action. |
| warning | Yellow | Configuration can run but deserves attention. |
| error | Red | Command failure or invalid configuration. |
| note / hint | Cyan or default | For example, group membership applies after signing in again. |

Deletion-behavior colors follow
`docs/delete-behavior-diagnostics-plan.md`. A general action color must not
override the deletion-behavior color, or all deletions appear to carry the same
risk.

## Logging Color Boundary

Two meanings of "log" must be distinguished:

- Terminal progress logs: short status lines shown during `apply`.
- Persistable or transferable logs: CI logs, debug logs, file logs, and remote
  command output.

Terminal progress may use color sparingly, only for risk and result state such
as warnings, errors, and destructive deletion. Ordinary execution lines,
remote command output, and debug information should not be colored by default.

Persistable or transferable logs remain uncolored by default because:

- ANSI interferes with grep, diffs, audits, and copying into issues.
- Other systems may parse logs again.
- Color provides no additional machine semantics.
- Escape sequences can complicate secret redaction, error locations, and golden
  tests.

## Implementation Requirements

- Centralize color in the CLI renderer or terminal-writer layer. Providers, the
  engine, and plan JSON must not construct ANSI strings.
- Retain behavior fields such as `action`, `delete_behavior`, and `delete_risk`
  in structured plans, then let renderers choose colors.
- Test goldens use uncolored output by default.
- Color tests cover only renderers, avoiding broad business-logic dependence on
  ANSI.
- With color disabled, text output must still include symbols, category names,
  and explanations.
- HTML plans represent color semantics through CSS classes without reusing ANSI
  logic.

## Verifiable Implementation Loops

### Loop 1: CLI Color-Control Foundation (Implemented)

Scope:

- Add `--color=auto|always|never`.
- In `auto`, support TTY detection, `NO_COLOR`, and `TERM=dumb`.
- Let options control ANSI emission from the text plan renderer.
- Add no ANSI to JSON, HTML, state, or debug logs.

Acceptance:

- `dbf plan --offline --color=always` text contains ANSI.
- `dbf plan --offline --color=never` text contains no ANSI.
- Default `auto` output from `NO_COLOR=1 dbf plan --offline` contains no ANSI.
- `dbf plan --offline --format json --color=always` remains ANSI-free JSON.
- Existing uncolored goldens do not change because default `auto` enables color
  in a non-TTY environment.

### Loop 2: Structured Deletion-Behavior Fields (Implemented)

Scope:

- Add `delete_behavior`, `delete_notes`, and `delete_risk` to `plan.Change`.
- Emit them only for delete/destroy/forget-style actions.
- Cover initial core paths including BBR/sysctl, APT source-file keep/restore,
  files, directories, packages, systemd/nftables, and operation side effects.

Acceptance:

- A BBR deletion plan in JSON contains
  `delete_behavior = "remove-managed-artifact"`.
- APT source-file keep and restore receive distinct JSON classifications.
- Non-deletion actions omit deletion-behavior fields.
- Sensitive content never enters `delete_notes`.

### Loop 3: Text and HTML Deletion Notices (Implemented)

Scope:

- Text plan/apply deletion entries display `delete behavior` and `note`.
- When deletion exists, display a legend and documentation path at the bottom.
- Add deletion-behavior badges and a legend to HTML plans.

Acceptance:

- A BBR deletion plan clearly states that only the persistent sysctl file is
  removed and the runtime value is not restored.
- Text and HTML distinguish destructive, external-side-effect,
  restore-original, and forget.
- The deletion legend is absent when there are no deletion actions.

### Loop 4: Apply/Check Progress Color Boundary (Partially Implemented)

Scope:

- Plan output from `apply` and `check` reuses the plan renderer's color option.
- Progress logs remain mostly uncolored; warnings, errors, and high-risk notices
  may be integrated separately later.

Acceptance:

- Plan output from `apply --color=never` has no ANSI.
- Plan output from `apply --color=always` has ANSI.
- Stderr progress logs are ANSI-free by default, avoiding CI/debug log pollution.

Current status:

- Plan output reuses `--color`.
- Progress logs remain ANSI-free.
- Coloring warnings, errors, and high-risk progress notices is deferred to a
  separate design.

## Acceptance Criteria

- In a TTY, `dbf plan` uses color to distinguish create/update/delete lines.
- Deletion entries use behavior-specific colors and display text categories.
- `NO_COLOR=1 dbf plan` emits no ANSI escape sequences.
- `dbf plan --format json` emits no ANSI escape sequences.
- Non-TTY and CI output is ANSI-free by default.
- Ordinary `dbf apply` progress is not over-colored; warnings, errors, and
  dangerous deletions have explicit colors and text.
- Persisted logs, debug logs, state, and plan JSON contain no ANSI sequences.

## Open Questions

- Should stdout/stderr from remote commands during `apply` pass through exactly,
  or should DebianForm strip ANSI consistently?
- Does Windows terminal support require additional detection, or is Unix TTY
  handling sufficient at the current stage?
