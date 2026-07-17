# 变更日志

<p align="right"><a href="CHANGELOG.md">English</a> | <strong>简体中文</strong></p>

DebianForm 的所有重要变更都会记录在此文件中。

公开 beta 版本线开始后，本项目遵循语义化版本控制。

## v0.8.1

- 为所有维护中的 Markdown 文档新增完整英文版本，并在全仓提供双向语言选择器和同语言导航。
- 新增双语文档政策和 `make docs-check` CI 门禁，覆盖 73 对文档、结构一致性、英文文档中的意外中文
  正文和本地链接完整性。
- 在既有双语 README 和 docs 目录之外，将中英文 CHANGELOG 和 SECURITY 一并加入 GoReleaser
  归档、curl 安装、`make install` 和 Homebrew package data。
- 这个文档和打包补丁不改变 CLI、DSL、resource address、state schema version 2、plan format
  `debianform.plan.alpha1` 或受管目标支持语义。

## v0.8.0

- 新增 Ubuntu 26.04 LTS（`resolute`）amd64 Server 作为 Preview 受管目标，并设置独立的阻塞式
  20 case libvirt 矩阵和目标 gate。同一提交继续保持 Debian 12、Debian 13 和 Ubuntu 24.04
  的 20/20 覆盖。
- 验证了官方发布的 Ubuntu 26.04 cloud image、APT 和 Docker `resolute` 仓库、共享 provider、
  对原生 Netplan 冲突的拒绝，以及由操作者预先准备的原生 systemd-networkd 工作流，且没有
  回退到 `noble`。
- 新增 Ubuntu 26.04 Preview 快速开始和可运行的非网络示例。管理仍仅限 root；Ubuntu arm64/桌面版、
  Netplan/NetworkManager 管理、sudo/become 以及 Ubuntu 原地升级仍不受支持。

## v0.7.0

- 新增 Ubuntu 24.04 LTS（`noble`）amd64 作为 Preview 受管目标，并设置独立的阻塞式 20 case
  libvirt 矩阵；Debian 13 仍是主要和默认目标，Debian 12/13 amd64 仍为 Beta。
- 新增显式目标 `platform.distribution` 和 `platform.version` facts、可识别 Ubuntu 的 Docker
  官方仓库选择，以及完整受管目标矩阵中的共享 provider 兼容性。
- 新增只读的 Ubuntu 网络 ownership 预检。DebianForm 不管理或迁移 Netplan；在 DebianForm
  管理 networkd 声明前，原生 systemd-networkd 目标必须由操作者预先准备。
- 新增 Ubuntu Preview 快速开始和可运行示例。此兼容性变更不改变既有 resource address、plan
  format、state schema 或 Debian 离线默认值。
- 修复 component 文件 source 的 graph 排序，确保一个受管 source 文件先于读取它作为输入的另一
  component 应用。
- 将 `golang.org/x/sync` 从 0.14.0 更新到 0.22.0。

## v0.6.0

- 新增顶层 `script` 声明和显式 `global.script.<name>` 引用，使不同 component 拥有的文件可以
  共享一个 host-scoped `mode = "once"` operation。
- 共享 script operation 由解析后的声明和目标 host 标识，合并并去重所有触发资源，在任一触发源
  变化时运行一次，并且不会在 no-op apply 中运行。
- 保留既有 component-local script address 和行为，并为未知引用、不支持的根 script 字段及
  component input 作用域违规新增面向 source 的校验。
- 新增可复用的原始 systemd-networkd 示例和真实 Debian 12/13 libvirt 覆盖。CI 现在以 40 job
  矩阵对每个受管目标的全部 20 个 case 设置 gate。

## v0.5.0

- 新增 Debian 12 amd64 作为 Beta 受管目标。CI 现在以 38 job 阻塞矩阵在 Debian 12 和 Debian 13
  amd64 上运行相同的 19 个 libvirt case；Debian 13 仍是主要和默认目标。
- 破坏性变更：移除 `docker.package.source = "debian"`。使用该值的既有配置现在会在 SSH 或 state
  访问前本地校验失败。省略 `source` 可使用默认 Docker 官方仓库；当 DebianForm 不应安装 Docker
  package 时使用 `"none"` 或 `"custom"`。
- 迁移：从 Debian 的 Docker package 切换到 Docker 官方 package 可能替换已安装的 package 并中断
  daemon。应用前先运行在线 plan，审查停机和 package 变更，并备份 Docker 数据。

## v0.4.0

