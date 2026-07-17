# DebianForm Deletion-Behavior Diagnostics Design

<p align="right"><strong>English</strong> | <a href="delete-behavior-diagnostics-plan.zh.md">简体中文</a></p>

This document records the requirements discussion for deletion diagnostics. The
conclusion is to abandon a two-stage deletion design and not implement
`prepare-destroy` / `prepare-remove`. Instead, `plan` and `apply` should explain
deletion precisely: what the system removes, what it retains, whether runtime
state is restored, and whether user data may be affected.

## Background

DebianForm takes over existing Debian systems rather than rebuilding them from
scratch like NixOS. When users remove configuration from `.dbf.hcl`, they may
expect the system to return to its pre-management state. For most resources,
the actual semantics are closer to:

- Remove a persistent artifact written by DebianForm.
- Remove the resource record from remote state.
- For adopted or shared resources, stop managing without necessarily restoring
  or destroying the remote object.

Automatic restoration should therefore not be a universal feature. A more
controlled approach is to provide explicit risk labels and explanations in
plan/apply so users know the consequences before execution.

## Goals

- Display deletion-behavior classifications on deletion entries in `plan` and
  `apply`.
- Use color to indicate deletion risk and restoration semantics.
- Display a documentation link at the bottom of `plan` and `apply`, explaining
  colors and deletion semantics.
- Retain machine-readable deletion diagnostics in JSON and HTML plans.
- Provide a matrix of the deletion operation performed by each resource or
  provider type.
- Provide a matrix explaining whether and how default deletion behavior can be
  changed, including resources that do not support customization.

## Non-Goals

- Do not implement `prepare-destroy` / `prepare-remove`.
- Do not guess system defaults during deletion.
- Do not restore pre-management state by default.
- Do not force every resource into a restoration model.
- Do not make color the only signal. Text and JSON fields must carry identical
  semantics for colorless terminals and CI.

## Deletion-Diagnostic Model

Add a deletion-behavior classification to every delete/destroy/forget action:

| Classification | Suggested color | Meaning | User expectation |
| --- | --- | --- | --- |
| `forget` | Gray | Stop managing only in DebianForm state; do not change the remote resource. | The remote system remains as it is. |
| `remove-managed-artifact` | Yellow | Remove a persistent file or managed artifact written by DebianForm. | Runtime state is not guaranteed to be restored. |
| `restore-original` | Blue | Attempt to restore pre-management content saved in state. | Applies only to resources that explicitly support restoration. |
| `destructive` | Red | May delete user data, uninstall packages, delete accounts, stop projects, or recursively delete directories. | Requires close review before execution. |
| `external-side-effect` | Purple | Deletion triggers another operation such as reload, restart, or update. | Review the service-impact window. |
| `unknown` | Bright yellow | The provider cannot state deletion consequences clearly. | Treat conservatively and confirm manually. |

In text output, a deletion entry should add an explanation beneath the existing
summary, for example:

```text
- host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]
  remove sysctl net.ipv4.tcp_congestion_control
  delete behavior: remove-managed-artifact
  note: removes /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf; runtime value is not restored
```

The bottom of `plan` and `apply` should display one consistent notice:

```text
Delete behavior legend: grey=forget, yellow=remove managed artifact, blue=restore original,
red=destructive, purple=external side effect. See docs/delete-behavior-diagnostics-plan.md.
```

## Output-Format Requirements

Text plans:

- Display the classification and explanation for delete/destroy/forget entries.
- Display the color explanation and documentation path at the bottom only when
  a deletion-style action exists.
- Continue displaying the textual classification without color or under
  `NO_COLOR`.

JSON plans:

- Add these fields to `changes[]`:
  - `delete_behavior`
  - `delete_notes`
  - `delete_risk`
- Emit them only for delete/destroy/forget-style actions.

HTML plans:

- Render a colored badge from the same classification.
- Display a color legend and documentation link at the bottom.

Apply:

- `apply` already prints a preview and then the actual execution plan.
- Preserve deletion explanations in both.
- If the actual execution plan contains no deletion, omit the legend.

## Changing Default Deletion Behavior

Deletion behavior must follow explicit configuration; DebianForm must not guess
user intent. Users should express changes to defaults through DSL fields already
supported by the resource. When no such field exists, `plan` / `apply` can only
explain current behavior and limitations. It must not silently restore, destroy,
or retain a remote resource on the user's behalf.

