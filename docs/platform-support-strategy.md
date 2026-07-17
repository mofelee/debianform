<p align="right">
  <strong>English</strong> | <a href="platform-support-strategy.zh.md">简体中文</a>
</p>

# Target Platform Support Strategy

This document defines DebianForm's support strategy for CLI runtime platforms and for managed
target distributions, versions, and architectures. It supplements the
[Support Matrix](support-matrix.md) and determines when a new platform can move from Preview to
Beta.

## Separate Platform Dimensions

DebianForm has two distinct platform dimensions:

- CLI runtime platform: the control machine or CI runner that executes `dbf`.
- Managed target: an allowlisted Linux host managed over SSH by `dbf plan/apply/check`.

The CLI runs on Linux and macOS for amd64 and arm64. Debian 13 amd64 is the highest-priority target,
and Debian 12 amd64 is also Beta. Ubuntu 24.04 and 26.04 LTS amd64 are Preview targets with
independent gates.

## Current Support Tiers

| Scope | Status | Notes |
| --- | --- | --- |
| CLI Linux amd64 | Beta | Release artifact, curl installer, and local/CI checks. |
| CLI Linux arm64 | Preview | Release artifact is built; real arm64 installer verification still needs a runner or host. |
| CLI macOS amd64 | Beta | Release artifact, curl installer, and Homebrew install/test/upgrade are verified. |
| CLI macOS arm64 | Beta | Release artifact, curl installer, and Homebrew install/test/upgrade are verified. |
| Target Debian 13 amd64 | Beta | Primary target; all 20 libvirt cases block merge and release. |
| Target Debian 13 arm64 | Preview | Runtime facts and artifact-source selection support arm64, but no real target matrix exists. |
| Target Debian 12 amd64 | Beta | Runs the same 20 blocking libvirt cases as Debian 13. |
| Target Debian 12 arm64 | Preview | Requires a real Debian 12 arm64 apply/check matrix before promotion. |
| Debian 11 or earlier | Unsupported | Outside current support commitments and release gates. |
| Debian testing/unstable | Unsupported | Outside beta support commitments. |
| Target Ubuntu 24.04 LTS amd64 | Preview | Independent blocking 20-case matrix; does not change Debian defaults or Beta tiers. |
| Target Ubuntu 26.04 LTS amd64 | Preview | Official released image, independent blocking matrix, and exact `resolute` tuple. |
| Other Ubuntu tuple or desktop environment | Unsupported | Only Ubuntu 24.04 (`noble`) and 26.04 (`resolute`) amd64 Server are allowlisted. |
| Other distribution | Unsupported | Requires a separate contract, implementation, and real target matrix first. |

## Debian Version Strategy

### Debian 13

Debian 13 is the highest-priority target:

- New features and integration tests cover Debian 13 first.
- Local `make test-integration` defaults to a Debian 13 cloud image.
- CI and the release gate require every Debian 12/13 amd64 case to pass.
- Quickstart and real beta validation prioritize Debian 13 amd64.
- Docker's official repository, APT sources, systemd, nftables, networkd, and similar capabilities
  are validated on Debian 13 first.

### Debian 12

Debian 12 amd64 is Beta:

- It runs exactly the same 20 libvirt cases as Debian 13 amd64; every failure blocks release.
- Every case asserts Debian ID, version, `bookworm` codename, and `amd64` architecture.
- Each configured path covers `validate`, online JSON plan, drift rejection where applicable,
  `apply`, JSON no-op plan, `check`, and case-specific assertions.
- Distribution-sensitive behavior such as Docker's official repository, APT repositories,
  nftables, systemd, and networkd must pass on both Bookworm and Trixie.

Debian 12 arm64 remains Preview. An amd64 result cannot imply an arm64 Beta commitment.

## Ubuntu LTS Preview Strategy

Ubuntu 24.04 LTS (`noble`) and Ubuntu 26.04 LTS (`resolute`) amd64 share product boundaries but
must retain independent evidence:

- Use the same `dbf` CLI, DSL, resource addresses, plan format, and state schema; do not create
  UbuntuForm.
- Continue to support root SSH only; do not add sudo/become or the default `ubuntu` user.
- Run complete blocking 20-case matrices separately from Debian. The
  `Ubuntu 24.04 target matrix gate` and `Ubuntu 26.04 target matrix gate` block independently.
- Ordinary non-network resources may leave the target's existing network under Netplan.
- DebianForm does not manage Netplan or NetworkManager. Structured networkd declarations and raw
  files under `/etc/systemd/network/` require an operator-prepared native-networkd target; a
  read-only preflight rejects active Netplan ownership before mutation.
- Ubuntu 22.04, other Ubuntu versions, arm64, desktop, Snap, PPAs, Ubuntu Pro, cloud-init lifecycle
  management, and in-place 24.04-to-26.04 upgrades are outside support scope.

