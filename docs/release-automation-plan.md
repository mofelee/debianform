# Release Automation Implementation Plan

<p align="right"><strong>English</strong> | <a href="release-automation-plan.zh.md">简体中文</a></p>

This document divides DebianForm release automation into verifiable loops. Each
loop should form a mergeable closed cycle:

- [x] Code or configuration runs.
- [x] Documentation is updated.
- [x] Local or CI acceptance commands pass.
- [x] Required manual intervention is listed explicitly.

Overall goals:

- A tag triggers the GitHub Actions release workflow.
- Linux/macOS amd64 and arm64 tarballs are built automatically.
- `checksums.txt` is generated automatically.
- A GitHub Release is created automatically.
- The `curl` installer supports install, upgrade, and rollback.
- `Formula/dbf.rb` in the `mofelee/homebrew-debianform` tap is updated
  automatically.

## Recommended Toolchain

- Use GoReleaser for multi-platform builds, archives, checksums, and GitHub
  Releases.
- Use GitHub Actions for tag triggers, permission control, and dry-run
  verification.
- Use the dedicated Homebrew tap repository `mofelee/homebrew-debianform`.
- Keep a manually signed tag as the final release gate; an ordinary merge must
  not publish automatically.

## Loop 0: Complete Release Metadata

Goal: give the repository the minimum metadata for public releases so later
automation can reference it.

Code/files:

- [x] Add `LICENSE`.
- [x] Add `CHANGELOG.md` with `Unreleased` and a placeholder for the first beta.
- [x] Add `SECURITY.md`.
- [x] State beta/public-preview positioning at the top of README.
- [x] Put the real license SPDX identifier in `docs/release-process.md`.

Acceptance:

```bash
test -s LICENSE
test -s CHANGELOG.md
test -s SECURITY.md
rg -n "v0.1.0-beta|Unreleased" CHANGELOG.md
rg -n "license" docs/release-process.md README.md
```

Manual intervention:

- MIT was selected by default.
- GitHub Security Advisories are the default vulnerability-reporting channel.

## Loop 1: Local Release Packaging

Goal: generate four platform tarballs and a checksum locally without GitHub
Actions.

Code/files:

- [x] Add `.goreleaser.yaml`.
- [x] Configure `builds` for:
  - `linux/amd64`
  - `linux/arm64`
  - `darwin/amd64`
  - `darwin/arm64`
- [x] Inject `Version`, `Commit`, and `Date` with ldflags.
- [x] Configure archives to contain:
  - `dbf`
  - `README.md`
  - `docs/`
  - `examples/`
  - `LICENSE`
  - `CHANGELOG.md`
- [x] Name the checksum file `checksums.txt`.
- [x] Keep tarball names consistent with `docs/release-process.md`:
  - `dbf_<tag>_linux_amd64.tar.gz`
  - `dbf_<tag>_linux_arm64.tar.gz`
  - `dbf_<tag>_darwin_amd64.tar.gz`
  - `dbf_<tag>_darwin_arm64.tar.gz`

Acceptance:

```bash
goreleaser check
goreleaser release --snapshot --clean --skip publish
ls dist/*.tar.gz dist/checksums.txt
tar -tzf dist/dbf_*_linux_amd64.tar.gz | rg '(^|/)dbf$'
tar -tzf dist/dbf_*_linux_amd64.tar.gz | rg 'README.md|docs/|examples/|LICENSE|CHANGELOG.md'
```

Manual intervention:

- GoReleaser was temporarily installed at
  `/tmp/debianform-tools/goreleaser` in the current environment and used for
  local acceptance.

## Loop 2: Release Dry-Run CI

Goal: verify release configuration in a PR or manual workflow without creating
a GitHub Release.

Code/files:

- [x] Add `.github/workflows/release-dry-run.yml`.
- [x] Support `workflow_dispatch`.
- [x] Run on an Ubuntu runner:
  - checkout
  - setup-go
  - `go vet ./...`
  - `go test -race -count=1 ./...`
  - `goreleaser check`
  - `goreleaser release --snapshot --clean --skip publish`
  - confirm `dist/checksums.txt` and all four tarballs exist
