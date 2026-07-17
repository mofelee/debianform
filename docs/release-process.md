# DebianForm Release Process

<p align="right"><strong>English</strong> | <a href="release-process.zh.md">简体中文</a></p>

This document defines DebianForm's public release, installation, and upgrade
process. Users should be able to install `dbf` on Linux and macOS with `curl` or
Homebrew, on both `amd64` and `arm64`.

See the [release automation plan](release-automation-plan.md) for implementation
steps, the [release quick runbook](release-quick-runbook.md) for routine release
operations, and the [compatibility policy](compatibility-policy.md) for DSL,
CLI, state-schema, and plan-JSON compatibility rules.

## Support Matrix

See the [support matrix](support-matrix.md) for complete user capabilities, DSL
blocks, resource/domain types, and verification coverage. This section lists
only release-artifact platforms.

The DebianForm CLI runs on a control machine or CI runner and manages supported
Debian/Ubuntu targets over SSH. The CLI runtime platform and managed target
system are separate concepts.

CLI release artifacts cover:

| OS | Architecture | Artifact |
| --- | --- | --- |
| Linux | amd64 | `dbf_<tag>_linux_amd64.tar.gz` |
| Linux | arm64 | `dbf_<tag>_linux_arm64.tar.gz` |
| macOS | amd64 | `dbf_<tag>_darwin_amd64.tar.gz` |
| macOS | arm64 | `dbf_<tag>_darwin_arm64.tar.gz` |

Debian 13 remains the highest-priority managed target. Ubuntu 24.04 and 26.04
LTS amd64 are Preview targets with independent gates. Managed-target support is
determined by the complete platform tuple and integration coverage, not by the
CLI platform matrix. See the
[platform support strategy](platform-support-strategy.md) for details.

## Versioning Policy

- Public beta versions use SemVer prereleases such as `v0.1.0-beta.1` and
  `v0.1.0-beta.2`.
- Stable versions use `v0.1.0`, `v0.1.1`, `v0.2.0`, and so on.
- Tags must start with `v` and point to commits with fully passing CI.
- Breaking DSL, state, or plan-JSON changes may enter only a minor version and
  must be documented in release notes.
- Breaking changes are allowed during beta, but release notes must state their
  migration impact. The [compatibility policy](compatibility-policy.md) governs
  classification and state/plan-JSON migration requirements.

## Release Artifacts

Every GitHub Release must include:

- Tarballs for all four platforms.
- `checksums.txt` containing SHA-256 checksums for all tarballs.
- Release notes describing additions, fixes, compatibility, and migration.

Review release notes against the
[release notes template](release-notes-template.md), retaining its fixed
breaking-changes, known-issues, verification-matrix, and migration-notes
sections.

`<tag>` means the complete Git tag, for example `v0.1.0-beta.1`.

Every tarball contains:

- `dbf`
- `README.md`
- `README.zh-CN.md`
- `CHANGELOG.md`
- `CHANGELOG.zh-CN.md`
- `SECURITY.md`
- `SECURITY.zh-CN.md`
- `docs/`
- `examples/`
- `LICENSE`

Recommended future additions:

- `checksums.txt.sig` or a cosign signature.
- `.deb` packages.
- Homebrew bottles.

## Build Requirements

All release builds must use the same version metadata:

