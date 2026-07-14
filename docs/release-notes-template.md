# Release Notes Template

Copy this template into the GitHub Release body before publishing a DebianForm
release. Keep every top-level section, even when the answer is `None`.
The release workflow appends or replaces the final `Verification Matrix`
section after post-release verification jobs finish.

```markdown
## Summary

- <One to three bullets describing the user-visible purpose of this release.>

## Compatibility

- Release phase: <public beta | stable | patch>.
- Supported CLI platforms: <linux/amd64, linux/arm64, darwin/amd64, darwin/arm64>.
- Primary managed target: Debian 13 amd64.
- Additional Beta managed target: Debian 12 amd64.
- Preview managed targets: Ubuntu 24.04 LTS amd64, Debian 12 arm64, and Debian 13 arm64.
- DSL/state/plan JSON compatibility: <compatible | breaking | beta-limited>.

## Breaking Changes

- <None, or a bullet for each breaking CLI, DSL, state, plan JSON, provider, or
  release artifact change. Include the old behavior, new behavior, and who is
  affected.>

## Migration Notes

- <None, or exact steps users must take before/after upgrading. Include config
  edits, state handling, command changes, and rollback notes when relevant.>

## Added

- <New user-visible capabilities.>

## Changed

- <Behavior changes that are not breaking, including defaults and output shape.>

## Fixed

- <Bug fixes.>

## Security

- <Security fixes, dependency vulnerability notes, secret-handling changes, or
  "No security fixes in this release.">

## Known Issues

- <Known regressions, beta limitations, unsupported platforms, best-effort
  verification paths, or external-service dependencies.>

## Support Matrix

- CLI artifacts: <list built artifacts or link to assets>.
- Managed targets: Debian 13 amd64 (primary Beta), Debian 12 amd64 (Beta),
  Ubuntu 24.04 LTS amd64 (Preview), Debian 12/13 arm64 (Preview); other Ubuntu
  tuples, desktop environments, Debian 11 and earlier are Unsupported.
- Ubuntu network boundary: DebianForm does not manage or migrate Netplan;
  networkd requires an operator-prepared native-networkd target.
- Install paths: <curl/Homebrew/.deb/apt support status>.
- Feature support: see [support matrix](https://github.com/mofelee/debianform/blob/main/docs/support-matrix.zh.md).

## Verification

- Commit: `<full commit sha>`.
- Local checks:
  - `go vet ./...`: <pass | fail | skipped with reason>
  - `go test -race -count=1 ./...`: <pass | fail | skipped with reason>
  - `make build`: <pass | fail | skipped with reason>
  - `make vulncheck`: <pass | fail | skipped with reason>
  - `make test-integration-layout`: <pass | fail | skipped with reason>
- Managed-target CI evidence for this commit:
  - Ubuntu 24.04 LTS amd64 libvirt matrix (20/20): <pass | fail; CI run URL>
  - Debian 12 amd64 libvirt matrix (20/20): <pass | fail; CI run URL>
  - Debian 13 amd64 libvirt matrix (20/20): <pass | fail; CI run URL>
  - `Ubuntu 24.04 target matrix gate`: <pass | fail; CI run URL>
  - `Managed target matrix gate`: <pass | fail; CI run URL>
- Optional local managed-target checks:
  - `make test-integration TARGET=ubuntu-24.04`: <pass | fail | skipped with reason>
  - `make test-integration DEBIAN_VERSION=12`: <pass | fail | skipped with reason>
  - `make test-integration DEBIAN_VERSION=13`: <pass | fail | skipped with reason>
- Release workflow: <workflow run URL>.
- Manual checks:
  - GitHub Release assets present: <pass | fail | skipped with reason>
  - checksum verification: <pass | fail | skipped with reason>
  - cosign keyless bundle verification: <pass | fail | skipped with reason>
  - GitHub provenance attestation verification: <pass | fail | skipped with reason>
  - curl installer smoke: <pass | fail | skipped with reason>
  - Homebrew install/test smoke: <pass | fail | skipped with reason>

## Verification Matrix

This section is filled by the release workflow after post-release verification.
If editing release notes manually before workflow completion, leave this heading
in place or omit the section entirely.
```

## Required Review Before Tagging

Before creating the release tag, check that:

- `Breaking Changes` is explicit. Use `None` only after confirming there are no
  breaking CLI, DSL, state, plan JSON, provider, installer, artifact, or
  workflow behavior changes.
- `Known Issues` lists beta limitations and any platform verification paths that
  are manual or best-effort.
- `Verification Matrix` is either absent or present with the exact heading
  `## Verification Matrix`; the workflow uses that heading when replacing the
  generated matrix.
- `Migration Notes` explains required user action, including state handling and
  rollback guidance for breaking releases.
- Ubuntu 24.04, Debian 12, and Debian 13 managed-target evidence is listed
  separately as `20/20`, points to the release commit's CI run, and both
  aggregate gates passed.
- The release notes do not use stable/GA/production-ready language unless the
  release has passed the stable gates in
  [project maturity checklist](archive/legacy-design/project-maturity-and-launch-checklist.zh.md).
