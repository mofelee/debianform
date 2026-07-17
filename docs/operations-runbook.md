<p align="right">
  <strong>English</strong> | <a href="operations-runbook.zh.md">简体中文</a>
</p>

# DebianForm Operations Runbook

This document covers routine DebianForm recovery procedures: state locks, interrupted applies,
state/remote inconsistencies, resource removal and restoration, and common troubleshooting. New
users should still begin with the [Quickstart](quickstart.md). For release operations, see the
[Release Quick Runbook](release-quick-runbook.md).

DebianForm remains in public preview / beta. Before recovering a real host, validate the command
output on a low-risk host or during a test window.

## Core Principles

- Preserve evidence first: save `dbf plan` and `dbf check` output and copies of the remote state and
  lock files.
- Read the plan before deciding whether to fix configuration, restore the remote state, or apply.
- Do not edit state by hand unless you have a backup and have confirmed that the CLI cannot recover.
- Run only one `dbf apply` for a host at a time.
- Manually review every plan that includes delete, destroy, service stop, or package removal.
- Do not paste secret-bearing configuration or logs into a public issue. Plan and state are
  redacted, but you must still protect shell history and external-command logs yourself.

Example working variables:

```bash
export DBF_FILE=site.dbf.hcl
export DBF_HOST=server1
export DBF_TARGET=192.0.2.10
export DBF_STATE=/var/lib/debianform/state/server1.json
export DBF_LOCK=/var/lock/debianform/state/server1.lock
```

## Capture an Incident Snapshot

Save the current state before investigating a failure:

```bash
dbf validate -f "$DBF_FILE"
dbf plan -f "$DBF_FILE" --host "$DBF_HOST" > dbf-plan.txt 2> dbf-plan.err || true
dbf check -f "$DBF_FILE" --host "$DBF_HOST" > dbf-check.txt 2> dbf-check.err || true
ssh root@"$DBF_TARGET" "test -f '$DBF_STATE' && cp -a '$DBF_STATE' '$DBF_STATE.bak.$(date -u +%Y%m%dT%H%M%SZ)' || true"
ssh root@"$DBF_TARGET" "test -f '$DBF_LOCK' && cat '$DBF_LOCK' || true"
```

When `check` finds drift, it returns a non-zero status and prints:

```text
dbf: remote state does not match configuration
```

The current message retains a historical format name. Its meaning is that the remote state and the
current configuration differ.

This usually means that the remote state has drifted from configuration or that unapplied changes
exist. Read the plan in `dbf-check.txt` first.

## State and Lock Paths

Each host uses a separate remote state file by default:

```text
/var/lib/debianform/state/<host>.json
```

The default lock file, compatibility lock directory, and internal guard are:

```text
/var/lock/debianform/state/<host>.lock
/var/lock/debianform/state/<host>.lock.d/
/var/lock/debianform/state/<host>.lock.d/owner.v2
/var/lock/debianform/state/<host>.lock.guard
```

Configuration can override them:

```hcl
host "server1" {
  state {
    path      = "/var/lib/debianform/state/server1.json"
    lock_path = "/var/lock/debianform/state/server1.lock"
  }
}
```

State uses JSON schema version `2`. Apply writes state immediately after each resource node
succeeds, so after a partial failure state records only the nodes that completed successfully.

## Stale Locks

Symptom:

```text
timed out waiting for state lock /var/lock/debianform/state/server1.lock
```

Procedure:

1. Confirm that no other `dbf apply` is still running:

   ```bash
   ps aux | grep '[d]bf apply'
   ssh root@"$DBF_TARGET" "cat '$DBF_LOCK' 2>/dev/null || true"
   ```

2. If the command simply did not wait long enough, rerun it with a longer timeout.
   `--lock-timeout` controls only how long a waiter waits; it does not change the holder's default
   two-minute lease. An active holder renews that lease every 30 seconds:

   ```bash
   dbf apply -f "$DBF_FILE" --host "$DBF_HOST" --lock-timeout 10m
   ```

3. If a valid version 2 lease has expired, DebianForm revalidates it under the guard and takes it
   over automatically, writing this message to stderr:

   ```text
   taking over stale lock /var/lock/debianform/state/server1.lock
   ```

4. DebianForm does not automatically take over a lock directory without a `.d/owner.v2` marker or
   a legacy/unknown lease, because an older holder may still be writing metadata after creating the
   directory. Remove a lock manually only after confirming that no apply process is running and
   that automatic takeover is clearly impossible. Do not remove `.guard` at the same time; another
   process may already have it open while waiting:

   ```bash
   ssh root@"$DBF_TARGET" "cp -a '$DBF_LOCK' '$DBF_LOCK.manual-backup.$(date -u +%Y%m%dT%H%M%SZ)' 2>/dev/null || true"
   ssh root@"$DBF_TARGET" "rm -f '$DBF_LOCK' && rm -rf '$DBF_LOCK.d'"
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   ```

If unlock reports:

```text
state lock token mismatch for /var/lock/debianform/state/server1.lock
```

the token held by the current process differs from the remote lock token. Do not immediately retry
concurrent applies. First determine whether another process has taken over the lock, then use the
manual cleanup procedure above if necessary.

