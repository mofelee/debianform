# 发布自动化执行计划

本文档把 DebianForm 的发布自动化拆成可验证 loop。每个 loop 都应该形成一个可以合并的闭环：

- [x] 代码或配置可运行
- [x] 文档同步更新
- [x] 本地或 CI 验收命令通过
- [x] 需要人工介入的事项明确列出

总体目标：

- tag 触发 GitHub Actions release workflow。
- 自动构建 Linux/macOS 的 amd64 和 arm64 tarball。
- 自动生成 `checksums.txt`。
- 自动创建 GitHub Release。
- `curl` installer 可安装、升级和回滚。
- 自动更新 `mofelee/homebrew-debianform` tap 的 `Formula/dbf.rb`。

## 推荐工具链

- 使用 GoReleaser 负责多平台构建、archive、checksum 和 GitHub Release。
- 使用 GitHub Actions 负责 tag 触发、权限控制和 dry-run 验证。
- 使用单独的 Homebrew tap 仓库 `mofelee/homebrew-debianform`。
- 使用手动 signed tag 作为最终发布闸门，不让普通 merge 自动发布。

## Loop 0: 发布元数据补齐

目标：让仓库具备公开 release 的最低元数据，后续自动化可以引用这些文件。

代码/文件：

- [x] 新增 `LICENSE`。
- [x] 新增 `CHANGELOG.md`，包含 `Unreleased` 和第一个 beta 版本占位。
- [x] 新增 `SECURITY.md`。
- [x] README 顶部明确 beta/public preview 定位。
- [x] `docs/release-process.zh.md` 中填入真实 license SPDX id。

验收：

```bash
test -s LICENSE
test -s CHANGELOG.md
test -s SECURITY.md
rg -n "v0.1.0-beta|Unreleased" CHANGELOG.md
rg -n "license" docs/release-process.zh.md README.md
```

需要你介入：

- 已按默认选择 MIT。
- 已默认使用 GitHub Security Advisory 作为安全漏洞报告方式。

## Loop 1: 本地 release 打包能力

目标：不依赖 GitHub Actions，先在本地生成四个平台 tarball 和 checksum。

代码/文件：

- [x] 新增 `.goreleaser.yaml`。
- [x] 配置 `builds` 覆盖：
  - `linux/amd64`
  - `linux/arm64`
  - `darwin/amd64`
  - `darwin/arm64`
- [x] 配置 ldflags 注入 `Version`、`Commit`、`Date`。
- [x] 配置 archives，tarball 包含：
  - `dbf`
  - `README.md`
  - `docs/`
  - `examples/`
  - `LICENSE`
  - `CHANGELOG.md`
- [x] 配置 checksum 文件名为 `checksums.txt`。
- [x] 保持 tarball 命名与 `docs/release-process.zh.md` 一致：
  - `dbf_<tag>_linux_amd64.tar.gz`
  - `dbf_<tag>_linux_arm64.tar.gz`
  - `dbf_<tag>_darwin_amd64.tar.gz`
  - `dbf_<tag>_darwin_arm64.tar.gz`

验收：

```bash
goreleaser check
goreleaser release --snapshot --clean --skip publish
ls dist/*.tar.gz dist/checksums.txt
tar -tzf dist/dbf_*_linux_amd64.tar.gz | rg '(^|/)dbf$'
tar -tzf dist/dbf_*_linux_amd64.tar.gz | rg 'README.md|docs/|examples/|LICENSE|CHANGELOG.md'
```

需要你介入：

- 当前环境已临时安装 GoReleaser 到 `/tmp/debianform-tools/goreleaser` 并用于本地验收。

## Loop 2: release dry-run CI

目标：在 PR 或手动 workflow 中验证 release 配置，但不创建 GitHub Release。

代码/文件：

- [x] 新增 `.github/workflows/release-dry-run.yml`。
- [x] workflow 支持 `workflow_dispatch`。
- [x] workflow 在 Ubuntu runner 上执行：
  - checkout
  - setup-go
  - `go vet ./...`
  - `go test -race -count=1 ./...`
  - `goreleaser check`
  - `goreleaser release --snapshot --clean --skip publish`
  - 检查 `dist/checksums.txt` 和四个平台 tarball 存在
