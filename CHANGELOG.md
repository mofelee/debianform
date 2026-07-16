# Changelog

All notable changes to DebianForm will be documented in this file.

This project follows semantic versioning after the public beta line begins.

## v0.8.0

- Added Ubuntu 26.04 LTS (`resolute`) amd64 Server as a Preview managed target
  with an independent blocking 20-case libvirt matrix and target gate. The same
  commit preserves 20/20 coverage for Debian 12, Debian 13, and Ubuntu 24.04.
- Verified the official released Ubuntu 26.04 cloud image, APT and Docker
  `resolute` repositories, shared providers, stock Netplan conflict rejection,
  and operator-prepared native systemd-networkd workflows without a `noble`
  fallback.
- Added an Ubuntu 26.04 Preview quickstart and runnable non-network example.
  Management remains root-only; Ubuntu arm64/desktop, Netplan/NetworkManager
  management, sudo/become, and in-place Ubuntu upgrades remain unsupported.

## v0.7.0

- Added Ubuntu 24.04 LTS (`noble`) amd64 as a Preview managed target with an
  independent blocking 20-case libvirt matrix; Debian 13 remains the primary
  and default target, and Debian 12/13 amd64 remain Beta.
- Added explicit target `platform.distribution` and `platform.version` facts,
  Ubuntu-aware Docker official repository selection, and shared provider
  compatibility across the full managed-target matrix.
- Added a read-only Ubuntu network ownership preflight. DebianForm does not
  manage or migrate Netplan; native systemd-networkd targets must be prepared
  by the operator before DebianForm manages networkd declarations.
- Added an Ubuntu Preview quickstart and runnable example. This compatibility
  addition does not change existing resource addresses, plan format, state
  schema, or Debian offline defaults.
- Fixed graph ordering for component file sources so a managed source file is
  applied before another component reads it as input.
- Updated `golang.org/x/sync` from 0.14.0 to 0.22.0.

## v0.6.0

- Added top-level `script` declarations and explicit
  `global.script.<name>` references so files owned by separate components can
  share one host-scoped `mode = "once"` operation.
- Shared-script operations are identified by the resolved declaration and
  target host, union and deduplicate every triggering resource, run once when
  any trigger changes, and do not run on a no-op apply.
- Preserved existing component-local script addresses and behavior, and added
  source-oriented validation for unknown references, unsupported root-script
  fields, and component-input scope violations.
- Added a reusable raw systemd-networkd example and real Debian 12/13 libvirt
  coverage. CI now gates all 20 cases on each managed target as a 40-job
  matrix.

## v0.5.0

- Added Debian 12 amd64 as a Beta managed target. CI now runs the same 19
  libvirt cases on Debian 12 and Debian 13 amd64 as a 38-job blocking matrix;
  Debian 13 remains the primary and default target.
- Breaking: removed `docker.package.source = "debian"`. Existing configurations
  using that value now fail local validation before SSH or state access. Omit
  `source` to use the default Docker official repository, or use `"none"` or
  `"custom"` when DebianForm should not install Docker packages.
- Migration: changing from Debian's Docker packages to Docker's official
  packages can replace installed packages and interrupt the daemon. Run an
  online plan first, review downtime and package changes, and back up Docker
  data before applying.

## v0.4.0

- Breaking: restricted host, profile, component, and component instance labels
  to valid HCL identifiers. Configurations that used an FQDN, IP address, path,
  whitespace, or punctuation as a label must replace it with a stable logical
  identifier and keep the remote address in `ssh.host`.
- Changed `apply` to acquire the state lock and recompute the execution plan
  after the preview is approved. If the locked plan changed, DebianForm prints
  it and requests approval again before modifying the host or state.
- Changed `check` to hold every target host's state lock throughout state reads
  and provider inspection, preventing it from observing an in-progress apply.
- Added explicit host fields to graph operations, state records, and plan JSON
  changes/operations so multi-host plans no longer infer ownership from resource
  address strings.
- Fixed SSH state locking with atomic, renewable version 2 leases, guarded stale
  takeover, precise deadlines, and surfaced renewal or cleanup failures.
- Fixed state validation to reject unsupported schemas, mismatched top-level
  hosts, and foreign resource records before provider inspection or writes.
- Fixed state revision tracking so every successful backend write advances and
  returns exactly one committed serial, while failed writes leave the visible
  revision unchanged.
- Fixed per-host execution capacity acquisition so unsafe resources reserve all
  host slots atomically instead of deadlocking through partial reservations.
- Fixed sensitive APT source, APT signing key, and nftables content so derived
  plaintext cannot appear in plans, state, debug output, or diagnostics; these
  fields now reject unsupported ephemeral values.

## v0.3.0

