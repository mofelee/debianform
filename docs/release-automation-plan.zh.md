# 发布自动化执行计划

本文档把 DebianForm 的发布自动化拆成可验证 loop。每个 loop 都应该形成一个可以合并的闭环：

- [ ] 代码或配置可运行
- [ ] 文档同步更新
- [ ] 本地或 CI 验收命令通过
- [ ] 需要人工介入的事项明确列出

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

- [ ] 新增 `LICENSE`。
- [ ] 新增 `CHANGELOG.md`，包含 `Unreleased` 和第一个 beta 版本占位。
- [ ] 新增 `SECURITY.md`。
- [ ] README 顶部明确 beta/public preview 定位。
- [ ] `docs/release-process.zh.md` 中填入真实 license SPDX id。

验收：

```bash
test -s LICENSE
test -s CHANGELOG.md
test -s SECURITY.md
rg -n "v0.1.0-beta|Unreleased" CHANGELOG.md
rg -n "license" docs/release-process.zh.md README.md
```

需要你介入：

- 选择 license。建议 MIT 或 Apache-2.0；如果没有偏好，默认 MIT。
- 确认安全漏洞报告方式，例如 GitHub Security Advisory 或指定邮箱。

## Loop 1: 本地 release 打包能力

目标：不依赖 GitHub Actions，先在本地生成四个平台 tarball 和 checksum。

代码/文件：

- [ ] 新增 `.goreleaser.yaml`。
- [ ] 配置 `builds` 覆盖：
  - `linux/amd64`
  - `linux/arm64`
  - `darwin/amd64`
  - `darwin/arm64`
- [ ] 配置 ldflags 注入 `Version`、`Commit`、`Date`。
- [ ] 配置 archives，tarball 包含：
  - `dbf`
  - `README.md`
  - `docs/`
  - `examples/`
  - `LICENSE`
  - `CHANGELOG.md`
- [ ] 配置 checksum 文件名为 `checksums.txt`。
- [ ] 保持 tarball 命名与 `docs/release-process.zh.md` 一致：
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

- 如果当前环境没有 GoReleaser，需要允许安装或在 CI 中验证。

## Loop 2: release dry-run CI

目标：在 PR 或手动 workflow 中验证 release 配置，但不创建 GitHub Release。

代码/文件：

- [ ] 新增 `.github/workflows/release-dry-run.yml`。
- [ ] workflow 支持 `workflow_dispatch`。
- [ ] workflow 在 Ubuntu runner 上执行：
  - checkout
  - setup-go
  - `go vet ./...`
  - `go test -race -count=1 ./...`
  - `goreleaser check`
  - `goreleaser release --snapshot --clean --skip publish`
  - 检查 `dist/checksums.txt` 和四个平台 tarball 存在
- [ ] dry-run workflow 权限使用只读 `contents: read`。

验收：

```bash
gh workflow run release-dry-run.yml
gh run watch
```

或在没有 `gh` 时，从 GitHub Actions 页面手动触发 workflow。

需要你介入：

- 允许 GitHub Actions 运行新 workflow。
- 如果仓库未安装 GoReleaser action pinning 策略，需要确认是否固定 action commit SHA。

## Loop 3: curl installer

目标：用户可以用 `curl | sh` 安装 latest 或指定版本，并能本地用假 release 目录测试核心逻辑。

代码/文件：

- [ ] 新增 `scripts/install.sh`。
- [ ] 支持参数：
  - `--version`
  - `--prefix`
  - `--bin-dir`
  - `--os`
  - `--arch`
  - `--dry-run`
  - `--force`
- [ ] 自动检测 OS：
  - `Linux` -> `linux`
  - `Darwin` -> `darwin`
- [ ] 自动检测 arch：
  - `x86_64`/`amd64` -> `amd64`
  - `aarch64`/`arm64` -> `arm64`
- [ ] 下载对应 tarball 和 `checksums.txt`。
- [ ] 校验 SHA256。
- [ ] 原子替换 `dbf`。
- [ ] 安装 docs/examples 到 `<prefix>/share/debianform`。
- [ ] 安装完成后执行 `dbf version`。

测试：

- [ ] `scripts/install.sh --dry-run --version v0.1.0-beta.1 --os linux --arch amd64`。
- [ ] `scripts/install.sh --dry-run --version v0.1.0-beta.1 --os darwin --arch arm64`。
- [ ] 使用本地 `file://` 或测试 URL 模式验证 checksum 失败会中止。
- [ ] 使用 `/tmp/debianform-install` 验证无需 root 的安装路径。

验收：

```bash
sh scripts/install.sh --dry-run --version v0.1.0-beta.1 --os linux --arch amd64
sh scripts/install.sh --dry-run --version v0.1.0-beta.1 --os darwin --arch arm64
```

需要你介入：

- 确认是否接受 README 里的 `curl | sh` 入口。
- 第一次真实发布前，需要确认 GitHub Release URL 和 artifact 命名稳定。

## Loop 4: Homebrew tap 初始仓库

目标：建立 Homebrew tap 仓库，并能手工安装一个测试版本。

代码/文件：

- [ ] 创建仓库 `mofelee/homebrew-debianform`。
- [ ] 新增 `Formula/dbf.rb`。
- [ ] formula 使用 GitHub Release 四个平台预编译 tarball。
- [ ] formula 安装：
  - `bin.install "dbf"`
  - `pkgshare.install "README.md", "docs", "examples"`