## Apply Fails Partway Through

Symptom:

```text
host.server1.files.file["/etc/app/config.yaml"] failed: ...
```

Procedure:

1. Save the failure output and a backup of remote state:

   ```bash
   ssh root@"$DBF_TARGET" "test -f '$DBF_STATE' && cp -a '$DBF_STATE' '$DBF_STATE.failed.$(date -u +%Y%m%dT%H%M%SZ)' || true"
   ```

2. Run an online plan to see which resources still need changes:

   ```bash
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   ```

3. Correct the provider's root cause, such as target-path permissions, an unreachable APT source,
   an invalid systemd unit, a failed Docker Compose `config` check, or a remote network problem.

4. Apply again:

   ```bash
   dbf apply -f "$DBF_FILE" --host "$DBF_HOST"
   ```

5. After apply completes, confirm no-op and check:

   ```bash
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   dbf check -f "$DBF_FILE" --host "$DBF_HOST"
   ```

Do not delete the entire state merely because apply failed partway through. State already records
successful nodes. Deleting it removes DebianForm's ownership context and may cause unnecessary
adopt/create/destroy actions in the next plan.

## Investigate Remote Calls with the Apply Debugger

When ordinary progress logs do not reveal which remote command, stdin payload, or stdout/stderr
caused a failure, temporarily use `apply --debug`:

```bash
dbf apply -f "$DBF_FILE" --host "$DBF_HOST" --debug --auto-approve
```

Starting with fact discovery, the debugger intercepts every SSH call. It writes the call number,
host, phase, address, action, summary, remote script or command, and stdin/stdout/stderr summaries to
stderr. Common commands are:

```text
step
next 5
continue
show
show stdin
show stdout
show stderr
retry
quit
```

After a failure, the prompt becomes `(dbfdbg failed)`. Do not use `step`, `next`, or `continue` in
that state. First inspect the failure context with `show stderr`, `show stdout`, or `show stdin`. If
you fix a transient remote problem, enter `retry` to rerun the same remote call. If execution cannot
continue, enter `quit`. DebianForm cancels ordinary calls but still makes a best effort to run
cleanup calls such as state unlock.

`apply --debug` is a high-risk troubleshooting mode. Fully expanded data may include secrets,
remote scripts, stdin payloads, stdout, or stderr. Long text and binary data show summaries by
default and expand only after an explicit `show ...`. When retaining logs, save stdout and stderr
separately and handle both as sensitive logs:

```bash
dbf apply -f "$DBF_FILE" --host "$DBF_HOST" --debug --auto-approve \
  > dbf-apply.out 2> dbf-debug.err
```

Debug mode forces remote calls to run serially; `--debug --parallel 2` fails. When investigating a
multi-host problem, first add `--host "$DBF_HOST"` to narrow the scope.

## State and Remote Host Disagree

Symptom:

```text
dbf: remote state does not match configuration
```

Procedure:

1. Read the plan in `check` output and identify the difference:

   ```bash
   dbf check -f "$DBF_FILE" --host "$DBF_HOST"
   ```

2. If a manual remote change is wrong and configuration is authoritative, repair it with apply:

   ```bash
   dbf apply -f "$DBF_FILE" --host "$DBF_HOST"
   ```

3. If the remote change is desired, update `.dbf.hcl`, validate it, and then plan:

   ```bash
   dbf validate -f "$DBF_FILE"
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   ```

4. If an external system manages the resource, first stop two tools from managing the same file,
   service, or package. Before removing the resource from DebianForm configuration, run plan to
   determine whether removal forgets or destroys it.

5. If state may be corrupt, back it up before checking its JSON syntax:

   ```bash
   ssh root@"$DBF_TARGET" "cp -a '$DBF_STATE' '$DBF_STATE.corrupt.$(date -u +%Y%m%dT%H%M%SZ)'"
   ssh root@"$DBF_TARGET" "python3 -m json.tool '$DBF_STATE' >/dev/null"
   ```

Only when state JSON cannot be parsed and no usable backup exists should you consider moving state
aside before replanning. Expect to lose ownership information:

```bash
ssh root@"$DBF_TARGET" "mv '$DBF_STATE' '$DBF_STATE.disabled.$(date -u +%Y%m%dT%H%M%SZ)'"
dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
```

## Resource Removal and Restoration

Removing a resource from configuration is not the same as declaring it absent:

- Removal from configuration: state ownership determines whether the action is destroy or forget.
- `ensure = "absent"`: explicitly requests deletion of the remote object.
- `lifecycle.prevent_destroy = true`: blocks delete, destroy, and replace during planning.

Before removing a resource:

```bash
dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
```

If the plan contains deletion or destruction, confirm that the action is intended. Protect a
high-risk resource in configuration when appropriate:

```text
files {
  file "/etc/critical.conf" {
    content = file("./critical.conf")

    lifecycle {
      prevent_destroy = true
    }
  }
}
```

If configuration was deleted accidentally but has not been applied:

1. Restore the `.dbf.hcl` file from Git.
2. Run `dbf plan` and confirm that the result returns to no-op or contains only intended updates.

