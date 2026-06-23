# DebianForm 发布流程

本文档规定 DebianForm 的公开发布、安装和升级流程。目标是让用户可以在 Linux 和
macOS 上用 `curl` 或 Homebrew 安装 `dbf`，并支持 `amd64` 和 `arm64`。

自动化落地步骤见 [release automation plan](release-automation-plan.zh.md)。
日常发布操作见 [release quick runbook](release-quick-runbook.zh.md)。

## 支持矩阵

完整用户能力、DSL block、resource/domain 类型和验证覆盖见
[support matrix](support-matrix.zh.md)。本节只列 release artifact 平台。

DebianForm 的 CLI 可以运行在控制机或 CI runner 上，通过 SSH 管理目标 Debian 主机。
CLI 运行平台和被管理目标系统是两个不同概念。

CLI 发布产物覆盖：

| OS | Architecture | Artifact |
| --- | --- | --- |
| Linux | amd64 | `dbf_<tag>_linux_amd64.tar.gz` |
| Linux | arm64 | `dbf_<tag>_linux_arm64.tar.gz` |
| macOS | amd64 | `dbf_<tag>_darwin_amd64.tar.gz` |
| macOS | arm64 | `dbf_<tag>_darwin_arm64.tar.gz` |

目标主机支持优先级仍以 Debian 13 为第一目标。目标主机架构优先级由 v2 runtime facts
和集成测试覆盖决定，不等同于 CLI 运行平台支持矩阵。

## 版本策略

- 公开 beta 使用 semver prerelease，例如 `v0.1.0-beta.1`、`v0.1.0-beta.2`。
- stable 后使用 `v0.1.0`、`v0.1.1`、`v0.2.0`。
- tag 必须以 `v` 开头，并且必须来自 CI 全绿的 commit。
- 破坏性 DSL、state 或 plan JSON 变更只能进入 minor 版本，并必须写入 release notes。
- beta 阶段允许破坏性变更，但 release notes 必须明确迁移影响。

## Release Artifacts

每个 GitHub Release 必须包含：

- 四个平台的 tarball。
- `checksums.txt`，包含所有 tarball 的 SHA256。
- release notes，说明新增、修复、兼容性和迁移事项。

`<tag>` 使用完整 git tag，例如 `v0.1.0-beta.1`。

每个 tarball 包含：

- `dbf`
- `README.md`
- `docs/`
- `examples/`
- `LICENSE`
- `CHANGELOG.md`

建议后续增加：

- `checksums.txt.sig` 或 cosign 签名。
- `.deb` 包。
- Homebrew bottle。

## 构建要求

所有 release build 必须使用同一份 version metadata：

```bash
VERSION=v0.1.0-beta.1
COMMIT="$(git rev-parse --short=12 HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

Go build 需要注入：

```bash
-X github.com/mofelee/debianform/internal/version.Version=$VERSION
-X github.com/mofelee/debianform/internal/version.Commit=$COMMIT
-X github.com/mofelee/debianform/internal/version.Date=$BUILD_DATE
```

构建矩阵：

```bash
GOOS=linux  GOARCH=amd64
GOOS=linux  GOARCH=arm64
GOOS=darwin GOARCH=amd64
GOOS=darwin GOARCH=arm64
```

构建出的二进制必须满足：

```bash
dbf version
dbf --version
```

其中 `dbf version` 应显示 version、commit、built、go 和 platform。

## Release Gate

打 tag 前必须通过：

```bash
go vet ./...
go test -race -count=1 ./...
make build
make test-integration-layout
make test-integration
```

发布 commit 必须满足：

- `LICENSE` 存在。
- `CHANGELOG.md` 存在并包含当前版本条目。
- README 包含安装、升级和支持矩阵入口。
- GitHub Actions 在目标 commit 上全绿。
- 至少一个真实或 libvirt Debian 13 flow 完成 `validate`、`apply`、再次 `plan`
  no-op 和 `check`。

## GitHub Release 流程

1. 更新 `CHANGELOG.md`。
2. 确认 `README.md` 和 `docs/` 已同步。
3. 确认 CI 全绿。
4. 创建 tag：

   ```bash
   git tag -s v0.1.0-beta.1
   git push origin v0.1.0-beta.1
   ```

5. Release workflow 构建四个平台 tarball。
6. Release workflow 生成 `checksums.txt`。
7. Release workflow 创建 GitHub Release。
8. Release workflow 更新 Homebrew tap。
9. 发布后用全新环境验证 curl 和 Homebrew 安装。

如果暂时没有 signing key，可以用 annotated tag 代替 signed tag，但 stable 前必须切到 signed
tag。

## curl 安装

用户入口：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh -s -- --version v0.1.0-beta.1
```

推荐支持的 installer 参数：

