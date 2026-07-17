<p align="right">
  <strong>English</strong> | <a href="ubuntu-26.04-support-contract.zh.md">简体中文</a>
</p>

# Ubuntu 26.04 Preview Support Contract

This document records the implementation and support boundaries delivered by issue
[#53](https://github.com/mofelee/debianform/issues/53). Platform, provider, libvirt, CI,
documentation, and release reviews must follow it. Ubuntu 26.04 LTS amd64 is currently Preview; the
[Support Matrix](support-matrix.md) remains authoritative for current status.

## Goals and Non-goals

The managed target is fixed at Ubuntu 26.04 LTS (`resolute`) amd64 Server with an initial Preview
tier. This work extends the delivered Ubuntu 24.04 path rather than creating a second Ubuntu
implementation:

- Continue using the same `dbf` CLI, `*.dbf.hcl` DSL, resource addresses, plan format, and remote
  state.
- Debian 13 amd64 remains the default and highest-priority target. Existing Debian 12/13 and Ubuntu
  24.04 gate names, coverage, and blocking semantics remain unchanged.
- Continue to permit root SSH only; do not add sudo, become, or management through the default
  `ubuntu` user.
- Do not manage Netplan or NetworkManager; do not generate, read, modify, remove, or migrate Netplan
  YAML, and do not invoke `netplan`.
- Do not support Ubuntu 26.04 arm64, desktop, Snap, PPAs, Ubuntu Pro, cloud-init lifecycle
  management, or in-place upgrades from Ubuntu 24.04 to 26.04.
- Do not duplicate the parser, graph, engine, or complete provider stack for a different Ubuntu
  version.

## Platform Tuple and Failure Rules

The only allowed Ubuntu 26.04 tuple is:

```hcl
host "server1" {
  platform {
    distribution = "ubuntu"
    version      = "26.04"
    architecture = "amd64"
    codename     = "resolute"
  }
}
```

| Field | Online fact source | Target value | Meaning |
| --- | --- | --- | --- |
| `distribution` | `/etc/os-release` `ID` | `ubuntu` | Distribution identity; never inferred from codename. |
| `version` | `/etc/os-release` `VERSION_ID` | `26.04` | Ubuntu release identity. |
| `architecture` | `dpkg --print-architecture`, with normalized `uname -m` fallback | `amd64` | Package and artifact architecture. |
| `codename` | `VERSION_CODENAME`, with compatible `UBUNTU_CODENAME` fallback | `resolute` | Repository suite and assertion. |

Failure rules:

1. Online `plan/apply/check` treats detected remote values as facts; explicit fields are assertions.
2. `26.04+noble`, `24.04+resolute`, non-amd64, and unknown Ubuntu versions must fail before provider
   observation or mutation.
3. Diagnostics must contain the host, field, declared value, detected value, and source path.
4. `validate` does not connect to the host; it checks field shape and the complete-tuple allowlist.
5. An offline plan that needs distribution dispatch must declare all four fields. Do not infer the
   Ubuntu version from codename, command presence, or the control machine.
6. Never silently substitute a `noble` repository, suite, package, or fixture for `resolute`.

## Released Image and Repository Contract

VM evidence must use Ubuntu's official released image:

- Endpoint: `https://cloud-images.ubuntu.com/releases/26.04/release/`
- Image: `ubuntu-26.04-server-cloudimg-amd64.img`
- Checksum: parse and record the real digest from `SHA256SUMS` in the same directory on every run.
- Never use a `resolute/current` daily image as support or release evidence.

Ubuntu archive and third-party repository rules:

- The official Ubuntu archive/security suites are authoritative for the baseline. A third-party
  mirror's synchronization delay is not a DebianForm defect.
- Docker verification must cover the key, suite, architecture, packages, and conflict removal at
  `https://download.docker.com/linux/ubuntu/dists/resolute/`. An HTTP endpoint existing is not
  compatibility evidence.
- If upstream does not provide installable `resolute` content, keep that domain Unsupported and
  block the complete Preview claim; never fall back to `noble`.

## Compatibility Classification

Under the [Compatibility Policy](compatibility-policy.md), this work is a compatible addition:

- Do not add or change DSL fields; extend only the valid tuple set of the existing platform
  allowlist.
- Top-level state remains version `2`; do not run state migration.
- Plan JSON remains `debianform.plan.alpha1`.
- Resource addresses, ownership, desired digests, destroy/forget, and lock semantics remain
  unchanged.
- Valid Ubuntu 24.04 and Debian 12/13 configuration and repository output remain compatible.
- Only a complete validated platform tuple may select the new Ubuntu version branch.

## Domain Difference Matrix

"Shared regression" means reusing the current implementation and revalidating it on a 26.04 VM.
"Version evidence" permits a narrow branch only when the baseline proves a difference. "Native
networkd" means an administrator must remove Netplan ownership outside DebianForm first.

| Domain/block | Ubuntu 26.04 classification | Evidence and constraint |
| --- | --- | --- |
| `variable`, `locals`, `profile`, `component`, `host` | Shared regression | Parser/eval/merge unchanged; full unit/golden coverage. |
| `ssh` | Shared regression | Root-only; no sudo/become. |
| `state` | Shared regression | Schema, paths, locks, ownership, and redaction unchanged. |
| `platform` | Allowlist extension | Accept exactly `ubuntu/26.04/resolute/amd64`. |
| `system` | Shared regression plus version evidence | Adapt hostname/timezone/locale only after VM failure. |
| `kernel` | Shared regression plus version evidence | Preserve module/sysctl semantics. |
| `packages` | Shared regression plus version evidence | APT/dpkg; package differences must link to baseline evidence. |
| `apt` | Shared regression plus version evidence | Verify deb822, keyring, refresh, drift, and removal. |
| `files`, `secrets`, `directories` | Shared regression | Paths, permissions, ownership, and redaction unchanged. |
| `groups`, `users` | Shared regression plus version evidence | Verify UID/GID, home, shell, and membership. |
| `systemd.unit`, `systemd.service_unit`, `systemd.timer` | Shared regression plus version evidence | Verify units, enablement, state, timer firing, and removal. |
| `systemd.resolved`, `systemd.journald` | Shared regression plus version evidence | Adapt service/path differences narrowly from baseline. |
| `systemd.networkd` | Native networkd | Reject active Netplan ownership before mutation. |
| `services` | Shared regression | systemctl path unchanged; networkd subject to ownership preflight. |
| `nftables` | Shared regression plus version evidence | Validate, activate, rollback, drift, and removal. |
| `docker` | Distro shared plus version evidence | `linux/ubuntu` plus `resolute`; reuse daemon/users/Compose. |
| `assert` | Shared regression | Preserve all four `self.platform`/`target.platform` fields. |

## Resource/Provider Difference Matrix

| Resource/provider | Ubuntu 26.04 classification | Evidence and constraint |
| --- | --- | --- |
| `system_hostname`, `system_timezone`, `system_locale` | Shared regression plus version evidence | Preserve addresses and observed structure. |
| `kernel_module`, `sysctl` | Shared regression plus version evidence | Do not preemptively branch by version. |
| `package` | Shared regression plus version evidence | Install/remove/virtual/conflict behavior comes from baseline. |
| `apt_signing_key`, APT source `file` | Shared regression plus version evidence | Create/update/drift/delete, refresh, and redaction. |
| Ordinary `file`, `directory` | Shared regression | Preserve ownership, mode, sensitive, and removal behavior. |
| `group`, `user`, `user_group_membership` | Shared regression | Preserve identity, home, membership, and removal. |
| `systemd_unit`, `service` | Shared regression plus version evidence | Networkd service subject to preflight. |
| `nftables_file` | Shared regression plus version evidence | Validation, activation, rollback, and removal. |
| `component_artifact` | Shared regression plus version evidence | binary/file/archive/CA/source and amd64 selection. |
| `docker_repository` | Distro shared plus version evidence | Must generate `linux/ubuntu` plus `resolute`, without cross-version fallback. |
| `docker_package_conflicts` | Version evidence | Add package names only from the 26.04 baseline. |
| `docker_daemon`, `docker_compose_project` | Shared regression plus version evidence | Verify no-op, drift, and removal after packages are ready. |
| `operation` | Shared regression | Preserve APT refresh, daemon-reload, restart, and on-change identities. |

## Network Ownership

An ordinary Ubuntu 26.04 host may keep its network under Netplan as long as DebianForm configuration
does not declare structured `systemd.networkd` resources or manage files under
`/etc/systemd/network`. `/etc/netplan/*` must remain unchanged before and after non-network work.

Before managing networkd:

- The read-only preflight must cover 26.04 Netplan configuration, generated files, renderer, and
  service state.
- Active ownership fails before file writes, service enable/reload, or other mutation.
- Diagnostics list the host, ownership evidence, affected declarations, and external preparation
  requirement.
- No automatic takeover, override, or Netplan YAML exists. Native-networkd preparation in the test
  harness is not a product capability.
- Arbitrary script content is not used to infer ownership; it remains the author's responsibility.

### Network Ownership Verification Results

Issue #59 verified both conflict and native paths on the official Ubuntu 26.04 released image:

- The stock image contained both `/etc/netplan/50-cloud-init.yaml` and Netplan-generated
  `/run/systemd/network/10-netplan-primary.network`; preflight reported both as ownership evidence.
- With both structured networkd and a raw `/etc/systemd/network` file declared, `plan` and `apply`
  failed before provider mutation. Diagnostics contained the host, both declaration addresses,
  conflict paths, `no provider changes were made`, and external-preparation guidance.
- Networkd service state was unchanged across the conflict check, and neither target file nor
  managed-resource state was created.
- The stock image then completed non-network file create, drift repair, and destroy while the
  Netplan file set and SHA-256 values remained unchanged in all three phases.
- On operator-prepared native-networkd fixtures, `shared-script-networkd`, `wireguard`, and
  `wireguard-three-host` passed 14, 39, and 30 explicit assertions respectively, including reload
  deduplication, no-op, drift, deletion, and cleanup.
- Runner SSH gained server-alive bounds so guest reboot windows no longer leave an established SSH
  probe hanging indefinitely; one-, two-, and three-host fixtures completed without intervention.

Ownership checks read only `/lib/netplan`, `/etc/netplan`, `/run/netplan`, and Netplan-generated
systemd-networkd/NetworkManager runtime files. They do not run the Netplan CLI, `systemctl`, or file
mutations.

## 20-Case 26.04 Baseline and Acceptance Mapping

"Released image" preserves the image's Netplan ownership. "Native-networkd image" means the
harness performs external preparation before DebianForm.

| Case | Fixture | Expected classification | Evidence loop |
| --- | --- | --- | --- |
| `apt-source` | released image | Shared APT plus resolute suite | #56/#57 |
| `apt-virtual-package` | released image | Shared package/virtual package | #56/#57 |
| `bbr` | released image | Shared kernel | #56/#58 |
| `component-inputs` | released image | Shared component | #56/#58 |
| `docker-compose` | released image | Packages plus shared Compose | #56/#57/#58 |
| `docker-daemon` | released image | Packages plus shared daemon | #56/#57/#58 |
| `docker-engine` | released image | Docker `resolute` repository | #56/#57 |
| `files` | released image | Shared file/directory/redaction | #56/#58 |
| `hostname` | released image | Shared system hostname | #56/#58 |
| `multi-directory` | released image | Shared merge/files | #56/#58 |
| `nftables` | released image | Shared nftables | #56/#58 |
| `script-on-change` | released image | Shared operation | #56/#58 |
| `shadowsocks-rust` | released image | Shared artifact/systemd | #56/#58 |
| `shared-script-networkd` | native-networkd image | Ownership plus reload deduplication | #56/#59 |
| `source-build` | released image | Shared source/package/CA | #56/#57/#58 |
| `system-settings` | released image | Hostname/timezone/locale | #56/#58 |
| `systemd-extensions` | released image | Timer/resolved/journald | #56/#58 |
| `systemd-service-unit` | released image | Shared structured systemd | #56/#58 |
| `wireguard` | native-networkd image | Two-host networkd | #56/#59 |
| `wireguard-three-host` | native-networkd image | Three-host networkd | #56/#59 |

Every case covers configured validation, target-fact assertions, online JSON plan, drift rejection,
apply, JSON no-op plan, check, case assertions, and cleanup. #56 records the first failing phase and
owner without opportunistically fixing providers in the baseline commit. #57-#59 fix only
evidence-backed differences.

### Non-network Provider Verification Results

On the official released image (SHA-256
`0826c5005ebc70edcfc4519e5d65eca766782f16426231c4c3e92b811ba8df0b`), all 14 non-network cases
covered by #58 passed their full lifecycle:

| Case | Explicit assertions | Verified domain |
| --- | ---: | --- |
| `bbr` | 18 | Kernel module, runtime/persistent sysctl, drift, removal |
| `component-inputs` | 6 | Component input, sensitive-file mode, state redaction |
| `docker-compose` | 16 | Compose project, generated files, systemd service, stop/forget/delete |
| `docker-daemon` | 10 | Daemon JSON, service restart, drift, recovery |
| `files` | 8 | File/directory ownership, mode, restore/keep, removal |
| `hostname` | 11 | Runtime/persistent hostname, drift, recovery |
| `multi-directory` | 9 | Multi-directory merge, file state, removal |
| `nftables` | 18 | Check, activate, drift, rollback, removal |
| `script-on-change` | 11 | Operation identity, trigger, no-op, removal |
| `shadowsocks-rust` | 19 | Artifact, user/group/membership, file, systemd, service |
| `source-build` | 4 | Source build, dependency packages, CA, artifact, cleanup |
| `system-settings` | 12 | Timezone, locale, resolved, journald, recovery |
| `systemd-extensions` | 18 | Raw unit, timer, drop-in, reload, drift, removal |
| `systemd-service-unit` | 22 | Structured unit, environment, restart, drift, removal |

The baseline found no Ubuntu 26.04-specific non-network provider or path difference, so it added no
release branch. Shared implementation retains existing resource addresses, plan/state structure,
redaction, and destroy/forget semantics. Full race tests and the redaction matrix continue to gate
sensitive text, JSON, HTML, debug, and state output.

## CI and Support-Claim Gates

- Add a separately named 20-case Ubuntu 26.04 matrix and `Ubuntu 26.04 target matrix gate`.
- Do not rename or weaken `Managed target matrix gate` or `Ubuntu 24.04 target matrix gate`.
- A support claim requires Debian 12, Debian 13, Ubuntu 24.04, and Ubuntu 26.04 at 20 cases each on
  the same commit: all 80 target-case results green.
- Record the exact commit, CI run URL, released-image digest, and hypervisor cleanup after success or
  failure.
- Preview describes maturity rather than permission to merge known regressions. After the claim is
  delivered, the 26.04 gate blocks release.

## Delivery Evidence

The Ubuntu 26.04 Preview implementation-gate baseline is commit
[`0211ab2c98d674182dc91a9af7bd887dc91e5539`](https://github.com/mofelee/debianform/commit/0211ab2c98d674182dc91a9af7bd887dc91e5539)
and [CI run 29418825778](https://github.com/mofelee/debianform/actions/runs/29418825778):

- Debian 12, Debian 13, Ubuntu 24.04, and Ubuntu 26.04 amd64 each passed `20/20`, for 80 target-case
  results.
- `Managed target matrix gate`, `Ubuntu 24.04 target matrix gate`, and
  `Ubuntu 26.04 target matrix gate` succeeded; including unit tests, `84/84` jobs passed.
- The official 26.04 released-image URL is
  `https://cloud-images.ubuntu.com/releases/26.04/release/ubuntu-26.04-server-cloudimg-amd64.img`,
  SHA-256 `0826c5005ebc70edcfc4519e5d65eca766782f16426231c4c3e92b811ba8df0b`.
- Target-evidence and cleanup steps succeeded in all 20 Ubuntu 26.04 jobs. Cleanup checked domains,
  networks, libvirt volumes, disk/NVRAM files, temporary workdirs, and SSH artifacts.
- Failure diagnostics on all three runners include target facts, Netplan ownership, network
  services/links/addresses/routes, package/service status, journals, plans/state/logs, libvirt
  XML/console, and cleanup evidence.

First-run commands and an ordinary non-network example are in the
[Ubuntu 26.04 Preview Quickstart](ubuntu-26.04-quickstart.md) and
[`examples/ubuntu-26.04-preview.dbf.hcl`](../examples/ubuntu-26.04-preview.dbf.hcl).

## Preview Promotion and Closure Criteria

One green matrix permits only a Preview release. Promotion to Beta still requires sustained
blocking CI, revalidation after released-image updates, release verification, no unresolved
high-risk feedback, and an explicit support-tier decision.

Before closing #53, #54-#61 must each contain current remote evidence. Ubuntu 24.04 success cannot
prove 26.04, and an HTTP endpoint existing cannot replace real package, apply, no-op, check, and
drift evidence.
