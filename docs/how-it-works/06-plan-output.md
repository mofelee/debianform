# 06. Plan Documents, Diffs, and Output Formats

<p align="right"><strong>English</strong> | <a href="06-plan-output.zh.md">简体中文</a></p>

This chapter explains how `internal/core/plan` turns a resource graph or engine
plan into user-readable and machine-readable output.

## Two Plan Sources

DebianForm has two paths that generate plan documents:

```text
offline:
  graph.ResourceGraph -> plan.New -> plan.Document

online:
  engine.Plan -> engine.Plan.Document -> plan.Document
```

An offline plan does not know remote reality, so `plan.New` treats every graph
node as `create`.

An online plan already has engine-computed actions, so `engine.Plan.Document`
preserves actions such as `create`, `update`, `delete`, `adopt`, `forget`,
`destroy`, and `no-op`.

## Document Format

Important fields of `plan.Document` include:

- `FormatVersion`: currently `debianform.plan.alpha1`.
- `GeneratedAt`: UTC RFC3339 timestamp.
- `Command`: file/host context for the invocation.
- `Summary`: create/update/delete/no-op/operation counts.
- `Changes`: resource changes.
- `Operations`: operations that will run.
- `Diagnostics`: diagnostic information, currently an empty list by default.

This structure is the public interface of `dbf plan --format json`; change its
fields carefully.

## Change

A `plan.Change` represents one resource action:

- `Host`
- `Address`
- `Action`
- `Summary`
- `Source`
- `ProviderAddress`
- `DeleteBehavior`
- `DeleteNotes`
- `DeleteRisk`
- `Diff`
- `LowLevelActions`

`ProviderAddress` appears only with `--debug`. It exists to maintain provider
mappings, not as a normal user interface. Changes and operations both carry an
explicit `Host` for JSON consumers and the HTML host filter; they never rely on
parsing the address string. Deletion fields appear only for actions such as
`delete`, `destroy`, and `forget`, allowing JSON, text, and HTML renderers to
reuse one deletion-risk model.

## Diff Tree

`DiffNode` is a recursive structure that represents value changes:

- `Path`
- `Kind`
- `Action`
- `Sensitive`
- `Before`
- `After`
- `BeforeSummary`
- `AfterSummary`
- `Children`
- `Hunks`

`BuildDiff(action, before, after)` is the main entry point. It organizes map,
list, scalar, text, and sensitive content into a tree.

Text content produces hunks so plan text and HTML can display file-content
changes. Sensitive content displays only summaries, never plaintext.

## Summary Counts

The offline `plan.New` summary is simple:

- Every node is create.
- The operation count comes from graph operations.
- Update, delete, and no-op are 0.

Online `engine.summarize` counts step actions as follows:

- `create` -> create.
- `update` and `adopt` -> update.
- `delete`, `destroy`, and `forget` -> delete.
- `no-op` -> no-op.
- Operation-step count -> operations.

These are presentation-layer statistics; they do not directly control apply.

## Text Output

`PrintText` emits:

- A `Plan:` heading.
- An action symbol and address for each change.
- Summary and source.
- Debug provider address.
- Diff child nodes.
- Operation triggers and command previews.
- The final summary.

Action symbols:

- `+`: create/adopt.
- `~`: update.
- `-`: delete/destroy/forget.
- `=`: no-op.
- `!`: operation.

## JSON Output

`PrintJSON` directly encodes an indented `Document` as JSON. This is a
machine-readable interface whose consumers may depend on field names and action
strings.

Before changing the JSON structure, check:

- `docs/plan-format.md`
- CLI goldens or unit tests.
- The redaction matrix.
- Downstream automation compatibility.

## HTML Output

`PrintHTML` renders static HTML from an embedded template. It consumes the same
`Document` as text and JSON and does not recalculate the plan.

HTML correctness therefore depends on:

- Correct diffs and sources already present in `Document`.
- A template that does not bypass redacted fields.
- Path and directory-creation logic remaining in the CLI's `writePlanHTML`.

## Source Presentation

Plan sources originate as `SourceRef` values in the parser and are carried
through every stage. A resource without a correctly assigned source still
works, but users and maintainers cannot locate the corresponding configuration.

## Design Boundaries

- The plan layer presents information; it does not discover remote state.
- The plan layer must not call a provider or backend.
- Plan diffs may compare before and after, but the engine or graph must supply
  those values.
- The plan layer must handle sensitive and content-write-only values
  conservatively.

## Change Checklist

- `Document` JSON change: update `docs/plan-format.md` and tests together.
- Text-output change: update CLI/golden coverage and assess Chinese and English
  user documentation.
- Diff-algorithm change: add scalar, map, list, text, and sensitive cases.
- New action: update action symbols, summaries, engine, provider, and docs.
- HTML change: generate an example and confirm that sensitive fields do not leak.