- [x] Use read-only `contents: read` permissions.

Acceptance:

```bash
gh workflow run release-dry-run.yml
gh run watch
```

When `gh` is unavailable, trigger the workflow from the GitHub Actions page.

Manual intervention:

- Allow GitHub Actions to execute the new workflow.
- The dry-run workflow installs GoReleaser with `go install` rather than adding
  a GoReleaser action, avoiding another action-pinning policy decision.

## Loop 3: curl Installer

Goal: allow users to install latest or a specific version with `curl | sh`, and
test core behavior locally against a fake release directory.

Code/files:

- [x] Add `scripts/install.sh`.
- [x] Support:
  - `--version`
  - `--prefix`
  - `--bin-dir`
  - `--os`
  - `--arch`
  - `--dry-run`
  - `--force`
- [x] Detect OS:
  - `Linux` -> `linux`
  - `Darwin` -> `darwin`
- [x] Detect architecture:
  - `x86_64`/`amd64` -> `amd64`
  - `aarch64`/`arm64` -> `arm64`
- [x] Download the matching tarball and `checksums.txt`.
- [x] Verify SHA-256.
- [x] Atomically replace `dbf`.
- [x] Install docs/examples under `<prefix>/share/debianform`.
- [x] Run `dbf version` after installation.

Tests:

- [x] `scripts/install.sh --dry-run --version v0.1.0-beta.1 --os linux --arch amd64`.
- [x] `scripts/install.sh --dry-run --version v0.1.0-beta.1 --os darwin --arch arm64`.
- [x] Verify through a local `file://` or test URL mode that checksum mismatch aborts.
- [x] Verify an unprivileged installation path under `/tmp/debianform-install`.

Acceptance:

```bash
sh scripts/install.sh --dry-run --version v0.1.0-beta.1 --os linux --arch amd64
sh scripts/install.sh --dry-run --version v0.1.0-beta.1 --os darwin --arch arm64
```

Manual intervention:

- README contains the `curl | sh` entry point; confirm it once more before the
  first real release.
- Confirm stable GitHub Release URLs and artifact names before the first real
  release.

## Loop 4: Initial Homebrew Tap Repository

Goal: create the Homebrew tap and manually install a test version.

Code/files:

- [x] Create `mofelee/homebrew-debianform`.
- [x] Add `Formula/dbf.rb`.
- [x] Use prebuilt GitHub Release tarballs for all four platforms.
- [x] Formula installation:
  - `bin.install "dbf"`
  - `pkgshare.install "README.md", "docs", "examples"`
- [x] Formula test runs `dbf version`.

Acceptance:

```bash
brew tap mofelee/debianform
brew audit --strict --online dbf
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
dbf version
```

Initial tap commit `6c0b64f` was pushed to
`mofelee/homebrew-debianform`. The formula currently points to test release
`v0.0.0-homebrew-test.1`. Its Linux amd64 tarball was downloaded, its SHA-256
verified, and the tarball confirmed to contain `dbf`, `README.md`, `docs/`, and
`examples/`. The current environment has no `brew`, so
`brew audit/install/test` still requires an environment with Homebrew.

Manual intervention:

- The `mofelee/homebrew-debianform` repository exists and the current account
  has write access.
- Automated CI updates still require a `HOMEBREW_TAP_GITHUB_TOKEN` secret.
- The tap name follows Homebrew conventions:
  `mofelee/homebrew-debianform` maps to `brew tap mofelee/debianform`.

## Loop 5: Tag-Triggered GitHub Release

Goal: after pushing a `v*` tag, automatically create a GitHub Release and upload
four platform tarballs plus checksums.

Code/files:

- [x] Add `.github/workflows/release.yml`.
- [x] Trigger on:

  ```yaml
  on:
    push:
      tags:
        - "v*"
  ```

