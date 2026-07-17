# 09. Backends, SSH, and Remote State

<p align="right"><strong>English</strong> | <a href="09-backend-state.zh.md">简体中文</a></p>

This chapter explains the boundaries among backends, the SSH runner, and state.
They own how DebianForm accesses hosts and stores management records. They
neither parse configuration nor decide resource actions.

## Data Flow

```text
ir.Program
  -> HostsFromProgram
  -> SSHRunner
  -> SSHBackend.Read/Write/Lock
  -> state.Decode/Encode
```

Online plan reads state only. Apply locks, reads, and writes state.

## Runner Interface

`Runner` is the low-level interface through which providers and backends invoke
remote commands:

- `Run(ctx, host, script)`: execute a script through `ssh <host> sh -s`.
- `RunInput(ctx, host, remoteCommand, input)`: execute a remote command and
  write input to its stdin.
- `RunCommand(ctx, host, remoteCommand)`: execute one remote command.

`SSHRunner` is the current implementation. Tests may replace it with a memory
or fake runner.

## SSH Argument Parsing

`HostsFromProgram` extracts the following for each host in `ir.Program`:

- name
- SSH host/address
- port
- user
- identity file

`SSHRunner.SSHArgs` generates SSH arguments:

- The default user is root.
- `BatchMode=yes` prevents interaction.
- `NumberOfPasswordPrompts=0`, `PasswordAuthentication=no`, and
  `KbdInteractiveAuthentication=no` prevent password and askpass fallback.
- `StrictHostKeyChecking=accept-new`.
- `DBF_SSH_CONFIG` may identify an SSH config file.
- A port other than 22 adds `-p`.
- Identity files support `~` expansion.

DebianForm delegates complex connection behavior to OpenSSH and SSH config,
adding only necessary overrides. If SSH config uses a handwritten
`ProxyCommand ssh ...` for the inner SSH process, add
`-o BatchMode=yes -o NumberOfPasswordPrompts=0 -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no`
to that command as well, or prefer `ProxyJump`.

## Backend Interface

`Backend` defines the state backend:

- `Read(ctx, host)`: read host state.
- `Write(ctx, host, st)`: write host state and return committed state on success.
- `Lock(ctx, host, timeout)`: acquire the host state lock.

`SSHBackend` is the primary current implementation. Tests also use a memory
backend.

The result of backend `Read` is not a trust boundary. On plan, check, apply, and
host-fact persistence paths, the engine validates state again against the
requested host. This prevents another backend from introducing incompatible or
foreign state into provider planning.

## SSHBackend.Read

`Read` checks whether `host.State.Path` exists remotely:

- Missing or empty output -> `state.Empty(host.Name)`.
- Content present -> `state.Decode`.

`state.Decode` strictly invokes `Normalize`: it accepts only current version 2,
requires the top-level `host` to match the requested host, and validates every
resource's `host`. A compatible version 2 resource may omit `host`, in which
case it is explicitly filled from the top-level host. Missing, old, or unknown
newer versions, a missing or foreign top-level host, and foreign resource hosts
all fail. There is currently no automatic old-schema migrator, and invalid
state is not written back.

`Read` tolerates non-JSON text before stdout's JSON object by decoding from the
first `{`. This defends against noise in the remote environment.

## SSHBackend.Write

`Write` performs this sequence:

1. `state.PrepareWrite(st, host.Name)` strictly normalizes a copy, increments
   `serial` by 1, and updates `updated_at`, producing an uncommitted candidate.
   MemoryBackend follows the same preparation contract.
2. `state.Encode(candidate)` validates and generates JSON. Encode does not
   alter the serial or timestamp.
3. Base64-encode the JSON to avoid shell-heredoc escaping issues.
4. Create the remote state directory.
5. Write `state.path.tmp`.
6. Atomically replace the state path with `mv tmp state.path`.
7. Return the candidate as committed state only after the remote write succeeds.

The engine adopts returned committed state only after `Write` succeeds. If the
remote command, encoding, or validation fails, the candidate is discarded and
the caller retains the original serial. Retrying from the original state still
produces only `N+1`. MemoryBackend copies committed state when storing and
returning it, preserving the same revision contract as the SSH backend.

