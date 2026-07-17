# 02. HCL Parsing, Variables, and the Value Model

<p align="right"><strong>English</strong> | <a href="02-parser-values.zh.md">简体中文</a></p>

This chapter explains how `internal/core/parser` turns `.dbf.hcl` files,
variable files, environment variables, and CLI variables into `parser.Config`.
This stage reads configuration and evaluates expressions; it does not turn the
configuration into final resources.

## Data Flow

```text
files
  -> hclparse.ParseHCLFile
  -> parseLocals
  -> parseVariables
  -> resolveVariableValues
  -> parseTopLevel
  -> parser.Config
```

`ParseFilesWithOptions` is the main entry point. It has two important options:

- `AllowMissingVariables`: permits variables without final values, commonly for
  inspect or the first pass that parses variable declarations.
- `SkipTopLevel`: parses only `locals` and `variable`, skipping `profile`,
  `host`, and `component`.

## Why Parsing Happens in Phases

The parser processes all files' `locals`, then all `variable` declarations,
and finally the top-level blocks. This order is required because top-level
blocks may refer to `local.*` and `var.*`, while variable values may originate
outside the configuration files.

The complete parsing sequence is:

1. Read all HCL files, retaining each `file + hclsyntax.Body`.
2. Parse `locals` across all files.
3. Parse `variable` declarations across all files.
4. Resolve external values and defaults into `cfg.VariableValues`.
5. Unless `SkipTopLevel` is set, parse `profile`, `host`, and `component`.

Consequently, multiple files share one variable and locals namespace. Duplicate
definitions are errors.

## `parser.Config`

`parser.Config` is the parser's output:

- `Files`: configuration files read for this invocation.
- `Locals`: evaluated values from all `locals` blocks.
- `Variables`: variable declarations.
- `VariableValues`: normalized final values for variables.
- `ExplicitVariableValues`: identifies variables assigned explicitly by an
  external source.
- `Profiles`: raw profile definitions.
- `Hosts`: raw host definitions.
- `Components`: raw component-template definitions.

The host, profile, and component values still closely resemble the user's
configuration. Profile imports have not been applied and values have not been
expanded into `ir.HostSpec`.

## Value Model

Instead of scattering HCL values across Go primitive types, the parser uses
`parser.Value` consistently:

- `KindNull`
- `KindString`
- `KindBool`
- `KindNumber`
- `KindList`
- `KindMap`

A `Value` also carries:

- `Source`: file, line, and path for error locations and later plan sources.
- `Modifier`: `force`, `before`, `after`, or `unset`.
- `Sensitive`: the value carries a sensitive mark.
- `Ephemeral`: the value carries an ephemeral mark.

This metadata is foundational for later merge behavior, redaction, error
messages, and test assertions.

## Expression Evaluation

`evalValue` is the central entry point for normal attributes and value
expressions. It supports:

- List and map literals.
- Scalar expressions.
- `local.*` and `var.*`.
- `path.module`.
- Functions: `file`, `jsonencode`, `templatefile`, and `toset`.
- Modifier functions: `force()`, `before()`, `after()`, and `unset()`.

`evalValue` ultimately converts cty values back into `parser.Value`. Sensitive
or ephemeral marks on a cty value are preserved on the `Value`.

Two important constraints apply:

- Unknown values are not supported.
- Map keys and set elements cannot be ephemeral because they become part of
  addresses or stable identities.

## Variable Sources and Precedence

The CLI layer collects external variable values, while the parser interprets
and normalizes them according to declarations. `collectExternalVariableValues`
appends sources in this order:

```text
DBF_VAR_*
  -> debianform.dbfvars / debianform.dbfvars.json in each participating configuration directory
  -> *.auto.dbfvars / *.auto.dbfvars.json in each participating configuration directory
  -> -var-file
  -> -var
```

Directory order follows each configuration directory's first appearance.
`resolveVariableValues` processes the external list in order, so later values
for the same variable override earlier ones.

If a variable has no explicit value:

- Use its default when present.
- Return a required-value error when it has no default and missing values are
  not allowed.
- Skip it when it has no default and `AllowMissingVariables` is true.

## Variable Files

`ParseVariableFile` supports two formats:

- `.json`: must be a JSON object.
- Other extensions: parsed as HCL attributes; blocks are not allowed.

Values in an HCL variable file pass through `evalValue`, so they may use
supported functions and literals. A JSON variable file preserves the string
representation of JSON numbers to avoid losing their representation too early.

## Special Sources for CLI `-var`

`-var name=value` generally interprets the value as a string, HCL expression,
or JSON according to the variable type and content.

When the variable is declared and the value begins with a special prefix:

- `@path`: read the variable value from a local file.
- `@-`: read it from stdin.
- `env:NAME`: read it from an environment variable.

For a sensitive variable, read or parse errors hide source-path details to
avoid leaking information.

## Top-Level Blocks

`parseTopLevel` accepts only:

- `profile`
- `host`
- `component`

`locals` and `variable` have already been handled in earlier phases. Every
other top-level block produces an `unknown top-level block` error.

At this stage, the parser checks syntax and local structure, including the
number of block labels, duplicate definitions, and supported attributes and
blocks. Semantic merging across profiles, components, and hosts belongs to the
merge layer.

## The Role of SourceRef

The parser creates many `ir.SourceRef` values even though the type is defined in
`internal/core/ir`. A source reference records:

- `File`
- `Line`
- `Path`

Later stages carry this source into IR, graph nodes, plan changes, and lifecycle
errors. When adding a field, preserve its source first; without it, maintainers
cannot readily trace a plan entry or error back to user configuration.

## Design Boundaries

- The parser may understand HCL and the syntactic shape of the DebianForm DSL.
- The parser must not understand remote state, provider commands, or the state
  file format.
- The parser may mark values sensitive or ephemeral, but it does not own final
  plan/state redaction policy.
- Parser host/profile/component output is an intermediate representation, not
  an execution plan.

## Change Checklist

- New expression function: add `eval.go` tests and verify sensitive/ephemeral
  mark propagation.
- New variable type or constraint: add normalization and variable-file/CLI
  parsing tests.
- New top-level block: update `parseTopLevel`, the merge compile entry point,
  and documentation.
- Variable-precedence change: add CLI-layer tests and state the override order
  among `.dbfvars`, auto files, `-var-file`, and `-var` explicitly.
- SourceRef change: confirm that error messages, plan sources, and golden files
  are updated together.