- 破坏性变更：将 host、profile、component 和 component instance label 限制为合法 HCL identifier。
  使用 FQDN、IP 地址、路径、空白或标点作为 label 的配置必须换成稳定的逻辑 identifier，并把远端地址
  保留在 `ssh.host`。
- 变更 `apply`：preview 获准后获取 state lock 并重新计算执行计划。如果持锁计划发生变化，DebianForm
  会在修改 host 或 state 前打印计划并再次请求批准。
- 变更 `check`：在 state read 和 provider inspect 的整个周期持有每个目标 host 的 state lock，
  防止观察到进行中的 apply。
- 为 graph operation、state record 以及 plan JSON change/operation 新增显式 host 字段，多 host plan
  不再从 resource address 字符串推断 ownership。
- 修复 SSH state locking：使用原子、可续租的 version 2 lease、受 guard 保护的 stale takeover、
  精确 deadline，并显式返回续租或 cleanup 失败。
- 修复 state 校验：在 provider inspect 或 write 前拒绝不支持的 schema、顶层 host 不匹配和 foreign
  resource record。
- 修复 state revision 跟踪：每次成功 backend write 恰好推进并返回一个 committed serial，失败写入
  不改变可见 revision。
- 修复每 host 执行容量获取：unsafe resource 原子预留全部 host slot，不再因部分预留发生死锁。
- 修复 sensitive APT source、APT signing key 和 nftables content，确保派生明文不会出现在 plan、
  state、debug output 或 diagnostics 中；这些字段现在会拒绝不支持的 ephemeral 值。

## v0.3.0

- 新增受管 `system.timezone` 和 `system.locale` resource；在线 plan、apply 和 check 现在会收敛显式
  声明的 host timezone 和系统 `LANG`，同时保持省略的设置不受管理。
- 破坏性变更：移除旧 DSL alias `system.architecture` 和 `system.codename`；使用
  `platform.architecture` 和 `platform.codename` 声明目标 platform facts。
- 破坏性变更：移除旧 expression alias `target.system.architecture`、`target.system.codename`、
  `self.system.architecture` 和 `self.system.codename`；改用 `target.platform.*` 或
  `self.platform.*`。持久化 state facts 仍位于 `facts.system.*`。

## v0.2.0-alpha.3

- 新增交互式 SSH apply 调试器和用于远端诊断的 `debug run` 命令，包括彩色调试器输出和更安全的
  失败调用恢复。
- 允许 `locals` 值引用变量和其他 locals，并为依赖排序与循环提供校验覆盖。
- 新增 component script output 的 drift detection，并更新相关 graph、hostspec、plan 和 text golden
  覆盖。
- 将混合 Docker Compose project 状态报告为 degraded，而不是把部分 healthy/running 的 service 集合
  当作完全健康。
- 改进人类可读的 plan/delete 输出和 SSH 排障提示。
- 更新 checkout、setup-go、cache 和 provenance attestation workflow 所固定的 GitHub Actions 依赖。

## v0.2.0-alpha.2

- 将默认在线 SSH host 并发限制为 4，覆盖 fact discovery、state lock/read、host inspect 和 apply
  阶段，同时保留 `--parallel` 供显式调整。
- 允许后续 host 在较早 host 认证失败后重试初始 SSH/auth 路径，不再全局缓存第一次认证失败。
- 记录新的 apply 并发默认值和使用 1Password/大量 agent identity 时的 SSH 排障指导。

## v0.2.0-alpha.1

- 新增并行 host plan 和批量 host inspect 路径，加快多 host 在线运行。
- 将 `--parallel` 应用于 fact discovery，并在非交互运行中禁用交互式 SSH prompt。
- 新增 SSH ControlMaster multiplexing 和短 control path 目录及 cleanup，提高重复 SSH 命令性能。
- 修复 SSH 执行，保留传入的 `PATH`、串行化初始 auth 设置并更可靠地探测 virtual provider。
- 新增 APT virtual package libvirt 覆盖，并提高 libvirt 重试行为的韧性。

## v0.1.0-beta.8

- 新增带 `on_change` 触发模式的 component `script` operation，使 component 能在受管输入变化时运行
  自定义 hook。
- 新增 graph 和 engine 对 component script operation 的计划与执行支持。
- 新增 script/on_change 示例和实施文档。

## v0.1.0-beta.7

- 新增向可重复 `-f` flag 传递目录的支持，将每个目录展开为其直属 `.dbf.hcl` 文件。
- 新增按目录自动加载变量，使目录展开的配置能够一致使用同目录 `.auto.dbfvars` 文件。
- 新增多目录 libvirt 覆盖，并更新快速开始 demo 录制。