- [x] dry-run workflow 权限使用只读 `contents: read`。

验收：

```bash
gh workflow run release-dry-run.yml
gh run watch
```

或在没有 `gh` 时，从 GitHub Actions 页面手动触发 workflow。

需要你介入：

- 需要允许 GitHub Actions 运行新 workflow。
- dry-run workflow 未引入 GoReleaser action；GoReleaser 通过 `go install` 安装，
  避免额外 action pinning 策略问题。

## Loop 3: curl installer

目标：用户可以用 `curl | sh` 安装 latest 或指定版本，并能本地用假 release 目录测试核心逻辑。

代码/文件：

- [x] 新增 `scripts/install.sh`。
- [x] 支持参数：
  - `--version`
  - `--prefix`
  - `--bin-dir`
  - `--os`
  - `--arch`
  - `--dry-run`
  - `--force`
- [x] 自动检测 OS：
  - `Linux` -> `linux`
  - `Darwin` -> `darwin`
- [x] 自动检测 arch：
  - `x86_64`/`amd64` -> `amd64`
  - `aarch64`/`arm64` -> `arm64`
- [x] 下载对应 tarball 和 `checksums.txt`。
- [x] 校验 SHA256。
- [x] 原子替换 `dbf`。
- [x] 安装 docs/examples 到 `<prefix>/share/debianform`。
- [x] 安装完成后执行 `dbf version`。

测试：

- [x] `scripts/install.sh --dry-run --version v0.1.0-beta.1 --os linux --arch amd64`。
- [x] `scripts/install.sh --dry-run --version v0.1.0-beta.1 --os darwin --arch arm64`。
- [x] 使用本地 `file://` 或测试 URL 模式验证 checksum 失败会中止。
- [x] 使用 `/tmp/debianform-install` 验证无需 root 的安装路径。

验收：

```bash
sh scripts/install.sh --dry-run --version v0.1.0-beta.1 --os linux --arch amd64
sh scripts/install.sh --dry-run --version v0.1.0-beta.1 --os darwin --arch arm64
```

需要你介入：

- README 已包含 `curl | sh` 入口；第一次真实发布前仍需最终确认。
- 第一次真实发布前，需要确认 GitHub Release URL 和 artifact 命名稳定。

## Loop 4: Homebrew tap 初始仓库

目标：建立 Homebrew tap 仓库，并能手工安装一个测试版本。

代码/文件：

- [x] 创建仓库 `mofelee/homebrew-debianform`。
- [x] 新增 `Formula/dbf.rb`。
- [x] formula 使用 GitHub Release 四个平台预编译 tarball。
- [x] formula 安装：
  - `bin.install "dbf"`
  - `pkgshare.install "README.md", "docs", "examples"`
- [x] formula test 执行 `dbf version`。

验收：

```bash
brew tap mofelee/debianform
brew audit --strict --online dbf
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
dbf version
```

已推送初始 tap commit `6c0b64f` 到 `mofelee/homebrew-debianform`。当前公式指向测试 release
`v0.0.0-homebrew-test.1`；已下载 Linux amd64 tarball、校验 sha256，并确认 tarball 包含
`dbf`、`README.md`、`docs/` 和 `examples/`。当前执行环境没有 `brew`，因此
`brew audit/install/test` 仍需在有 Homebrew 的环境执行。

需要你介入：

- 已创建 `mofelee/homebrew-debianform` 仓库，并已授予当前账号写权限。
- CI 自动更新仍需要 `HOMEBREW_TAP_GITHUB_TOKEN` secret。
- tap 仓库命名已按 Homebrew 规则确认：`mofelee/homebrew-debianform` 对应
  `brew tap mofelee/debianform`。

## Loop 5: tag 触发 GitHub Release

目标：push `v*` tag 后自动创建 GitHub Release，并上传四个平台 tarball 和 checksum。

代码/文件：

- [x] 新增 `.github/workflows/release.yml`。
- [x] workflow 触发条件：

  ```yaml
  on:
    push:
      tags:
        - "v*"
  ```

