<p align="right">
  <strong>English</strong> | <a href="release-quick-runbook.zh.md">简体中文</a>
</p>

# DebianForm Release Quick Runbook

Use this short procedure for routine releases. See the [release process](release-process.md) for
design details and background.

## Before Release

1. Update `CHANGELOG.md`. Confirm that the changes since the current tag, compatibility impact, and
   migration instructions are complete.
2. Prepare the GitHub Release notes from the
   [release notes template](release-notes-template.md), explicitly covering breaking changes,
   known issues, the verification matrix, and migration notes.
3. Confirm that the local checkout is on `main` and the worktree is clean:

   ```bash
   git switch main
   git pull --ff-only
   git status --short
   ```

4. Run the baseline checks:

   ```bash
   make docs-check
   test -z "$(gofmt -l $(git ls-files '*.go'))"
   go vet ./...
   go test -race -count=1 ./...
   make build
   make vulncheck
   make test-integration-layout
   goreleaser check
   git diff --check
   ```

5. Confirm that all three target CI gates passed on the release commit:

   ```bash
   SHA="$(git rev-parse HEAD)"
   gh run list --workflow ci.yml --commit "$SHA" --limit 1 \
     --json databaseId,headSha,conclusion,url
   gh run view <ci-run-id>
   ```

   Ubuntu 24.04 LTS amd64, Ubuntu 26.04 LTS amd64, Debian 12 amd64, and Debian 13 amd64 must each
   pass 20/20 cases on the same commit. `Ubuntu 24.04 target matrix gate`,
   `Ubuntu 26.04 target matrix gate`, and `Managed target matrix gate` must all succeed. Record each
   target's result, the exact commit, and the CI run URL separately in the release notes. Also
   record the Ubuntu 26.04 released-image URL and SHA-256; one combined statement that "libvirt
   passed" is not sufficient.

6. Trigger the dry-run workflow and confirm it passes:

   ```bash
   gh workflow run release-dry-run.yml --ref main
   gh run list --workflow release-dry-run.yml --limit 1
   gh run watch <run-id> --exit-status
   ```

## Release

1. Create a signed tag:

   ```bash
   TAG=v0.1.0-beta.1
   git tag -s "$TAG" -m "$TAG"
   git push origin "$TAG"
   ```

2. Watch the release workflow:

   ```bash
   gh run list --workflow release.yml --limit 1
   gh run watch <run-id> --exit-status
   ```

3. After the workflow passes, inspect the GitHub Release:

   ```bash
   gh release view "$TAG" --json url,assets,body
   ```

   It must contain tarballs for all four platforms, `checksums.txt`,
   `checksums.txt.sigstore.json`, SBOMs, and a Verification Matrix in the release notes.

4. Spot-check signatures and provenance:

   ```bash
   rm -rf /tmp/dbf-release-check
   gh release download "$TAG" \
     --pattern "dbf_${TAG}_linux_amd64.tar.gz" \
     --pattern checksums.txt \
     --pattern checksums.txt.sigstore.json \
     --dir /tmp/dbf-release-check
   cd /tmp/dbf-release-check
   sha256sum --check --ignore-missing checksums.txt
   cosign verify-blob \
     --bundle checksums.txt.sigstore.json \
     --certificate-identity-regexp 'https://github.com/mofelee/debianform/.github/workflows/release.yml@refs/tags/v.*' \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     checksums.txt
   gh attestation verify "dbf_${TAG}_linux_amd64.tar.gz" --repo mofelee/debianform
   ```

5. Confirm that the Homebrew tap was updated:

   ```bash
   brew update
   brew install mofelee/debianform/dbf
   brew test mofelee/debianform/dbf
   dbf version
   ```

6. Confirm the curl installation path:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh \
     | sh -s -- --version "$TAG" --prefix "$HOME/.local" --force
   "$HOME/.local/bin/dbf" version
   ```

## Rollback and Remediation

- Installer users can roll back to an older version:

  ```bash
  curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh \
    | sh -s -- --version v0.1.0-beta.1 --force
  ```

- Homebrew users can run `brew pin dbf` before a release to pause upgrades. If a bad version was
  published, prefer publishing a new fixed tag so the tap moves to the fixed version.
- If the GitHub Release exists but the Homebrew tap update failed, do not reuse the tag. Fix the
  workflow or tap and publish a new patch or prerelease tag.
- If this was a temporary test tag and the tap already points to another existing version, clean it
  up with:

  ```bash
  gh release delete "$TAG" --cleanup-tag --yes
  git tag -d "$TAG"
  ```
