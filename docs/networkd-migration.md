<p align="right">
  <strong>English</strong> | <a href="networkd-migration.zh.md">简体中文</a>
</p>

# Migrating systemd-networkd ownership

This guide covers migrations into the Preview native `systemd.networkd` provider. It does not
promote the provider to Beta or Stable, and it does not authorize DebianForm to take ownership from
Netplan, NetworkManager, or another configuration manager.

## Scope and support level

Native networkd resources keep the existing `networkd_netdev` and `networkd_network` addresses,
ownership preflight, drift detection, reload aggregation, lifecycle protection, and runtime netdev
cleanup. Generic `section` blocks and raw `content`/`source` are content forms of those resources,
not aliases for `files.file`.

Use a maintenance window and recovery access for any ownership change that can affect SSH or
routing. DebianForm does not provide transactional network rollback.

## Compatibility syntax to generic sections

Changing an existing native `netdev` or `network` resource from compatibility attributes to generic
sections keeps its DebianForm resource address and provider identity. Before apply:

- keep the resource label and `path` unchanged;
- render both forms and compare the complete file bytes, owner, group, and mode;
- review the plan for an in-place update only, with no destroy/create action;
- retain the prior configuration for rollback.

Generic sections use declaration order for sections and lexical order for setting keys. Compatibility
attributes retain their historical rendering order, so mechanically rewriting syntax is not a claim
of byte equality. Raw-to-structured changes have the same review requirement.

## files.file to native networkd

Changing `files.file` into `systemd.networkd.netdev` or `systemd.networkd.network` changes the state
address and provider kind. DebianForm does not automatically adopt the old record and the current
`moved` block supports component-instance moves, not leaf-resource ownership transfer. The two
declarations also cannot coexist at one remote path because path ownership must be unique.

### Before the handoff

- confirm the file is owned by exactly one declaration and inspect current remote state ownership;
- run `dbf check` and save text and JSON plans;
- back up the remote state file and every affected networkd file with permissions preserved;
- verify a local console, out-of-band console, or second management path works;
- record current `networkctl status`, addresses, routes, and routing-daemon state;
- use `lifecycle { prevent_destroy = true }` on the old resource until the handoff is approved.

For Ubuntu, complete the read-only ownership preflight first. Active Netplan ownership is a blocker,
not a migration input.

### Reviewed handoff

Remove the old `files.file` declaration and add the native resource with the same `path`. The plan is
expected to show the old file resource being destroyed and the new native resource being created or
adopted according to observed state. Do not apply if the plan includes an unexpected runtime netdev
deletion, a path change, or unrelated network changes.

Run the handoff from the recovery path, then verify:

```bash
dbf plan -f site.dbf.hcl
dbf apply -f site.dbf.hcl
dbf check -f site.dbf.hcl
networkctl status
ip address show
ip route show table all
```

The transition can briefly remove and recreate a managed file. DebianForm does not claim a
zero-downtime migration from `files.file`, and an apply interruption must be recovered manually.

### Rollback

Keep the old configuration, state backup, and networkd file backup until post-change checks pass.
If rollback is required, use the recovery path, restore a consistent set of configuration, state,
and files, reload networkd, restore routing-daemon exports, and run `dbf plan` before another apply.
Do not restore only the state file while leaving new ownership active.

## Ubuntu ownership boundary

DebianForm does not generate, edit, disable, or migrate Netplan. Ubuntu networkd management is
limited to targets that an operator has already prepared outside DebianForm as persistent native
systemd-networkd hosts. If active Netplan ownership is detected, plan/apply fails before provider
changes. NetworkManager management is also outside scope.
