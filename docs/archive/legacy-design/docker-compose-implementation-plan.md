# DebianForm Docker / Docker Compose Implementation Plan

<p align="right"><strong>English</strong> | <a href="docker-compose-implementation-plan.zh.md">简体中文</a></p>

This document divides the
[Docker / Docker Compose requirements](docker-compose-requirements.md) into
implementable, verifiable development loops. Every loop must form a mergeable
closed cycle with:

- A runnable code path.
- Tests or goldens for affected parser / merge / HostSpec / ResourceGraph / plan layers.
- Fake-runner or provider tests for apply / check semantics.
- At least one example in acceptance inputs.
- Synchronized documentation.
- Passing `make test`.

Status:

- `[x]` Complete
- `[ ]` Incomplete

## Current Baseline

- [x] Docker / Docker Compose requirements are saved in `docs/archive/legacy-design/docker-compose-requirements.md`.
- [x] Parser, merge, HostSpec, ResourceGraph, plan, apply, and check pipelines exist.
- [x] APT repository, signing key, package, file, directory, group, systemd
  unit, service, and operation providers exist.
- [x] Runtime fact discovery provides online `system.architecture` and `system.codename`.
- [x] Fake-runner / memory-provider test foundations exist.
- [x] Top-level `docker` DSL validation and HostSpec compilation are supported.
- [x] High-level Docker daemon JSON validation and HostSpec compilation are supported.
- [x] High-level Docker Compose project validation and HostSpec compilation are supported.
- [x] `docker.enable` expands into official-source ResourceGraph / offline plans.
- [x] Docker Engine apply / check reuses existing provider loops.
- [x] Docker daemon ResourceGraph / plan / apply / check is complete.
- [x] Compose directory / file / env-file / config-validation ResourceGraph is complete.
- [x] Compose project state expands into ResourceGraph and supports drift detection.
- [x] Compose systemd ResourceGraph / apply / check is complete.
- [x] Docker / daemon / Compose libvirt cases and drift hooks exist.
- [x] Users expand into ResourceGraph.
- [x] Users apply / check is complete.

## Overall Implementation Boundary

MVP completion line:

- `docker { enable = true }` uses Docker's official APT repository by default.
- Install official Docker packages and enable/start `docker.service`.
- Manage `/etc/docker/daemon.json` and restart on change.
- Write Compose directory / Compose file / env files.
- Validate with `docker compose config`.
- Manage Compose project `running` / `stopped` / `absent`.
- Generate and enable/start a Compose systemd unit.
- Complete basic plan / apply / check / drift loop.

Excluded from MVP:

- Generating Compose YAML from HCL.
- Low-level container / image / network / volume resources.
- Registry login.
- Rootless Docker.
- Swarm / Kubernetes.
- Podman backend.
- Full private-registry lifecycle.

## Global Design Constraints

- Users write only high-level `docker` / `docker.compose` DSL, not low-level Docker resources.
- ResourceGraph may reuse existing low-level providers, but plan addresses retain
  Docker domain semantics, for example
  `host.server1.docker.package["docker-ce"]`; debug mode may expose the provider address.
- `docker { enable = false }` means DebianForm does not manage Docker; by
  default, it neither uninstalls packages nor stops services.
- Docker's official APT source depends on target `architecture` and `codename`.
  Online `plan/apply/check` uses runtime facts. `plan --offline` requires
  explicit matching system facts or fails clearly.
- Treat `env_file` as sensitive by default. Plans, state, and logs emit only
  summaries and never `.env` content.
- Unit tests and goldens must not rely on real Docker network downloads. Real
  installation and Compose execution belong in libvirt integration.

## Loop 1: Docker DSL Parsing, Merging, and HostSpec

Goal: validate `docker` blocks and produce stable HostSpec without ResourceGraph yet.

Scope:

- Top-level `docker`.
- `docker.enable`.
- `docker.package`.
- `docker.service`.
- `docker.daemon.settings`.
- `docker.users`.
- `docker.compose "<name>"`.
- `compose.file`.
- `compose.env_file "<name>"`.

Code:

