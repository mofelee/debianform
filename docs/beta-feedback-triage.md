<p align="right">
  <strong>English</strong> | <a href="beta-feedback-triage.zh.md">简体中文</a>
</p>

# DebianForm Beta Feedback and Triage Process

This document defines feedback channels, issue triage, priorities, and closure criteria during the
public beta. Do not report security vulnerabilities in public issues. Follow
[SECURITY.md](../SECURITY.md) and use GitHub Security Advisories instead.

## Feedback Channels

Use GitHub Issues for public beta feedback:

- Beta experience, documentation problems, and adoption blockers: use the `Beta feedback` issue
  template.
- Reproducible defects: use the `Bug report` issue template.
- Security vulnerabilities: use GitHub Security Advisories, not a public issue.

Do not include any of the following in a report:

- SSH private keys, API tokens, passwords, or WireGuard private keys.
- Unredacted private hostnames, public IP addresses, customer names, or internal paths.
- Complete state, plan, shell-history, or CI-log output that has not been reviewed for secrets.

Include the following information when possible:

- Output from `dbf version`.
- Control-machine OS and architecture.
- Target distribution, version, architecture, and codename.
- Installation method: Homebrew, curl installer, source build, or local development build.
- The command involved: `validate`, `plan`, `apply`, `check`, `fmt`, or an inspection command.
- A minimal reproducible `.dbf.hcl` fragment with secrets removed.
- Expected result, actual result, and the complete error text.

## Issue Labels

| Label | Meaning |
| --- | --- |
| `needs-triage` | Default state for a new issue whose scope and priority are not yet confirmed. |
| `beta-feedback` | Beta user experience, adoption blocker, or other non-bug feedback. |
| `bug` | Reproducible behavior defect. |
| `docs` | Documentation gap, error, or example problem. |
| `release` | Installation, upgrade, release artifact, Homebrew, or curl installer. |
| `integration` | Debian target, libvirt case, or provider apply/check behavior. |
| `security` | A non-vulnerability concern about the security model, secret handling, or permission boundaries. Vulnerabilities still use an advisory. |
| `priority/p0` | Blocks the beta's primary workflow or may cause data destruction, disclosure, or an incorrect apply. |
| `priority/p1` | Affects common beta users but has a clear workaround. |
| `priority/p2` | Documentation, usability, boundary clarification, or later optimization. |
| `needs-info` | The reporter must provide more reproduction information. |
| `accepted` | Confirmed as work the project intends to do. |
| `known-issue` | Confirmed limitation or defect that will be recorded in release notes or the support matrix. |

## Triage Procedure

1. Determine whether the report concerns a security vulnerability. If it does, ask the reporter to
   move it to GitHub Security Advisories and close the public issue.
2. Check that the issue includes the version, environment, command, configuration fragment, and
   error output.
3. If reproduction details are missing, add `needs-info` and list the minimum information needed.
4. Classify the issue as bug, beta feedback, docs, release, integration, or security-boundary.
5. Assign `priority/p0`, `priority/p1`, or `priority/p2`.
6. For a reproducible bug, try to turn the reproduction into a minimal fixture, unit test, or
   libvirt integration case.
7. For a known limitation, add `known-issue` and decide whether to update the support matrix,
   release-notes template, operations runbook, or README.
8. When accepting work, add `accepted` and state the next step in the issue.

## Priority Definitions

`priority/p0`:

- May disclose a secret through a plan, state, log, error, debug output, or shell-command preview.
- May apply a destructive operation to an undeclared resource.
- Breaks the primary path through state locking, state writes, or check/drift behavior.
- Makes installation artifacts unusable or causes release-artifact verification to fail.
- Makes the Quickstart impossible on a supported Debian 13 amd64 target.

`priority/p1`:

- A common DSL use case fails, but an acceptable workaround exists.
- A reproducible problem affects a beta primary path such as Docker/Compose, APT, systemd, or
  nftables.
- A documentation example disagrees with actual CLI behavior.
- An error lacks enough context for a user to recover.

`priority/p2`:

- Non-blocking documentation improvement.
- Design suggestion, syntactic convenience, or long-term stable-policy discussion.
- A trend that requires a real environment or several releases to validate.

## Response and Closure

- `priority/p0`: confirm the affected scope first, then provide a workaround, rollback guidance, or
  remediation plan as soon as possible.
- `priority/p1`: confirm the reproduction path and schedule it in the nearest executable loop.
- `priority/p2`: converge it into documentation, a future plan, or a design discussion.

Before closing an issue, at least one of the following must be true:

- A fix has been merged and the verification commands are documented.
- Documentation or the support matrix has been updated to explain the current boundary.
- The issue has had `needs-info` for an extended period without a response.
- The report is confirmed not to be a DebianForm problem, with the reason documented.
- A security report has moved to a private advisory.

## Adding an Item to Known Issues

Synchronize an item to release notes or the support matrix when it is:

- A limitation that users may encounter during ordinary beta use.
- A bug with a clear impact that cannot be fixed in the current release.
- A platform path verified only as `manual/best-effort`.
- A boundary involving state, plan JSON, secret redaction, root-only SSH, or release artifacts.

Before every release, review all issues labeled `known-issue` and ensure the `Known Issues` section
of the release notes is current.
