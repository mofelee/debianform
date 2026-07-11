# Debian 版本和架构支持策略

本文档定义 DebianForm 对 CLI 运行平台、被管理目标主机 Debian 版本和目标主机架构的支持策略。
它补充 [support matrix](support-matrix.zh.md)，用于判断新平台何时可以从 Preview 进入 Beta。

## 概念区分

DebianForm 有两个不同平台维度：

- CLI 运行平台：运行 `dbf` 的控制机或 CI runner。
- 被管理目标主机：通过 SSH 被 `dbf plan/apply/check` 管理的 Debian 主机。

CLI 可以运行在 Linux/macOS 的 amd64/arm64；目标主机当前只承诺 Debian 系列。Debian 13
amd64 是最高优先级目标，Debian 12 amd64 也进入 Beta 和阻断式 release gate。

## 当前支持等级

| 范围 | 状态 | 说明 |
| --- | --- | --- |
| CLI Linux amd64 | Beta | release artifact、curl installer 和本地/CI 检查覆盖。 |
| CLI Linux arm64 | Preview | release artifact 已构建；真实 arm64 installer 验证仍需 runner 或机器。 |
| CLI macOS amd64 | Beta | release artifact、curl installer、Homebrew install/test/upgrade 已验证。 |
| CLI macOS arm64 | Beta | release artifact、curl installer、Homebrew install/test/upgrade 已验证。 |
| Target Debian 13 amd64 | Beta | 当前主目标；19 个 libvirt cases 全部阻断合并和发布。 |
| Target Debian 13 arm64 | Preview | runtime facts 和 artifact source selection 支持 arm64，但缺少真实目标矩阵。 |
| Target Debian 12 amd64 | Beta | 与 Debian 13 运行相同的 19 个阻断式 libvirt cases。 |
| Target Debian 12 arm64 | Preview | 需要 Debian 12 + arm64 真实 apply/check 矩阵后才能提升。 |
| Debian 11 或更早版本 | Unsupported | 不进入当前支持承诺或 release gate。 |
| Debian testing/unstable | Unsupported | 不进入 beta 支持承诺。 |
| Ubuntu 或其他非 Debian 系统 | Unsupported | 当前项目定位是 Debian 主机配置。 |

## Debian 版本策略

### Debian 13

Debian 13 是当前最高优先级目标：

- 新功能和集成测试优先覆盖 Debian 13。
- 本地 `make test-integration` 默认使用 Debian 13 cloud image。
- CI 和 release gate 同时要求 Debian 12/13 amd64 的全部 case 通过。
- quickstart 和真实 beta 验证优先要求 Debian 13 amd64。
- Docker 官方源、APT source、systemd、nftables、networkd 等能力都先按 Debian 13 验证。

### Debian 12

Debian 12 amd64 作为 Beta：

- 运行与 Debian 13 amd64 完全相同的 19 个 libvirt cases，任何失败都是 release blocker。
- 每个 case 都断言 Debian ID、版本、`bookworm` codename 和 `amd64` architecture。
- 每个配置步骤覆盖 `validate`、online JSON plan、可用时的 drift rejection、`apply`、JSON
  no-op plan 和 `check`，并执行 case-specific assertions。
- Docker 官方源、APT repository、nftables、systemd/networkd 等发行版相关能力必须保持
  Bookworm 和 Trixie 双版本通过。

Debian 12 arm64 仍为 Preview，不能从 amd64 的验证结果推导出 arm64 Beta 承诺。

### 更早版本

Debian 11 或更早版本当前不进入支持承诺。主要原因：

- systemd、nftables、APT deb822、Docker 官方源和 cloud image 行为差异更大。
- 维护多个旧版本会放大 integration matrix 和文档成本。
- 当前项目资源优先维护 Debian 12/13 amd64 双版本矩阵和 Debian 13 主路径。

## 架构策略

### amd64

amd64 是当前目标主机主路径：

- Debian 13 amd64 是本地 libvirt integration 默认路径和新功能最高优先级目标。
- Debian 12 amd64 和 Debian 13 amd64 都是 release blocker。
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
- Debian 12 amd64 被管理目标主机 integration。
- Debian 13 amd64 被管理目标主机 integration。
- managed-target aggregate gate。

Debian 12 和 Debian 13 的证据不能合并成一句“libvirt passed”。当前 19-case 矩阵必须分别记录
`19/19` 和 CI run URL，并确认两者来自 release commit；aggregate gate 也必须单独记录结果。

如果某个平台无法自动验证，必须写成：

```text
manual/best-effort
```

并说明缺少 runner、缺少真实主机或缺少 Homebrew 环境。

## 后续 Loop

### Loop A: Debian 13 arm64 target case

目标：在真实 arm64 主机或 arm64 libvirt 环境验证目标主机路径。

验收：

```bash
dbf plan
dbf apply
dbf check
```

并确认 `target.platform.architecture == "arm64"` 的 component source selection 生效。

### Loop B: Linux arm64 CLI installer verification

目标：在真实 Linux arm64 runner 或机器上验证 curl installer。

验收：

```bash
scripts/install.sh --version <tag> --prefix /tmp/dbf-install-check --force
/tmp/dbf-install-check/bin/dbf version
```

### Loop C: Linux Homebrew strategy

目标：决定 Linux Homebrew 是否继续 best-effort，或引入有 Homebrew 的 Linux runner。

验收：

```bash
brew install mofelee/debianform/dbf
brew test mofelee/debianform/dbf
brew upgrade dbf
```

## 当前状态

- Debian 13 amd64 是最高优先级目标，Debian 12 amd64 同为 Beta 和 release blocker。
- CI 当前按 2 个版本 x 19 个 cases 运行 38 个 amd64 libvirt jobs。
- Debian 12 arm64、Debian 13 arm64 和 Linux arm64 CLI installer 仍是 Preview/best-effort。
- Debian 11 及更早版本为 Unsupported。
