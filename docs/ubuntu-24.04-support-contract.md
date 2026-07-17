<p align="right">
  <strong>English</strong> | <a href="ubuntu-24.04-support-contract.zh.md">简体中文</a>
</p>

# Ubuntu 24.04 Preview Support Contract

This document records the implementation and support boundaries delivered by issue
[#43](https://github.com/mofelee/debianform/issues/43). Future parser, fact, provider, libvirt,
documentation, and release reviews must follow it. Ubuntu 24.04 LTS amd64 is currently Preview; the
[Support Matrix](support-matrix.md) remains authoritative for current status.

## Goals and Non-goals

The first managed Ubuntu target is fixed at Ubuntu 24.04 LTS (`noble`) amd64 with an initial Preview
tier. Debian 13 amd64 remains the default and highest-priority target, and existing Debian 12/13
support tiers and blocking gates remain unchanged.

This work continues to use one `dbf` CLI, `*.dbf.hcl` DSL, resource-address space, plan format, and
remote state:

- Do not create UbuntuForm, `ubf`, `*.ubf.hcl`, or a second state namespace.
- Continue to permit only root SSH; do not add sudo, become, or management through the default
  `ubuntu` user.
- Do not manage Netplan or NetworkManager, generate or read Netplan YAML content, invoke `netplan`,
  or modify, remove, or migrate `/etc/netplan/*`.
- Ubuntu 26.04 LTS amd64 has a separate support contract and gate; do not extrapolate 24.04 results
  to 26.04.
- Do not support Ubuntu 22.04, Ubuntu arm64, desktop, Snap, PPAs, Ubuntu Pro, or cloud-init lifecycle
  management.
- Do not duplicate the parser, graph, engine, or complete provider stack because the distribution
  name differs.

## Platform Fields and Precedence

`platform` has four optional assertion/offline-fact fields:

```hcl
host "server1" {
  platform {
    distribution = "ubuntu"
    version      = "24.04"
    architecture = "amd64"
    codename     = "noble"
  }
}
```

| Field | Online fact source | Ubuntu 24.04 value | Meaning |
| --- | --- | --- | --- |
| `distribution` | `/etc/os-release` `ID` | `ubuntu` | Distribution identity; never inferred from codename. |
| `version` | `/etc/os-release` `VERSION_ID` | `24.04` | Distribution release identity. |
| `architecture` | `dpkg --print-architecture`, with normalized `uname -m` fallback | `amd64` | Target package and artifact architecture. |
| `codename` | `VERSION_CODENAME`, with compatible `UBUNTU_CODENAME` fallback | `noble` | Repository suite and user assertion. |

Precedence and failure rules:

1. Online `plan/apply/check` treats detected remote values as facts.
2. Explicit configuration fields are assertions. Any mismatch fails before provider observation or
   mutation, with an error containing the host, field, declared value, detected value, and source
   path.
3. Online facts fill omitted fields. Never infer the distribution from codename, command presence,
   or the control machine's OS.
4. `validate` does not connect to the host; it checks field shape and an allowlist when a complete
   tuple is declared.
5. Ordinary offline plan does not require platform fields. A resource that selects a distribution
   implementation on Ubuntu must explicitly declare all four fields.
6. To preserve existing Debian configuration, an offline configuration without
   `distribution/version` keeps historical Debian repository semantics. This compatibility default
   must never infer Ubuntu from a codename such as `noble`.

Managed-target allowlist:

| Target | Execution allowed | Current/target tier |
| --- | --- | --- |
| Debian 13 amd64 | Yes | Beta, primary path |
| Debian 12 amd64 | Yes | Beta |
| Debian 12/13 arm64 | Yes | Preview, unchanged |
| Ubuntu 24.04 amd64 | Yes | Preview |
| Ubuntu 26.04 amd64 | Yes | Preview with separate released-image matrix and gate |
| Other Debian/Ubuntu tuple | No | Unsupported |
| Other distribution | No | Unsupported |

## Compatibility Boundary

This work is a compatible addition:

- The DSL adds optional fields only; existing `platform.architecture/codename` remain valid.
- `facts.system` gains ignorable `distribution/version` fields; top-level state remains version `2`.
- Old state without the new fields remains readable and is populated naturally after the next
  online fact discovery; no state migration runs.
- Resource addresses, ownership, desired digests, and destroy/forget semantics remain unchanged.
- Plan JSON remains `debianform.plan.alpha1`; no existing field is removed, renamed, or retyped.
- Existing Debian offline Docker repository output remains compatible. Ubuntu selects
  `linux/ubuntu` only from explicit or validated distribution facts, never a codename guess.

## Domain Capability Classification

"Shared" means reuse the Debian implementation and permit only narrow branches backed by VM
evidence. "Distribution dispatch" requires a validated `distribution`. "Native networkd" requires
an operator to remove Netplan ownership before DebianForm runs.

| Domain/block | Ubuntu classification | Constraint |
| --- | --- | --- |
| `variable`, `locals`, `profile`, `component`, `host` | Shared | Parser/evaluation/merge semantics unchanged. |
| `ssh` | Shared | Still root-only. |
| `state` | Shared | Paths, locking, schema, and ownership unchanged. |
| `system` | Shared with real-host verification | Adapt hostname/timezone/locale only from failure evidence. |
| `platform` | Distribution dispatch | Add distribution/version facts and assertions. |
| `kernel` | Shared with real-host verification | Preserve module/sysctl semantics. |
| `packages` | Shared with real-host verification | Continue using APT/dpkg; package-name differences require baseline evidence. |
| `apt` | Shared with real-host verification | Preserve deb822, source-file, keyring, and refresh semantics. |
| `files`, `secrets`, `directories` | Shared | Preserve paths, permissions, redaction, and removal semantics. |
| `groups`, `users` | Shared with real-host verification | Preserve UID/GID, home, shell, and membership semantics. |
| `systemd.unit`, `systemd.service_unit`, `systemd.timer` | Shared with real-host verification | Continue using systemd. |
| `systemd.resolved`, `systemd.journald` | Shared with real-host verification | Adapt default service-state differences narrowly. |
| `systemd.networkd` | Native networkd | Reject active Netplan ownership before changes. |
| `services` | Shared | Continue using systemctl. |
| `nftables` | Shared with real-host verification | Preserve validate/activate/rollback semantics. |
| `docker` | Partial distribution dispatch | Official repo/key/conflicts by distro; daemon/users/Compose shared. |
| `assert` | Shared | `self.platform`/`target.platform` include all four platform fields. |

## Resource/Provider Capability Classification

| Resource/provider | Ubuntu classification | Constraint |
| --- | --- | --- |
| `system_hostname`, `system_timezone`, `system_locale` | Shared with real-host verification | Preserve addresses and observed structure. |
| `kernel_module`, `sysctl` | Shared with real-host verification | Do not branch merely on distro name. |
| `package` | Shared with real-host verification | APT/dpkg; add mappings only for proven package differences. |
| `apt_signing_key`, `file`, `directory` | Shared | Preserve ownership/redaction. |
| `group`, `user`, `user_group_membership` | Shared | Preserve identity and removal semantics. |
| `systemd_unit`, `service` | Shared | Networkd service remains subject to ownership preflight. |
| `nftables_file` | Shared with real-host verification | Continue nft validation and activation. |
| `component_artifact` | Shared with real-host verification | Architecture selection uses platform facts. |
| `docker_package_conflicts` | Distribution dispatch | Ubuntu conflicts must come from baseline evidence. |
| `docker_compose_project` | Shared with real-host verification | Preserve Compose semantics after packages are ready. |
| `operation` | Shared | Preserve identities for APT refresh, daemon-reload, restart, and related operations. |

## Network Ownership

An ordinary Ubuntu host may continue using Netplan for its existing network as long as DebianForm
configuration does not declare structured `systemd.networkd` resources or manage files under
`/etc/systemd/network`.

Before managing either, the online preflight must inspect `/etc/netplan/*.yaml` ownership read-only:

- Active Netplan ownership fails before file writes, service enable/reload, or any provider mutation.
- The diagnostic lists the host, conflict source, and affected DebianForm declarations.
- No automatic takeover or override exists; an administrator must prepare native networkd outside
  DebianForm.
- Arbitrary script command text is not inferred; its safety remains the configuration author's
  responsibility.

## 20-Case Ubuntu Matrix

These 20 cases run as an independent blocking Ubuntu matrix. "Native-networkd image" means the test
harness performs external preparation before DebianForm; it is not product-managed migration.

| Case | Ubuntu fixture | Classification/implementation loop |
| --- | --- | --- |
| `apt-source` | official image | Shared APT, #47 |
| `apt-virtual-package` | official image | Shared package, #47 |
| `bbr` | official image | Shared kernel, #48 |
| `component-inputs` | official image | Shared component, #48 |
| `docker-compose` | official image | Distro packages plus shared Compose, #47/#48 |
| `docker-daemon` | official image | Distro packages plus shared daemon, #47/#48 |
| `docker-engine` | official image | Official Ubuntu Docker repository, #47 |
| `files` | official image | Shared, #48 |
| `hostname` | official image | Shared system, #48 |
| `multi-directory` | official image | Shared parser/files, #48 |
| `nftables` | official image | Shared nftables, #48 |
| `script-on-change` | official image | Shared operation, #48 |
| `shadowsocks-rust` | official image | Shared artifact/systemd, #48 |
| `shared-script-networkd` | native-networkd image | Ownership guard plus networkd, #49 |
| `source-build` | official image | Shared build/package, #47/#48 |
| `system-settings` | official image | Hostname/timezone/locale, #48 |
| `systemd-extensions` | official image | Timer/resolved/journald, #48 |
| `systemd-service-unit` | official image | Shared systemd, #48 |
| `wireguard` | native-networkd image | Ownership guard plus two-host networkd, #49 |
| `wireguard-three-host` | native-networkd image | Ownership guard plus three-host networkd, #49 |

## Delivery Evidence

The authoritative initial Ubuntu Preview implementation baseline is commit
[`49c741e848bbbcc7c95f1969bf8952d5cc425e7d`](https://github.com/mofelee/debianform/commit/49c741e848bbbcc7c95f1969bf8952d5cc425e7d).
[CI run 29334736664](https://github.com/mofelee/debianform/actions/runs/29334736664) proved on that
same commit:

- Ubuntu 24.04 LTS amd64: `20/20`.
- Debian 12 amd64: `20/20`.
- Debian 13 amd64: `20/20`.
- `Ubuntu 24.04 target matrix gate` and `Managed target matrix gate` succeeded.
- Including unit tests: `63/63` jobs.

That is the historical baseline for the initial 24.04 delivery. The current four-target gate uses
commit `0211ab2c98d674182dc91a9af7bd887dc91e5539` and CI run `29418825778` recorded in the
[Support Matrix](support-matrix.md), with separate `20/20` matrices and gates for 24.04 and 26.04.

Preview describes maturity, does not permit a known Ubuntu regression, and is not Beta. Promotion
to Beta requires sustained blocking CI, release-verification evidence, resolved high-risk user
feedback, and an explicit support-tier decision.