- [x] Parser supports `docker` blocks in host/profile.
- [x] Parser supports labeled `compose "<name>"` and `env_file "<name>"` blocks.
- [x] IR adds `DockerSpec`, `DockerPackageSpec`, `DockerServiceSpec`, and `DockerDaemonSpec`.
- [x] IR adds `DockerComposeSpec`, `DockerComposeFileSpec`, and `DockerComposeEnvFileSpec`.
- [x] Merge combines `docker` blocks across profile and host.
- [x] Merge combines Compose projects by label.
- [x] Merge combines env files by label.
- [x] Default `package.source = "official"` and `package.channel = "stable"`.
- [x] Default `service.enable = true` and `service.state = "running"`.
- [x] Default `compose.enable = true` and `compose.state = "running"`.
- [x] Default `compose.project = <compose label>`.
- [x] Default Compose file owner/group/mode to `root/root/0644`.
- [x] Default env-file owner/group/mode to `root/root/0600`.
- [x] Preserve `daemon.settings` as a JSON-compatible map/list/scalar and reject
  values that cannot serialize to JSON.
- [x] Restrict `package.source` to `official`, `debian`, `none`, or `custom`.
- [x] Restrict `package.remove_conflicts` to `auto`, `true`, or `false`.
- [x] Restrict service state to `running` or `stopped`.
- [x] Restrict Compose state to `running`, `stopped`, or `absent`.
- [x] Restrict Compose `pull` to `never`, `missing`, or `always`.
- [x] Restrict Compose `recreate` to `auto`, `always`, or `never`.
- [x] Require Compose `directory` for an enabled project.
- [x] Validate Compose `file.path` and `file.content/source` semantics.
- [x] Require non-empty stable Compose project labels, project names, and
  systemd service names.

Tests:

- [x] Parser unit test for minimal `docker { enable = true }`.
- [x] Parser unit test for nested daemon map/list settings.
- [x] Parser unit test for Compose file, inline YAML, and several env files.
- [x] Merge unit test for host overrides of profile Docker defaults.
- [x] HostSpec golden for `docker-minimal`.
- [x] HostSpec golden for `docker-daemon`.
- [x] HostSpec golden for `docker-compose`.
- [x] Negative cases for invalid enums, duplicate Compose label, missing
  directory, and invalid file content/source combinations.

Examples:

- [x] Add `examples/docker-minimal.dbf.hcl`.
- [x] Add `examples/docker-daemon.dbf.hcl`.
- [x] Add `examples/docker-compose.dbf.hcl`.

Documentation:

- [x] Add minimal Docker syntax to README and state that this loop completes validate only.

Acceptance:

```bash
dbf validate -f examples/docker-minimal.dbf.hcl
dbf validate -f examples/docker-daemon.dbf.hcl
dbf validate -f examples/docker-compose.dbf.hcl
make test
```

## Loop 2: `docker.enable` Official-Source ResourceGraph and Plan

Goal: compile `docker { enable = true }` into Docker's official APT repository,
official packages, and `docker.service`, with stable plans.

Scope:

- Implement only `package.source = "official"`.
- Implement only the default package list.
- Implement only `docker.service`.
- Defer daemon, users, and Compose.
- Defer `source = "debian" | "none" | "custom"`.
- Defer conflict-package removal.

Code:

- [x] Compile Docker official APT signing-key node.
- [x] Compile Docker official APT repository node.
- [x] Compile host-scoped APT cache-refresh operation.
- [x] Compile default packages:
  - `docker-ce`
  - `docker-ce-cli`
  - `containerd.io`
  - `docker-buildx-plugin`
  - `docker-compose-plugin`
- [x] Compile `docker.service` service node.
- [x] Use target `system.codename` in the official repository.
- [x] Use target `system.architecture` in the official repository.
- [x] Recompile the Docker repository after online plan discovers runtime facts.
- [x] Produce a clear error when offline plan lacks architecture/codename.
- [x] Use high-level addresses for Docker domain nodes while reusing existing
  APT/package/service provider payloads.
- [x] Derive dependencies: signing key -> repository -> APT cache refresh -> packages -> service.
- [x] Generate no Docker nodes when `enable = false`.

Tests:

- [x] ResourceGraph golden covers official signing-key, repository, package,
  and service addresses.
- [x] ResourceGraph unit test covers dependency order.
- [x] Plan JSON golden covers minimal Docker create plan.
- [x] Plan text golden covers high-level Docker addresses.
- [x] Negative case covers offline missing runtime facts.
- [x] Negative case returns a not-implemented diagnostic for
  `package.source = "debian"` in this loop.

