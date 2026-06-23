# DebianForm 支持矩阵

本文档把 DebianForm 当前可承诺的 CLI 平台、目标主机、v2 DSL block、domain/resource 类型和
验证覆盖放在一处。项目仍处于 public preview / beta 阶段；这里的“支持”表示当前仓库中存在实现、
文档和自动化验证入口，不等同于 stable/GA 兼容性承诺。
DSL、CLI、state schema 和 plan JSON 的具体兼容规则见
[compatibility policy](compatibility-policy.zh.md)。
root-only SSH、权限边界、secret 处理和漏洞响应见
[security model](security-model.zh.md)。
Debian 版本和架构进入 Beta/Preview 的条件见
[platform support strategy](platform-support-strategy.zh.md)。

## 状态定义

| 状态 | 含义 |
| --- | --- |
| Beta | 当前主路径，已有实现、测试或集成用例；stable 前仍可能发生破坏性变更。 |
| Preview | 已实现但验证覆盖或用户反馈较少；真实生产使用前需要额外小范围验证。 |
| Compat | 为兼容旧配置保留；新配置不推荐继续扩展。 |
| Design-only | 文档或 fixture 表达方向，当前不作为可运行能力承诺。 |
| Unsupported | 当前版本明确不支持。 |

## CLI 和目标主机

| 项目 | 当前状态 | 说明 |
| --- | --- | --- |
| CLI on Linux amd64 | Beta | release tarball、curl installer 和本地/CI 检查覆盖。 |
| CLI on Linux arm64 | Preview | release artifact 已构建；真实 arm64 curl installer 仍需人工或 runner 验证。 |
| CLI on macOS amd64 | Beta | release artifact、curl installer 和 Homebrew install/test/upgrade 已验证。 |
| CLI on macOS arm64 | Beta | release artifact、curl installer 和 Homebrew install/test/upgrade 已验证。 |
| Target Debian 13 amd64 | Beta | 最高优先级目标；libvirt integration cases 使用 Debian 13 cloud VM。 |
| Target Debian 13 arm64 | Preview | runtime facts 和架构选择支持 arm64，但缺少真实 arm64 目标主机矩阵。 |
| Target Debian 12 或更早版本 | Preview | 部分 DSL 可能可用，但不是当前最高优先级验证目标。 |
| 非 Debian 目标系统 | Unsupported | 当前项目定位是 Debian 主机配置。 |
| root SSH 管理连接 | Beta | `ssh.user` 只能省略或为 `"root"`。 |
| sudo/become/非 root 管理连接 | Unsupported | 当前不支持 sudo 提权、become 或非 root 管理连接。 |

## CLI 命令

| 命令 | 当前状态 | 验证范围 |
| --- | --- | --- |
| `dbf validate` | Beta | parser、merge、HostSpec、runnable examples 和 CLI 单测覆盖。 |
| `dbf plan` | Beta | offline/online plan、text/json/html renderer、debug provider address 覆盖。 |
| `dbf apply` | Beta | state lock、apply state 写入、DAG 调度和 libvirt integration cases 覆盖。 |
| `dbf check` | Beta | 在线 drift 检测和非零退出路径覆盖。 |
| `dbf fmt` | Beta | HCL formatter 原地格式化。 |
| `dbf component inspect` | Beta | component input API JSON 输出覆盖。 |
| `dbf variable inspect` | Beta | variable API JSON 输出覆盖。 |
| `dbf version` / `--version` | Beta | release metadata 和 CLI 单测覆盖。 |

## v2 顶层 DSL

| Block | 当前状态 | 说明 |
| --- | --- | --- |
| `variable` | Beta | 支持 type/default/description/nullable/sensitive/ephemeral/const/deprecated 和 validation。 |
| `locals` | Beta | 支持本地表达式和值复用。 |
| `profile` | Beta | 支持 imports、domain block 和 assert；用于 host 配置复用。 |
| `component` | Beta | 支持 binary/file/archive/ca_certificate/source artifact，以及 typed input 和 validation。 |
| `host` | Beta | 当前唯一主线入口；承载 SSH、state、system 和各领域 block。 |

## Host Domain Blocks

