<p align="right">
  <strong>English</strong> | <a href="support-matrix.zh.md">简体中文</a>
</p>

# DebianForm Support Matrix

This document brings together DebianForm's current commitments for CLI platforms, managed targets,
configuration blocks, domain/resource types, and verification coverage. The project remains in
public preview / beta. "Supported" here means that the current repository contains an
implementation, documentation, and an automated verification entry point; it is not a stable/GA
compatibility commitment.

See the [DSL Reference](dsl-reference.md) for fields, defaults, constraints, and testable examples
for every implemented DSL declaration. See the [Compatibility Policy](compatibility-policy.md) for
specific DSL, CLI, state schema, and plan JSON rules. See the [Security Model](security-model.md)
for root-only SSH, permission boundaries, secret handling, and vulnerability response. See the
[Platform Support Strategy](platform-support-strategy.md) for requirements to add or promote a
distribution, version, or architecture. See the
[Linux Homebrew Verification Policy](linux-homebrew-verification-policy.md) for its best-effort
rules.

## Status Definitions

| Status | Meaning |
| --- | --- |
| Beta | Current primary path with implementation and test or integration coverage; breaking changes remain possible before stable. |
| Preview | Implemented with less verification coverage or user feedback; requires additional small-scale validation before real production use. |
| Compat | Retained for compatibility with old configuration; not recommended for new use. |
| Design-only | A document or fixture describes direction but is not a runnable capability commitment. |
| Unsupported | Explicitly unsupported by the current version. |

## CLI and Managed Targets

| Item | Current status | Notes |
| --- | --- | --- |
| CLI on Linux amd64 | Beta | Release tarball, curl installer, and local/CI checks; Homebrew install/test/upgrade is currently best-effort. |
| CLI on Linux arm64 | Preview | Release artifact is built; real arm64 curl-installer verification still requires a runner or manual host. |
| CLI on macOS amd64 | Beta | Release artifact, curl installer, and Homebrew install/test/upgrade are verified. |
| CLI on macOS arm64 | Beta | Release artifact, curl installer, and Homebrew install/test/upgrade are verified. |
| Target Debian 13 amd64 | Beta | Highest-priority target; all 20 libvirt cases are blocking gates. |
| Target Debian 13 arm64 | Preview | Runtime facts and architecture selection support arm64, but no real arm64 target matrix exists. |
| Target Debian 12 amd64 | Beta | The same blocking gate as Debian 13 runs all 20 cases. |
| Target Debian 12 arm64 | Preview | Runtime facts recognize arm64, but no Debian 12 arm64 apply/check matrix exists. |
| Target Debian 11 or earlier | Unsupported | Outside current support commitments and release gates. |
| Debian testing/unstable | Unsupported | Outside current beta support commitments. |
| Target Ubuntu 24.04 LTS amd64 | Preview | Independent blocking 20-case matrix; see the Ubuntu support contract for scope and exclusions. |
| Target Ubuntu 26.04 LTS amd64 | Preview | Official released image, independent blocking 20-case matrix, and independent gate; only `resolute` amd64 Server is allowed. |
| Ubuntu 22.04, other Ubuntu versions, Ubuntu arm64, or desktop | Unsupported | Outside current support commitments and release gates. |
| Other distributions | Unsupported | Not in the managed-target allowlist. |
| Root SSH management connection | Beta | `ssh.user` must be omitted or set to `"root"`. |
| sudo/become/non-root management connection | Unsupported | Sudo elevation, become, and non-root management are unsupported. |

CI discovers 20 libvirt cases and expands them across four amd64 matrices: Debian 12, Debian 13,
Ubuntu 24.04, and Ubuntu 26.04, for 80 blocking libvirt jobs. The Ubuntu versions have separate
`Ubuntu 24.04 target matrix gate` and `Ubuntu 26.04 target matrix gate` jobs. Debian 12/13 use
`Managed target matrix gate`. Debian 13 remains the local default and the highest-priority target
for new features.

