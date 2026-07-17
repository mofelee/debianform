# 发布说明模板

<p align="right"><a href="release-notes-template.md">English</a> | <strong>简体中文</strong></p>

发布 DebianForm 版本前，将此模板复制到 GitHub Release 正文中。即使答案是 `无`，也要保留
每个一级章节。发布后验证作业完成后，发布工作流会追加或替换最后的
`Verification Matrix` 章节。

```markdown
## 摘要

- <用一至三条列表项说明此版本面向用户的目的。>

## 兼容性

- 发布阶段：<公开测试版 | 稳定版 | 补丁版>。
- 支持的 CLI 平台：<linux/amd64、linux/arm64、darwin/amd64、darwin/arm64>。
- 主要受管目标：Debian 13 amd64。
- 其他 Beta 受管目标：Debian 12 amd64。
- Preview 受管目标：Ubuntu 24.04 LTS amd64、Ubuntu 26.04 LTS amd64、
  Debian 12 arm64 和 Debian 13 arm64。
- DSL/state/plan JSON 兼容性：<兼容 | 破坏性 | 仅限测试版>。

## 破坏性变更

- <无，或为每项破坏性的 CLI、DSL、state、plan JSON、provider 或发布产物变更写一条列表项。
  包括旧行为、新行为和受影响的用户。>

## 迁移说明

- <无，或用户升级前后必须执行的确切步骤。适用时包括配置编辑、state 处理、命令变更和
  回滚说明。>

## 新增

- <面向用户的新能力。>

## 变更

- <非破坏性行为变更，包括默认值和输出结构。>

## 修复

- <缺陷修复。>

## 安全

- <安全修复、依赖漏洞说明、secret 处理变更，或“此版本没有安全修复”。>

## 已知问题

- <已知回归、测试版限制、不支持的平台、尽力而为的验证路径或外部服务依赖。>

## 支持矩阵

- CLI 产物：<列出构建产物或提供产物链接>。
- 受管目标：Debian 13 amd64（主要 Beta）、Debian 12 amd64（Beta）、
  Ubuntu 24.04/26.04 LTS amd64（Preview）、Debian 12/13 arm64（Preview）；其他
  Ubuntu 组合、桌面环境、Debian 11 及更早版本为 Unsupported。
- Ubuntu 网络边界：DebianForm 不管理或迁移 Netplan；networkd 要求目标由操作者预先配置为
  原生 networkd。
- 安装路径：<curl/Homebrew/.deb/apt 支持状态>。
- 功能支持情况：见[支持矩阵](https://github.com/mofelee/debianform/blob/main/docs/support-matrix.zh.md)。

## 验证

- 提交：`<完整 commit sha>`。
- 本地检查：
  - `go vet ./...`：<通过 | 失败 | 跳过并说明原因>
  - `go test -race -count=1 ./...`：<通过 | 失败 | 跳过并说明原因>
  - `make build`：<通过 | 失败 | 跳过并说明原因>
  - `make vulncheck`：<通过 | 失败 | 跳过并说明原因>
  - `make test-integration-layout`：<通过 | 失败 | 跳过并说明原因>
- 此提交的受管目标 CI 证据：
  - Ubuntu 24.04 LTS amd64 libvirt 矩阵（20/20）：<通过 | 失败；CI 运行 URL>
  - Ubuntu 26.04 LTS amd64 libvirt 矩阵（20/20）：<通过 | 失败；CI 运行 URL>
  - Debian 12 amd64 libvirt 矩阵（20/20）：<通过 | 失败；CI 运行 URL>
  - Debian 13 amd64 libvirt 矩阵（20/20）：<通过 | 失败；CI 运行 URL>
  - `Ubuntu 24.04 target matrix gate`：<通过 | 失败；CI 运行 URL>
  - `Ubuntu 26.04 target matrix gate`：<通过 | 失败；CI 运行 URL>
  - `Managed target matrix gate`：<通过 | 失败；CI 运行 URL>
  - Ubuntu 26.04 已发布镜像 URL 和 SHA-256：<URL；摘要>
- 可选的本地受管目标检查：
  - `make test-integration TARGET=ubuntu-24.04`：<通过 | 失败 | 跳过并说明原因>
  - `make test-integration TARGET=ubuntu-26.04`：<通过 | 失败 | 跳过并说明原因>
  - `make test-integration DEBIAN_VERSION=12`：<通过 | 失败 | 跳过并说明原因>
  - `make test-integration DEBIAN_VERSION=13`：<通过 | 失败 | 跳过并说明原因>
- 发布工作流：<工作流运行 URL>。
- 人工检查：
  - GitHub Release 产物齐全：<通过 | 失败 | 跳过并说明原因>
  - checksum 验证：<通过 | 失败 | 跳过并说明原因>
  - cosign keyless bundle 验证：<通过 | 失败 | 跳过并说明原因>
  - GitHub provenance attestation 验证：<通过 | 失败 | 跳过并说明原因>
  - curl 安装器冒烟测试：<通过 | 失败 | 跳过并说明原因>
  - Homebrew 安装/测试冒烟测试：<通过 | 失败 | 跳过并说明原因>

## Verification Matrix

此章节由发布工作流在发布后验证完成时填充。工作流完成前若手工编辑发布说明，请保留此标题，
或完全省略本章节。
```

## 创建标签前的必要审查

创建发布标签前，确认：

- `破坏性变更` 明确。只有在确认不存在破坏性的 CLI、DSL、state、plan JSON、provider、安装器、
  产物或工作流行为变更后，才使用 `无`。
- `已知问题` 列出测试版限制，以及任何手工或尽力而为的平台验证路径。
- `Verification Matrix` 不存在，或者以完全一致的 `## Verification Matrix` 标题存在；工作流替换
  生成的矩阵时使用此标题。
- `迁移说明` 解释用户必须执行的操作；破坏性版本还须包括 state 处理和回滚指导。
- Ubuntu 24.04、Ubuntu 26.04、Debian 12 和 Debian 13 的受管目标证据分别以 `20/20`
  列出，指向发布提交对应的 CI 运行，且三个聚合 gate 全部通过。记录 26.04 已发布镜像的 URL
  和摘要。
- 除非该版本已通过[项目成熟度检查清单](archive/legacy-design/project-maturity-and-launch-checklist.zh.md)
  中的稳定版 gate，否则发布说明不得使用 stable/GA/production-ready 等措辞。