### Basic Rules

- Removing a configuration section from `.dbf.hcl` means it no longer declares
  target state. Whether that causes destroy, forget, restore, or an operation
  depends on the resource provider's semantics.
- Retaining a resource in `.dbf.hcl` and setting `ensure = "absent"`,
  `state = "absent"`, `enabled = false`, `state = "stopped"`, or similar means
  the user explicitly requests convergence to that remote state.
- `lifecycle.prevent_destroy = true` is a safety switch, not a restoration
  strategy. It should block dangerous deletion, replacement, or explicit-absent
  actions so the user must first change configuration or intervene manually.
- `on_destroy` is provider-specific. Only resources that explicitly support it
  may use it; it is not a universal resource field.
- Fields such as `remove_orphans`, `remove_conflicts`, `validate`, and `activate`
  control additional behavior of a particular provider. They do not imply that
  pre-management state can be restored after resource deletion.
- If a resource cannot customize deletion behavior, docs and plans should say
  so explicitly and explain safe alternatives.

### Common Ways to Change Behavior

| User intent | Recommended configuration | Scope | Explanation |
| --- | --- | --- | --- |
| Converge to a known value before deletion | Retain the resource, change the field to the target value, and run `plan/apply`; after confirming the system state, consider removing the configuration. | Resources with target-state fields, including `sysctl`, `service`, and `docker.compose`. | This is an explicitly declared new target, not automatic restoration of pre-management state. |
| Remove configuration but retain the remote artifact | Use provider-supported keep/forget behavior such as `apt.source_file.on_destroy = "keep"`. | Only resources explicitly supporting keep/forget. | A universal field cannot force keep where unsupported. |
| Remove configuration and restore pre-management content | Use provider-supported restoration such as `apt.source_file.on_destroy = "restore"`. | Only resources whose provider supports restoration and whose state retains original content. | Restoration cannot be generalized to secrets, ordinary files, directories, packages, users, and similar resources. |
| Explicitly require the remote object to be absent | Use `ensure = "absent"` or `state = "absent"`. | Resources supporting ensure/state. | This is stronger than removing configuration and should show destructive or external-side-effect risk in the plan. |
| Prevent accidental deletion | Add `lifecycle { prevent_destroy = true }`. | Resources supporting lifecycle. | Appropriate for high-risk resources such as packages, users, directories, files, systemd units, and nftables files. |
| Control cleanup accompanying deletion | Use a provider-specific field such as `docker.compose.remove_orphans`. | Specific providers such as Docker Compose. | Changes only the scope of additional cleanup, not restoration of old state. |
| Avoid reload/activation on deletion | Change provider trigger fields such as `nftables.validate` or `nftables.activate`. | Resources supporting validate/activate. | The matrix must state which operations are affected. |

### BBR / sysctl Example

For BBR, removing
`kernel.sysctl["net.ipv4.tcp_congestion_control"]` should by default remove only
the `/etc/sysctl.d/99-dbf-...conf` file written by DebianForm. It must not guess
and restore a runtime value.

To restore `cubic`, first declare that target explicitly:

```hcl
host "server1" {
  kernel {
    sysctl "net.ipv4.tcp_congestion_control" {
      value = "cubic"
    }
  }
}
```

After `plan/apply`, the system converges to the declared value. If this
configuration is later removed, deletion still only cleans up the persistent
file managed by DebianForm and must not be interpreted as restoring a default.

## Deletion-Behavior Matrix

The following matrix describes current or intended deletion behavior and is the
source for subsequent implementation and tests.