## v0.1.0-beta.6

- 为人类可读的 plan、progress 和 warning 输出新增样式化 badge、更丰富的 ANSI text rendering 和可选
  Unicode status symbol。
- 新增 systemd service 扩展，并扩大 systemd drop-in、environment file、timer、socket、path unit
  和 tmpfiles 的 libvirt 覆盖。
- 更新可运行 fleet 示例和 README 快速开始 demo 产物。

## v0.1.0-beta.5

- 为 `plan`、`apply` 和 `check` 文本输出新增 `--color=auto|always|never`，同时保持 JSON 和 HTML
  输出没有 ANSI escape。
- 为 plan 输出新增 delete behavior diagnostics，使删除类动作可以标识其行为是 forget state、移除受管
  artifact、恢复原始内容、执行 destructive delete，还是具有 external side effect。
- 将内部 v2 package 和示例文件重命名为规范的 `internal/core` 与 `examples/*.dbf.hcl` 布局。
- 新增 core graph plan/apply engine 路径，并为规范布局更新示例、支持矩阵、CLI 文档和用户手册链接。

## v0.1.0-beta.4

- 新增在线 `plan`、`apply` 和 `check` progress logging 到 stderr，使长时间 SSH 操作能够展示 active
  host、phase 和 resource action，同时不改变机器可读 stdout。
- 新增 CLI 和 DSL 文档示例的可运行覆盖，包括更新后的 DSL reference 和支持矩阵措辞。

## v0.1.0-beta.3

- 为可复用 component 新增 file 和 secret path override 支持，包括 WireGuard peer map 示例。
- 当 desired ownership 和 mode 匹配时，允许重复 component instance 安全共享目录。
- 完成 Docker source、package source、daemon config、Compose project 和 user membership loop，并扩大
  libvirt 覆盖。
- 新增兼容性和迁移政策，覆盖 CLI/DSL 兼容性、state schema migration rule 和 plan JSON format
  兼容性。
- 新增安全模型文档，覆盖仅 root SSH、permission boundary、secret handling、state/lock behavior 和
  vulnerability response。
- 新增 `.deb` 与 APT repository 可行性计划。
- 新增 Debian 版本和 architecture 支持策略。
- 新增 Linux Homebrew best-effort 验证政策。

## v0.1.0-beta.2

- 自动把 beta release 标记为 GitHub prerelease。
- 忽略 GoReleaser 的 `dist/` 输出目录，避免 Go VCS metadata 把 release binary 标记为 dirty。

## v0.1.0-beta.1

- DebianForm 版本线的第一个公开 beta / public preview release。
- 包含 `validate`、`fmt`、`plan`、`apply`、`check`、`version`、`component inspect` 和
  `variable inspect` 的 CLI 流程。
- 支持 `host`、`profile`、`component`、`locals` 和 `variable` 模型，包括 profile/host merge、
  component input、validation warning 和 sensitive metadata propagation。
- 提供通过 SSH 的在线 plan/apply/check，包含 runtime facts、observed drift detection、state locking、
  state persistence 和离线 plan preview。
- Debian 13 是最高优先级受管目标系统，当前目标 host 重点为 amd64。
- 发布 Linux 和 macOS 的 amd64/arm64 产物，以及 checksums、cosign keyless checksum bundle、SBOM
  和 GitHub provenance attestation。
- 提供 Homebrew 和 curl installer 路径，包括 version pinning、checksum verification、dry-run、
  自定义安装目录和发布后验证 job。
- 包含 BBR、APT source、file、nftables、systemd、user/group、component input、source build、
  shadowsocks-rust 和 WireGuard/networkd pattern 的可运行示例。

已知 beta 限制：

- 这不是 stable/GA release。CLI、DSL、state shape 和 plan JSON 在 stable 前仍可能变化。
- Debian 13 是主要受管目标。其他 Debian 版本和非 Debian 目标不在 beta 支持承诺内。
- 受管目标 host 当前优先支持 amd64。会构建 Linux arm64 release artifact，但在增加真实 arm64 runner
  或 host 前，Linux arm64 installer 验证仍是 best-effort。
- 当 runner 不提供 Homebrew 时，Linux Homebrew 验证为 best-effort。
- 此 beta 不包含 `.deb` package 和 apt repository。
- Stable 级 compatibility policy、state migration policy 和 operations recovery 文档仍待完成。