```bash
VERSION=v0.1.0-beta.1
COMMIT="$(git rev-parse --short=12 HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

Go builds inject:

```bash
-X github.com/mofelee/debianform/internal/version.Version=$VERSION
-X github.com/mofelee/debianform/internal/version.Commit=$COMMIT
-X github.com/mofelee/debianform/internal/version.Date=$BUILD_DATE
```

Build matrix:

```bash
GOOS=linux  GOARCH=amd64
GOOS=linux  GOARCH=arm64
GOOS=darwin GOARCH=amd64
GOOS=darwin GOARCH=arm64
```

Every built binary must support:

```bash
dbf version
dbf --version
```

`dbf version` should display version, commit, build time, Go version, and
platform.

## Release Gate

Before tagging, run:

```bash
make docs-check
test -z "$(gofmt -l $(git ls-files '*.go'))"
go vet ./...
go test -race -count=1 ./...
make build
make vulncheck
make test-integration-layout
git diff --check
```

The release commit must satisfy:

- `LICENSE` exists.
- `CHANGELOG.md` exists and has an entry for the current version.
- README links to installation, upgrade, and the support matrix.
- Compatibility review covers breaking DSL/state/plan-JSON changes, state
  migration, and plan JSON format-version impact.
- GitHub Actions is fully passing for the target commit.
- CI evidence for the same target commit records `20/20` independently for
  Ubuntu 24.04 LTS amd64, Ubuntu 26.04 LTS amd64, Debian 12 amd64, and Debian 13
  amd64; the `Ubuntu 24.04 target matrix gate`,
  `Ubuntu 26.04 target matrix gate`, and `Managed target matrix gate` all pass;
  and the Ubuntu 26.04 released-image URL and SHA-256 are recorded.
- Every libvirt case on all four targets completes `validate`, online `plan`,
  `apply`, a second no-op JSON `plan`, and `check`. Cases with a drift hook must
  also reject drift.

To reproduce a managed-target case locally, select the target explicitly:

```bash
make test-integration-case CASE=files DEBIAN_VERSION=12
make test-integration-case CASE=files DEBIAN_VERSION=13
make test-integration-case CASE=files TARGET=ubuntu-24.04
make test-integration-case CASE=files TARGET=ubuntu-26.04
```

## GitHub Release Procedure

1. Update `CHANGELOG.md`.
2. Prepare GitHub Release notes from the
   [release notes template](release-notes-template.md).
3. Run `make docs-check` and confirm that every maintained Markdown pair is synchronized.
4. Confirm that CI is fully passing.
5. Create the tag:

   ```bash
   git tag -s v0.1.0-beta.1
   git push origin v0.1.0-beta.1
   ```

6. The release workflow builds tarballs for all four platforms.
7. The release workflow generates `checksums.txt`.
8. The release workflow creates the GitHub Release.
9. The release workflow updates the Homebrew tap.
10. Verify curl and Homebrew installation in clean environments.

When a signing key is temporarily unavailable, an annotated tag may substitute
for a signed tag, but signed tags are mandatory before stable.

## curl Installation

User entry point:

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh -s -- --version v0.1.0-beta.1
```

Supported installer options:

| Option | Default | Description |
| --- | --- | --- |
| `--version` | Latest release | Install a specific version. |
| `--prefix` | Auto | Installation prefix. Use `/usr/local` as root or when writable; otherwise use `$HOME/.local`. |
| `--bin-dir` | `<prefix>/bin` | Override only the binary installation directory. |
| `--os` | Auto | Override OS detection for testing; `linux` or `darwin`. |
| `--arch` | Auto | Override architecture detection for testing; `amd64` or `arm64`. |
| `--dry-run` | false | Print downloads and installation actions without executing them. |
| `--force` | false | Reinstall even when the target version is already installed. |

Installer behavior:

- Detect `linux` or `darwin` through `uname -s`.
- Map `x86_64`/`amd64` to `amd64` and `aarch64`/`arm64` to `arm64` using
  `uname -m`.
- Download the matching tarball and `checksums.txt` from the GitHub Release.
- Verify the tarball with SHA-256.
- Extract into a temporary directory.
- Install as a temporary file, then atomically replace the target `dbf`.
- Install `README.md`, `README.zh-CN.md`, `docs/`, and `examples/` under
  `<prefix>/share/debianform`.
- Run `dbf version` after installation.

Upgrade by running the installer again:

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
```

Roll back by reinstalling an older version:

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh -s -- --version v0.1.0-beta.1
```

## Homebrew

The first phase uses a dedicated tap rather than `homebrew/core`:

```bash
brew install mofelee/debianform/dbf
```

The tap can also be added explicitly:

```bash
brew tap mofelee/debianform
brew install dbf
```

Tap repository:

```text
github.com/mofelee/homebrew-debianform
```

Formula path:

```text
Formula/dbf.rb
```

The first-phase formula installs prebuilt tarballs from GitHub Releases. Users
need neither a local Go installation nor a source build, which is the preferred
experience for a dedicated tap. Reevaluate source builds and bottles before
entering `homebrew/core`.