The current complete four-target baseline is commit
[`0211ab2c98d674182dc91a9af7bd887dc91e5539`](https://github.com/mofelee/debianform/commit/0211ab2c98d674182dc91e5539).
In [CI run 29418825778](https://github.com/mofelee/debianform/actions/runs/29418825778), Debian 12,
Debian 13, Ubuntu 24.04, and Ubuntu 26.04 amd64 each passed `20/20` libvirt cases, and all three
aggregate gates succeeded; including unit tests, the result was `84/84`. Ubuntu 26.04 used the
official released image `ubuntu-26.04-server-cloudimg-amd64.img` with SHA-256
`0826c5005ebc70edcfc4519e5d65eca766782f16426231c4c3e92b811ba8df0b`.

The host-scoped shared-script `shared-script-networkd` case passed 14 assertions on a real Debian 13
amd64 VM on 2026-07-12, covering one reload for two files, zero reload on no-op, one reload after
single-file drift, raw policy-route/rule behavior, and cleanup. The CI run and gates above prove the
current four complete 20-case matrices.

## CLI Commands

| Command | Current status | Verification |
| --- | --- | --- |
| `dbf validate` | Beta | Parser, merge, HostSpec, runnable examples, and CLI unit tests. |
| `dbf plan` | Beta | Offline/online plan; text, JSON, and HTML renderers; debug provider addresses. |
| `dbf apply` | Beta | State locks, apply state writes, DAG scheduling, and libvirt integration cases. |
| `dbf check` | Beta | Online drift detection and non-zero exit path. |
| `dbf fmt` | Beta | In-place HCL formatting. |
| `dbf component inspect` | Beta | Component-input API JSON output. |
| `dbf variable inspect` | Beta | Variable API JSON output. |
| `dbf version` / `--version` | Beta | Release metadata and CLI unit tests. |

## Top-Level Configuration Blocks

| Block | Current status | Notes |
| --- | --- | --- |
| `variable` | Beta | type/default/description/nullable/sensitive/ephemeral/deprecated/validation; `const` currently appears as metadata. |
| `locals` | Beta | Local expressions and value reuse. |
| `profile` | Beta | Imports, domain blocks, and assertions for host configuration reuse. |
| `component` | Beta | binary/file/archive/ca_certificate/source artifacts, typed inputs, and validation. |
| `host` | Beta | Primary entry point carrying SSH, state, system, and domain blocks. |

## Host Domain Blocks

| Domain/block | Current status | Primary capability | Primary verification |
| --- | --- | --- | --- |
| `ssh` | Beta | Root SSH host/port/user/identity_file. | CLI online plan/apply/check. |
| `state` | Beta | Per-host state and lock paths, atomic writes, stale-lock takeover. | State, engine, and SSH backend unit tests. |
| `system` | Beta | Desired hostname, timezone, and default locale; omitted fields remain unmanaged. | Merge/graph/engine unit tests and `hostname`/`system-settings` integration cases. |
| `platform` | Beta | Target distribution/version/architecture/codename facts for target validation, Docker repository selection, component sources, and offline plan. | Merge/graph/plan tests and four target matrices. |
| `kernel` | Beta | Kernel modules and persistent/runtime sysctl. | BBR example and libvirt `bbr` case. |
| `packages` | Beta | Package installation with repository dependencies. | Graph/plan and integration cases. |
| `apt` | Beta | deb822 repository, source file, signing key, APT-refresh operation. | `apt-source` integration case. |
| `files` | Beta | content/source, write-only content_version, sensitive redaction, ownership/mode. | Files integration and secret-redaction matrix. |
| `secrets` | Compat | Compatibility form for `secrets.file`. | Deprecation warning and redaction tests. |
| `directories` | Beta | Directory ownership/mode/ensure. | Graph/engine coverage. |
| `groups` | Beta | Group gid/system/ensure. | User/group example and tests. |
| `users` | Beta | uid/home/shell/primary group/supplementary groups/authorized keys. | User/group example and tests. |
| `systemd.unit` | Beta | Raw unit-file management. | systemd examples and integration case. |
| `systemd.service_unit` | Beta | Structured `.service` generation. | `systemd-service-unit` integration case. |
| `systemd.timer` | Preview | Structured `.timer` generation and timer enabled/state. | Merge/graph tests, `fleet` example, and `systemd-extensions` integration case. |
| `systemd.resolved` | Preview | `/etc/systemd/resolved.conf.d/debianform.conf` drop-in and optional service management/restart. | Merge/graph tests, `fleet` example, and `systemd-extensions` case. |
| `systemd.journald` | Preview | `/etc/systemd/journald.conf.d/debianform.conf` drop-in and journald reload/restart. | Merge/graph tests, `fleet` example, and `systemd-extensions` case. |
| `systemd.networkd` | Preview | netdev/network, WireGuard peers, and networkd enablement; on Ubuntu only operator-prepared native-networkd targets pass the read-only active-Netplan preflight. | WireGuard examples and three networkd integration cases. |
| `services` | Beta | systemd service enabled/state with running/stopped/restarted/reloaded. | Service tests and integration cases. |
| `nftables` | Beta | `/etc/nftables.conf`, snippet files, validation, and activation. | `nftables` integration case. |
| `docker` | Beta | Docker Engine, daemon, users, and Compose projects. | Docker graph/plan/apply tests and integration cases. |
| `assert` | Beta | Post-merge host/profile assertion that blocks validate/plan/apply on failure. | Parser/merge unit tests. |

## Resource and Provider Types

| Type | Current status | DSL source | Notes |
| --- | --- | --- | --- |
| `system_hostname` | Beta | `system.hostname` | Manages hostname only when declared; removing it forgets state without resetting the remote value. |
| `system_timezone` | Beta | `system.timezone` | Manages timezone only when declared; removing it forgets state without resetting the remote value. |
| `system_locale` | Beta | `system.locale` | Manages default `LANG`, generates glibc locales as needed, and preserves unmanaged `LC_*`. |
| `kernel_module` | Beta | `kernel.modules` | Loads and persists a kernel module. |
| `sysctl` | Beta | `kernel.sysctl` | Writes sysctl configuration and applies runtime values. |
| `package` | Beta | `packages`, `docker.package` | APT package installation. |
| `apt_signing_key` | Beta | `apt.repository.signing_key`, Docker official repo | Manages keyring files. |
| `file` | Beta | `apt.source_file`, `files.file`, Docker daemon/Compose, systemd drop-ins | Manages ordinary files and sensitive summaries. |
| `directory` | Beta | `directories.directory`, Docker Compose | Manages directories. |
| `group` | Beta | `groups.group`, `docker.users` | Manages groups. |
| `user` | Beta | `users.user` | Manages users. |
| `user_group_membership` | Beta | `docker.users` | Adds an existing or declared user to a supplementary group. |
| `systemd_unit` | Beta | `systemd.unit`, `systemd.service_unit`, `systemd.timer`, Docker Compose | Manages systemd unit files. |
| `service` | Beta | `services.service`, systemd timer/resolved/journald, Docker/Compose services | Manages systemd enabled/state. |
| `nftables_file` | Beta | `nftables.file` | Manages nftables files and triggers validate/activate. |
| `component_artifact` | Beta | `component.source` | Downloads or prepares binary/file/archive/ca_certificate/source artifacts. |
| `docker_package_conflicts` | Beta | `docker.package.source = "official"` | Detects and removes conflicting packages according to policy. |
| `docker_compose_project` | Beta | `docker.compose` | Converges a Compose project to running/stopped/absent. |
| `operation` | Beta | Graph operations | Command steps such as APT refresh, daemon-reload, service restart, and Compose config. |

## Docker DSL

| Capability | Current status | Notes |
| --- | --- | --- |
| `docker { enable = true }` | Beta | Uses Docker's official APT repository and packages by default. |
| `package.source = "official"` | Beta | Default; selects the official Docker Debian/Ubuntu repository from the validated distribution and installs docker-ce, CLI, containerd, buildx, and the Compose plugin. |
| `package.repository_url` / `package.gpg_url` | Beta | Overrides official repo/key URLs for the current distribution; custom URLs are not rewritten across distributions. |
| `package.source = "none"` | Beta | Does not install Docker packages but can still manage daemon/service/Compose. |
| `package.source = "custom"` | Beta | User declares repo/key/packages; the Docker block creates no package nodes. |
| `package.channel = "stable"` | Beta | The only implemented channel. |
| `package.version` | Unsupported | Version pinning is not implemented. |
| `remove_conflicts = "auto"/true/false` | Beta | Detects conflicting packages with the official source. |
| `daemon.settings` | Beta | Generates `/etc/docker/daemon.json` and restarts Docker after change. |
| `docker.users` | Beta | Creates/reuses the `docker` group and adds supplementary memberships. |
| `compose "<name>"` | Beta | Manages the Compose project, files, env files, systemd unit, and service. |
| Multiple `compose file` blocks | Unsupported | One primary `file` block per Compose project. |
| Multiple `env_file` blocks | Beta | Manages several env files by label. |
| Generate Compose YAML entirely from HCL | Design-only | Future syntactic convenience, not a primary-path capability. |
| Registry login / rootless Docker / Swarm / Kubernetes / Podman backend | Unsupported | Outside the current MVP. |

## Components and Variables

| Capability | Current status | Notes |
| --- | --- | --- |
| Component artifact `binary` | Beta | Architecture selection, URL SHA-256, and install path. |
| Component artifact `file` | Beta | Downloads or reads and installs a local file. |
| Component artifact `archive` | Beta | Extract/build/install path. |
| Component artifact `ca_certificate` | Beta | Installs a CA and triggers `update-ca-certificates`. |
| Component artifact `source` | Beta | Source-build workflow. |
| Component input type system | Beta | Primitive, list/map/set/object/tuple/optional. |
| Component input validation | Beta | Current-input validation with a constrained function set. |
| Component script/on_change | Beta | Runs a script after component file changes with `once`/`each` and trigger-context environment variables. |
| Host-scoped shared script/on_change | Beta | Aggregates component-file triggers by root declaration identity and host; supports `once` only. |
| Sensitive input propagation | Beta | Redacts derived file/unit content from plans and state. |
| Top-level variable | Beta | CLI vars, var files, automatic var files, environment variables, and validation. |
| Ephemeral variable | Beta | Not written to state; constrained in structural fields. |
| `secrets.file` | Compat | Still works with a warning; prefer `variable + files.file` in new configuration. |

## Examples and Integration Verification

| Example or case | Current status | Coverage |
| --- | --- | --- |
| `examples/bbr.dbf.hcl` | Beta | Kernel module, sysctl, assertion. |
| `examples/apt-source-file.dbf.hcl` | Beta | APT source file. |
| `examples/apt-repository.dbf.hcl` | Beta | APT repository and signing key. |
| `examples/bird2.dbf.hcl` | Beta | Component expansion and service/file/package. |
| `examples/component-binary.dbf.hcl` | Beta | Binary artifact; replace SHA-256 before a real apply. |
| `examples/component-source-build.dbf.hcl` | Beta | Source-build component. |
| `examples/component-inputs.dbf.hcl` | Beta | Typed inputs, validation, sensitive data. |
| `examples/component-script-on-change.dbf.hcl` | Beta | Component `files.file.on_change` script operation. |
| `examples/shared-networkd-reload.dbf.hcl` | Beta | Two components' raw networkd files share one host-scoped reload script. |
| `examples/debian12-amd64.dbf.hcl` | Beta | Bookworm amd64 platform assertion and offline-plan smoke. |
| `examples/ubuntu-24.04-preview.dbf.hcl` | Preview | Ubuntu 24.04 noble amd64 tuple and ordinary-file apply/no-op/check without network resources. |
| `examples/ubuntu-26.04-preview.dbf.hcl` | Preview | Ubuntu 26.04 resolute amd64 tuple and ordinary-file apply/no-op/check without network resources. |
| `examples/docker-*.dbf.hcl` | Beta | Docker minimal, official mirror, daemon, Compose, and users. |
| `examples/fleet.dbf.hcl` | Preview | Current syntax overview covering profile/component/host, systemd timer/resolved/journald, Docker, nftables, and combinations. |
| `examples/nftables.dbf.hcl` | Beta | nftables validate/activate. |
| `examples/realistic-systemd-app.dbf.hcl` | Beta | Least-privilege systemd app with user/group, directories, files, unit, and service. |
| `examples/shadowsocks-rust.dbf.hcl` | Beta | shadowsocks-rust binary component, configuration, and systemd service. |
| `examples/systemd-service*.dbf.hcl` | Beta | Raw unit and structured service_unit. |
| `examples/user-group.dbf.hcl` | Beta | Users/groups/directories/files. |
| `examples/variable-secret-file.dbf.hcl` | Beta | Variable plus sensitive file write. |
| `examples/wireguard-networkd.dbf.hcl` | Preview | WireGuard networkd component with multiple peers and interfaces; requires local secrets. |
| `test/integration/libvirt/cases/systemd-extensions` | Preview | `service_config`, timer enable/state and firing, resolved/journald drop-ins, drift repair, and removal. |
| `test/integration/libvirt/cases/script-on-change` | Beta | Script on component-file change, no trigger on no-op, trigger again after configuration update. |
| `test/integration/libvirt/cases/*` | Beta/Preview | 20 Beta cases each on Debian 12/13 amd64 and 20 Preview cases each on Ubuntu 24.04/26.04 amd64, covering platform assertions, validate, online plan, drift check, apply, JSON no-op plan, check, and case-specific assertions. |

## Currently Unsupported or Not Yet Committed

- Real beta user feedback is still being collected; see
  [Beta Feedback and Triage](beta-feedback-triage.md).
- The stable/GA compatibility policy is written, but still needs evidence across several releases.
- `.deb` and APT repository distribution are not implemented; see
  [APT Repository Feasibility](apt-repository-feasibility.md).
- Sudo/become/non-root management connections.
- Ubuntu 22.04, Ubuntu arm64, desktop environments, and other unlisted Ubuntu versions or targets.
- Netplan, NetworkManager, cloud-init, Snap, PPA, and Ubuntu Pro lifecycle management.
- Complete private-registry lifecycle management.
- Rootless Docker, Swarm, Kubernetes, and a Podman backend.
- Low-level DSL resources for individual Docker containers/images/networks/volumes.
- Complete generation of Compose YAML from HCL objects.