- [x] Workflow permissions:

  ```yaml
  permissions:
    contents: write
  ```

- [x] Release job:
  - checkout with full history
  - setup-go
  - `go vet ./...`
  - `go test -race -count=1 ./...`
  - `goreleaser release --clean`
- [x] GoReleaser creates the GitHub Release and `checksums.txt`.

Acceptance:

```bash
git tag -a v0.0.0-test.1 -m "test release"
git push origin v0.0.0-test.1
gh release view v0.0.0-test.1
gh release download v0.0.0-test.1 --pattern 'checksums.txt' --dir /tmp/dbf-release-test
```

Verified: release workflow run `28011062473`, triggered by
`v0.0.0-test.1`, passed. The GitHub Release contained four platform tarballs and
`checksums.txt`, which was downloaded successfully to `/tmp/dbf-release-test`.

Delete test tags after verification:

```bash
gh release delete v0.0.0-test.1 --cleanup-tag
```

Cleanup completed: the test release and remote test tag were removed.

Manual intervention:

- The real release workflow was verified with a temporary test tag and cleaned up afterward.
- If test tags are disallowed later, only a snapshot workflow can verify the path before publishing.
- A maintainer still creates a signed tag manually for a formal release.

## Loop 6: Automatic Homebrew Tap Update

Goal: update `Formula/dbf.rb` in `mofelee/homebrew-debianform` automatically
after a formal release succeeds.

Code/files:

- [x] Configure Homebrew formula publishing in `.goreleaser.yaml`, or add a
  release-workflow step that generates and pushes `Formula/dbf.rb`.
- [x] Use binary tarball URLs and SHA-256 for all four platforms.
- [x] Use `HOMEBREW_TAP_GITHUB_TOKEN`, not default `GITHUB_TOKEN`, for a
  cross-repository push.
- [x] Include the version in the tap commit, for example `dbf v0.1.0-beta.1`.
- [x] Fail the release workflow if the Homebrew update fails, while retaining
  the GitHub Release.

Acceptance:

```bash
brew update
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

The automatic update path was verified: release workflow run `28011757388`,
triggered by `v0.0.0-homebrew-test.2`, passed. Its `Update Homebrew tap` step
pushed tap commit `efebb289ead82df3b2f94b303c14049cbf5b14b7` with message
`dbf v0.0.0-homebrew-test.2` and author `github-actions[bot]`. All four platform
URLs and SHA-256 values in `Formula/dbf.rb` matched the release's
`checksums.txt`. The current environment has no `brew`, so
`brew install/test/upgrade` still requires an environment with Homebrew. Test
release `v0.0.0-homebrew-test.2` remains available so the tap does not point to
missing artifacts; a formal release will overwrite the formula automatically.

Manual intervention:

- `HOMEBREW_TAP_GITHUB_TOKEN` exists.
- The token can push to `mofelee/homebrew-debianform`.
- The CI bot author was verified as
  `github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com>`.

## Loop 7: Automated Post-Release Verification

Goal: verify the curl and Homebrew user paths automatically after release.

Code/files:

- [x] Add or extend the release workflow's verification job.
- [x] Linux amd64 verification:
  - install the selected tag with the `curl` installer into a temporary prefix
  - ensure `dbf version` contains the tag
  - run `dbf validate -f examples/bbr.dbf.hcl`
  - run `dbf plan -f examples/bbr.dbf.hcl --offline`
  - run the Homebrew path when the runner has `brew`; otherwise mark it
    `manual/best-effort` in release notes
- [x] Verify macOS amd64/arm64 with GitHub-hosted runners when possible.
- [x] Until a Linux arm64 runner exists, mark artifact build as verified and
  the installation path as best effort in release notes.

Acceptance:

```bash
gh run view --log
```

Verified: release workflow run `28012012498`, triggered by
`v0.0.0-verify-test.1`, passed with `GitHub Release` and
`Post-release verification` jobs. The verification job installed the Linux
amd64 tarball through the curl installer, confirmed the tag in `dbf version`,
and passed `dbf validate -f examples/bbr.dbf.hcl` and
`dbf plan -f examples/bbr.dbf.hcl --offline`. The workflow wrote a verification
matrix into GitHub Release notes and uploaded artifact
`release-verification-v0.0.0-verify-test.1`. The Ubuntu runner had no Homebrew,
so the Homebrew path was marked `manual/best-effort`.

Release notes must contain this verification matrix:

| Path | linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64 |
| --- | --- | --- | --- | --- |
| Artifact build | yes | yes | yes | yes |
| curl install | yes | manual/best-effort | yes | yes |
| Homebrew install | manual/best-effort | manual/best-effort | yes | yes |

Final end-to-end acceptance: release workflow run `28012984066`, triggered by
`v0.0.0-final-release-test.1`, passed. It contained `GitHub Release`,
`Post-release verification (linux/amd64)`,
`Post-release verification (darwin/amd64)`,
`Post-release verification (darwin/arm64)`, and
`Release verification summary`, all successful. Release notes contained the
verification matrix. Both macOS amd64 and arm64 passed the curl installer,
`dbf validate`, `dbf plan --offline`, and Homebrew install/test/upgrade paths.
Linux amd64 passed curl installation, signature, SBOM, and provenance checks.
The Ubuntu runner lacked Homebrew, so Homebrew was marked
`manual/best-effort`.

Manual intervention:

- Provide a runner for real Linux arm64 verification or accept the best-effort label.
- Confirm verification scope if macOS runner cost or quota is limited.

## Loop 8: Signatures and Supply-Chain Hardening

Goal: improve release trust without blocking the first beta automation.

Code/files:

- [x] Generate `checksums.txt.sigstore.json`.
- [x] Use cosign keyless signing.
- [x] Generate SBOMs.
- [x] Generate provenance.
- [x] Add manual signature-verification instructions to README.

Acceptance:

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp 'https://github.com/mofelee/debianform/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Verified: release workflow run `28012412654`, triggered by
`v0.0.0-supply-chain-test.1`, passed. The GitHub Release contained
`checksums.txt.sigstore.json` and four `*.sbom.spdx.json` platform SBOMs. The
post-release verification job ran
`cosign verify-blob --bundle checksums.txt.sigstore.json ... checksums.txt` and
returned `Verified OK`; it also passed
`gh attestation verify dbf_v0.0.0-supply-chain-test.1_linux_amd64.tar.gz --repo mofelee/debianform`.

Final end-to-end acceptance `v0.0.0-final-release-test.1` / run `28012984066`
passed the same supply-chain checks. That release contained four platform
tarballs, `checksums.txt`, `checksums.txt.sigstore.json`, and four
`*.sbom.spdx.json` files. The Linux verification job checked checksums, the
cosign keyless bundle, and GitHub provenance attestation.

Manual intervention:

- Cosign keyless was selected; no long-lived GPG private key is needed.

## Minimum Viable Release Path

To reach a usable release quickly, merge in this order:

1. Loop 0
2. Loop 1
3. Loop 2
4. Loop 3
5. Loop 5
6. Loop 4
7. Loop 6
8. Loop 7

GitHub Release and `curl` can therefore launch first, with the Homebrew tap
immediately afterward. Loop 8 does not block beta.

## Manual Intervention Checklist

Status of manual items during release-automation implementation:

- MIT license selected.
- GitHub Security Advisories selected for security reports.
- `mofelee/homebrew-debianform` created.
- `HOMEBREW_TAP_GITHUB_TOKEN` created and verified.
- Temporary test tags permitted and used to verify the release workflow.
- A maintainer still creates the signed tag for formal releases.
- No hosted Linux arm64 runner is available; release notes mark it
  `manual/best-effort`.
- macOS amd64 and arm64 are covered by automated verification.
- Cosign keyless selected for signatures.