```ruby
class Dbf < Formula
  desc "Configuration tool for Debian hosts"
  homepage "https://github.com/mofelee/debianform"
  version "0.1.0-beta.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/mofelee/debianform/releases/download/v0.1.0-beta.1/dbf_v0.1.0-beta.1_darwin_arm64.tar.gz"
      sha256 "..."
    else
      url "https://github.com/mofelee/debianform/releases/download/v0.1.0-beta.1/dbf_v0.1.0-beta.1_darwin_amd64.tar.gz"
      sha256 "..."
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/mofelee/debianform/releases/download/v0.1.0-beta.1/dbf_v0.1.0-beta.1_linux_arm64.tar.gz"
      sha256 "..."
    else
      url "https://github.com/mofelee/debianform/releases/download/v0.1.0-beta.1/dbf_v0.1.0-beta.1_linux_amd64.tar.gz"
      sha256 "..."
    end
  end

  def install
    bin.install "dbf"
    pkgshare.install "README.md", "README.zh-CN.md", "docs", "examples"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/dbf version")
  end
end
```

Homebrew upgrade:

```bash
brew update
brew upgrade dbf
```

When updating the tap, the release workflow must:

- Change the `version`, four platform `url` values, and four `sha256` values in
  `Formula/dbf.rb`.
- Run `brew audit --strict --online dbf`.
- Run `brew install mofelee/debianform/dbf`.
- Run `brew test mofelee/debianform/dbf`.
- Commit and push the tap update.

The second phase will decide whether to add a source formula and bottles. A
dedicated tap using upstream binary tarballs already covers macOS/Linux and
amd64/arm64. Entering `homebrew/core` requires a new review of source builds,
dependency downloads, and bottles under Homebrew core rules.

## User Installation Documentation

README should reduce installation to two primary paths:

```bash
# Homebrew
brew install mofelee/debianform/dbf

# curl
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
```

It should explain:

- `dbf` is installed on a control machine or CI runner.
- Managed targets must be reachable over SSH. Debian 13 has the highest
  priority; Ubuntu 24.04 and 26.04 LTS amd64 are Preview.
- Use `dbf version` to confirm successful installation.
- Use `brew upgrade dbf` or rerun the installer to upgrade.

## Post-Release Verification

After every release, verify in clean environments:

```bash
dbf version
dbf validate -f examples/bbr.dbf.hcl
dbf plan -f examples/bbr.dbf.hcl --offline
```

curl verification matrix:

| Platform | Required |
| --- | --- |
| Linux amd64 | yes |
| Linux arm64 | yes |
| macOS amd64 | yes |
| macOS arm64 | yes |

Homebrew verification matrix:

| Platform | Required |
| --- | --- |
| Linux amd64 | yes |
| Linux arm64 | best effort until a CI runner exists |
| macOS amd64 | yes |
| macOS arm64 | yes |

When a platform cannot be verified automatically in CI, release notes must mark
it as manually verified or unverified. See the
[Linux Homebrew verification policy](linux-homebrew-verification-policy.md) for
Linux Homebrew best-effort rules.

## Failure Handling

If the GitHub Release is published but the Homebrew tap update fails:

- Do not delete the published release.
- Fix the tap and commit `Formula/dbf.rb` again.
- Record the Homebrew availability time or known issue in release notes.

If a tarball checksum is wrong:

- Immediately mark the GitHub Release as prerelease/draft or withdraw the
  affected artifact.
- Do not republish different content under the same tag.
- Create a new patch/prerelease tag, such as `v0.1.0-beta.2`.

If a released version has a severe defect:

- Publish a new fix version.
- Point the Homebrew tap to the new version.
- Mark affected versions in release notes.

## Roadmap

P0:

- GitHub Release tarballs for four platforms.
- `checksums.txt`.
- `scripts/install.sh`.
- Homebrew tap binary formula.
- README installation and upgrade instructions.

P1:

- Automatic Homebrew tap updates.
- Release-artifact signatures.
- `.deb` packages.
- Feasibility review for a Homebrew source formula and bottles.

P2:

- APT repository. See [APT repository feasibility](apt-repository-feasibility.md)
  for feasibility and implementation loops.
- Feasibility review for entering `homebrew/core`.
- Continue running the stable gates in the compatibility policy.

See the [release automation plan](release-automation-plan.md) for concrete
loops, acceptance commands, and manual intervention points.
