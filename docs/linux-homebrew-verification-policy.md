<p align="right">
  <strong>English</strong> | <a href="linux-homebrew-verification-policy.zh.md">简体中文</a>
</p>

# Linux Homebrew Verification Policy

This document defines how DebianForm verifies its Linux Homebrew installation path. The current
public beta provides a project Homebrew tap and verifies it where possible in the release workflow.
Because the default Linux runner does not necessarily include Homebrew, Linux Homebrew verification
is currently best-effort.

## Current Decision

- Homebrew install/test/upgrade on macOS amd64 and macOS arm64 is part of the primary release
  verification path.
- Linux amd64 Homebrew is verified automatically when the runner has `brew`; otherwise its status
  is `manual/best-effort`.
- Linux arm64 Homebrew requires both a Linux arm64 runner and a Homebrew environment. It is
  currently `manual/best-effort`.
- The Homebrew formula still includes Linux amd64 and Linux arm64 tarball URLs and SHA-256 values.
  Best-effort means the installation path lacks full verification, not that the artifact is absent.

## Current Workflow Behavior

The Homebrew verification step in the release workflow follows this behavior:

```bash
if ! command -v brew >/dev/null 2>&1; then
  status=manual/best-effort
  exit 0
fi

brew update
brew tap mofelee/debianform
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
status=yes
```

The release verification matrix must record the Linux Homebrew result in release notes as:

```text
manual/best-effort
```

It may report a successful result only when the workflow actually runs and passes `brew install`,
`brew test`, and `brew upgrade` in the corresponding Linux environment.

## Support Status

| Path | Current status | Explanation |
| --- | --- | --- |
| macOS amd64 Homebrew | Beta | Automatically verified on a GitHub-hosted macOS runner. |
| macOS arm64 Homebrew | Beta | Automatically verified on a GitHub-hosted macOS arm64 runner. |
| Linux amd64 Homebrew | Preview | The Ubuntu runner does not include Homebrew by default; the workflow verifies it when `brew` exists. |
| Linux arm64 Homebrew | Preview | Requires both a Linux arm64 runner and a Homebrew environment. |

## Requirements for Promotion to Beta

To promote a Linux Homebrew path from Preview to Beta, all of the following must be true:

- The release workflow uses a Linux runner with Homebrew.
- Every release automatically runs:
  - `brew update`
  - `brew tap mofelee/debianform`
  - `brew install mofelee/debianform/dbf`
  - `brew test mofelee/debianform/dbf`
  - `brew upgrade dbf`
- The verification matrix no longer reports `manual/best-effort`; it reports `yes`, or a failure
  blocks the release.
- The support matrix and release process are updated together.

Linux arm64 additionally requires:

- A real arm64 runner, or evidence that Homebrew used the linux/arm64 bottle or tarball.
- `dbf version` output whose platform matches the expected artifact.

## Prohibited Claims

- Do not report Linux Homebrew as passing on a runner without `brew`.
- Do not treat a successfully generated formula as proof that install/test/upgrade passed.
- Do not infer Linux Homebrew results from macOS Homebrew results.
- Do not infer Linux arm64 results from Linux amd64 results.

## User-Facing Documentation

While Linux Homebrew remains Preview:

- The README may retain the Homebrew installation path because the formula supports Linux.
- Release notes must show the actual Linux Homebrew status in the verification matrix.
- The support matrix must describe Linux Homebrew as best-effort unless a real runner has verified it.

## Follow-up Loops

### Loop A: Linux amd64 Homebrew Runner

Goal: provide Homebrew on a Linux amd64 runner and have the release workflow verify it
automatically.

Acceptance checks:

```bash
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

### Loop B: Linux arm64 Homebrew Runner

Goal: verify Homebrew install/test/upgrade on a Linux arm64 runner.

Acceptance checks:

```bash
brew install mofelee/debianform/dbf
dbf version
brew test mofelee/debianform/dbf
brew upgrade dbf
```

### Loop C: Release-Gate Adjustment

Goal: once automated Linux Homebrew verification is stable, change the corresponding path in the
release workflow from best-effort to release-blocking on failure.

## Current State

- The Linux Homebrew strategy is explicitly best-effort.
- Automatic verification depends on whether the runner has Homebrew installed.
- No real Linux Homebrew install/test/upgrade run has been completed in the current repository
  environment.
