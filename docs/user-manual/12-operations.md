<p align="right">
  <strong>English</strong> | <a href="12-operations.zh.md">简体中文</a>
</p>

# 12. Daily Operations: Plan Review, Drift, Locks, State, and Recovery

This chapter collects the operations used throughout the manual into a daily runbook: reviewing a
plan, generating JSON and HTML plans, detecting drift with `check`, inspecting state, and handling a
state lock.

The example has been verified on a Debian 13 amd64 test host. It manages one small file:
`/etc/debianform-manual/operations.txt`.

## Create a Working Directory

```bash
mkdir -p debianform-manual/12-operations
cd debianform-manual/12-operations
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/12-state.json"
    lock_path = "/var/lock/debianform/manual/12-state.lock"
  }

  files {
    file "/etc/debianform-manual/operations.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "operations baseline\n"
    }
  }
}
```

## Review a Text Plan

Run:

```bash
dbf validate
dbf plan --offline
dbf plan
```

Useful interpretation rules:

- `--offline` does not connect to the host. Use it for a quick view of resource addresses, desired
  content, and the general change shape.
- Online `plan` connects to the host and reads state and observed state. Use it to review the real
  actions before apply.
- Online `plan`, `apply`, and `check` write discovery/execution progress to stderr while the plan
  body remains on stdout.
- `+` means create, `~` means update, `-` means delete, and `!` means operation.

The first offline plan should show:

```text
Summary: 1 create, 0 update, 0 delete, 0 no-op, 0 operations
```

## Generate a JSON Plan

Run:

```bash
dbf plan --offline --format json > plan.json
python3 - <<'PY'
import json

with open("plan.json", encoding="utf-8") as f:
    doc = json.load(f)

assert doc["format_version"] == "debianform.plan.alpha1", doc["format_version"]
assert doc["summary"]["create"] == 1, doc["summary"]
assert doc["changes"][0]["address"] == 'host.manual1.files.file["/etc/debianform-manual/operations.txt"]'
print("json plan ok")
PY
```

A JSON plan is useful in CI, audits, and custom checks. See `docs/plan-format.md` for format details.

## Generate an HTML Plan

Run:

```bash
dbf plan --offline --html plan.html
test -s plan.html
```

Expected output:

```text
wrote HTML plan to plan.html
```

An HTML plan can be opened as an attachment during change review. `--html` is supported only by
`dbf plan` and cannot be combined with an explicit `--format`.

## Apply the Change

Run:

```bash
dbf apply --auto-approve
dbf check
```

Apply output normally contains two plans:

- `Preview plan (state lock not held)` is the online preview before confirmation.
- `Execution plan (state lock held)` is the real execution plan after acquiring the state lock and
  rereading state and observed state.

The execution plan is printed before any state write or provider mutation. In interactive mode,
DebianForm asks for confirmation again when the two plans differ. `--auto-approve` accepts the
change but still prints the second plan. It does not print the plan a third time after execution.

During execution, stderr shows the current host, resource address, action, and heartbeats for long
steps, such as `start update ...`, `still update ...`, and `done update ...`.

Successful output ends with:

```text
apply complete
```

`check` should return to no changes:

```text
Summary: 0 create, 0 update, 0 delete, 1 no-op, 0 operations
```

## Inspect Remote State

Run:

```bash
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/12-state.json", encoding="utf-8") as f:
    st = json.load(f)

assert st["version"] == 2, st
assert st["host"] == "manual1", st
assert st["serial"] >= 1, st

key = "host.manual1.files.file[\"/etc/debianform-manual/operations.txt\"]"
res = st["resources"][key]
assert res["kind"] == "file", res
assert res["ownership"] == "managed", res
assert "content" not in res.get("desired", {}), res
print("state ok")
PY'
```

State is DebianForm's progress and ownership record, not a full copy of configuration. File content
is not stored as plaintext; state stores summaries such as `content_sha256` and `content_bytes`.

## Detect Drift

Modify the remote file manually:

```bash
ssh manual1 'printf "manual drift\n" > /etc/debianform-manual/operations.txt'
```

Run:

```bash
dbf check
```

The command should fail with a non-zero status and show that the remote SHA differs from the desired
summary:

```text
~ host.manual1.files.file["/etc/debianform-manual/operations.txt"]
  update file /etc/debianform-manual/operations.txt

Summary: 0 create, 1 update, 0 delete, 0 no-op, 0 operations
dbf: remote state does not match configuration
```

`check` detects only; it does not change the remote host.

## Repair Drift

Run:

```bash
dbf apply --auto-approve
dbf check
ssh manual1 'cat /etc/debianform-manual/operations.txt'
```

Expected output:

```text
operations baseline
```

## State Locks

`apply` acquires a state lock on every target host. The default path comes from the host's
`state.lock_path`:

```text
/var/lock/debianform/manual/12-state.lock
/var/lock/debianform/manual/12-state.lock.d/
/var/lock/debianform/manual/12-state.lock.d/owner.v2
/var/lock/debianform/manual/12-state.lock.guard
```

The lock file is a version 2 lease in JSON. It contains the owner, pid, token, independent expiry,
and integrity check. By default the holder renews a two-minute lease every 30 seconds.
`--lock-timeout` controls only how long a contender waits. At the end of a running apply,
DebianForm removes the lock and lock directory but retains the internal guard file to preserve a
stable inode. If there are no changes, apply exits after printing the no-op plan and never enters the
locked execution phase.

Create an unexpired lock for testing:

```bash
ssh manual1 'lock=/var/lock/debianform/manual/12-state.lock; mkdir -p "$lock.d"; owner=$(printf %s manual-lock-demo | base64 -w0); pid=$$; token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; marker_payload="2|$token"; marker_checksum=$(printf %s "$marker_payload" | sha256sum | awk "{print \$1}"); printf "{\"protocol_version\":2,\"token\":\"%s\",\"checksum\":\"%s\"}\n" "$token" "$marker_checksum" > "$lock.d/owner.v2"; exp=$(( $(date +%s) + 600 )); payload="2|$owner|$pid|$token|$exp|0"; checksum=$(printf %s "$payload" | sha256sum | awk "{print \$1}"); printf "{\"protocol_version\":2,\"owner\":\"%s\",\"owner_encoding\":\"base64\",\"pid\":\"%s\",\"token\":\"%s\",\"lease_expires_at_unix\":%s,\"expires_at_unix\":0,\"checksum\":\"%s\"}\n" "$owner" "$pid" "$token" "$exp" "$checksum" > "$lock"'
ssh manual1 'printf "lock demo drift\n" > /etc/debianform-manual/operations.txt'
dbf apply --auto-approve --lock-timeout 2s
```

Expected failure:

```text
dbf: ssh manual1 failed: exit status 1: timed out waiting for state lock /var/lock/debianform/manual/12-state.lock
```

Inspect the lock first:

```bash
ssh manual1 'cat /var/lock/debianform/manual/12-state.lock'
```

Remove it manually only after confirming that no other `dbf apply` is running:

```bash
ssh manual1 'rm -f /var/lock/debianform/manual/12-state.lock; rm -rf /var/lock/debianform/manual/12-state.lock.d'
dbf apply --auto-approve
dbf check
```

When a valid version 2 lock has expired, DebianForm revalidates and atomically takes it over under
the guard, writing a notice to stderr. A legacy or unknown format is never taken over automatically;
confirm that no holder remains, then back up and remove it manually.

## Common Failure Handling

`check` fails:
Read the plan first and identify whether it concerns file content, permissions, service state,
package state, or an operation. After confirming the desired result, repair it with `apply`.

`apply` is interrupted:
Do not delete state first. Run `dbf plan` again to inspect remaining changes. DebianForm writes state
after every successful resource, so the next apply continues from the current real state.

Remote state looks wrong:
Back up the state file first, then check whether resource addresses match the current plan. An
address change turns the old state resource into an orphan.

Lock timeout:
Confirm that no other apply is running before removing `lock_path` and `lock_path.d/`. Never remove a
lock without checking process state.

Offline plan reports missing runtime facts:
Declare required facts such as `platform.distribution`, `platform.version`,
`platform.architecture`, and `platform.codename` on the host, or run an online `dbf plan`.

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/12-operations
cd debianform-manual/12-operations

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/12-state.json"
    lock_path = "/var/lock/debianform/manual/12-state.lock"
  }

  files {
    file "/etc/debianform-manual/operations.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "operations baseline\n"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf plan --offline --format json > plan.json
python3 - <<'PY'
import json

with open("plan.json", encoding="utf-8") as f:
    doc = json.load(f)

assert doc["format_version"] == "debianform.plan.alpha1", doc["format_version"]
assert doc["summary"]["create"] == 1, doc["summary"]
assert doc["changes"][0]["address"] == 'host.manual1.files.file["/etc/debianform-manual/operations.txt"]'
print("json plan ok")
PY
dbf plan --offline --html plan.html
test -s plan.html

dbf apply --auto-approve
dbf check
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/12-state.json", encoding="utf-8") as f:
    st = json.load(f)

assert st["version"] == 2, st
assert st["host"] == "manual1", st
assert st["serial"] >= 1, st

key = "host.manual1.files.file[\"/etc/debianform-manual/operations.txt\"]"
res = st["resources"][key]
assert res["kind"] == "file", res
assert res["ownership"] == "managed", res
assert "content" not in res.get("desired", {}), res
print("state ok")
PY'

ssh manual1 'printf "manual drift\n" > /etc/debianform-manual/operations.txt'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'cat /etc/debianform-manual/operations.txt'

ssh manual1 'lock=/var/lock/debianform/manual/12-state.lock; mkdir -p "$lock.d"; owner=$(printf %s manual-lock-demo | base64 -w0); pid=$$; token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; marker_payload="2|$token"; marker_checksum=$(printf %s "$marker_payload" | sha256sum | awk "{print \$1}"); printf "{\"protocol_version\":2,\"token\":\"%s\",\"checksum\":\"%s\"}\n" "$token" "$marker_checksum" > "$lock.d/owner.v2"; exp=$(( $(date +%s) + 600 )); payload="2|$owner|$pid|$token|$exp|0"; checksum=$(printf %s "$payload" | sha256sum | awk "{print \$1}"); printf "{\"protocol_version\":2,\"owner\":\"%s\",\"owner_encoding\":\"base64\",\"pid\":\"%s\",\"token\":\"%s\",\"lease_expires_at_unix\":%s,\"expires_at_unix\":0,\"checksum\":\"%s\"}\n" "$owner" "$pid" "$token" "$exp" "$checksum" > "$lock"'
ssh manual1 'printf "lock demo drift\n" > /etc/debianform-manual/operations.txt'
dbf apply --auto-approve --lock-timeout 2s || true
ssh manual1 'cat /var/lock/debianform/manual/12-state.lock; rm -f /var/lock/debianform/manual/12-state.lock; rm -rf /var/lock/debianform/manual/12-state.lock.d'
dbf apply --auto-approve
dbf check
```

## Cleanup

To remove the remote file, state, lock, and local plan files created by this chapter:

```bash
rm -f plan.json plan.html
ssh manual1 'rm -f /etc/debianform-manual/operations.txt /var/lib/debianform/manual/12-state.json /var/lock/debianform/manual/12-state.lock; rm -rf /var/lock/debianform/manual/12-state.lock.d'
```
