# Debian 版本和架构支持策略

本文档定义 DebianForm 对 CLI 运行平台、被管理目标主机 Debian 版本和目标主机架构的支持策略。
它补充 [support matrix](support-matrix.zh.md)，用于判断新平台何时可以从 Preview 进入 Beta。

## 概念区分

DebianForm 有两个不同平台维度：

- CLI 运行平台：运行 `dbf` 的控制机或 CI runner。
- 被管理目标主机：通过 SSH 被 `dbf plan/apply/check` 管理的 Debian 主机。

CLI 可以运行在 Linux/macOS 的 amd64/arm64；目标主机当前只承诺 Debian 系列，且最高优先级是
Debian 13 amd64。

## 当前支持等级

| 范围 | 状态 | 说明 |
| --- | --- | --- |
| CLI Linux amd64 | Beta | release artifact、curl installer 和本地/CI 检查覆盖。 |
| CLI Linux arm64 | Preview | release artifact 已构建；真实 arm64 installer 验证仍需 runner 或机器。 |
| CLI macOS amd64 | Beta | release artifact、curl installer、Homebrew install/test/upgrade 已验证。 |
| CLI macOS arm64 | Beta | release artifact、curl installer、Homebrew install/test/upgrade 已验证。 |
| Target Debian 13 amd64 | Beta | 当前主目标；libvirt integration 使用 Debian 13 cloud image。 |
| Target Debian 13 arm64 | Preview | runtime facts 和 artifact source selection 支持 arm64，但缺少真实目标矩阵。 |
| Target Debian 12 amd64 | Preview | 可能可用，但不是当前 release gate 主路径。 |
| Target Debian 12 arm64 | Preview | 需要 Debian 12 + arm64 双重真实验证后才能提升。 |
| Debian testing/unstable | Unsupported | 不进入 beta 支持承诺。 |
| Ubuntu 或其他非 Debian 系统 | Unsupported | 当前项目定位是 Debian 主机配置。 |

## Debian 版本策略

### Debian 13

Debian 13 是当前最高优先级目标：

- 新功能和集成测试优先覆盖 Debian 13。
- release gate 中的 libvirt integration matrix 默认使用 Debian 13 cloud image。
- quickstart 和真实 beta 验证优先要求 Debian 13 amd64。
- Docker 官方源、APT source、systemd、nftables、networkd 等能力都先按 Debian 13 验证。

### Debian 12

Debian 12 作为 Preview：

- 允许用户试用，但 release notes 必须标记验证不足。
- 不能把 Debian 12 失败直接视为 release blocker，除非同一问题也影响 Debian 13。
- 提升到 Beta 前，需要独立 libvirt case 或真实主机覆盖 validate、apply、no-op plan 和 check。
- 对 Docker、APT repository、nftables、systemd/networkd 这类依赖发行版行为的功能，需要分别验证。

### 更早版本

Debian 11 或更早版本当前不进入支持承诺。主要原因：

- systemd、nftables、APT deb822、Docker 官方源和 cloud image 行为差异更大。
- 维护多个旧版本会放大 integration matrix 和文档成本。
- 当前项目资源应先稳定 Debian 13 主路径。

## 架构策略

### amd64

amd64 是当前目标主机主路径：

- Debian 13 amd64 是 libvirt integration 默认路径。
- release blocker 优先按 Debian 13 amd64 判断。
- component artifact 示例和 Docker 官方源都必须保持 amd64 可用。

### arm64

arm64 当前是 Preview：

- CLI artifact 已覆盖 Linux/macOS arm64。
- `scripts/install.sh` 支持 `--arch arm64` 和自动检测 `aarch64`/`arm64`。
- runtime facts discovery 支持 `aarch64` -> `arm64`。
- component source selection 支持 `source "arm64"`。

限制：

- Linux arm64 curl installer 仍需要真实 arm64 runner 或机器验证。
- Target Debian 13 arm64 缺少真实 apply/check 矩阵。
- Linux Homebrew arm64 验证依赖有 Homebrew 的 Linux arm64 环境。

### 其他架构

`armhf` 等架构可被 facts discovery 识别为字符串，但不代表 release artifact、component source
或目标主机支持承诺。进入 Preview 前至少需要：

- CLI artifact 或明确不需要 CLI artifact 的定位。
- runtime facts normalization。
- component source selection 示例。
- 至少一个真实或 libvirt 目标验证路径。

## 提升支持等级的条件

Preview 提升到 Beta 必须满足：

- 至少一个 release 或 CI workflow 自动生成对应 artifact。
- install path 有自动验证，或 release notes 明确 manual/best-effort 原因。
- 目标主机路径完成 `validate`、online `plan`、`apply`、再次 `plan` no-op 和 `check`。
- 至少一个 drift case 或 failure recovery case 覆盖。
- support matrix、quickstart 或平台文档同步更新。

Beta 降级为 Preview 的条件：

- 连续 release 中自动验证不可用且没有替代人工记录。
- 真实用户反馈显示平台存在高风险缺陷。
- 上游平台或依赖发生变化，导致维护成本或安全风险不可接受。

Unsupported 提升到 Preview 必须先有独立设计或实现记录，不能只在 README 中增加承诺。

## Release Notes 要求

每次 release notes 的 verification matrix 必须区分：

- CLI artifact build。
- curl installer。
- Homebrew install/test/upgrade。
- 被管理目标主机 integration。

如果某个平台无法自动验证，必须写成：

```text
manual/best-effort
```

并说明缺少 runner、缺少真实主机或缺少 Homebrew 环境。

## 后续 Loop

### Loop A: Debian 12 amd64 preview case

目标：增加 Debian 12 amd64 libvirt 或真实主机 case，验证基础配置闭环。

验收：

```bash
dbf validate
dbf plan
dbf apply
dbf plan
dbf check
```

### Loop B: Debian 13 arm64 target case

目标：在真实 arm64 主机或 arm64 libvirt 环境验证目标主机路径。

验收：

```bash
dbf plan
dbf apply
dbf check
```

并确认 `target.system.architecture == "arm64"` 的 component source selection 生效。

### Loop C: Linux arm64 CLI installer verification

目标：在真实 Linux arm64 runner 或机器上验证 curl installer。

验收：

```bash
scripts/install.sh --version <tag> --prefix /tmp/dbf-install-check --force
/tmp/dbf-install-check/bin/dbf version
```

### Loop D: Linux Homebrew strategy

目标：决定 Linux Homebrew 是否继续 best-effort，或引入有 Homebrew 的 Linux runner。

验收：

```bash
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

## 当前状态

- Debian 13 amd64 是唯一 Beta 目标主机主路径。
- Debian 13 arm64、Debian 12 和 Linux arm64 CLI installer 仍是 Preview/best-effort。
- 本文档完成版本和架构支持策略；真实平台提升仍需要后续独立验证 loop。