Examples:

- [x] Add `examples/docker-minimal.dbf.hcl` to graph / plan goldens.
- [x] Declare `system.architecture` and `system.codename` explicitly for offline goldens.

Documentation:

- [x] State in README that `docker { enable = true }` can be planned.
- [x] Link this implementation status beside the Docker requirements.

Acceptance:

```bash
dbf plan -f examples/docker-minimal.dbf.hcl --offline
dbf plan -f examples/docker-minimal.dbf.hcl --offline --format json
make test
```

## Loop 3: Docker Engine Apply / Check Loop

Goal: apply/check Loop 2 Docker Engine resources through existing providers,
idempotently.

Code:

- [x] Confirm Docker official signing-key nodes use `apt_signing_key` provider.
- [x] Confirm Docker repository nodes use existing file / APT-source provider.
- [x] Confirm Docker package nodes use existing package provider.
- [x] Confirm `docker.service` uses existing service provider.
- [x] If high-level Docker addresses affect state, add state ownership / provider-address tests.
- [x] After apply, state records high-level Docker addresses and low-level provider addresses.
- [x] Check detects missing packages, disabled service, and stopped service.

Tests:

- [x] NativeProvider fake-runner test for Docker signing-key apply script.
- [x] NativeProvider fake-runner test for Docker package commands.
- [x] NativeProvider fake-runner test for Docker service enable/start.
- [x] Engine fake apply is immediately no-op on the next plan.
- [x] Check exit-status test covers Docker service drift.

Examples:

- [x] Use `examples/docker-minimal.dbf.hcl` in fake-runner apply tests.

Documentation:

- [x] State in README that Docker Engine supports apply/check.

Acceptance:

```bash
dbf plan -f examples/docker-minimal.dbf.hcl --offline
make test
```

## Loop 4: Docker Daemon Configuration

Goal: manage `/etc/docker/daemon.json` through `docker.daemon.settings` and
restart Docker after changes.

Code:

- [x] Serialize `daemon.settings` deterministically to `/etc/docker/daemon.json`.
- [x] Compile a Docker daemon file node defaulting to `root/root/0644`.
- [x] Make the daemon file depend on Docker package installation.
- [x] Make `docker.service` depend on the daemon file so configuration precedes first start.
- [x] Compile `docker.daemon.restart` operation.
- [x] Trigger restart on daemon-file change.
- [x] Make restart depend on daemon file and `docker.service`.
- [x] Allow daemon-file and service management with `package.source = "none"`.
- [x] Show daemon JSON content diffs or summaries for drift.

Tests:

- [x] HostSpec golden covers nested daemon settings.
- [x] ResourceGraph golden covers daemon file and restart operation.
- [x] Plan text golden covers line-level daemon JSON diff.
- [x] NativeProvider fake-runner test covers daemon-file write.
- [x] After fake apply, changing daemon desired produces a restart operation.
- [x] Negative case rejects daemon settings that cannot serialize to JSON.

Examples:

- [x] Add `examples/docker-daemon.dbf.hcl` to goldens.

Documentation:

- [x] Add daemon settings example to README.
- [x] State that MVP always restarts rather than attempting granular reload.

Acceptance:

```bash
dbf plan -f examples/docker-daemon.dbf.hcl --offline
dbf plan -f examples/docker-daemon.dbf.hcl --offline --format json
make test
```

## Loop 5: Compose Files, Env Files, and Configuration Validation

Goal: write the Compose project directory, Compose file, and env files, and run
`docker compose config` before apply.

Scope:

- `compose "<name>"`.
- `directory`.
- `file`.
- Several `env_file` blocks.
- `docker compose config`.
- Defer project up/stop/down.
- Defer systemd unit generation.

Code:

- [x] Compile Compose working-directory node defaulting to `root/root/0755`.
- [x] Compile Compose file node defaulting to `root/root/0644`.
- [x] Compile env-file nodes defaulting to `root/root/0600`.
- [x] Default env files to `sensitive = true` or `content_write_only = true`.
- [x] Make Compose/env files depend on the working directory.
- [x] Make Compose/env files depend on Docker Engine packages/service unless
  `package.source = "none"`.
