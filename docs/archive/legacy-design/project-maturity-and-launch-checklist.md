# Project Maturity and Launch Checklist

<p align="right"><strong>English</strong> | <a href="project-maturity-and-launch-checklist.zh.md">简体中文</a></p>

This document supports periodic DebianForm maturity reviews and organizes the
work required before launch, after public beta, and before stable into a
maintainable checklist.

Maintenance: during each review, change `- [ ]` to `- [x]` directly and append
the verification commands and conclusion to Review History.

## Current Review Summary

- Review date: 2026-06-23.
- Current recommended maturity: **4/5: public-beta launch candidate**.
- Recommended positioning: **`v0.1.0-beta` or public preview**.
- Not recommended: stable, GA, or production-ready.

Local acceptance in this review:

- [x] `go vet ./...`
- [x] `go test ./...`
- [x] `go test -race -count=1 ./...`
- [x] `make vulncheck`
- [x] `make build`
- [x] `make test-integration-layout`
- [x] GitHub Actions CI equivalent full Debian 13 libvirt VM matrix:
  `28015644419`, covering 9 cases.
- [x] GitHub Actions was fully passing for the commit tagged by formal release
  `v0.1.0-beta.2`.

Current assessment:

- [x] The core loop is complete: validate, plan, apply, check, state, locks,
  observed drift, and SSH execution paths all exist.
- [x] Release engineering is substantially complete: GoReleaser, GitHub
  Releases, checksums, curl installer, automatic Homebrew tap updates,
  post-release verification, cosign keyless, SBOMs, and provenance all have
  repository configuration or documentation.
- [x] README explicitly states beta/public-preview status, support scope,
  installation, upgrade, verification, and example boundaries.
- [x] Formal public-beta tag `v0.1.0-beta.2` completed end-to-end release confirmation.
- [ ] Small-scale beta feedback from a real, low-risk Debian 13 host is still missing.
- [ ] Stable requires a longer support commitment and evidence across several releases.

## Release-Positioning Review

- [x] The top of README states that the project is in public preview / beta.
- [x] README states that the current DSL is the sole mainline format.
- [x] README states that the old experimental format is retired.
- [x] README identifies Debian 13 as the highest-priority target system.
- [x] README identifies amd64 as the priority managed-target architecture.
- [x] README states that `dbf` CLI artifacts cover Linux/macOS on amd64 and arm64.
- [x] README distinguishes runnable examples from design-only fixtures.
- [x] README says to run plan before a real apply and requires a reachable,
  supported Debian host over SSH.
- [x] Formal public-beta release notes state beta risks, compatibility limits,
  and migration impact explicitly.
- [ ] Stable/GA wording is not misused in README, release notes, or installation docs.

## P0: Required Before Public-Beta Launch

### Support Scope

- [x] Supported Debian versions are documented in README.
- [x] Managed-target architecture priorities are documented in README.
- [x] README and the [release process](../../release-process.md) distinguish CLI
  platforms from managed-target platforms.
- [x] Runnable examples are listed in README.
- [x] `examples/fleet.dbf.hcl` is marked as a design-only fixture.
- [x] Runnable examples have validation coverage.
- [x] Formal public-beta release notes contain a short supported/unsupported matrix.

### CI and Test Gates

- [x] CI checks gofmt.
- [x] CI runs `go vet ./...`.
- [x] CI runs `go test -race -count=1 ./...`.
- [x] CI runs `make build`.
- [x] CI runs `make test-integration-layout`.
- [x] CI discovers libvirt integration cases dynamically.
- [x] CI has a Debian 13 libvirt integration job.
- [x] Libvirt cases cover `apt-source`, `bbr`, `component-inputs`, `files`,
  `nftables`, `shadowsocks-rust`, `source-build`, `systemd-service-unit`, and
  `wireguard`.
- [x] The `wireguard` case covers the two-host runner.
- [x] Before formal release, confirm CI is fully passing on the tagged commit.
- [x] Before formal release, complete `make test-integration` or an equivalent
  full CI libvirt matrix at least once.

### Release Artifacts and Installation