- [x] workflow 权限：

  ```yaml
  permissions:
    contents: write
  ```

- [x] release job 执行：
  - checkout with full history
  - setup-go
  - `go vet ./...`
  - `go test -race -count=1 ./...`
  - `goreleaser release --clean`
- [x] GoReleaser 自动生成 GitHub Release 和 `checksums.txt`。

验收：

```bash
git tag -a v0.0.0-test.1 -m "test release"
git push origin v0.0.0-test.1
gh release view v0.0.0-test.1
gh release download v0.0.0-test.1 --pattern 'checksums.txt' --dir /tmp/dbf-release-test
```

已验证：`v0.0.0-test.1` 触发的 release workflow run `28011062473` 通过，GitHub Release
包含四个平台 tarball 和 `checksums.txt`，并已成功下载 `checksums.txt` 到
`/tmp/dbf-release-test`。

测试 tag 验证完必须删除：

```bash
gh release delete v0.0.0-test.1 --cleanup-tag
```

已清理：测试 release 和远端测试 tag 已删除。

需要你介入：

- 已用临时 test tag 验证真实 release workflow，并在验证后删除。
- 如果后续不允许 test tag，则只能用 snapshot workflow 验证到发布前一步。
- 正式发布时由你手动创建 signed tag。

## Loop 6: 自动更新 Homebrew tap

目标：正式 release 成功后自动更新 `mofelee/homebrew-debianform` 的 `Formula/dbf.rb`。

代码/文件：

- [x] 在 `.goreleaser.yaml` 中配置 Homebrew formula 发布，或新增 release workflow step
  生成并推送 `Formula/dbf.rb`。
- [x] 使用二进制 tarball URL 和四个平台 sha256。
- [x] workflow 使用 `HOMEBREW_TAP_GITHUB_TOKEN`，不使用默认 `GITHUB_TOKEN` 跨仓库推送。
- [x] tap commit message 包含版本号，例如 `dbf v0.1.0-beta.1`。
- [x] release workflow 在 Homebrew 更新失败时失败，并保留 GitHub Release。

验收：

