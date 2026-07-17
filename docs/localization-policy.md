<p align="right"><strong>English</strong> | <a href="localization-policy.zh.md">简体中文</a></p>

# Documentation Localization Policy

Every maintained DebianForm Markdown document must be available in English and Simplified Chinese.
`AGENTS.md` is the only exception because it is repository-operational metadata rather than product,
user, or maintainer documentation.

## File Naming

- In the repository root, English uses `<name>.md` and Simplified Chinese uses `<name>.zh-CN.md`.
- Outside the repository root, English uses `<name>.md` and Simplified Chinese uses `<name>.zh.md`.
- Both files in a pair remain beside each other so relative assets and neighboring documentation use
  the same base directory.

## Translation Requirements

- A translation must cover the complete source document. It must not replace sections with summaries.
- Commands, code, URLs, hashes, versions, support tiers, identifiers, and verification evidence retain
  their technical meaning. Human-readable comments or sample prose inside a code block may be localized.
- Headings, fenced-code blocks, tables, and checklists retain equivalent structure across the pair.
- Product behavior and compatibility claims must remain identical in both languages.
- Update both languages in the same change whenever shared behavior, support status, or release policy
  changes.

## Navigation

- Each document has a prominent language selector near the top and links directly to its counterpart.
- English documents link to English documentation by default. Simplified Chinese documents link to
  Simplified Chinese documentation by default.
- The language selector is the only routine cross-language link.
- Relative links are preferred for files within this repository.

## Validation

Run the documentation gate before committing:

```bash
make docs-check
```

The gate verifies pair coverage, selectors, structural parity, English prose, same-language navigation,
and local link targets. CI runs the same command.