If apply already deleted the resource:

1. Restore configuration from Git or a backup.
2. Run `dbf apply` to recreate the resource.
3. For resources with runtime state, such as services, Docker Compose projects, or nftables, run
   `dbf check` afterward.

If you only want DebianForm to stop managing an existing resource, do not delete the remote object
directly. Adopt/forget capabilities are not yet uniform across resource types. Use `dbf plan` to
inspect the removal action first. If it reports destroy, use explicit lifecycle protection or wait
for that resource to provide a safe forget path.

## Common Troubleshooting

### SSH Is Unreachable

Common output:

```text
ssh: connect ...
Permission denied (publickey)
```

Diagnostic command:

```bash
ssh -vvv \
  -o BatchMode=yes \
  -o NumberOfPasswordPrompts=0 \
  -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no \
  root@"$DBF_TARGET" true
```

Check the network, port, root SSH key, agent, `ssh.identity_file`, `ProxyCommand`/`ProxyJump`, and
target `sshd_config`. If jump-host configuration uses `ProxyCommand ssh jump ...`, give the inner
SSH process `-o BatchMode=yes -o NumberOfPasswordPrompts=0 -o PasswordAuthentication=no
-o KbdInteractiveAuthentication=no` as well, or it may fall back to password or askpass.

With a 1Password SSH agent or another agent that needs desktop authorization, a multi-host apply
may show several authorization prompts at once. Validate first with `dbf apply --parallel 1 ...`,
then raise parallelism gradually. In each concrete `Host` entry in `~/.ssh/config`, set
`IdentityFile` and `IdentitiesOnly yes` to reduce how many keys the agent tries per host. DebianForm
does not currently support sudo, become, or non-root management connections.

### Insufficient Permissions

Common output:

```text
Permission denied
Read-only file system
Operation not permitted
```

First confirm that DebianForm connects through root SSH, the target filesystem is writable, and the
host is not constrained by a maintenance window, read-only snapshot, or external configuration
manager. DebianForm does not elevate through sudo. If an ordinary SSH login still requires sudo,
the current version cannot manage that host.

### Non-root Management Connection

Common output:

```text
ssh.user must be "root" or omitted
```

Remove `ssh.user` from configuration or set it to `"root"`, then confirm that root key-based login
works.

### Offline Plan Lacks Runtime Facts

Common output:

```text
offline plan cannot resolve runtime facts
must declare platform.architecture
must declare platform.codename
```

Run an online plan for a real host, or declare a matching `host.platform` only in a local fixture:

```text
platform {
  distribution = "debian"
  version      = "13"
  architecture = "amd64"
  codename     = "trixie"
}
```

### Declared Facts Do Not Match the Remote Host

Common output:

```text
declared platform.architecture "arm64" does not match detected architecture "amd64"
declared platform.codename "bookworm" does not match detected codename "trixie"
```

For a real managed host, prefer removing handwritten platform facts and letting online discovery
populate them. For an offline fixture, correct `distribution`, `version`, `architecture`, and
`codename` to the target's real values.

### Target Fact Discovery Fails

Common output:

```text
discover host facts for server1: architecture is empty
discover host facts for server1: codename is empty
```

Confirm that the target tuple is supported and that `dpkg --print-architecture` and
`/etc/os-release` are readable:

```bash
ssh root@"$DBF_TARGET" 'dpkg --print-architecture; . /etc/os-release; printf "%s %s %s\n" "$ID" "$VERSION_ID" "$VERSION_CODENAME"'
```

If output is empty, repair the target's base system first. The current allowlist includes Debian
12/13 and the documented architectures for Ubuntu 24.04 and 26.04. Other tuples are rejected before
provider observation.

### Check Fails

Common output:

```text
dbf: remote state does not match configuration
```

This means drift or an unapplied change, not merely a syntax error. Read the plan in check output,
then choose whether to apply, update configuration, or restore remote state.

### `prevent_destroy` Blocks Deletion

Common output:

```text
lifecycle.prevent_destroy
```

Confirm whether the plan's delete/destroy/replace is actually intended. If configuration was
removed accidentally, restore it. If deletion is intentional, remove `prevent_destroy` in the same
change, plan again, review the deletion, and only then apply.

### Docker Compose Config Validation Fails

The common output comes from Docker Compose itself. Diagnose it with:

```bash
ssh root@"$DBF_TARGET" "cd /opt/app && docker compose -p app -f /opt/app/compose.yaml config"
```

Correct the Compose YAML, environment file, or image reference before running `dbf apply` again.
DebianForm does not start a Compose project after config validation fails.

## Recovery Completion Criteria

Before concluding a recovery, run at least:

```bash
dbf validate -f "$DBF_FILE"
dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
dbf check -f "$DBF_FILE" --host "$DBF_HOST"
```

Expected results:

- `validate` succeeds.
- Online plan summary has zero create/update/delete/operations unless a specific pending change
  remains.
- `check` returns 0.
- No state lock remains:

  ```bash
  ssh root@"$DBF_TARGET" "test ! -e '$DBF_LOCK' && test ! -e '$DBF_LOCK.d'"
  ```
