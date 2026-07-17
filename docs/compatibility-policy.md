<p align="right">
  <strong>English</strong> | <a href="compatibility-policy.zh.md">简体中文</a>
</p>

# DebianForm Compatibility and Migration Policy

This document defines DebianForm's compatibility rules for the DSL, CLI, state schema, and plan
JSON format. The project is currently in public preview / beta. These rules constrain future
releases and describe the requirements that must be met before the project becomes stable.

## Scope

This policy covers:

- User-visible CLI commands, options, and exit-code semantics.
- The `.dbf.hcl` DSL, defaults, validation, and resource addresses.
- The remote state-file schema, state migration, and rollback boundaries.
- The output of `dbf plan --format json`.
- Breaking changes, migration notes, and known issues in release notes.

It does not cover:

- Internal Go package APIs.
- Design-only fixtures that are not marked Beta or Compat in the support matrix.
- Internal provider addresses and diagnostic details in `--debug` output.
- Behavior of external services that DebianForm does not promise to support, such as third-party
  APT sources, registries, or Homebrew tap availability.

## Release Phases

### Public Beta

Breaking changes are allowed during beta, subject to all of the following requirements:

- Breaking CLI, DSL, state, or plan JSON changes must appear under `Breaking Changes` in the
  release notes.
- Changes that require user action must appear under `Migration Notes`, including configuration
  edits, state handling, and rollback limitations.
- `CHANGELOG.md` must record the user-visible impact.
- A patch-style beta tag must not silently change existing resources in a dangerous way, such as
  changing an operation from no-op to destroy.

The purpose of beta is to converge on stable compatibility boundaries quickly. Old beta releases
do not receive a long-term maintenance commitment.

### Stable

After the project becomes stable, it follows semantic versioning:

- Patch releases may contain compatible bug fixes, security fixes, documentation updates, and
  non-breaking enhancements only.
- Minor releases may add DSL features, plan JSON fields, state fields, and provider capabilities.
- Breaking CLI, DSL, state, or plan JSON changes may appear only in minor releases and must include
  migration instructions.
- Security fixes are prioritized for the latest stable patch. Maintainers decide whether to
  backport them to an older minor line based on risk.

## DSL Compatibility

Compatible changes include:

- Adding an optional block, attribute, enum value, or provider capability.
- Adding a field to an existing block when its default is explicit and does not change the meaning
  of existing configurations.
- Adding a warning, deprecation diagnostic, or more specific error message.
- Correcting invalid validation behavior, provided the correction does not suddenly reject a
  configuration that was already valid and safe.

Breaking changes include:

- Removing or renaming an existing block, attribute, enum value, or function.
- Changing a default so the same configuration produces different remote resources or different
  destroy/update behavior.
- Changing a stable resource address so existing state can no longer associate with the remote
  resource.
- Rejecting a configuration that was previously valid, unless the old behavior was unsafe or
  produced an obviously invalid configuration.
- Removing a compatibility form without at least one minor release cycle of deprecation warnings
  and a migration path.

Deprecation process:

1. Mark the feature as deprecated in release notes and emit a CLI warning.
2. Document the replacement syntax and migration procedure.
3. Retain the old form until at least the next minor release. Beta may use a shorter period, but the
   release notes must say so explicitly.
4. Treat removal as a breaking change.

## CLI Compatibility

Compatible changes include:

- Adding a command or option.
- Adding unstructured explanatory text to successful output without changing a machine-readable
  JSON format.
- Adding context to an error message.

Breaking changes include:

- Removing or renaming a command or option.
- Changing the documented exit-code semantics of a command.
- Changing the safety semantics of `apply`, `check`, locking, or confirmation.
- Changing the meaning of primary-path options such as `-f`, `--host`, `--offline`, or
  `--format json`.

## State Schema Migration Policy

The current top-level state `version` is `2`. State files are the safety boundary for remote facts
and ownership, so migration must be conservative.

Compatible state changes include:

- Adding an ignorable field.
- Adding a summary field to a resource record.
- Adding information to an observed summary without changing ownership.
- Correcting a redacted summary field without needing to read old plaintext.

Breaking state changes include:

- Changing the top-level `version`.
- Removing, renaming, or changing the semantics of a `resources` key or address.
- Changing the meaning of `ownership`.
- Changing desired-digest calculation in a way that causes widespread meaningless drift or
  destroy actions.
- Requiring state to be rewritten before apply/check can continue.

Migration rules:

- The CLI must inspect `version` when it reads state.
- It must reject apply for an unknown newer version, tell the user to upgrade the CLI, and must not
  attempt to write the state back.
- If an automatic migrator exists for an older version, it must back up the original state before
  atomically writing the migrated result.
- Automatic migration must not write secret plaintext, weaken file permissions, or change the lock
  path.
- If safe automatic migration is impossible, the command must fail and provide manual steps.
- `Migration Notes` in the release notes must cover pre-migration checks, backup, rollback, and
  failure recovery.

Rollback boundaries:

- An older CLI is not guaranteed to read state after a newer version has migrated it.
- When rollback must remain possible, release notes must explicitly require retaining the
  pre-migration state backup.
- A patch release must not introduce an irreversible state migration.

## Plan JSON Format Compatibility

The current format version for `dbf plan --format json` is `debianform.plan.alpha1`.

Compatible changes include:

- Adding a top-level field or a field to a change, operation, or diagnostic.
- Adding an action or kind enum value, provided the release notes describe it.
- Adding a summary field.
- Adding more precise source, diagnostic, or non-debug metadata.

Breaking changes include:

- Changing `format_version` semantics without changing the version string.
- Removing or renaming an existing field.
- Changing a field's type.
- Changing the basic structure of `changes`, `operations`, or `diagnostics`.
- Adding a debug-only provider address to ordinary JSON output.
- Exposing sensitive plaintext.

Consumer guidance:

- Read and validate `format_version`.
- Ignore unknown fields.
- Treat an unknown action or kind conservatively and require manual review.
- Do not depend on object-key order.
- Do not parse text-renderer output as a machine interface.

Format-version rules:

- Adding fields compatibly does not require changing `format_version`.
- A breaking format change must change `format_version`.
- After stable, a breaking plan JSON format change may appear only in a minor release and must be
  listed in the release notes.

## Release Gate

Before every release, verify:

- `CHANGELOG.md` lists the user-visible compatibility impact.
- Release notes retain `Compatibility`, `Breaking Changes`, and `Migration Notes`.
- Breaking DSL, state, and plan JSON changes appear only on a permitted version line.
- State migrations have tests, a backup strategy, and failure-recovery instructions.
- Plan JSON format changes update `docs/plan-format.md`.
- The support matrix reflects added, deprecated, and unsupported capabilities.

Before stable / GA, also require:

- Multiple formal releases without undocumented breaking DSL, state, or plan JSON changes.
- No blocking compatibility reports from real users or real hosts.
- State migration and the plan JSON policy are enforced as gates in the release process.