See the [Ubuntu 24.04 Support Contract](ubuntu-24.04-support-contract.md) and
[Ubuntu 26.04 Support Contract](ubuntu-26.04-support-contract.md). The Ubuntu 26.04 released-image
baseline is `ubuntu-26.04-server-cloudimg-amd64.img`, SHA-256
`0826c5005ebc70edcfc4519e5d65eca766782f16426231c4c3e92b811ba8df0b`. At commit
[`0211ab2c98d674182dc91a9af7bd887dc91e5539`](https://github.com/mofelee/debianform/commit/0211ab2c98d674182dc91a9af7bd887dc91e5539),
[CI run 29418825778](https://github.com/mofelee/debianform/actions/runs/29418825778) proved each of
the four targets at `20/20` with all three aggregate gates passing. Promotion from Preview to Beta
cannot rest on one green matrix; it also requires sustained blocking CI, release verification, no
unresolved high-risk feedback, and an explicit support-tier decision.

### Earlier Versions

Debian 11 and earlier are outside current support commitments because:

- systemd, nftables, APT deb822, Docker repositories, and cloud images differ more substantially.
- Maintaining several old versions multiplies integration-matrix and documentation costs.
- Current project capacity prioritizes four amd64 target matrices and the Debian 13 primary path.

## Architecture Strategy

### amd64

amd64 is the primary managed-target architecture:

- Debian 13 amd64 is the default local libvirt integration path and highest priority for features.
- Debian 12 amd64 and Debian 13 amd64 are both release blockers.
- Ubuntu 24.04 and 26.04 amd64 have independent release-blocking Preview gates.
- Component artifact examples and Docker's official repository must remain usable on amd64.

### arm64

arm64 is currently Preview:

- CLI artifacts cover Linux and macOS arm64.
- `scripts/install.sh` supports `--arch arm64` and detects `aarch64`/`arm64` automatically.
- Runtime fact discovery normalizes `aarch64` to `arm64`.
- Component source selection supports `source "arm64"`.

Limitations:

- The Linux arm64 curl installer still needs verification on a real arm64 runner or host.
- Target Debian 13 arm64 lacks a real apply/check matrix.
- Ubuntu arm64 is not allowlisted and cannot inherit support from Ubuntu amd64 results.
- Linux Homebrew arm64 verification requires a Linux arm64 environment with Homebrew.

### Other Architectures

Fact discovery may recognize an architecture such as `armhf` as a string. That does not imply a
release artifact, component source, or managed-target support commitment. Before Preview, require at
least:

- A CLI artifact, or an explicit reason no CLI artifact is needed.
- Runtime-fact normalization.
- A component-source selection example.
- At least one real or libvirt managed-target verification path.

## Requirements to Change a Support Tier

Preview promotion to Beta requires:

- At least one release or CI workflow that generates the corresponding artifact automatically.
- Automated installation-path verification, or a release-note explanation of why it remains
  manual/best-effort.
- A managed-target path through `validate`, online `plan`, `apply`, a second no-op `plan`, and
  `check`.
- At least one drift or failure-recovery case.
- Synchronized support matrix, Quickstart, or platform documentation.

Beta is downgraded to Preview when:

- Automated verification is unavailable for consecutive releases without replacement manual
  evidence.
- Real user feedback reveals a high-risk platform defect.
- Upstream platform or dependency changes make maintenance cost or security risk unacceptable.

Moving Unsupported to Preview first requires a separate design or implementation record. A README
claim alone is insufficient.

## Release Notes Requirements

Every release-note verification matrix must distinguish:

- CLI artifact build.
- Curl installer.
- Homebrew install/test/upgrade.
- Debian 12 amd64 managed-target integration.
- Debian 13 amd64 managed-target integration.
- Ubuntu 24.04 amd64 managed-target integration.
- Ubuntu 26.04 amd64 managed-target integration.
- `Managed target matrix gate`, `Ubuntu 24.04 target matrix gate`, and
  `Ubuntu 26.04 target matrix gate`.

Do not collapse evidence for four targets into one "libvirt passed" statement. Record each current
20-case matrix as `20/20` with its CI run URL and prove all results came from the same release
commit. Record each aggregate gate separately as well.

When a platform cannot be verified automatically, write:

```text
manual/best-effort
```

and explain whether the missing prerequisite is a runner, a real host, or a Homebrew environment.

## Follow-up Loops

### Loop A: Debian 13 arm64 Target Case

Goal: verify the managed-target path on a real arm64 host or arm64 libvirt environment.

Acceptance:

```bash
dbf plan
dbf apply
dbf check
```

Also confirm component-source selection for `target.platform.architecture == "arm64"`.

### Loop B: Linux arm64 CLI Installer Verification

Goal: verify the curl installer on a real Linux arm64 runner or host.

Acceptance:

```bash
scripts/install.sh --version <tag> --prefix /tmp/dbf-install-check --force
/tmp/dbf-install-check/bin/dbf version
```

### Loop C: Linux Homebrew Strategy

Goal: decide whether Linux Homebrew remains best-effort or gains a Linux runner with Homebrew.

Acceptance:

```bash
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

## Current State

- Debian 13 amd64 is the highest-priority target. Debian 12 amd64 is also Beta and a release
  blocker.
- Ubuntu 24.04 and 26.04 LTS amd64 are Preview, each with an independent blocking 20-case matrix.
- CI runs 80 amd64 libvirt jobs as four targets times 20 cases and requires three aggregate gates.
- Debian 12 arm64, Debian 13 arm64, and the Linux arm64 CLI installer remain Preview/best-effort.
- Ubuntu arm64, desktop environments, and Ubuntu versions other than 24.04/26.04 are Unsupported.
- Debian 11 and earlier are Unsupported.