| Domain/block | 当前状态 | 主要能力 | 主要验证 |
| --- | --- | --- | --- |
| `ssh` | Beta | root SSH host/port/user/identity_file。 | CLI online plan/apply/check。 |
| `state` | Beta | 每 host state path、lock path、atomic write、stale lock 接管。 | state、engine、SSH backend 单测。 |
| `system` | Beta | hostname、architecture、codename、timezone、locale；online facts 校验。 | merge/facts 单测。 |
| `kernel` | Beta | kernel module、sysctl 持久化和运行时应用。 | BBR example 和 libvirt `bbr` case。 |
| `packages` | Beta | package install，含 repository dependency。 | graph/plan 和 integration cases。 |
| `apt` | Beta | deb822 repository、source file、signing key、APT refresh operation。 | `apt-source` integration case。 |
| `files` | Beta | content/source、write-only content_version、sensitive redaction、ownership/mode。 | files integration、secret redaction matrix。 |
| `secrets` | Compat | `secrets.file` 作为旧写法兼容层。 | deprecation warning、secret redaction tests。 |
| `directories` | Beta | directory ownership/mode/ensure。 | graph/engine coverage。 |
| `groups` | Beta | group gid/system/ensure。 | user/group example 和 tests。 |
| `users` | Beta | uid/home/shell/primary group/supplementary groups/authorized keys。 | user/group example 和 tests。 |
| `systemd.unit` | Beta | 原始 unit 文件管理。 | systemd examples 和 integration case。 |
| `systemd.service_unit` | Beta | 结构化 `.service` 生成。 | `systemd-service-unit` integration case。 |
| `systemd.networkd` | Preview | netdev/network、WireGuard peer、networkd enable。 | WireGuard examples 和 two-host integration case。 |
| `services` | Beta | systemd service enabled/state，支持 running/stopped/restarted/reloaded。 | service tests 和 integration cases。 |
| `nftables` | Beta | `/etc/nftables.conf`、snippet file、validate/activate。 | `nftables` integration case。 |
| `docker` | Beta | Docker Engine、daemon、users、Compose project。 | Docker graph/plan/apply tests 和 integration cases。 |
| `assert` | Beta | host/profile 合并后断言，失败阻止 validate/plan/apply。 | parser/merge 单测。 |

## Resource / Provider 类型

| 类型 | 当前状态 | 来源 DSL | 说明 |
| --- | --- | --- | --- |
| `kernel_module` | Beta | `kernel.modules` | 加载并持久化 kernel module。 |
| `sysctl` | Beta | `kernel.sysctl` | 写 sysctl 配置并应用运行时值。 |
| `package` | Beta | `packages`、`docker.package` | APT package 安装。 |
| `apt_signing_key` | Beta | `apt.repository.signing_key`、Docker official repo | 管理 keyring 文件。 |
| `file` | Beta | `apt.source_file`、`files.file`、Docker daemon/Compose | 管理普通文件和敏感摘要。 |
| `directory` | Beta | `directories.directory`、Docker Compose | 管理目录。 |
| `group` | Beta | `groups.group`、`docker.users` | 管理 group。 |
| `user` | Beta | `users.user` | 管理 user。 |
| `user_group_membership` | Beta | `docker.users` | 将已有或声明用户加入 supplementary group。 |
| `systemd_unit` | Beta | `systemd.unit`、`systemd.service_unit`、Docker Compose | 管理 systemd unit 文件。 |
| `service` | Beta | `services.service`、Docker service/Compose service | 管理 systemd enabled/state。 |
| `nftables_file` | Beta | `nftables.file` | 管理 nftables 文件并触发 validate/activate。 |
| `component_artifact` | Beta | `component.source` | 下载或准备 binary/file/archive/ca_certificate/source artifact。 |
| `docker_package_conflicts` | Beta | `docker.package.source = "official"` | 检测并按策略移除冲突包。 |
| `docker_compose_project` | Beta | `docker.compose` | 收敛 Compose project running/stopped/absent。 |
| `operation` | Beta | graph operations | APT refresh、systemd daemon-reload、service restart、Compose config 等命令型步骤。 |

## Docker DSL

| 能力 | 当前状态 | 说明 |
| --- | --- | --- |
| `docker { enable = true }` | Beta | 默认使用 Docker 官方 APT 源和官方 packages。 |
| `package.source = "official"` | Beta | 默认值；安装 `docker-ce`、CLI、containerd、buildx、compose plugin。 |
| `package.source = "debian"` | Beta | 使用 Debian 仓库中的 `docker.io` 和 `docker-compose-plugin`。 |
| `package.source = "none"` | Beta | 不安装 Docker package，但仍可管理 daemon/service/Compose。 |
| `package.source = "custom"` | Beta | 用户自行声明 repo/key/package；Docker block 不生成 package 节点。 |
| `package.channel = "stable"` | Beta | 当前唯一实现 channel。 |
| `package.version` | Unsupported | 版本 pinning 尚未实现。 |
| `remove_conflicts = "auto"/true/false` | Beta | official source 下检测 Docker 冲突包。 |
| `daemon.settings` | Beta | 生成 `/etc/docker/daemon.json`，变化后 restart Docker。 |
| `docker.users` | Beta | 创建/复用 `docker` group，并添加 supplementary membership。 |
| `compose "<name>"` | Beta | 管理 Compose project、文件、env file、systemd unit/service。 |
| 多个 `compose file` block | Unsupported | 当前每个 Compose project 只支持一个主 `file` block。 |
| 多个 `env_file` block | Beta | 支持按 label 管理多个 env file。 |
| HCL 自动生成 Compose YAML | Design-only | 后续语法糖，不属于当前主路径。 |
| Registry login / rootless Docker / Swarm / Kubernetes / Podman backend | Unsupported | 不属于当前 MVP。 |