- [x] `LICENSE` exists.
- [x] `CHANGELOG.md` exists.
- [x] `SECURITY.md` exists.
- [x] The [release process](../../release-process.md) exists.
- [x] The [release quick runbook](../../release-quick-runbook.md) exists.
- [x] `.goreleaser.yaml` covers Linux/macOS amd64/arm64.
- [x] Release tarballs contain `dbf`, README, docs, examples, LICENSE, and CHANGELOG.
- [x] The release workflow generates `checksums.txt`.
- [x] The release workflow creates a GitHub Release.
- [x] The release workflow generates `checksums.txt.sigstore.json`.
- [x] The release workflow generates SBOMs.
- [x] The release workflow generates GitHub provenance attestations.
- [x] `scripts/install.sh` installs latest or a selected version.
- [x] `scripts/install.sh` verifies tarball SHA-256.
- [x] `scripts/install.sh` detects or overrides Linux/macOS amd64/arm64.
- [x] `scripts/install.sh` supports `--prefix`, `--bin-dir`, `--dry-run`, and `--force`.
- [x] The release workflow integrates automatic Homebrew tap updates.
- [x] The release dry-run workflow verifies GoReleaser snapshot artifacts.
- [x] Post-release verification covers the Linux amd64 curl installer.
- [x] Post-release verification covers macOS amd64 and arm64 curl installers.
- [x] Post-release verification covers macOS Homebrew install/test/upgrade.
- [x] Linux arm64 artifact builds are covered.
- [ ] Verify the Linux arm64 curl installer on a real arm64 runner or machine.
- [ ] Verify Linux Homebrew install/test/upgrade on a Linux runner or machine with Homebrew.
- [x] Formal public-beta tag `v0.1.0-beta.2` was created and passed the release workflow.
- [x] Formal GitHub Release assets and verification matrix passed manual spot checks.
- [x] The formal Homebrew formula points to the public-beta tag, not a test tag.
- [x] `CHANGELOG.md` contains real changes for the formal public-beta tag, not placeholders.

### Security and Trust

- [x] `SECURITY.md` points to GitHub Security Advisories.
- [x] README documents checksum verification.
- [x] README documents cosign keyless bundle verification.
- [x] README documents GitHub provenance-attestation verification.
- [x] [State documentation](../../state.md) explains stored state fields.
- [x] [State documentation](../../state.md) explains that secret content,
  sensitive inputs, SSH private keys, command logs, and lock leases are not
  written to state.
- [x] [Plan format](../../plan-format.md) states that sensitive diffs emit no plaintext.
- [x] README states that remote URL artifacts require a 64-character SHA-256.
- [x] WireGuard integration checks ensure private-key plaintext is absent from state.
- [x] Add `govulncheck` or equivalent dependency vulnerability scanning.
- [x] Add Dependabot/Renovate or an equivalent dependency-update policy.
- [x] README documents the root-only SSH execution model and the lack of
  sudo/become/non-root management connections.
- [x] [CLI documentation](../../cli.md) documents the root-only SSH execution model.
- [x] [Requirements](requirements.md) documents the root-only privilege boundary.
- [x] Add a centralized secret-redaction regression matrix covering text/JSON/HTML
  plans, stdout/stderr, and state.

### User Documentation

- [x] README contains Homebrew installation.
- [x] README contains curl installation.
- [x] README contains upgrade and rollback instructions.
- [x] README uses `dbf version` for installation verification.
- [x] README contains basic validate, offline plan, JSON plan, HTML plan, apply,
  and check examples.
- [x] [CLI documentation](../../cli.md) covers `validate`, `plan`, `apply`,
  `check`, `fmt`, `variable inspect`, `component inspect`, and version.
- [x] [CLI documentation](../../cli.md) covers `--host`, `--parallel`, and
  `--lock-timeout`.
- [x] [State documentation](../../state.md) covers state path, lock path,
  ownership, locking, and atomic writes.
- [x] The [release quick runbook](../../release-quick-runbook.md) covers pre-release,
  release, post-release, and rollback procedures.
- [x] Add a standalone quickstart covering SSH-user preparation, first
  configuration, validate, online plan, apply, and check.
- [x] Add an operations runbook covering stale locks, partial apply failure,
  state/remote divergence, resource removal, and recovery.
- [x] Add common troubleshooting using real errors and repair steps.
- [x] Add a concise support matrix combining DSL blocks, resource/domain types,
  and current stability.

### Beta Verification

- [x] Libvirt integration tests use Debian 13 cloud VMs.
- [x] Libvirt integration tests execute real validate, apply, and check.
- [x] Integration cases include drift-check scripts.
- [x] Integration cases verify deletion, forget, or restore behavior.
- [x] The release automation plan records end-to-end workflow verification with test tags.
- [x] After formal beta publication, verify `dbf version`, `dbf validate`, and
  `dbf plan --offline` in a clean environment.
- [ ] After formal beta publication, verify online `plan`, `apply`, a second
  no-op `plan`, and `check` on at least one low-risk Debian 13 host.
- [ ] After formal beta publication, introduce drift manually and confirm
  `check` exits nonzero with understandable output.
