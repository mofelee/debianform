<p align="right">
  <strong>English</strong> | <a href="apt-repository-feasibility.zh.md">简体中文</a>
</p>

# DebianForm .deb and APT Repository Feasibility

This document evaluates the feasibility, boundaries, and implementation path for distributing
DebianForm as a `.deb` package and through an APT repository. The current public beta provides
GitHub Release tarballs, a curl installer, and a Homebrew tap. Neither `.deb` packages nor an APT
repository have been implemented, and they are not currently official installation paths.

## Current Decision

- A `.deb` package is feasible and would provide a native installation path for Linux amd64 and
  arm64.
- An APT repository is feasible, but requires an additional signing key, repository metadata
  generation, publication permissions, and a long-term operations commitment.
- The public beta should not rush an APT repository into service. First produce a `.deb` artifact
  that can be validated locally, then decide whether to publish a repository.
- Before launch, define key rotation, rollback, bad-package withdrawal, repository retention, and
  release-note notification procedures.

## User Value

A `.deb` package provides:

- Installation through `dpkg -i` or `apt install ./dbf_<version>_<arch>.deb`.
- Clear file ownership and uninstall behavior.
- Installation of the README, documentation, and examples under `/usr/share/debianform`.
- Distribution through an internal artifact repository or configuration-management system.

An APT repository additionally provides:

- `apt install dbf`.
- Version updates through `apt upgrade`.
- Standard Debian repository management for enterprise and lab environments.

## Risks and Costs

An APT repository is not a single artifact; it is a long-lived distribution channel. Its primary
costs are:

- Generating, protecting, rotating, and revoking the GPG signing key.
- Generating and validating `Release`, `InRelease`, `Packages`, by-hash, and related metadata.
- Repository URLs, suites, components, and retention policies become difficult to change after
  publication.
- A failed publication can leave mirrors, caches, and client-side APT metadata inconsistent.
- Linux amd64 and arm64 `.deb` installation, upgrade, downgrade, and removal need additional
  verification.

For these reasons, `.deb` packaging and an APT repository should be separate phases and must not be
coupled to the tarball/Homebrew primary path.

## Recommended Repository Layout

Use a dedicated publication location, for example:

```text
https://apt.debianform.example/debianform
```

Debian suites and components:

```text
stable main
beta main
```

During early public beta, publishing only this suite is also acceptable:

```text
beta main
```

Target architectures:

```text
amd64
arm64
```

Package name:

```text
dbf
```

Installed files:

```text
/usr/bin/dbf
/usr/share/debianform/README.md
/usr/share/debianform/docs/*
/usr/share/debianform/examples/*
/usr/share/doc/dbf/changelog.gz
/usr/share/doc/dbf/copyright
```

The Debian package should not install a systemd unit by default. `dbf` is a control-machine CLI,
not a daemon.

## Signing and Keys

An APT repository requires a dedicated signing key. Recommended practices:

- Use a dedicated APT repository signing key. Do not reuse a Git tag, cosign, or SSH key.
- Publish the ASCII-armored public key and its fingerprint.
- Refer to the complete fingerprint, not only a short key ID, in release notes and the README.
- Announce key rotation at least one release in advance.
- Keep the private key only in a controlled release environment or dedicated secret store.

Do not publish a permanent repository URL until the key-management process is defined.

## Implementation Loops

### Loop A: Local `.deb` Artifact

Goal: produce an installable `.deb` locally and in CI without publishing an APT repository.

Scope:

- Generate `.deb` files with GoReleaser or nfpm.
- Cover Linux amd64 and Linux arm64.
- Include description, license, homepage, and maintainer package metadata.
- Include `dbf`, README files, documentation, examples, LICENSE, and CHANGELOG.
- Ensure `dbf version` runs after installation.
- Ensure uninstall removes `/usr/bin/dbf`.

Acceptance checks:

```bash
goreleaser release --snapshot --clean --skip publish
test -n "$(find dist -maxdepth 1 -name 'dbf_*_linux_amd64.deb' -print -quit)"
dpkg-deb --info dist/dbf_*_linux_amd64.deb
dpkg-deb --contents dist/dbf_*_linux_amd64.deb
```

Real installation acceptance requires a Debian VM:

```bash
apt install ./dbf_<version>_linux_amd64.deb
dbf version
apt remove dbf
```

### Loop B: Repository Metadata Dry Run

Goal: generate an APT repository directory and metadata locally without publishing them.

Scope:

- Generate `pool/`, `dists/`, `Packages`, `Release`, and `InRelease`.
- Support amd64 and arm64.
- Sign with a temporary test key.
- Install from a `file://` source or local HTTP server in a Debian VM.

Acceptance checks:

```bash
apt-get update
apt-get install dbf
dbf version
apt-get remove dbf
```

### Loop C: Distribution-Channel Decision

Goal: decide where the repository is hosted and how publication permissions work.

Candidates:

- GitHub Pages.
- Cloudflare Pages or R2.
- An S3-compatible bucket.
- A self-hosted static file service.

Decision criteria:

- HTTPS support.
- Atomic or approximately atomic metadata publication.
- Rollback support or retention of older versions.
- Least-privilege release credentials.
- Acceptable cost and operational complexity.

### Loop D: Public Beta Repository

Goal: publish a public beta APT repository.

Prerequisites:

- `.deb` installation, upgrade, and removal pass on a real or libvirt Debian 13 amd64 VM.
- The arm64 artifact passes at least build and static validation; a real arm64 installation may
  initially remain best-effort.
- The signing-key fingerprint is documented.
- Rollback and bad-release handling are documented in the release runbook.
- The release-notes template includes APT repository verification results.

## Outside the Current Beta Primary Path

- Inclusion in the official Debian archive.
- Package repositories for non-Debian distributions such as Ubuntu, RHEL, Fedora, or Arch.
- Automatically installing shell completion, a systemd service, or a daemon.
- Automatic pinning across multiple channels.

## Current State

At the time this document was introduced:

- GitHub Release tarballs, the curl installer, and the Homebrew tap are the official installation
  paths.
- `.deb` packages have not been implemented.
- An APT repository has not been implemented.
- This document completes the feasibility assessment and loop decomposition for future work.
