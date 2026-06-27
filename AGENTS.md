# Repository Guidelines

## Project Structure & Module Organization

DebianForm is a Go CLI module (`github.com/mofelee/debianform`) with the `dbf` entrypoint in `cmd/dbf/`. Core parsing, IR, planning, graph, and state logic lives under `internal/core/`; version metadata is in `internal/version/`. User-facing examples are stored as `examples/*.dbf.hcl`, and documentation is in `docs/` with the main README in Chinese. Tests sit next to Go packages as `*_test.go`, with fixtures and golden files under `internal/core/testdata/` and `cmd/dbf/testdata/`. End-to-end VM cases live in `test/integration/libvirt/`; source build integration tests live in `test/integration/sourcebuild/`.

## Build, Test, and Development Commands

- `make build`: builds the `dbf` binary from `./cmd/dbf` with version ldflags.
- `make test`: runs `go test ./...`.
- `make test-unit`: runs `go test -race -count=1 ./...`.
- `go vet ./...`: matches the CI vet check.
- `test -z "$(gofmt -l $(git ls-files '*.go'))"`: checks Go formatting as CI does.
- `make update-golden`: regenerates golden files with `UPDATE_GOLDEN=1 go test ./...`; review diffs carefully.
- `make test-integration-layout`: validates libvirt case layout and helper scripts.
- `make test-integration`: runs full Debian libvirt VM integration tests; requires a working Linux x86_64 libvirt setup.

## Coding Style & Naming Conventions

Use standard Go formatting (`gofmt`) and idiomatic package-local tests. Keep package names short and lowercase. Prefer focused functions in the existing package boundaries rather than introducing new top-level modules. HCL examples and fixtures should use the repository convention `name.dbf.hcl`; golden outputs should use descriptive `*.golden.json` names.

## Testing Guidelines

Add or update unit tests beside the affected package. Parser, hostspec, plan, and CLI behavior often relies on golden fixtures, so update them only when the behavior change is intentional. Use `make test-unit` for race-sensitive changes and `make test-integration-source-build` for source build behavior. Use libvirt integration cases when a change must prove real Debian 13 apply/check behavior.

## Commit & Pull Request Guidelines

Recent history uses concise Conventional Commit-style subjects such as `feat(cli): ...`, `docs: ...`, and release commits like `Prepare v0.1.0-beta.7 release`. Keep commits scoped and imperative. Pull requests should describe the behavior change, list tests run, link related issues, and include CLI output or screenshots for user-visible plan, HTML, color, or documentation changes.

## Security & Configuration Tips

Do not commit real secrets, private SSH keys, state files, or generated VM artifacts. DebianForm redacts sensitive values in plans and state; preserve that behavior when touching parser, plan, state, or JSON/HTML output paths.