- [ ] After formal beta publication, verify a failed apply does not record an
  unsuccessful resource.
- [ ] After formal beta publication, verify plan, logs, and state contain no
  secret plaintext.
- [ ] Collect at least one round of real-use feedback and fix blockers or record
  them under known issues.

## P1: Complete Soon After Public Beta

- [ ] `.deb` package.
- [x] APT repository feasibility review or plan.
- [ ] Automated Linux arm64 installation-path verification.
- [x] Automated Linux Homebrew verification or an explicit best-effort policy.
- [x] More complete operations/runbook documentation.
- [x] More complete quickstart documentation.
- [x] Dependency vulnerability scanning in CI.
- [x] A release-notes template with fixed breaking-changes, known-issues,
  verification-matrix, and migration-notes sections.
- [x] A beta-feedback entry point and triage process.
- [x] A realistic deployment template or small case.
- [x] Guidance for `prevent_destroy` on high-risk resources.

## P2: Required Before Stable/GA

- [ ] Several consecutive releases without breaking DSL/state/plan-JSON changes.
- [ ] Stable use by several real users or groups of real hosts.
- [ ] Long-term stable CI and libvirt integration, with flaky behavior tracked
  and addressed.
- [x] Explicit backward-compatibility policy.
- [x] Explicit state-schema migration policy.
- [x] Explicit plan-JSON format compatibility policy.
- [ ] Release, install, upgrade, and rollback paths verified across several
  formal releases.
- [x] Security documentation covers root-only SSH, privilege boundaries, secret
  handling, and vulnerability response.
- [x] Every common failure scenario has an executable recovery procedure.
- [x] A broader Debian version/architecture support strategy.
- [x] Troubleshooting covers unavailable root SSH, insufficient permissions,
  and unsupported targets.
- [ ] Every README capability promise is covered by a test, example, or document.

## Detailed Maturity Review

### Core Product

- [x] Main CLI paths: `validate`, `fmt`, `plan`, `apply`, and `check`.
- [x] Auxiliary CLI: `version`, `component inspect`, and `variable inspect`.
- [x] Parser supports top-level structures and domain blocks.
- [x] Profile/host merging is implemented.
- [x] Component inputs, validation, deprecation warnings, and sensitive metadata
  are implemented.
- [x] Variables, variable files, automatic variable files, environment
  variables, and CLI variables are implemented.
- [x] HostSpec, ResourceGraph, plan, and state paths are implemented.
- [x] Online plan supports SSH, runtime facts, observed state, and drift comparison.
- [x] Offline plan supports local preview.
- [x] Apply supports remote state locks and state persistence.
- [x] Check detects drift through an online plan and exits nonzero on changes.
- [x] DAG scheduling and multi-host apply concurrency control are implemented.
- [x] Plan supports text, JSON, and static HTML.
- [x] Domains cover kernel/sysctl, files/secrets/directories, users/groups,
  systemd/services, APT, nftables, and component
  binary/archive/file/CA-certificate/source builds.
- [x] Stable-grade compatibility and migration policies are documented, but
  still require execution evidence across several releases.

### Test Coverage

- [x] Parser unit tests.
- [x] Merge unit tests.
- [x] Graph/scheduling unit tests.
- [x] Plan/diff unit tests.
- [x] State unit tests.
- [x] Engine unit tests.
- [x] CLI unit tests.
- [x] Version unit tests.
- [x] Source-build integration Go tests.
- [x] Golden tests cover parser, HostSpec, graph, and plan.
- [x] Runnable-example validation tests.
- [x] Libvirt case-layout validation.
- [x] Debian 13 libvirt VM integration design and CI job exist.
- [ ] This review did not rerun the complete Debian 13 libvirt VM matrix.
- [ ] Long-term flaky records and trend monitoring are still missing.

### Release Maturity

- [x] Local builds inject version, commit, and date.
- [x] GoReleaser multi-platform build configuration.
- [x] Release dry-run workflow.
- [x] Tag-triggered release workflow.
- [x] Curl installer.
- [x] Homebrew tap update script and workflow step.
- [x] Post-release verification summary written into release notes.
- [x] Checksums, cosign keyless, SBOMs, and provenance.
- [x] Formal public-beta release completed and verified through the runbook.
- [ ] `.deb` and APT repository are not implemented.

### Documentation Maturity

- [x] README covers positioning, installation, upgrade, verification, examples,
  basic commands, and integration-test entry points.
