# 11. Secrets, Sensitive Values, and the Redaction Pipeline

<p align="right"><strong>English</strong> | <a href="11-redaction.zh.md">简体中文</a></p>

This chapter explains how DebianForm handles sensitive data during parsing,
through IR, graph, plan, and state, and during provider execution. The whole
pipeline must be understood together because relaxing any one layer can leak a
secret.

## Sources of Sensitive Data

Common sources include:

- A `secret` file.
- A `files.file`, nftables, or similar resource explicitly set to
  `sensitive = true`.
- Sensitive content whose mark automatically propagates to file-like resources
  such as files, systemd units, APT sources/signing keys, or nftables.
- `content_write_only = true`.
- A sensitive variable.
- An ephemeral variable.
- A sensitive component input.
- External input through `-var @path`, `-var env:NAME`, stdin, or a similar
  source.

These concepts are related but not equivalent. Maintenance must distinguish
between plaintext required during execution and plaintext forbidden from output
or persistence.

## Parser Layer

The parser propagates cty marks and `parser.Value` fields:

- `SensitiveMark`
- `EphemeralMark`
- `Value.Sensitive`
- `Value.Ephemeral`

`sensitive = true` on a variable declaration marks its normalized value as
sensitive. `ephemeral = true` marks it as ephemeral.

The parser also prohibits ephemeral values in map keys and set elements because
those positions affect stable identity and output.

## Protecting CLI Variable Sources

The CLI can read variables from files, stdin, and environment variables. For a
sensitive variable:

- Read errors hide the sensitive source path.
- Parse errors do not include the raw value.
- Inspect output displays the default as `"<sensitive>"`.

This protection is implemented primarily in `cmd/dbf/main.go` and parser
variable logic.

## IR Layer

IR may still carry content required for execution, including file content. IR
itself is not a redaction boundary.

It must, however, retain sufficient metadata, including:

- `Sensitive`
- `ContentWriteOnly`
- `ContentSummary`
- source

Later graph, plan, and state stages use these fields to choose presentation and
persistence behavior.

Compilation paths for APT source, APT signing-key, and nftables content inspect
sensitive and ephemeral metadata before converting a `parser.Value` into a Go
string. The sensitive mark continues to propagate. These three resource kinds
do not yet implement ephemeral write-only semantics, so compilation rejects an
ephemeral value. Retaining only the string would let these independent
file-like resources bypass downstream redaction.

## Graph Layer

A graph node has two content representations:

- `Desired`
- `ProviderPayload`

The provider payload may hold real execution content. `Node.MarshalJSON` clears
`ProviderPayload` for a content-write-only or sensitive node, preventing graph
JSON from leaking it.

This protects only JSON serialization; the in-memory graph still needs the
payload for execution. The node's `Desired` must also remove plaintext and
retain necessary summaries according to sensitive/write-only semantics. Hiding
only `ProviderPayload` in `Node.MarshalJSON` is insufficient.

## Plan Layer

Plan diffs must ensure that:

- Ordinary text can display hunks.
- Sensitive content displays only summaries.
- Write-only content never displays plaintext.
- HTML and JSON are as safe as text.

`BuildDiff` and related formatting functions enforce these rules. Run
redaction tests whenever adding a diff kind or output format.

## State Layer

State is a persistence boundary and must be redacted. `state.SanitizeDesired`:

- Removes `content` and stores `content_sha256` and `content_bytes`.
- Removes `source_path` and `summary` from sensitive desired data.

`DesiredDigest` is computed from sanitized desired, allowing content-change
detection without persisting plaintext.

Observed data also passes through `SanitizeObserved`.

## Provider Execution Layer

A provider sometimes must send plaintext to a remote target, such as when
writing a file. The rules are:

- Prefer stdin or a safe heredoc/base64 transfer.
- Never interpolate a secret into a shell command line or command preview.
- Errors, stdout, and stderr must not include a write-only payload in returned
  observed data.
- Command previews must not contain sensitive plaintext because plans and
  operations display them.

The redaction matrix specifically tests native-provider command previews,
errors, stdout/stderr, and observed data.

## Redaction Regression Matrix

`cmd/dbf/redaction_matrix_test.go` covers many output paths:

- Plan text stdout.
- Plan JSON stdout.
- Plan HTML artifacts.
- Hostspec JSON.
- Resource-graph desired JSON.
- State JSON.
- Native-provider command previews and errors.
- Native-provider stdout/stderr.
- The sensitive-output matrix for APT source, APT signing-key, and nftables
  content.

`internal/core/testassert/secrets.go` maintains sentinel secret strings.
`NoSecretLeak` asserts that output contains none of them. Fail-closed ephemeral
cases for the three content kinds above live in merge regression tests.

When adding a sensitive path, add it to this matrix first.

## Special Properties of Ephemeral Values

An ephemeral value is stricter than a sensitive value: it generally must not
enter a persisted structure. The current implementation prevents leaks through
marks, key restrictions, state sanitization, and redaction tests.

`files.file.content` supports a write-only provider payload and a non-sensitive
`content_version` trigger. APT source, APT signing-key, and nftables content do
not currently have those semantics and therefore reject ephemeral values during
compilation.

When a new capability lets an ephemeral value affect resource content, check:

- Whether it enters a resource address.
- Whether it enters state desired or observed data.
- Whether it appears in errors, plans, or command previews.

## Design Boundaries

- A sensitive mark is not permission to skip output review; it must drive
  redaction.
- State, plans, JSON, HTML, and error messages are all disclosure surfaces.
- A provider may hold plaintext briefly for execution but cannot place it in a
  persisted or displayed structure.
- Redaction tests must cover real paths, not only individual helpers.

## Change Checklist

- New secret or sensitive input: add parser/merge mark-propagation tests.
- New output path: add it to the redaction matrix.
- New state field: verify sanitized output has no plaintext or path disclosure.
- New provider command: verify commands, errors, and stdout/stderr contain no
  secret.
- New HTML/JSON field: perform an end-to-end check using sentinel secrets.