## Lock

`SSHBackend.Lock` uses these remote paths:

- Lock file: `host.State.LockPath`.
- Lock directory: `host.State.LockPath + ".d"`.
- Ownership marker: `host.State.LockPath + ".d/owner.v2"`.
- Guard file: `host.State.LockPath + ".guard"`.

Acquisition works as follows:

- Acquire the stable guard with non-blocking `flock` and check the wait deadline
  before and after each attempt.
- A version 2 JSON lease contains owner, PID, token,
  `lease_expires_at_unix`, compatibility value `expires_at_unix: 0`, and a
  checksum. It is written fully to a temporary file before atomic publication
  with `mv`.
- A fresh acquisition must create the lock directory exclusively. On failure,
  it releases the guard and rereads; another holder's directory cannot count as
  success.
- After winning the lock directory, it writes a token/checksum marker. Only an
  incomplete v2 lock with a verifiable marker can be recovered after the grace
  period. A markerless directory may belong to an old client still writing
  metadata and is never taken over automatically.
- The wait timeout and lease TTL are separate. The default lease lasts 2 minutes
  and renews every 30 seconds.
- Stale takeover rereads and validates the complete record under the guard,
  then replaces it atomically. It cannot delete another contender's new lease.
- A fresh, partially written, or corrupt v2 record waits through the recovery
  grace period. Legacy and unknown records require manual confirmation and
  cleanup.
- Acquisition fails if the wait timeout expires.
- After the remote acquisition script returns, a deadline-bound synchronous
  renewal/token check runs before the lock is returned to the caller.

`Unlock` validates the complete record and token under the same guard and only
then removes the lock file and lock directory. The guard file remains, ensuring
waiters always lock the same inode. A token mismatch returns an error and
prevents deletion of another apply's lock. When the Engine invokes unlock, it
uses a separate short deadline per host and `context.WithoutCancel` so the
caller's cancellation or deadline is not inherited. Cleanup is attempted for
every host, and host-qualified errors are aggregated and returned.

## State Format

Fields of `state.State` are:

- `Version`
- `Host`
- `Serial`
- `UpdatedAt`
- `Facts`
- `Resources`

Keys in `Resources` are graph addresses.

## Resource State

Fields of `state.Resource` are:

- `Host`
- `Kind`
- `ProviderType`
- `ProviderAddress`
- `Ownership`
- `Lifecycle`
- `Desired`
- `DesiredDigest`
- `Observed`
- `UpdatedAt`
- `Order`

Desired state stored in state is sanitized. Its digest is a desired digest
processed according to the sanitization rules.

## Digest and Sanitization

`DesiredDigest` calls `Digest(SanitizeDesired(desired))`. `SanitizeDesired`:

- Removes plaintext `content` strings and replaces them with `content_sha256`
  and `content_bytes`.
- Removes `source_path` and `summary` when a resource is marked `sensitive`.

`SanitizeObserved` currently reuses the same rules.

State can therefore determine whether content changed without storing file
content or sensitive source paths.

## Ownership

Ownership affects orphan handling:

- `managed`: DebianForm created or manages the resource; removing it from
  configuration destroys it by default.
- `adopted`: the host already had a matching resource; apply only writes state,
  and removal from configuration forgets it by default.

The provider and engine maintain ownership jointly during plan and apply.

## Design Boundaries

- A backend owns state storage and locking, not resource semantics.
- A runner executes commands without interpreting plans or actions.
- State is DebianForm's record, not a complete snapshot of host reality.
- State files must never hold plaintext secrets or write-only content.

## Change Checklist

- State-schema change: update `docs/state.md`, state tests, and compatibility
  reading logic.
- Digest-rule change: add drift, content, sensitive, and write-only tests.
- SSH-argument change: add `SSHArgs` tests and verify SSH config defaults.
- Lock change: add stale-lock, timeout, and token-mismatch tests.
- New backend: implement Read/Write/Lock and run engine apply/plan tests.