- [x] [CLI documentation](../../cli.md) covers primary commands and options.
- [x] [Requirements](requirements.md) and related design documents exist.
- [x] [State documentation](../../state.md) exists.
- [x] [Plan format](../../plan-format.md) exists.
- [x] [Support matrix](../../support-matrix.md) exists.
- [x] [Security model](../../security-model.md) exists.
- [x] [APT repository feasibility](../../apt-repository-feasibility.md) exists.
- [x] [Platform support strategy](../../platform-support-strategy.md) exists.
- [x] [Linux Homebrew verification policy](../../linux-homebrew-verification-policy.md) exists.
- [x] [Beta feedback and triage](../../beta-feedback-triage.md) exists.
- [x] Release process, automation plan, and quick runbook exist.
- [x] A one-page quickstart for new users exists independently.
- [x] The operations recovery runbook covers stale locks, failed applies,
  drift, resource removal, and common error recovery.
- [x] Stable-oriented compatibility and migration policies are documented.

## Recommended Release Decision

Conditions for public beta:

- [x] Core functionality forms a complete loop.
- [x] Local Go checks and builds pass.
- [x] Release automation has end-to-end capability.
- [x] Full CI passes before the formal tag.
- [x] The full libvirt matrix has completed or its result has been accepted
  before the formal tag.
- [x] Release notes state beta risks and support boundaries.

Reasons not to release stable/GA:

- [ ] Insufficient real-user and real-host verification.
- [x] State/schema migration policy is documented.
- [x] Backward-compatibility policy is documented.
- [ ] No stable record across several formal releases yet.

Mitigated stable blockers:

- [x] Operations-recovery documentation covers the local recovery procedures
  required during public beta.

## Review History

### 2026-06-23

Review commands:

- [x] `go vet ./...`
- [x] `go test ./...`
- [x] `go test -race -count=1 ./...`
- [x] `make vulncheck`
- [x] `go test ./cmd/dbf -run TestSecretRedactionRegressionMatrix`
- [x] `make build`
- [x] `make test-integration-layout`
- [x] GitHub Actions CI equivalent full libvirt matrix: `28015644419`
- [x] GitHub Actions release dry-run: `28015644510`
- [x] GitHub Actions release workflow: `28015905534`
- [x] `cosign verify-blob ... checksums.txt`
- [x] `gh attestation verify dbf_v0.1.0-beta.2_linux_amd64.tar.gz --repo mofelee/debianform`
- [x] `scripts/install.sh --version v0.1.0-beta.2 --prefix /tmp/dbf-install-check-v0.1.0-beta.2 --os linux --arch amd64 --force`

Conclusion:

- [x] Project maturity advanced from "early beta" to "public-beta launch candidate."
- [x] Local code quality, race tests, build, and integration-layout checks passed.
- [x] Added a centralized secret-redaction regression matrix covering
  text/JSON/HTML plans, CLI stdout/stderr, HostSpec, ResourceGraph desired,
  state, and native-provider previews/errors.
- [x] Added a standalone quickstart covering root SSH preparation, first
  configuration, validate, offline/online plan, apply, no-op plan, and check.
- [x] Release, installation, and supply-chain automation advanced from plans to
  executable repository configuration.
- [x] Formal public beta `v0.1.0-beta.2` was tagged through the release runbook;
  CI, GitHub Release, Homebrew tap, and clean installation all passed verification.
- [ ] Stable/GA still needs real-use feedback and several releases proving the
  compatibility policies in practice.

### 2026-06-23 Operations Runbook Addition

Review commands:

- [x] Reviewed `dbf` CLI, state, and SSH lock implementation semantics.
- [x] Added stale-lock, partial-apply, drift, resource-removal,
  `prevent_destroy`, and common recovery steps to `docs/operations-runbook.md`.
- [x] Covered unavailable root SSH, insufficient permissions, and failed target
  fact discovery in `docs/operations-runbook.md`.

Conclusion:

- [x] Operations-recovery documentation now covers common public-beta failures
  and is linked from README, CLI docs, and quickstart.

### 2026-06-23 Support Matrix Addition

Review commands:

- [x] Reviewed README, release process, parser allowed attributes, IR types, and
  Docker graph implementation.
- [x] Added `docs/support-matrix.md`, covering CLI platforms, target hosts, CLI
  commands, top-level DSL, host domains, resource/provider types, Docker DSL,
  components/variables, and example verification.
- [x] Added the missing `variable inspect` entry to CLI documentation.

Conclusion:

- [x] The support matrix now combines DSL blocks, resource/domain types, and
  current beta/preview/compat/design-only status, linked from README and the
  release process.

### 2026-06-23 Release Notes Template Addition

Review commands:

- [x] Reviewed `CHANGELOG.md`, release process, release quick runbook, and the
  release workflow's Verification Matrix append behavior.
- [x] Added `docs/release-notes-template.md` with fixed Summary, Compatibility,
  Breaking Changes, Migration Notes, Known Issues, Support Matrix, Verification,
  and Verification Matrix sections.
- [x] Linked the template from the release process, quick runbook, and README.

Conclusion:

- [x] A fixed release-notes template now prevents omission of breaking changes,
  known issues, verification matrix, and migration notes.

### 2026-06-23 Beta Feedback Triage Addition

Review commands:

- [x] Reviewed `.github`, README, `SECURITY.md`, the support matrix, and existing
  feedback/issue entry points.
- [x] Added GitHub Issue Forms `Beta feedback` and `Bug report`, with the default
  `needs-triage` label.
- [x] Added `.github/ISSUE_TEMPLATE/config.yml`, directing security
  vulnerabilities to GitHub Security Advisories.
- [x] Added `docs/beta-feedback-triage.md`, defining feedback channels, labels,
  priorities, triage steps, closure criteria, and known-issues synchronization.

Conclusion:

- [x] Public beta now has explicit feedback and triage paths; collection of real
  feedback still depends on subsequent external use.

### 2026-06-23 Realistic Deployment Template Addition

Review commands:

- [x] Added `examples/realistic-systemd-app.dbf.hcl`, covering a low-privilege
  systemd application's group, user, directory, file, structured service unit,
  and service state.
- [x] Added `docs/realistic-deployment-example.md`, describing local acceptance,
  real-host trial order, and customization guidance.
- [x] Linked it from README and the support matrix.

Conclusion:

- [x] Public beta now has a realistic deployment template that needs no network,
  contains no real secret, and supports `validate` and `plan --offline`.

### 2026-06-23 Compatibility and Migration Policy Addition

Review commands:

- [x] Reviewed release process, release-notes template, state, and plan-format
  semantics.
- [x] Added `docs/compatibility-policy.md`, covering beta/stable phases,
  DSL/CLI compatibility, state-schema migration, and plan-JSON format compatibility.
- [x] Linked it from README, release process, state, plan format, and support matrix.

Conclusion:

- [x] Backward-compatibility, state-migration, and plan-JSON compatibility
  policies required before stable are documented. Stable still needs execution
  evidence across several releases and real feedback.

### 2026-06-23 Security Model Addition

Review commands:

- [x] Reviewed SECURITY, README privilege model, requirements secret semantics,
  and beta-feedback triage.
- [x] Added `docs/security-model.md`, consolidating root-only SSH, privilege
  boundaries, secret handling, state/locks, supply-chain verification, and the
  vulnerability-response process.
- [x] Linked it from SECURITY, README, and the support matrix.

Conclusion:

- [x] Security documentation required before stable is complete. Real
  vulnerability-response capability still requires future releases and advisory practice.

### 2026-06-23 APT Repository Feasibility Review

Review commands:

- [x] Reviewed release process, release automation plan, GoReleaser
  configuration, and install script.
- [x] Added `docs/apt-repository-feasibility.md`, documenting `.deb` and APT
  repository user value, risks, signing keys, recommended layout, and staged
  implementation loops.
- [x] Linked it from README, release process, and support matrix.

Conclusion:

- [x] APT repository feasibility and planning are complete. The `.deb` package
  and repository channel remain unimplemented and require independent loops.

### 2026-06-23 Debian Version and Architecture Support Strategy

Review commands:

- [x] Reviewed README, support matrix, release process, runtime-facts docs, and
  fact-discovery implementation.
- [x] Added `docs/platform-support-strategy.md`, distinguishing CLI platforms
  from managed targets and classifying Debian 13 amd64 as Beta, Debian 13
  arm64/Debian 12 as Preview, and non-Debian as unsupported.
- [x] Linked it from README, release process, and support matrix.

Conclusion:

- [x] Debian version/architecture strategy required before stable is complete.
  Actual platform promotion still requires later arm64/Debian 12 verification loops.

### 2026-06-23 Linux Homebrew Best-Effort Policy

Review commands:

- [x] Reviewed release workflow, release automation plan, release process, and
  Homebrew formula update script.
- [x] Added `docs/linux-homebrew-verification-policy.md`, explicitly classifying
  Linux Homebrew as best effort: verify automatically when brew exists and use
  `manual/best-effort` in release notes otherwise.
- [x] Linked it from README, release process, and support matrix.

Conclusion:

- [x] The Linux Homebrew path has an explicit best-effort policy. Real automated
  verification still requires a future Linux runner with Homebrew.