- [ ] formula test 执行 `dbf version`。

验收：

```bash
brew tap mofelee/debianform
brew audit --strict --online dbf
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
dbf version
```

需要你介入：

- 创建 `mofelee/homebrew-debianform` 仓库。
- 给我或 CI 一个可以推送该 tap 仓库的 token。
- 确认 tap 仓库命名。按 Homebrew 规则，`mofelee/homebrew-debianform` 对应
  `brew tap mofelee/debianform`。

## Loop 5: tag 触发 GitHub Release

目标：push `v*` tag 后自动创建 GitHub Release，并上传四个平台 tarball 和 checksum。

代码/文件：

- [ ] 新增 `.github/workflows/release.yml`。
- [ ] workflow 触发条件：

  ```yaml
  on:
    push:
      tags:
        - "v*"
  ```

- [ ] workflow 权限：

  ```yaml
  permissions:
    contents: write
  ```

- [ ] release job 执行：
  - checkout with full history
  - setup-go
  - `go vet ./...`
  - `go test -race -count=1 ./...`
  - `goreleaser release --clean`
- [ ] GoReleaser 自动生成 GitHub Release 和 `checksums.txt`。

验收：

```bash
git tag -a v0.0.0-test.1 -m "test release"
git push origin v0.0.0-test.1
gh release view v0.0.0-test.1
gh release download v0.0.0-test.1 --pattern 'checksums.txt' --dir /tmp/dbf-release-test
```

测试 tag 验证完必须删除：

```bash
gh release delete v0.0.0-test.1 --cleanup-tag
```

需要你介入：

- 确认是否允许用临时 test tag 验证真实 release workflow。
- 如果不允许 test tag，则只能用 snapshot workflow 验证到发布前一步。
- 正式发布时由你手动创建 signed tag。

## Loop 6: 自动更新 Homebrew tap

目标：正式 release 成功后自动更新 `mofelee/homebrew-debianform` 的 `Formula/dbf.rb`。

代码/文件：

- [ ] 在 `.goreleaser.yaml` 中配置 Homebrew formula 发布，或新增 release workflow step
  生成并推送 `Formula/dbf.rb`。
- [ ] 使用二进制 tarball URL 和四个平台 sha256。
- [ ] workflow 使用 `HOMEBREW_TAP_GITHUB_TOKEN`，不使用默认 `GITHUB_TOKEN` 跨仓库推送。
- [ ] tap commit message 包含版本号，例如 `dbf v0.1.0-beta.1`。
- [ ] release workflow 在 Homebrew 更新失败时失败，并保留 GitHub Release。

验收：

```bash
brew update
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

需要你介入：

- 创建 `HOMEBREW_TAP_GITHUB_TOKEN` secret。
- token 需要对 `mofelee/homebrew-debianform` 有 contents write 权限。
- 确认 CI bot 的 commit author 名称和邮箱。

## Loop 7: 发布后自动验证

目标：release 完成后自动验证 curl 和 Homebrew 两条用户路径。

代码/文件：

- [ ] 新增或扩展 release workflow 的 verify job。
- [ ] Linux amd64 验证：
  - `curl` installer 安装指定 tag 到临时 prefix。
  - `dbf version` 包含 tag。
  - `dbf validate -f examples/v2-bbr.dbf.hcl`。
  - `dbf plan -f examples/v2-bbr.dbf.hcl --offline`。
  - Homebrew 安装和 `brew test`。
- [ ] macOS amd64/arm64 验证尽量使用 GitHub hosted runners。
- [ ] Linux arm64 如果暂时没有 runner，在 release notes 中标记为 artifact build verified，
  install path best-effort。

验收：

```bash
gh run view --log
```

release notes 中必须包含验证矩阵：

| Path | linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64 |
| --- | --- | --- | --- | --- |
| Artifact build | yes | yes | yes | yes |
| curl install | yes | manual/best-effort | yes | yes |
| Homebrew install | yes | manual/best-effort | yes | yes |

需要你介入：

- 如果需要真实 Linux arm64 runner，需要你提供 runner 或接受 best-effort 标记。
- 如果 macOS runner 费用或配额有限，需要你确认验证范围。

## Loop 8: 签名和供应链增强

目标：提升发布可信度，但不阻塞第一版 beta 自动化。

代码/文件：

- [ ] 生成 `checksums.txt.sig`。
- [ ] 使用 cosign keyless signing 或 GPG signing。
- [ ] 生成 SBOM。
- [ ] 生成 provenance。
- [ ] README 增加手工校验签名说明。

验收：

```bash
cosign verify-blob --signature checksums.txt.sig checksums.txt
```

或：

```bash
gpg --verify checksums.txt.sig checksums.txt
```

需要你介入：

- 选择 cosign keyless 还是 GPG。
- 如果使用 GPG，需要提供 release signing key 策略。

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

发布自动化推进过程中，需要你处理或确认：

- 选择 license。
- 确认安全报告方式。
- 创建 `mofelee/homebrew-debianform` 仓库。
- 创建 `HOMEBREW_TAP_GITHUB_TOKEN` secret。
- 确认是否允许临时 test tag 验证 release workflow。
- 正式发布时创建 signed tag。
- 确认 Linux arm64 和 macOS 验证范围。
- stable 前选择签名方案。
