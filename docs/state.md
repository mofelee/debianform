# DebianForm State

<p align="right"><strong>English</strong> | <a href="state.zh.md">简体中文</a></p>

DebianForm stores a separate remote JSON state for each host:

```text
/var/lib/debianform/state/<host>.json
```

The corresponding lease file, compatibility lock directory, and internal guard
file are:

```text
/var/lock/debianform/state/<host>.lock
/var/lock/debianform/state/<host>.lock.d/
/var/lock/debianform/state/<host>.lock.d/owner.v2
/var/lock/debianform/state/<host>.lock.guard
```

These paths are derived from the default lock path. You normally do not need a
`state` block in the configuration. To customize the remote state or lock
location, override it within the host:

```hcl
host "server1" {
  state {
    path      = "/var/lib/debianform/state/server1.json"
    lock_path = "/var/lock/debianform/state/server1.lock"
  }
}
```

## State Schema

Top-level state fields:

- `version`: currently `2`.
- `host`: the host label.
- `serial`: increments by exactly 1 for each successfully committed state write.
- `updated_at`: UTC RFC3339.
- `facts`: host facts detected at runtime. Target-platform facts in the DSL are
  written as `platform.architecture` and `platform.codename`; the state schema
  continues to store these values under `facts.system.*`.
  `facts.system.hostname` is the observed value and is not the same as the
  desired `system.hostname` in configuration. When `system.hostname` is omitted
  from the configuration, DebianForm does not manage the remote hostname.
- `resources`: resource records keyed by stable address.

A resource record stores:

- `host`; compatible version 2 records may omit it, in which case it is
  normalized to the top-level `host` when read.
- The resource kind, provider type, and debug-only provider address.
- Ownership.
- The redacted desired value and desired digest.
- Any required observed summary.
- Lifecycle, update time, and execution order.

Secret content, sensitive component input plaintext, and file, systemd unit,
APT source/signing-key, or nftables content derived from sensitive input are
never written to state. SSH private keys, command logs, and lock leases are also
excluded. Sensitive content is represented only by summary fields such as
`content_sha256` and `content_bytes`, which support drift comparison and no-op
detection.

## Read Validation

State is the safety boundary for ownership and remote operation targets. A
missing or empty state file is treated as an empty version 2 state for the
current host. Every non-empty state must satisfy all of these requirements:

- Top-level `version` must be exactly `2`. Missing, older, and unknown newer
  versions are all rejected.
- Top-level `host` must be present and exactly match the host label in the
  current request.
- Each resource's `host` may be omitted or equal the top-level `host`. An
  omitted value is explicitly normalized in memory; a value that points to a
  different host is rejected.

These checks run after JSON decoding, and the engine validates again any state
returned by a backend. Validation fails before provider inspection, state
writes, or remote resource changes. The CLI will not rewrite an unrecognized
state using the current schema.

There is currently no automatic migrator for an older state schema. When you
encounter an older version, first back up the original state, then use a
DebianForm version that can read it or a reviewed manual migration procedure.
When you encounter a newer version, upgrade DebianForm. Do not bypass these
checks by deleting or tampering with `version` or `host`.

## Ownership

- `managed`: a resource created or managed by DebianForm; destroyed when
  removed from configuration.
- `adopted`: a resource that already existed remotely; removing it from
  configuration cleans up only the state record.
- `external`: used only for dependencies or observation; the remote object is
  not destroyed.

An explicit `ensure = "absent"` differs from removal from configuration. The
former requests deletion of the remote object; the latter selects destroy or
forget according to ownership. `lifecycle.prevent_destroy` blocks delete and
destroy during planning.

## Locking

When acquiring a lock, DebianForm uses a `<lock_path>.guard` file, which is not
deleted during normal unlock, together with `flock` to serialize lease reads,
renewals, takeovers, and deletion. A version 2 JSON lease is first written in
full to a temporary file in the same directory, then atomically published with
`mv`. It contains a base64-encoded owner, PID, random token,
`lease_expires_at_unix`, and integrity checksum. A fresh acquisition still uses
the exclusive `mkdir <lock_path>.d` as the atomic bridge recognized by both old
and new clients. The winner then writes a token- and checksum-bearing
`.d/owner.v2` marker. After the acquisition SSH call returns, DebianForm
synchronously renews and revalidates both the lease and marker once more. It
returns the lock to the engine only after confirming that the remote lease is
still owned by the current token. Each successful renewal also refreshes the
marker's mtime, so the incomplete-recovery grace period is measured from the
holder's most recent activity.

The wait timeout and lease TTL are independent. The default lease TTL is 2
minutes and is renewed every 30 seconds, so a long-running apply does not lose
its lock when `--lock-timeout` expires. The compatibility field
`expires_at_unix` is fixed at `0`, preventing old clients that do not understand
the version 2 protocol from treating an actively renewed lease as stale.

- A second apply for the same host waits and then fails after the lock timeout.
- An expired version 2 lock can be taken over only after revalidation inside
  the guard critical section and an atomic takeover; a stale-lock notice is
  written to stderr.
- A fresh, partially written, or corrupt version 2 lease is conservatively
  left waiting. Only an incomplete lock with a verifiable `.d/owner.v2` marker
  can be taken over after the recovery grace period.
- A markerless lock directory or a legacy or unknown lease format is never
  taken over automatically. First confirm that no holder exists, then back it
  up and clean it manually.
- Unlock must match the token. A token mismatch does not delete the lock file
  or directory.
- Lock files do not participate in plan diffs and are not written to state.

State is written to a temporary file and atomically replaced with `mv` in the
same directory. Apply writes state back immediately after each resource node
succeeds, so after an interrupted failure, state contains only the nodes that
completed successfully.

Before writing, `state.PrepareWrite` advances `serial` and updates `updated_at`
on a strictly normalized copy. `state.Encode` only validates and serializes; it
does not modify the revision. A backend returns this committed state only after
persistence succeeds, and the engine then uses it to replace its in-memory
snapshot. If the write fails, the candidate revision never becomes visible to
the caller, so retrying from the original snapshot still increments by only 1.
When one operation returns multiple outputs, they are committed in the same
state write and together increment `serial` only once. Persisting host facts is
also a separate successful state write and increments `serial` independently.

For state-schema compatibility, version detection, automatic migration, backup,
and rollback boundaries, see the [compatibility policy](compatibility-policy.md).