## Component 和变量

| 能力 | 当前状态 | 说明 |
| --- | --- | --- |
| Component artifact `binary` | Beta | 架构选择、URL sha256、install path。 |
| Component artifact `file` | Beta | 下载或读取本地文件并安装。 |
| Component artifact `archive` | Beta | extract/build/install 路径。 |
| Component artifact `ca_certificate` | Beta | 安装 CA 并触发 `update-ca-certificates`。 |
| Component artifact `source` | Beta | source build workflow。 |
| Component input type system | Beta | primitive、list/map/set/object/tuple/optional。 |
| Component input validation | Beta | 当前 input 自校验和受限函数集合。 |
| Sensitive input propagation | Beta | 派生 file/unit content 在 plan/state 中脱敏。 |
| Top-level variable | Beta | CLI var、var file、auto var file、env var、validation。 |
| Ephemeral variable | Beta | 不写入 state；结构性字段限制。 |
| `secrets.file` | Compat | 继续可用但会 warning；新配置优先 `variable + files.file`。 |

## 示例和集成验证

| 示例或 case | 当前状态 | 覆盖内容 |
| --- | --- | --- |
| `examples/v2-bbr.dbf.hcl` | Beta | kernel module、sysctl、assert。 |
| `examples/v2-apt-source-file.dbf.hcl` | Beta | APT source file。 |
| `examples/v2-apt-repository.dbf.hcl` | Beta | APT repository 和 signing key。 |
| `examples/v2-bird2.dbf.hcl` | Beta | component 展开和 service/file/package。 |
| `examples/v2-component-binary.dbf.hcl` | Beta | binary artifact，真实 apply 前需替换 sha256。 |
| `examples/v2-component-source-build.dbf.hcl` | Beta | source build component。 |
| `examples/v2-component-inputs.dbf.hcl` | Beta | typed input、validation、sensitive。 |
| `examples/v2-docker-*.dbf.hcl` | Beta | Docker minimal、daemon、Compose、users、package source。 |
| `examples/v2-nftables.dbf.hcl` | Beta | nftables validate/activate。 |
| `examples/v2-realistic-systemd-app.dbf.hcl` | Beta | 低权限 systemd app 部署模板，覆盖 user/group、目录、文件、unit 和 service。 |
| `examples/v2-systemd-service*.dbf.hcl` | Beta | raw unit 和 structured service_unit。 |
| `examples/v2-user-group.dbf.hcl` | Beta | users/groups/directories/files。 |
| `examples/v2-variable-secret-file.dbf.hcl` | Beta | variable + sensitive file 写入。 |
| `examples/v2-wireguard-networkd.dbf.hcl` | Preview | WireGuard networkd，需准备本地 secrets。 |
| `examples/v2-systemd-networkd-wireguard.dbf.hcl` | Preview | systemd-networkd 原生写法，需准备本地 secrets。 |
| `examples/v2-fleet.dbf.hcl` | Design-only | fleet 方向 fixture，不作为当前可运行样例。 |
| `test/integration/libvirt/cases/*` | Beta | Debian 13 VM 上 validate/apply/check/drift/remove/restore 覆盖。 |

## 当前不支持或尚未承诺

- 真实 beta 用户反馈仍在收集中；反馈入口和 triage 流程见
  [beta feedback and triage](beta-feedback-triage.zh.md)。
- Stable/GA 级别兼容性政策已经成文；仍需多个 release 验证执行效果。
- `.deb` 包和 apt repository 发布渠道尚未实现；可行性评估见
  [apt repository feasibility](apt-repository-feasibility.zh.md)。
- sudo/become/非 root 管理连接。
- 非 Debian 目标系统。
- 完整私有 registry 生命周期管理。
- rootless Docker、Swarm、Kubernetes、Podman backend。
- 单个 Docker container/image/network/volume 的低阶资源 DSL。
- HCL 对象完整生成 Compose YAML。
