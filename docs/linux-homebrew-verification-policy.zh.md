# Linux Homebrew 验证策略

本文档定义 DebianForm 对 Linux Homebrew 安装路径的验证策略。当前 public beta 已支持自建
Homebrew tap，并在 release workflow 中尽力验证；Linux runner 默认不一定安装 Homebrew，
因此 Linux Homebrew 路径目前是 best-effort。

## 当前结论

- macOS amd64 和 macOS arm64 Homebrew install/test/upgrade 是 release verification 主路径。
- Linux amd64 Homebrew 在 runner 存在 `brew` 时自动验证；没有 `brew` 时标记为
  `manual/best-effort`。
- Linux arm64 Homebrew 需要同时具备 Linux arm64 runner 和 Homebrew 环境；当前标记为
  `manual/best-effort`。
- Homebrew formula 仍会包含 Linux amd64 和 Linux arm64 tarball URL/sha256；best-effort
  只表示安装路径验证不足，不表示 artifact 不构建。

## 当前 workflow 行为

release workflow 的 Homebrew 验证步骤遵循：

```bash
if ! command -v brew >/dev/null 2>&1; then
  status=manual/best-effort
  exit 0
fi

brew update
brew tap mofelee/debianform
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
status=yes
```

release verification matrix 必须把 Linux Homebrew 结果写入 release notes：

```text
manual/best-effort
```

除非 workflow 在对应 Linux 环境中实际执行并通过 `brew install`、`brew test` 和
`brew upgrade`。

## 支持状态

| 路径 | 当前状态 | 说明 |
| --- | --- | --- |
| macOS amd64 Homebrew | Beta | GitHub hosted macOS runner 可自动验证。 |
| macOS arm64 Homebrew | Beta | GitHub hosted macOS arm64 runner 可自动验证。 |
| Linux amd64 Homebrew | Preview | Ubuntu runner 默认无 Homebrew；有 brew 时 workflow 会验证。 |
| Linux arm64 Homebrew | Preview | 需要 Linux arm64 runner 和 Homebrew 环境。 |

## 提升到 Beta 的条件

Linux Homebrew 路径要从 Preview 提升到 Beta，必须满足：

- release workflow 使用有 Homebrew 的 Linux runner。
- 每个 release 自动执行：
  - `brew update`
  - `brew tap mofelee/debianform`
  - `brew install mofelee/debianform/dbf`
  - `brew test mofelee/debianform/dbf`
  - `brew upgrade dbf`
- verification matrix 不再写 `manual/best-effort`，而是写 `yes` 或失败并阻断 release。
- support matrix 和 release process 同步更新。

Linux arm64 还需要：

- runner 架构真实为 arm64，或能证明 Homebrew 使用 linux/arm64 bottle/tarball。
- `dbf version` 输出平台与期望 artifact 匹配。

## 不应做的事

- 不应在没有 `brew` 的 runner 上把 Linux Homebrew 标记为通过。
- 不应只因为 formula 生成成功就把 install/test/upgrade 视为已验证。
- 不应把 macOS Homebrew 结果推断为 Linux Homebrew 结果。
- 不应把 Linux amd64 结果推断为 Linux arm64 结果。

## 用户文档口径

在 Linux Homebrew 仍为 Preview 时：

- README 可以保留 Homebrew 安装入口，因为 formula 支持 Linux。
- release notes 必须在 verification matrix 标出 Linux Homebrew 的真实状态。
- support matrix 必须说明 Linux Homebrew 是 best-effort，除非实际 runner 已验证。

## 后续 Loop

### Loop A: Linux amd64 Homebrew runner

目标：在 Linux amd64 runner 上提供 Homebrew，并让 release workflow 自动验证。

验收：

```bash
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

### Loop B: Linux arm64 Homebrew runner

目标：在 Linux arm64 runner 上验证 Homebrew install/test/upgrade。

验收：

```bash
brew install mofelee/debianform/dbf
dbf version
brew test mofelee/debianform/dbf
brew upgrade dbf
```

### Loop C: Release gate 调整

目标：当 Linux Homebrew 自动验证稳定后，把 release workflow 中对应路径从 best-effort 改成
失败即阻断 release。

## 当前状态

- Linux Homebrew strategy 已明确为 best-effort。
- 自动验证能力取决于 runner 是否安装 Homebrew。
- Linux Homebrew install/test/upgrade 本身仍未在当前仓库环境中完成真实验证。
