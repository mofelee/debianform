<p align="right">
  <strong>English</strong> | <a href="cli-repeatable-f-requirements.zh.md">简体中文</a>
</p>

# Requirements for Repeatable CLI `-f` Options

## Background

Most `dbf` configuration commands currently support `-f file` to select one `.dbf.hcl`
configuration file precisely. Without `-f`, the CLI reads all `*.dbf.hcl` files in the current
directory, sorts them by filename, and parses them together.

In the original implementation, `-f` was a single string option. If a user supplied it more than
once:

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

only the final file took effect, making the command equivalent to:

```bash
dbf validate -f app.dbf.hcl
```

That behavior conflicts with the natural expectation that repeating an option selects several
inputs. It also left no common way to load an explicit set of files without loading every
`*.dbf.hcl` file in the current directory.

## Goal

Allow `-f` to be repeated so the CLI can load several configuration files explicitly:

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
dbf plan -f base.dbf.hcl -f host.dbf.hcl --offline
dbf fmt -f base.dbf.hcl -f app.dbf.hcl
dbf component inspect -f components.dbf.hcl -f hosts.dbf.hcl reverse_proxy
```

Every file supplied through `-f` must be passed to the existing `ParseFiles([]string)` path and
parsed in command-line order.

## Applicable Commands

Repeatable `-f` must cover every command that already supports `-f`:

- `dbf validate`
- `dbf plan`
- `dbf apply`
- `dbf check`
- `dbf fmt`
- `dbf component inspect`

## Behavior

### No `-f`

Preserve the existing behavior:

- Read every `*.dbf.hcl` file in the current directory.
- Sort by filename.
- Return an error when no file matches.

Example:

```bash
dbf validate
```

### One `-f`

Preserve the existing behavior:

- Read only the selected file.
- Do not automatically read other `.dbf.hcl` files from that file's directory.
- Do not treat the argument as a directory.

Example:

```bash
dbf validate -f app.dbf.hcl
```

### Multiple `-f` Options

Add the following behavior:

- Read every file selected explicitly through `-f`.
- Preserve command-line order.
- Do not read other `*.dbf.hcl` files from the current directory.
- Each `-f` still accepts exactly one file path.

Example:

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

The command must parse:

```text
base.dbf.hcl
app.dbf.hcl
```

It must not parse only `app.dbf.hcl`.

### `fmt` Behavior

When several `-f` options are provided, `dbf fmt` must format only those explicit files and retain
the current output format:

```text
formatted N file(s)
```

`N` is the number of files whose content actually changed, not the total number of input files.

### Plan Command Metadata

The command-file metadata in plan output must continue to use the existing `commandFile(files)`
logic:

- One file: show that file's path.
- Several files: show their paths joined with commas.

This requirement does not change the plan JSON schema.

## Non-goals

This change does not add any of the following:

- Comma-separated syntax such as `-f a.dbf.hcl,b.dbf.hcl`.
- Recursive directory loading through `-f dir/`.
- Internal expansion of glob arguments such as `-f "configs/*.dbf.hcl"`.
- A long `--file` option, unless specified by a separate requirement.
- Changes to merge behavior, duplicate-block behavior, or source-reference semantics between
  `.dbf.hcl` files.

## Compatibility

This is a small behavior change:

- Old behavior: the final repeated `-f` replaced every earlier value.
- New behavior: every file named by a repeated `-f` is parsed.

The new behavior matches normal CLI expectations more closely, but it can affect a script that
depends on the old replacement behavior. Such scripts are expected to be uncommon.

Documentation and help text must explicitly say that `-f` is repeatable to reduce misuse.

## Implementation Guidance

A custom option type can store the file list, for example:

```go
type fileFlags []string
```

Implement `flag.Value`:

- `String() string`
- `Set(value string) error`

Then replace the current declaration:

```go
file := fs.String("f", "", "configuration file")
```

with a repeatable option, and make `configFiles` accept `[]string`:

```go
var filesFlag fileFlags
fs.Var(&filesFlag, "f", "configuration file; may be repeated")
```

Recommended `configFiles` semantics:

```text
When filesFlag is non-empty, return a copy of it.
When filesFlag is empty, use the existing logic to glob *.dbf.hcl in the current directory.
```

Ensure callers cannot accidentally mutate the internal slice.

## Test Requirements

Add at least the following tests:

- `configFiles(nil)` still returns current-directory `*.dbf.hcl` files sorted by name.
- `configFiles([]string{"custom.dbf.hcl"})` returns the single file.
- `configFiles([]string{"base.dbf.hcl", "app.dbf.hcl"})` returns both files in input order.
- `run validate -f base -f app` parses both files together.
- `run fmt -f a -f b` formats only the two explicit files.
- `component inspect -f components -f hosts name` parses the component using both files.

If a custom option type is introduced, add a focused unit test that covers ordering after repeated
calls to `Set`.

## Documentation Requirements

Update all of the following during implementation:

- `docs/cli.md`
- Relevant `-f` documentation in `README.md`, if necessary.
- CLI `usage()` output.

Avoid continuing to describe the option as selecting one configuration file. Prefer wording such
as:

```text
`-f file` may be repeated. With one or more `-f` options, only the explicitly selected files are
read. Without `-f`, all `*.dbf.hcl` files in the current directory are read.
```

## Acceptance Criteria

The following commands must behave as described.

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

Parse `base.dbf.hcl` and `app.dbf.hcl` together.

```bash
dbf plan -f base.dbf.hcl -f host.dbf.hcl --offline
```

Generate the plan from those two files without reading any other `.dbf.hcl` file in the current
directory.

```bash
dbf fmt -f base.dbf.hcl -f app.dbf.hcl
```

Check and format only those two files.

Final acceptance:

```bash
make test
```

The command must pass.
