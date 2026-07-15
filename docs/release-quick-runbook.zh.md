# DebianForm Release 快速操作手册

日常发布按这份短流程执行；设计和背景见
[release process](release-process.zh.md)。

## 发布前

1. 更新 `CHANGELOG.md`，确认当前 tag 的变更、兼容性和迁移说明完整。
2. 按 [release notes template](release-notes-template.md) 准备 GitHub Release notes，
   明确 breaking changes、known issues、verification matrix 和 migration notes。
3. 确认本地在 `main` 且工作区干净：

   ```bash
   git switch main
   git pull --ff-only
   git status --short
   ```

4. 运行基础检查：

   ```bash
   test -z "$(gofmt -l $(git ls-files '*.go'))"
   go vet ./...
   go test -race -count=1 ./...
   make build
   make vulncheck
   make test-integration-layout
   goreleaser check
   git diff --check
   ```

5. 确认 release commit 的三个 target CI gates 通过：

   ```bash
   SHA="$(git rev-parse HEAD)"
   gh run list --workflow ci.yml --commit "$SHA" --limit 1 \
     --json databaseId,headSha,conclusion,url
   gh run view <ci-run-id>
   ```

   必须确认同一提交上的 Ubuntu 24.04 LTS amd64、Ubuntu 26.04 LTS amd64、Debian 12 amd64、
   Debian 13 amd64 均为 20/20，且 `Ubuntu 24.04 target matrix gate`、
   `Ubuntu 26.04 target matrix gate` 与 `Managed target matrix gate` 都成功。把四个目标各自的
   结果、exact commit 和 CI run URL 分开写入 release notes；同时记录 26.04 released-image URL
   和 SHA-256，不能只写一个合并后的“libvirt passed”。

6. 触发 dry-run workflow，并确认通过：

   ```bash
   gh workflow run release-dry-run.yml --ref main
   gh run list --workflow release-dry-run.yml --limit 1
   gh run watch <run-id> --exit-status
   ```

## 发布

1. 创建 signed tag：

   ```bash
   TAG=v0.1.0-beta.1
   git tag -s "$TAG" -m "$TAG"
   git push origin "$TAG"
   ```

2. 观察 release workflow：

   ```bash
   gh run list --workflow release.yml --limit 1
   gh run watch <run-id> --exit-status
   ```

3. workflow 通过后确认 GitHub Release：

   ```bash
   gh release view "$TAG" --json url,assets,body
   ```

   必须看到四个平台 tarball、`checksums.txt`、`checksums.txt.sigstore.json`、SBOM，
   以及 release notes 中的 Verification Matrix。

4. 抽查签名和 provenance：

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

5. 确认 Homebrew tap 已更新：

   ```bash
   brew update
   brew install mofelee/debianform/dbf
   brew test mofelee/debianform/dbf
   dbf version
   ```

6. 确认 curl 安装路径：

   ```bash
   curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh \
     | sh -s -- --version "$TAG" --prefix "$HOME/.local" --force
   "$HOME/.local/bin/dbf" version
   ```

## 回滚和补救

- installer 用户回滚到旧版本：

  ```bash
  curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh \
    | sh -s -- --version v0.1.0-beta.1 --force
  ```

- Homebrew 用户发布前可用 `brew pin dbf` 暂停升级；已发布坏版本时优先发布新的修复 tag，
  让 tap 自动指向修复版本。
- 如果 GitHub Release 已创建但 Homebrew tap 更新失败，不复用 tag；修复 workflow 或 tap 后
  发布新的 patch/prerelease tag。
- 如果是临时测试 tag，且 tap 已指向其他仍存在的版本，可以清理：

  ```bash
  gh release delete "$TAG" --cleanup-tag --yes
  git tag -d "$TAG"
  ```