| Option | Default | Description |
| --- | --- | --- |
| `--version` | latest release | 安装指定版本。 |
| `--prefix` | auto | 安装前缀。root 或可写 `/usr/local` 时使用 `/usr/local`，否则使用 `$HOME/.local`。 |
| `--bin-dir` | `<prefix>/bin` | 只覆盖二进制安装目录。 |
| `--os` | auto | 覆盖 OS 检测，用于测试。值为 `linux` 或 `darwin`。 |
| `--arch` | auto | 覆盖架构检测，用于测试。值为 `amd64` 或 `arm64`。 |
| `--dry-run` | false | 只打印将执行的下载和安装动作。 |
| `--force` | false | 即使目标版本已安装也重新安装。 |

installer 行为：

- 通过 `uname -s` 检测 `linux` 或 `darwin`。
- 通过 `uname -m` 把 `x86_64`/`amd64` 映射为 `amd64`，把 `aarch64`/`arm64`
  映射为 `arm64`。
- 从 GitHub Release 下载对应 tarball 和 `checksums.txt`。
- 使用 SHA256 校验 tarball。
- 解压到临时目录。
- 先安装为临时文件，再原子替换目标 `dbf`。
- 安装 `README.md`、`docs/` 和 `examples/` 到 `<prefix>/share/debianform`。
- 安装完成后运行 `dbf version`。

升级方式就是重新运行 installer：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
```

回滚方式是指定旧版本重新安装：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh -s -- --version v0.1.0-beta.1
```

## Homebrew

第一阶段使用自建 tap，不进入 `homebrew/core`：

```bash
brew install mofelee/debianform/dbf
```

也可以手动 tap：

```bash
brew tap mofelee/debianform
brew install dbf
```

tap 仓库：

```text
github.com/mofelee/homebrew-debianform
```

formula 路径：

```text
Formula/dbf.rb
```

第一阶段 formula 直接安装 GitHub Release 中的预编译 tarball。这样用户不需要本地安装 Go，
也不需要等待源码构建；这是自建 tap 的推荐体验。进入 `homebrew/core` 前再评估源码构建和
bottle 流程。

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
    pkgshare.install "README.md", "docs", "examples"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/dbf version")
  end
end
```

Homebrew 升级：

```bash
brew update
brew upgrade dbf
```

release workflow 更新 tap 时必须：

- 修改 `Formula/dbf.rb` 的 `version`、四个平台 `url` 和四个 `sha256`。
- 运行 `brew audit --strict --online dbf`。
- 运行 `brew install mofelee/debianform/dbf`。
- 运行 `brew test mofelee/debianform/dbf`。
- 提交并推送 tap 更新。

第二阶段再决定是否增加 source formula 和 bottles。自建 tap 使用 upstream 二进制 tarball
已经可以覆盖 macOS/Linux 和 amd64/arm64；如果未来要进入 `homebrew/core`，需要按
Homebrew core 的规则重新评估源码构建、依赖下载和 bottle。

## 用户安装文档

README 应把用户入口压缩为两个主路径：

```bash
# Homebrew
brew install mofelee/debianform/dbf

# curl
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
```

并说明：

- `dbf` 安装在控制机或 CI runner 上。
- 被管理目标主机需要 SSH 可达，并且当前最高优先级目标是 Debian 13。
- `dbf version` 用于确认安装成功。
- `brew upgrade dbf` 或重新运行 installer 用于升级。

## 发布后验证

每次发布完成后，在干净环境中验证：

```bash
dbf version
dbf validate -f examples/v2-bbr.dbf.hcl
dbf plan -f examples/v2-bbr.dbf.hcl --offline
```

curl 验证矩阵：

| Platform | Required |
| --- | --- |
| Linux amd64 | yes |
| Linux arm64 | yes |
| macOS amd64 | yes |
| macOS arm64 | yes |

Homebrew 验证矩阵：

| Platform | Required |
| --- | --- |
| Linux amd64 | yes |
| Linux arm64 | best effort until CI runner exists |
| macOS amd64 | yes |
| macOS arm64 | yes |

如果某个平台无法在 CI 中自动验证，release notes 必须标记为手工验证或暂未验证。

## 失败处理

如果 GitHub Release 已发布但 Homebrew tap 更新失败：

- 不删除已发布 release。
- 修复 tap 后重新提交 `Formula/dbf.rb`。
- 在 release notes 中记录 Homebrew 可用时间或已知问题。

如果 tarball checksum 错误：

- 立即把 GitHub Release 标记为 prerelease/draft 或撤下有问题 artifact。
- 不复用同一个 tag 重新发布不同内容。
- 创建新 patch/prerelease tag，例如 `v0.1.0-beta.2`。

如果发现发布版本有严重 bug：

- 发布新的修复版本。
- Homebrew tap 指向新版本。
- release notes 标记受影响版本。

## 后续路线

P0：

- GitHub Release 四平台 tarball。
- `checksums.txt`。
- `scripts/install.sh`。
- Homebrew tap binary formula。
- README 安装和升级说明。

P1：

- 自动更新 Homebrew tap。
- Release artifact 签名。
- `.deb` 包。
- Homebrew source formula 和 bottles 可行性评估。

P2：

- apt repository。
- 进入 `homebrew/core` 的可行性评估。
- state/schema migration policy。

发布自动化的具体 loop、验收命令和人工介入点见
[release automation plan](release-automation-plan.zh.md)。