```bash
brew update
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

已验证自动更新链路：`v0.0.0-homebrew-test.2` 触发的 release workflow run
`28011757388` 通过，`Update Homebrew tap` step 成功推送 tap commit
`efebb289ead82df3b2f94b303c14049cbf5b14b7`，commit message 为
`dbf v0.0.0-homebrew-test.2`，作者为 `github-actions[bot]`。`Formula/dbf.rb`
中的四个平台 URL 和 sha256 已与该 release 的 `checksums.txt` 对齐。
当前执行环境没有 `brew`，因此 `brew install/test/upgrade` 仍需在有 Homebrew 的环境执行。
测试 release `v0.0.0-homebrew-test.2` 暂时保留，避免 tap 公式指向不存在的 artifact；
正式 release 会自动覆盖该公式。

需要你介入：

- 已创建 `HOMEBREW_TAP_GITHUB_TOKEN` secret。
- token 已验证可以向 `mofelee/homebrew-debianform` 推送。
- CI bot 的 commit author 已验证为 `github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com>`。

## Loop 7: 发布后自动验证

目标：release 完成后自动验证 curl 和 Homebrew 两条用户路径。

代码/文件：

- [x] 新增或扩展 release workflow 的 verify job。
- [x] Linux amd64 验证：
  - `curl` installer 安装指定 tag 到临时 prefix。
  - `dbf version` 包含 tag。
  - `dbf validate -f examples/bbr.dbf.hcl`。
  - `dbf plan -f examples/bbr.dbf.hcl --offline`。
  - Homebrew 路径在 runner 有 `brew` 时执行，否则 release notes 标记为
    `manual/best-effort`。
- [x] macOS amd64/arm64 验证尽量使用 GitHub hosted runners。
- [x] Linux arm64 如果暂时没有 runner，在 release notes 中标记为 artifact build verified，
  install path best-effort。

验收：

```bash
gh run view --log
```

已验证：`v0.0.0-verify-test.1` 触发的 release workflow run `28012012498`
通过，包含 `GitHub Release` 和 `Post-release verification` 两个 job。verify job 已通过：
curl installer 安装 Linux amd64 tarball、`dbf version` 包含 tag、
`dbf validate -f examples/bbr.dbf.hcl`、`dbf plan -f examples/bbr.dbf.hcl --offline`。
验证矩阵已写入 GitHub Release notes，并上传为 artifact
`release-verification-v0.0.0-verify-test.1`。Ubuntu runner 未安装 Homebrew，因此 Homebrew
路径记录为 `manual/best-effort`。

release notes 中必须包含验证矩阵：

| Path | linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64 |
| --- | --- | --- | --- | --- |
| Artifact build | yes | yes | yes | yes |
| curl install | yes | manual/best-effort | yes | yes |
| Homebrew install | manual/best-effort | manual/best-effort | yes | yes |

最终端到端验收：`v0.0.0-final-release-test.1` 触发的 release workflow run
`28012984066` 通过。该 run 包含 `GitHub Release`、`Post-release verification
(linux/amd64)`、`Post-release verification (darwin/amd64)`、`Post-release verification
(darwin/arm64)` 和 `Release verification summary`，所有 job 均为 success。release notes
已写入验证矩阵；macOS amd64 和 macOS arm64 均通过 curl installer、`dbf validate`、
`dbf plan --offline`、Homebrew install/test/upgrade 路径。Linux amd64 通过 curl installer、
签名、SBOM 和 provenance 验证；Ubuntu runner 没有 Homebrew，因此 Homebrew 标记为
`manual/best-effort`。

需要你介入：

- 如果需要真实 Linux arm64 runner，需要你提供 runner 或接受 best-effort 标记。
- 如果 macOS runner 费用或配额有限，需要你确认验证范围。

## Loop 8: 签名和供应链增强

目标：提升发布可信度，但不阻塞第一版 beta 自动化。

代码/文件：

- [x] 生成 `checksums.txt.sigstore.json`。
- [x] 使用 cosign keyless signing。
- [x] 生成 SBOM。
- [x] 生成 provenance。
- [x] README 增加手工校验签名说明。

验收：

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp 'https://github.com/mofelee/debianform/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

已验证：`v0.0.0-supply-chain-test.1` 触发的 release workflow run `28012412654`
通过。GitHub Release 包含 `checksums.txt.sigstore.json` 和四个平台 SBOM
`*.sbom.spdx.json`；post-release verify job 已执行
`cosign verify-blob --bundle checksums.txt.sigstore.json ... checksums.txt` 并返回
`Verified OK`，同时 `gh attestation verify dbf_v0.0.0-supply-chain-test.1_linux_amd64.tar.gz
--repo mofelee/debianform` 通过。

最终端到端验收 `v0.0.0-final-release-test.1` / run `28012984066` 也通过了相同供应链验证。
该 release 包含四个平台 tarball、`checksums.txt`、`checksums.txt.sigstore.json` 和四个
`*.sbom.spdx.json`；Linux verify job 已校验 checksum、cosign keyless bundle 和 GitHub
provenance attestation。

需要你介入：

- 已选择 cosign keyless；无需长期 GPG 私钥。

## 最小可上线路径

如果要尽快做到可用，按下面顺序合并：

1. Loop 0
2. Loop 1
3. Loop 2
4. Loop 3
5. Loop 5
6. Loop 4
7. Loop 6
8. Loop 7

也就是说，GitHub Release 和 `curl` 可以先上线；Homebrew tap 可以紧随其后。Loop 8 不阻塞
beta。

## 人工介入清单

发布自动化推进过程中，人工事项状态：

- 已选择 MIT license。
- 已确认使用 GitHub Security Advisory 作为安全报告方式。
- 已创建 `mofelee/homebrew-debianform` 仓库。
- 已创建并验证 `HOMEBREW_TAP_GITHUB_TOKEN` secret。
- 已允许并使用临时 test tag 验证 release workflow。
- 正式发布时仍由维护者手动创建 signed tag。
- Linux arm64 暂无 hosted runner，release notes 标记为 `manual/best-effort`。
- macOS amd64 和 macOS arm64 已纳入自动验证。
- 已选择 cosign keyless 作为签名方案。