| Resource / provider type | Source DSL | Default remote action on deletion | Restores pre-management state by default | Current customization | Unsupported / limitations | Suggested classification |
| --- | --- | --- | --- | --- | --- | --- |
| `sysctl` | `kernel.sysctl` | Remove `/etc/sysctl.d/99-dbf-<key>.conf`; remove from state. | No | No deletion-behavior field; retain the configuration and change its value to converge explicitly to a desired restoration value. | Cannot restore runtime values automatically or guess defaults. | `remove-managed-artifact` |
| `kernel_module` | `kernel.modules` | Remove `/etc/modules-load.d/dbf-<module>.conf`; attempt `modprobe -r`. | No | No deletion-behavior field. | Cannot request removal of persistence without unload; unloadability depends on kernel state and dependencies. | `external-side-effect` |
| `package` | `packages.install`, Docker packages | Run `apt-get remove -y <package>`, or forget when adopted. | No | Adopted/managed ownership affects behavior indirectly; Docker conflicts use `remove_conflicts`. | Ordinary packages have no explicit `on_destroy = keep/remove` field. | `destructive` |
| `docker_package_conflicts` | `docker.package.remove_conflicts` | Remove conflicting packages; removing configuration normally does not restore them. | No | `remove_conflicts = "auto" / true / false` controls conflict removal. | Cannot reinstall removed conflicts. | `destructive` |
| `apt_signing_key` | `apt.repository.signing_key` | Remove the keyring file. | No | No deletion-behavior field. | Cannot restore old keyring content. | `remove-managed-artifact` |
| `apt_source_file` | `apt.source_file` | Forget under `on_destroy = keep`; under `restore`, restore original content or remove a newly created file. | `restore` only | `on_destroy = "keep"` or `"restore"`. | Applies only to source files; not every file supports restoration. | `forget` or `restore-original` |
| `file` | `files.file`, Docker daemon/Compose files | Remove the target file. | No | Ordinary `files.file` has no deletion-behavior field; `lifecycle.prevent_destroy` can block deletion. | Cannot restore original content; write-only/sensitive content cannot be reconstructed. | `destructive` or `remove-managed-artifact` |
| `secret` | `secrets.file` | Remove the target secret file. | No | `lifecycle.prevent_destroy` can block deletion. | Cannot save or restore original secret plaintext. | `destructive` |
| `directory` | `directories.directory`, Compose directory | May currently remove the directory recursively. | No | `ensure = "absent"` explicitly requests absence; `lifecycle.prevent_destroy` can block deletion. | No field to forget without removing the directory; directory content cannot be restored safely. | `destructive` |
| `group` | `groups.group`, Docker group | Remove the group. | No | `ensure = "absent"` explicitly requests absence; `lifecycle.prevent_destroy` can block deletion. | Cannot restore original membership; deleting a system group is high risk. | `destructive` |
| `user` | `users.user` | Remove the user. | No | `ensure = "absent"` explicitly requests absence; `lifecycle.prevent_destroy` can block deletion. | Home/spool deletion policy needs confirmation; original account state cannot be restored. | `destructive` |
| `user_group_membership` | `docker.users` | Remove the user from the supplementary group. | No | Controlled indirectly by the `docker.users` list. | No per-user membership deletion policy; existing sessions do not update immediately. | `external-side-effect` |
| `ssh_authorized_key` | `users.user.authorized_keys` | Remove the authorized-key line. | No | Controlled by the key list; `lifecycle.prevent_destroy` protects within the owning user resource boundary. | Cannot restore the prior key set after replacement or deletion. | `destructive` |
| `systemd_unit` | `systemd.unit`, `systemd.service_unit`, Compose unit | Remove the unit file and run daemon-reload. | No | `lifecycle.prevent_destroy` can block deletion; `services.service` separately controls service state. | Cannot restore pre-management unit content. | `external-side-effect` |
| `service` | `services.service`, Docker service, Compose service | Removing configuration usually forgets or does not change the service directly; explicit desired state can stop/disable it. | No | Set desired state through `enabled` and `state`. | Removing configuration must not mean stop/disable; current behavior needs implementation review. | `forget` or `external-side-effect` |
| `nftables_file` | `nftables.file` | Remove the nftables file and trigger validation/activation. | No | `validate` and `activate` control post-change operations; `lifecycle.prevent_destroy` can block deletion. | Cannot restore old rule content. | `external-side-effect` |
| `component_download` | `component.source` | Remove the download cache or source file. | No | Controlled indirectly by the component declaration; custom behavior is normally unnecessary. | No restoration. | `remove-managed-artifact` |
| `component_build` | `component.source build` | Remove build output. | No | Controlled indirectly by the component declaration; output can be rebuilt. | No restoration. | `remove-managed-artifact` |
| `component_binary` | `component.install binary` | Remove the binary from its target path. | No | `lifecycle.prevent_destroy` can block deletion. | Cannot restore an old binary. | `destructive` |
| `component_file` | `component.install file` | Remove the installed file. | No | `lifecycle.prevent_destroy` can block deletion. | Cannot restore an old file. | `destructive` |
| `component_archive` | `component.install archive` | Remove the target directory. | No | `lifecycle.prevent_destroy` can block deletion. | Cannot restore directory content. | `destructive` |
| `component_ca_certificate` | Component CA certificate | Remove the certificate file and trigger `update-ca-certificates`. | No | Controlled indirectly by the component declaration. | Cannot restore an old certificate; affects the system trust chain. | `external-side-effect` |
| `docker_compose_project` | `docker.compose` | Run `docker compose down` or stop/remove the project. | No | `state = "running"`, `"stopped"`, or `"absent"`; `remove_orphans` controls orphan cleanup. | Volume deletion behavior must remain explicit; cannot restore prior Compose project state. | `destructive` |
| `operation` | Graph operations | Not a resource itself; triggered by resource changes. | Not applicable | Controlled indirectly through upstream resource fields such as reload/activate/pull. | Cannot customize deletion behavior independently. | `external-side-effect` |