- [x] Compile `docker.compose["<name>"].validate` operation.
- [x] Preview validation as `docker compose -p <project> -f <file> config`.
- [x] Make validation depend on Compose and env files.
- [x] Make later project-state and systemd nodes depend on validation.
- [x] Reject path conflicts among Compose/env files and
  files/secrets/nftables/networkd/systemd units on the same host.

Tests:

- [x] ResourceGraph golden covers directory, Compose file, env files, and validation operation.
- [x] Plan golden covers line-level Compose YAML diff.
- [x] Sensitive tests ensure env-file content is absent from plan/state/logs.
- [x] Operation unit test covers validation command preview.
- [x] Negative cases cover path conflicts, duplicate env-file label, and missing Compose file.

Examples:

- [x] Add `examples/docker-compose.dbf.hcl` to graph / plan goldens.

Documentation:

- [x] Add Compose-file management example to README.
- [x] State that DebianForm neither parses nor rewrites the Compose schema.

Acceptance:

```bash
dbf plan -f examples/docker-compose.dbf.hcl --offline
dbf plan -f examples/docker-compose.dbf.hcl --offline --format json
make test
```

## Loop 6: Compose Project-State Provider

Goal: plan/apply/check Compose project `running`, `stopped`, and `absent`.

Code:

- [x] Add provider kind `docker_compose_project`.
- [x] Compile `host.<host>.docker.compose["<name>"].project` node.
- [x] Include directory, project, Compose files, env files, and `state` in desired.
- [x] Include `pull`, `recreate`, and `remove_orphans` in desired.
- [x] Make the project node depend on validation.
- [x] Make the project node depend on Docker Engine service unless
  `package.source = "none"`.
- [x] Provider plan reads current Compose project state.
- [x] Provider apply runs `docker compose up -d` for `running`.
- [x] Provider apply runs `docker compose stop` for `stopped`.
- [x] Provider apply runs `docker compose down` for `absent`.
- [x] Map `pull = never|missing|always` to supported Compose command behavior.
- [x] Map `recreate = auto|always|never` to supported Compose command behavior.
- [x] Add orphan cleanup to up/down when `remove_orphans = true`.
- [x] Provider observed summaries contain no container environment variables or secrets.
- [x] Check detects a project that is not running, is stopped, or is absent.

Tests:

- [x] NativeProvider fake-runner test generates up for `running`.
- [x] NativeProvider fake-runner test generates stop for `stopped`.
- [x] NativeProvider fake-runner test generates down for `absent`.
- [x] NativeProvider fake-runner test covers pull/recreate/remove-orphans flags.
- [x] Provider plan tests cover running/no-op, stopped drift, and absent/no-op.
- [x] Engine fake apply is immediately no-op on the next plan.
- [x] Check exit-status test covers stopped Compose project drift.

Examples:

- [x] Use `examples/docker-compose.dbf.hcl` in fake-runner apply tests.

Documentation:

- [x] State that Compose project state supports apply/check.
- [x] Document actual command mapping for state/pull/recreate/remove-orphans.

Command mapping:

- `state = "running"`: `docker compose -p <project> -f <file> up -d`.
- `state = "stopped"`: `docker compose -p <project> -f <file> stop`.
- `state = "absent"`: `docker compose -p <project> -f <file> down`.
- `pull = "never" | "missing" | "always"`: append `--pull never|missing|always` to `up -d`.
- `recreate = "auto"`: append no recreate flag and let Compose decide.
- `recreate = "always"`: append `--force-recreate` to `up -d`.
- `recreate = "never"`: append `--no-recreate` to `up -d`.
- `remove_orphans = true`: append `--remove-orphans` to `up -d` and `down`.

Acceptance:

```bash
dbf plan -f examples/docker-compose.dbf.hcl --offline
make test
```

## Loop 7: Compose systemd Unit Integration

Goal: generate a systemd unit for each Compose project and use systemd for boot
startup and service supervision.

Code:

- [x] Generate default unit name `debianform-compose-<name>.service`.
- [x] Support `compose.service.enable`.
- [x] Support `compose.service.name`.
- [x] Support `after`.
- [x] Support `wanted_by`.
- [x] Include `Requires=docker.service` in the unit.
- [x] Include default `After=docker.service network-online.target`.
- [x] Use the Compose directory for unit `WorkingDirectory`.
- [x] Use `docker compose -p <project> -f <file> up -d` for unit `ExecStart`.
- [x] Use `docker compose -p <project> -f <file> stop` for unit `ExecStop`.
- [x] Compile the systemd unit node through existing `systemd_unit` provider.
- [x] Compile systemd daemon-reload operation.
- [x] Compile Compose service node through existing service provider.
- [x] Default service to enabled/running for `state = "running"`.
- [x] Default service to enabled/stopped for `state = "stopped"`.
- [x] Default service to disabled/stopped for `state = "absent"`, with project provider down.
- [x] Keep project-state provider and systemd service ordering stable and idempotent.

Tests:

- [x] ResourceGraph golden covers generated systemd unit.
- [x] Plan golden covers unit-content diff.
- [x] Unit tests cover default and custom unit names.
- [x] Unit tests cover running/stopped/absent effects on service desired.
- [x] Engine fake-runner test covers daemon-reload and service enable/start.
- [x] Negative cases reject invalid service names and empty after/wanted-by values.

Examples:

- [x] Document default systemd behavior in `examples/docker-compose.dbf.hcl`.

Documentation:

- [x] Add a short Compose systemd unit explanation to README.
- [x] Record current unit-template limits beside the Docker requirements.

Acceptance:

```bash
dbf plan -f examples/docker-compose.dbf.hcl --offline
dbf plan -f examples/docker-compose.dbf.hcl --offline --format json
make test
```

## Loop 8: MVP End-to-End Integration and Drift Cases

Goal: verify the Docker Engine, daemon, Compose project, systemd, and check/drift
MVP loop on a real Debian test host.

Code/tests:

- [x] Add libvirt case `docker-engine`.
- [x] Add libvirt case `docker-daemon`.
- [x] Add libvirt case `docker-compose`.
- [x] `docker-engine` verifies official repository exists.
- [x] `docker-engine` verifies official packages are installed.
- [x] `docker-engine` verifies `docker.service` is enabled/running.
- [x] `docker-daemon` verifies `/etc/docker/daemon.json` content and permissions.
- [x] `docker-compose` uses a minimal BusyBox Compose project.
- [x] `docker-compose` verifies `docker compose config` ran.
- [x] `docker-compose` verifies the Compose project is running.
- [x] `docker-compose` verifies generated systemd unit content.
- [x] `docker-compose` verifies the systemd service is enabled/running.
- [x] Drift case: after manual daemon JSON change, `dbf check` is nonzero.
- [x] Drift case: after manual compose.yaml change, `dbf check` is nonzero.
- [x] Drift case: after manually stopping the Compose project, `dbf check` is nonzero.
- [x] Drift case: after manually disabling the generated unit, `dbf check` is nonzero.

Documentation:

- [x] Mark the available Docker / Compose MVP scope in README.
- [x] Add Docker MVP usage example and limits.
- [x] State that real installation requires access to Docker's official repository.

Acceptance:

```bash
make test
make test-integration-layout
make test-integration-case CASE=docker-engine
make test-integration-case CASE=docker-daemon
make test-integration-case CASE=docker-compose
```

MVP completion:

- [x] Online apply of `docker { enable = true }` is no-op under check afterward.
- [x] Daemon settings changes plan a file diff and restart.
- [x] Compose file changes plan a file diff, validation, and project convergence.
- [x] Check detects stopped Compose-project drift.
- [x] `make test` and the libvirt cases above pass.

Note: a full libvirt case requires a runner that can create a Debian 13 VM on
the current libvirt URI. The single-host runner supports remote
`qemu+ssh://...` and puts VM disk/seed under a hypervisor-visible storage-pool
path. The two-host WireGuard runner still uses local libvirt paths.

## Loop 9: Docker Users and Docker Group Membership

Goal: support `docker.users = ["deploy"]` by adding users to the `docker` group
without taking over their complete definitions.

Scope:

- `docker.users`.
- `group["docker"]`.
- `user_group_membership`.

Code:

- [x] Add IR or graph-level membership spec.
- [x] Add provider kind `user_group_membership`.
- [x] Compile `docker.users` into a `docker` group node.
- [x] Compile one Docker group-membership node per user.
- [x] Manage only supplementary membership, not user home/shell/UID.
- [x] Make membership depend on the `docker` group.
- [x] If the same host declares the user, make membership depend on its user node.
- [x] Warn in plan/apply that a new login is required for group-session changes.
- [x] Check detects a user missing from the Docker group.

