<p align="right">
  <strong>English</strong> | <a href="security-model.zh.md">简体中文</a>
</p>

# DebianForm Security Model

This document describes DebianForm's security boundaries, secret handling, and vulnerability
response process during public beta. It is not a production-hardening checklist. Its purpose is to
make clear what the current tool does and does not do, and which risks users must control before
trying it on a low-risk host.

## Execution Model

DebianForm is a configuration tool that manages supported Debian and Ubuntu targets over SSH. Root
SSH is currently the only supported management-connection model:

- `ssh.user` must be omitted or set to `"root"`.
- Omitting `ssh.user` still connects as root.
- sudo, become, sudoers management, and non-root management connections are unsupported.
- Online `plan`, `apply`, and `check` read target facts, state, and observed state over SSH.

Preview support for Ubuntu 24.04 and 26.04 LTS amd64 does not change this boundary. The default
`ubuntu` account plus sudo, become, sudoers management, and non-root management connections remain
Unsupported.

Root-only management is necessary because current resources write to `/etc`, `/usr/local`, systemd,
APT, nftables, kernel settings, and `/var/lib/debianform` state. Managing the same scope as a
non-root account would add many privilege-escalation and troubleshooting branches, making the
primary path unreliable.

## Permission Boundary

The DebianForm management connection has root access to the target. The security assumptions are:

- Run `apply` only on a low-risk test host or a controlled host whose risk has been assessed.
- Run an online `plan` before every real `apply` and review the complete change scope.
- The control machine, CI runner, SSH key, and configuration repository are one trust boundary.
- Do not apply an unreviewed third-party `.dbf.hcl` configuration to a real host.
- Do not run several applies concurrently against one state path. The state lock prevents
  concurrent writes for the same host.

The managed service itself may run with lower privileges. For example,
`systemd.service_unit.user/group` can run the target service under a non-root account. This affects
only the service process and does not change the requirement that DebianForm connect as root.

The project currently makes no commitment to provide:

- A fine-grained least-privilege management connection.
- sudo or become support.
- Multi-tenant control-plane isolation.
- Sandboxed execution of malicious configuration files.
- Complete forensics or remediation after a target has already been compromised.

## Secret Handling

DebianForm aims to keep secret plaintext out of plans, state, ordinary logs, and release/debug
artifacts. The current semantics are:

- `files.file sensitive = true` treats an ordinary file resource as sensitive.
- `secrets.file` is a compatibility layer with sensitive-file deployment semantics. New
  configurations should prefer `variable + files.file sensitive = true`.
- File-like content derived from a sensitive variable or component input inherits the sensitive
  mark. This includes files, systemd units, APT sources/signing keys, and nftables content.
- Plan text, plan JSON, HTML plans, state, and HostSpec/ResourceGraph debug output should store only
  summaries such as hashes, byte counts, and changed status, never plaintext.
- An ephemeral variable must not be written to HostSpec, ResourceGraph, a plan, state, cache, golden
  fixture, or ordinary log.
- APT source, APT signing-key, and nftables content do not yet have a write-only boundary. They
  therefore reject ephemeral values during compilation instead of compiling an ordinary string
  after its mark has been lost.
- A write-only value may enter only the provider apply path. It must not enter desired state, state,
  or a diff.

Users must still account for the following:

- `sensitive` does not mean the value is absent from the target filesystem. If a service requires a
  file, the secret is ultimately written to that file on the target.
- SHA-256 and byte-count summaries in state support drift and no-op detection, but may create a
  guessable fingerprint for a low-entropy secret.
- DebianForm cannot completely control shell history, CI logs, external-command output, custom
  scripts, or third-party service logs.
- Never commit real secrets, SSH private keys, WireGuard private keys, tokens, or `.env` files.
- Redact public issues, feedback, and pasted logs yourself before publishing them.

## State and Locks

The default state path is:

```text
/var/lib/debianform/state/<host>.json
```

The default lock path is:

```text
/var/lock/debianform/state/<host>.lock
```

State stores resource ownership, redacted desired summaries, and observed summaries. It must not
store:

- Secret content.
- Sensitive component-input plaintext.
- Plaintext from files, systemd units, APT sources/signing keys, or nftables content derived from a
  sensitive input.
- SSH private keys.
- Command logs.
- Runtime details other than the lock lease token.

State writes are atomic. `apply` writes state immediately after each resource node succeeds. If an
apply fails partway through, state should contain only the nodes that completed successfully. See
the [Operations Runbook](operations-runbook.md) for recovery procedures.

## Supply-Chain and Installation Security

Every public release should contain tarballs, checksums, a cosign keyless bundle, SBOMs, and a
GitHub provenance attestation. Before installation or upgrade:

- Obtain artifacts from the GitHub Release, Homebrew tap, or official installation script.
- Verify `checksums.txt`.
- Verify the cosign keyless bundle.
- Verify the GitHub provenance attestation.
- Run `validate`, an online `plan`, and `check` against a low-risk target first.

`.deb` packages and an APT repository are not currently published. They must not be treated as
official installation channels until they exist.

## Vulnerability Response

Do not open a public issue for a security vulnerability. Use GitHub Security Advisories:

```text
https://github.com/mofelee/debianform/security/advisories/new
```

A useful report includes:

- Affected version and commit.
- Control-machine OS and architecture.
- Target distribution, version, architecture, and codename.
- A minimal reproduction configuration with secrets removed.
- Impact: secret disclosure, incorrect destructive apply, permission-boundary bypass, release
  artifact verification failure, or another concrete consequence.
- Any known workaround and whether the issue is already public.

Maintainer response process:

1. Acknowledge the report in the advisory thread.
2. Determine scope and priority without exposing details in a public issue.
3. Prepare a fix, regression tests, and release notes.
4. For an affected published version, prefer a new fixed tag; never reuse an existing tag.
5. Update the support matrix, known issues, operations runbook, or compatibility policy when
   necessary.

Security-relevant P0 cases include:

- A secret appears in a plan, state, log, error, debug output, or shell-command preview.
- A destructive operation is applied to an undeclared resource.
- State locking or state writes produce incorrect ownership or an incorrect no-op.
- A release artifact checksum, signature, attestation, or installer path is compromised.

## Feedback Boundary

Use GitHub Issues for ordinary bugs and beta-experience feedback. Use a security advisory for a
vulnerability. Do not include any of the following in public feedback:

- SSH private keys, API tokens, passwords, or WireGuard private keys.
- Unredacted private hostnames, public IP addresses, customer names, or internal paths.
- Complete state, plan, shell-history, or CI-log output that has not been checked for secrets.

See [Beta Feedback and Triage](beta-feedback-triage.md) for the public feedback process.