## Color and Accessibility

- Enable color only in a TTY by default.
- Support `NO_COLOR` to disable color.
- JSON contains classification fields, never ANSI color.
- Text must display category names and cannot rely on color alone.
- Reserve red for genuinely destructive actions or those that may affect user
  data or service availability.
- See `docs/cli-color-output-policy.md` for general colors, log colors, and
  CI/JSON boundaries.

## Verifiable Implementation Loops

### Loop 1: Deletion-Behavior Data Model (Implemented)

Scope:

- Add deletion-behavior fields to plan changes.
- Define classification and risk-level enumerations.
- Have providers/engine return or derive machine-readable classifications and
  notes without constructing colors.

Acceptance:

- Deletion entries in JSON plans expose `delete_behavior`, `delete_notes`, and
  `delete_risk`.
- Create/update/no-op/run omit deletion-behavior fields.
- New plan-format fields follow the compatibility policy.

### Loop 2: Core Provider Classifications (Implemented; Matrix Review Is Ongoing)

Scope:

- First cover high-frequency and high-risk resources: `sysctl`,
  `apt_source_file`, `file`, `secret`, `directory`, `package`, `user`, `group`,
  `systemd_unit`, `nftables_file`, and `docker_compose_project`.
- Classify adopted, keep, shared-directory, and similar cases as `forget`.

Acceptance:

- BBR/sysctl deletion is `remove-managed-artifact`.
- An APT source file with `on_destroy = "keep"` is `forget`; `restore` is
  `restore-original`.
- Directory/package/user/group/Docker Compose project deletion is `destructive`.
- Systemd/nftables and other reload/activation deletion is
  `external-side-effect`.

### Loop 3: Text Plan/Apply Diagnostics (Implemented)

Scope:

- Print `delete behavior` and `note` beneath deletion entries.
- Print a color legend and documentation path at the bottom when deletion exists.
- Preserve complete text under `NO_COLOR` or outside a TTY.

Acceptance:

- Text plans explain deletion without requiring color.
- Plans without deletion omit the legend.
- Both the preview and actual plan in `apply` retain deletion diagnostics.

### Loop 4: HTML Plans and Documentation Synchronization (Partially Implemented)

Scope:

- Render deletion-behavior badges and a legend in HTML plans.
- Synchronize `docs/plan-format.md`, CLI docs, and how-it-works documentation.
- Reconcile differences between the provider matrix and actual implementation.

Acceptance:

- Deletion behavior is visible, and preferably filterable, in HTML.
- Plan JSON documentation lists the new fields and compatibility implications.
- No core matrix entry remains marked as requiring implementation review.

Current status:

- HTML badges and the legend are implemented.
- `docs/plan-format.md`, CLI docs, and how-it-works are synchronized.
- The provider matrix still requires comparison against actual provider
  behavior, especially default deletion policy for `service` and directories.

## Acceptance Criteria

- A BBR deletion plan states clearly that sysctl deletion removes only a
  persistent file and does not restore the runtime value.
- APT source-file `keep` and `restore` appear as distinct classifications.
- Deleting a directory, package, user, or Docker Compose project is marked
  destructive.
- Deleting systemd/nftables/CA-certificate resources that trigger operations is
  marked external side effect.
- The plan/apply legend appears only when deletion exists.
- CI can read deletion behavior from `--format json` without relying on color.

## Open Questions

- Which current provider deletion behaviors differ from the matrix and require
  implementation review?
- When service configuration is removed, should it be forgotten, stopped,
  disabled, or retain current behavior with an explanation?
- Should directories change from destructive-by-default to a more conservative
  forget or require-explicit-absent model?