- Added managed `system.timezone` and `system.locale` resources; online plan,
  apply, and check now converge explicitly declared host timezone and system
  `LANG` while leaving omitted settings unmanaged.
- Breaking: removed the legacy DSL aliases `system.architecture` and
  `system.codename`; declare target platform facts with
  `platform.architecture` and `platform.codename`.
- Breaking: removed the legacy expression aliases
  `target.system.architecture`, `target.system.codename`,
  `self.system.architecture`, and `self.system.codename`; use
  `target.platform.*` or `self.platform.*` instead. Persisted state facts
  remain under `facts.system.*`.

## v0.2.0-alpha.3

- Added the interactive SSH apply debugger and `debug run` command for remote
  diagnostics, including colorized debugger output and safer failed-call
  recovery.
- Allowed `locals` values to reference variables and other locals, with
  validation coverage for dependency ordering and cycles.
- Added drift detection for component script outputs and refreshed the
  related graph, hostspec, plan, and text golden coverage.
- Reported mixed Docker Compose project states as degraded instead of treating
  partial healthy/running service sets as fully healthy.
- Refined human-readable plan/delete output and SSH troubleshooting hints.
- Bumped pinned GitHub Actions dependencies for checkout, setup-go, cache, and
  provenance attestation workflows.

## v0.2.0-alpha.2

- Limited default online SSH host concurrency to 4 across fact discovery,
  state locking/reads, host inspection, and apply phases, while keeping
  `--parallel` available for explicit tuning.
- Allowed later hosts to retry the initial SSH/auth path after an earlier host
  fails authentication, instead of caching the first auth failure globally.
- Documented the new apply concurrency default and 1Password/agent-heavy SSH
  troubleshooting guidance.

## v0.2.0-alpha.1

- Added parallel host planning and bulk host inspection paths for faster
  multi-host online runs.
- Applied `--parallel` to fact discovery and disabled interactive SSH prompts
  during noninteractive runs.
- Added SSH ControlMaster multiplexing with short control path directories and
  cleanup to improve repeated SSH command performance.
- Fixed SSH execution to preserve the provided `PATH`, serialize initial auth
  setup, and detect virtual providers more reliably.
- Added APT virtual package libvirt coverage and made libvirt retry behavior
  more resilient.

## v0.1.0-beta.8

- Added component `script` operations with `on_change` trigger modes so
  components can run custom hooks when managed inputs change.
- Added graph and engine support for planning and executing component script
  operations.
- Added script/on_change examples and implementation documentation.

## v0.1.0-beta.7

- Added support for passing directories to repeated `-f` flags, expanding each
  directory into its local `.dbf.hcl` files.
- Added per-directory auto variable loading so directory-expanded configs can
  use colocated `.auto.dbfvars` files consistently.
- Added multi-directory libvirt coverage and refreshed the quickstart demo
  recording.

## v0.1.0-beta.6

- Added styled badges, richer ANSI text rendering, and optional Unicode status
  symbols for human-readable plan, progress, and warning output.
- Added systemd service extensions and expanded libvirt coverage for systemd
  drop-ins, environment files, timers, sockets, path units, and tmpfiles.
- Refreshed the runnable fleet example and README quickstart demo assets.

## v0.1.0-beta.5

- Added `--color=auto|always|never` for `plan`, `apply`, and `check` text
  output, while keeping JSON and HTML output free of ANSI escapes.
- Added delete behavior diagnostics to plan output so delete-like actions
  identify whether they forget state, remove managed artifacts, restore
  original content, perform destructive deletes, or have external side effects.
- Renamed the internal v2 packages and example files to the canonical
  `internal/core` and `examples/*.dbf.hcl` layout.
- Added the core graph plan/apply engine path and refreshed examples, support
  matrix, CLI docs, and user-manual links for the canonical layout.

## v0.1.0-beta.4

- Added online `plan`, `apply`, and `check` progress logging to stderr so
  long-running SSH-backed operations expose the active host, phase, and
  resource action without changing machine-readable stdout.
- Added runnable coverage for CLI and DSL documentation examples, including a
  refreshed DSL reference and support matrix wording.

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

- First public beta / public preview release for the DebianForm line.
- Includes the CLI flow for `validate`, `fmt`, `plan`, `apply`, `check`,
  `version`, `component inspect`, and `variable inspect`.
- Supports the `host`, `profile`, `component`, `locals`, and `variable`
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
- Includes runnable examples for BBR, APT sources, files, nftables,
  systemd, users/groups, component inputs, source builds, shadowsocks-rust, and
  WireGuard/networkd patterns.

Known beta limits:

- This is not a stable/GA release. The CLI, DSL, state shape, and plan JSON
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
