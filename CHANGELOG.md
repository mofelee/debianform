# Changelog

All notable changes to DebianForm will be documented in this file.

This project follows semantic versioning after the public beta line begins.

## Unreleased

## v0.1.0-beta.3

- Added file and secret path override support for reusable components, including
  WireGuard peer map examples.
- Allowed repeated component instances to share directories safely when their
  desired ownership and mode match.
- Completed Docker source, package source, daemon config, Compose project, and
  user membership loops with expanded libvirt coverage.
- Added a compatibility and migration policy covering CLI/DSL compatibility,
  state schema migration rules, and plan JSON format compatibility.
- Added a security model document covering root-only SSH, permission boundaries,
  secret handling, state/lock behavior, and vulnerability response.
- Added a `.deb` and APT repository feasibility plan.
- Added a Debian version and architecture support strategy.
- Added a Linux Homebrew best-effort verification policy.

## v0.1.0-beta.2

- Mark beta releases as GitHub prereleases automatically.
- Ignore GoReleaser's `dist/` output directory so release binaries are not
  stamped as dirty by Go VCS metadata.

## v0.1.0-beta.1

- First public beta / public preview release for the v2 DebianForm line.
- Includes the v2 CLI flow for `validate`, `fmt`, `plan`, `apply`, `check`,
  `version`, `component inspect`, and `variable inspect`.
- Supports the v2 `host`, `profile`, `component`, `locals`, and `variable`
  model, with profile/host merging, component inputs, validation warnings, and
  sensitive metadata propagation.
- Provides online SSH-backed plan/apply/check with runtime facts, observed drift
  detection, state locking, state persistence, and offline plan previews.
- Covers Debian 13 as the highest-priority managed target system, with the
  current target host focus on amd64.
- Ships release artifacts for Linux and macOS on amd64 and arm64, plus
  checksums, cosign keyless checksum bundles, SBOMs, and GitHub provenance
  attestations.
- Provides Homebrew and curl installer paths, including version pinning,
  checksum verification, dry-run support, custom install directories, and
  post-release verification jobs.
- Includes runnable v2 examples for BBR, APT sources, files, nftables,
  systemd, users/groups, component inputs, source builds, shadowsocks-rust, and
  WireGuard/networkd patterns.

Known beta limits:

- This is not a stable/GA release. The CLI, v2 DSL, state shape, and plan JSON
  may still change before a stable release.
- Debian 13 is the primary managed target. Other Debian versions and
  non-Debian targets are not part of the beta support promise.
- Managed target hosts currently prioritize amd64. Linux arm64 release artifacts
  are built, but Linux arm64 installer validation remains best-effort until a
  real arm64 runner or host is added.
- Linux Homebrew verification is best-effort when the runner does not provide
  Homebrew.
- `.deb` packages and an apt repository are not included in this beta.
- Stable-grade compatibility policy, state migration policy, and operations
  recovery documentation are still pending.