Tests:

- [x] ResourceGraph golden covers Docker group and membership.
- [x] NativeProvider fake-runner test covers `usermod -aG docker <user>`.
- [x] Provider plan test covers no-op for existing membership.
- [x] Provider plan test gives a clear diagnostic for a missing user.
- [x] Negative case rejects an empty username.

Examples:

- [x] Add `examples/docker-users.dbf.hcl`.

Documentation:

- [x] Add Docker users example to README.
- [x] Explain that permissions take effect after a new login.

Acceptance:

```bash
dbf plan -f examples/docker-users.dbf.hcl --offline
make test
```

## Loop 10: Package-Source Variants and Conflict Handling

Goal: complete `package.source = "debian" | "none" | "custom"` and
`remove_conflicts`.

Scope:

- `package.source = "debian"`.
- `package.source = "none"`.
- `package.source = "custom"`.
- `package.remove_conflicts`.
- Docker conflict-package detection and removal.

Code:

- [x] `source = "debian"` generates no Docker official repository.
- [x] `source = "debian"` defaults to `docker.io` and `docker-compose-plugin`.
- [x] `source = "none"` generates no repository or package nodes.
- [x] `source = "none"` still allows daemon, service, and Compose.
- [x] `source = "custom"` generates no repository/key/package and expects
  user-declared dependencies or accepts no package dependency.
- [x] Add a conflict-detection node or provider-plan extension.
- [x] Detect conflicting packages:
  - `docker.io`
  - `docker-doc`
  - `docker-compose`
  - `podman-docker`
  - `containerd`
  - `runc`
- [x] `remove_conflicts = "auto"` plans replacements and apply removes conflicts
  that require replacement.
- [x] `remove_conflicts = true` forcibly removes installed conflicts.
- [x] `remove_conflicts = false` fails plan/apply with guidance when conflicts exist.
- [x] Run conflict-package removal before official Docker package installation.

Tests:

- [x] HostSpec golden covers debian/none/custom sources.
- [x] ResourceGraph golden covers `source = "debian"`.
- [x] ResourceGraph golden covers `source = "none"`.
- [x] Provider fake-runner test covers conflict detection.
- [x] Provider fake-runner test covers conflict removal.
- [x] Negative case covers conflicts under `remove_conflicts = false`.

Examples:

- [x] Add `examples/docker-package-sources.dbf.hcl`.

Documentation:

- [x] Document source variants and conflict-package policy.

Acceptance:

```bash
dbf plan -f examples/docker-package-sources.dbf.hcl --offline
make test
```

## Loop 11: Compose Multi-File and Operational Enhancements

Goal: add common Compose operational details that do not block MVP.

Scope:

- Several Compose files.
- Project-name update policy.
- Orphan-container detection.
- More granular drift output.
- More complete `pull` / `recreate` behavior.

Code:

- [x] Support several `file` blocks or reject them explicitly with a future-version error.
- [x] Compose provider observed data records a project service/container summary.
- [x] Check reports orphan containers.
- [x] Apply cleans orphans under `remove_orphans = true`.
- [x] Project-name changes show old-project down and new-project up impact.
- [x] State stores Compose file/env-file desired digests and project desired digest.
- [x] Plans show Compose project-state drift instead of only low-level commands.

Tests:

- [x] Provider fake-runner coverage for orphan containers.
- [x] Provider fake-runner coverage for project rename.
- [x] Plan golden covers project-state drift output.
- [x] Check exit-status test covers orphan drift.

Documentation:

- [x] Document Compose drift-detection scope and limitations.

Acceptance:

```bash
make test
```

## Loop 12: Future Extension Boundary

Goal: reserve clear boundaries for later versions outside MVP.

Candidate capabilities:

- HCL `spec` generation of Compose YAML.
- Registry login.
- Rootless Docker.
- Docker mirror configuration.
- Image lifecycle.
- Volume lifecycle.
- Network lifecycle.
- Secret management.
- Podman backend.

Before implementation, add a separate requirements document or extension plan;
do not insert these capabilities directly into the MVP loops.
