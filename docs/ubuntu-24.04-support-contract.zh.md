# Ubuntu 24.04 Preview 支持契约

本文档记录 issue [#43](https://github.com/mofelee/debianform/issues/43) 已交付的实现和支持
边界，供后续 parser、facts、provider、libvirt、文档和 release review 共同遵守。Ubuntu
24.04 LTS amd64 当前为 Preview；当前支持状态同时以[支持矩阵](support-matrix.zh.md)为准。

## 目标和非目标

首个 Ubuntu 被管理目标固定为 Ubuntu 24.04 LTS (`noble`) amd64，初始支持等级为 Preview。
Debian 13 amd64 继续是默认和最高优先级目标，Debian 12/13 的现有支持等级及阻断门禁不变。

本轮继续使用同一个 `dbf` CLI、`*.dbf.hcl` DSL、resource address、plan 格式和远端 state：

- 不创建 UbuntuForm、`ubf`、`*.ubf.hcl` 或第二套 state namespace。
- 继续只允许 root SSH；不增加 sudo、become 或默认 `ubuntu` 用户管理。
- 不管理 Netplan 或 NetworkManager，不生成或读取 Netplan YAML 内容，不调用 `netplan`，也不修改、
  删除或迁移 `/etc/netplan/*`。
- Ubuntu 26.04 LTS amd64 由独立支持契约和门禁覆盖；本契约不把 24.04 的结果外推到 26.04。
- 不支持 Ubuntu 22.04、Ubuntu arm64、桌面、Snap、PPA、Ubuntu Pro 或 cloud-init 生命周期
  管理。
- 不因为发行版名称不同而复制 parser、graph、engine 或整套 provider。

## Platform 字段和优先级

`platform` 包含四个可选 assertion/offline fact 字段：

```hcl
host "server1" {
  platform {
    distribution = "ubuntu"
    version      = "24.04"
    architecture = "amd64"
    codename     = "noble"
  }
}
```

字段语义如下：

| 字段 | 在线事实来源 | Ubuntu 24.04 值 | 语义 |
| --- | --- | --- | --- |
| `distribution` | `/etc/os-release` 的 `ID` | `ubuntu` | 发行版 identity；不能从 codename 推断。 |
| `version` | `/etc/os-release` 的 `VERSION_ID` | `24.04` | 发行版版本 identity。 |
| `architecture` | `dpkg --print-architecture`，失败时归一化 `uname -m` | `amd64` | 目标包和 artifact 架构。 |
| `codename` | `VERSION_CODENAME`，兼容读取 `UBUNTU_CODENAME` | `noble` | repository suite 和用户 assertion。 |

优先级和失败规则：

1. 在线 `plan/apply/check` 以远端探测值为事实来源。
2. 配置中显式字段是 assertion；任一字段与远端不一致时，在 provider observation 和 mutation
   前失败，错误必须包含 host、字段、声明值、探测值和 source path。
3. 未声明字段由在线事实补齐；不能从 codename、命令是否存在或控制机系统推断发行版。
4. `validate` 不连接主机，只检查字段形状和已完整声明 tuple 的已知组合。
5. 普通离线 plan 不强制声明 platform。需要选择发行版实现的资源在 Ubuntu 上必须显式声明
   `distribution`、`version`、`architecture` 和 `codename`。
6. 为保持已有 Debian 配置兼容，离线配置省略 `distribution/version` 时继续走历史 Debian
   repository 语义；该兼容默认绝不能把 `noble` 等 codename 推断成 Ubuntu。

被管理目标 allowlist：

| Target | 允许执行 | 当前/目标等级 |
| --- | --- | --- |
| Debian 13 amd64 | 是 | Beta，主路径 |
| Debian 12 amd64 | 是 | Beta |
| Debian 12/13 arm64 | 是 | Preview，保持现状 |
| Ubuntu 24.04 amd64 | 是 | Preview |
| Ubuntu 26.04 amd64 | 是 | Preview，使用独立 released-image 矩阵和 gate |
| 其他 Debian/Ubuntu tuple | 否 | Unsupported |
| 其他发行版 | 否 | Unsupported |

## 兼容性边界

本轮是兼容性新增：

- DSL 只增加可选字段；现有 `platform.architecture/codename` 继续有效。
- `facts.system` 增加可忽略的 `distribution/version` 字段；state 顶层版本保持 `2`。
- 旧 state 缺少新字段时仍可读取，下一次在线事实探测后自然补齐；不运行 state migration。
- resource address、ownership、desired digest 和 destroy/forget 语义不变。
- plan JSON 保持 `debianform.plan.alpha1`；本轮不删除、重命名或改变现有字段类型。
- 现有 Debian 离线 Docker repository 输出保持兼容；Ubuntu 必须由显式或已验证的发行版 facts
  选择 `linux/ubuntu`，不能使用 codename 猜测。

## Domain 能力分类

“共享”表示优先复用当前 Debian 实现，只有 VM 证据证明行为不同才允许窄分支；“发行版分派”
表示实现必须使用已验证的 `distribution`；“原生 networkd”表示目标必须由管理员预先移除
Netplan ownership。

| Domain/block | Ubuntu 分类 | 约束 |
| --- | --- | --- |
| `variable`、`locals`、`profile`、`component`、`host` | 共享 | parser/eval/merge 语义不变。 |
| `ssh` | 共享 | 仍为 root-only。 |
| `state` | 共享 | 路径、lock、schema 和 ownership 不变。 |
| `system` | 共享并实机验证 | hostname、timezone、locale 仅按失败证据适配。 |
| `platform` | 发行版分派 | 新增 distribution/version facts 和 assertion。 |
| `kernel` | 共享并实机验证 | module/sysctl 保持现有语义。 |
| `packages` | 共享并实机验证 | 继续使用 APT/dpkg；包名差异必须来自 baseline。 |
| `apt` | 共享并实机验证 | deb822、source file、keyring 和 refresh 语义不变。 |
| `files`、`secrets`、`directories` | 共享 | 路径、权限、redaction 和删除语义不变。 |
| `groups`、`users` | 共享并实机验证 | UID/GID、home、shell 和 membership 语义不变。 |
| `systemd.unit`、`systemd.service_unit`、`systemd.timer` | 共享并实机验证 | 继续使用 systemd。 |
| `systemd.resolved`、`systemd.journald` | 共享并实机验证 | 默认 service 状态差异只能窄适配。 |
| `systemd.networkd` | 原生 networkd | active Netplan ownership 时必须在变更前拒绝。 |
| `services` | 共享 | 继续使用 systemctl。 |
| `nftables` | 共享并实机验证 | validate/activate/rollback 语义不变。 |
| `docker` | 部分发行版分派 | official repository/key/package conflict 按 distro；daemon/users/Compose 共享。 |
| `assert` | 共享 | `self.platform`/`target.platform` 包含四个 platform 字段。 |

## Resource/provider 能力分类

| Resource/provider | Ubuntu 分类 | 约束 |
| --- | --- | --- |
| `system_hostname`、`system_timezone`、`system_locale` | 共享并实机验证 | 不改变地址和 observed 结构。 |
| `kernel_module`、`sysctl` | 共享并实机验证 | 不因 distro 名称分叉。 |
| `package` | 共享并实机验证 | APT/dpkg；仅对已证实包名增加映射。 |
| `apt_signing_key`、`file`、`directory` | 共享 | 保留 ownership/redaction。 |
| `group`、`user`、`user_group_membership` | 共享 | 保留 identity 和删除语义。 |
| `systemd_unit`、`service` | 共享 | networkd service 受 ownership preflight 约束。 |
| `nftables_file` | 共享并实机验证 | 继续使用 nft 校验和激活。 |
| `component_artifact` | 共享并实机验证 | architecture selection 使用 platform facts。 |
| `docker_package_conflicts` | 发行版分派 | Ubuntu 冲突包必须来自 baseline。 |
| `docker_compose_project` | 共享并实机验证 | package 就绪后保持 Compose 语义。 |
| `operation` | 共享 | APT refresh、daemon-reload、restart 等 identity 不变。 |

## Network ownership

普通 Ubuntu 主机可以继续由 Netplan 维护现有网络，只要 DebianForm 配置没有声明结构化
`systemd.networkd` 资源，也没有管理 `/etc/systemd/network` 下的文件。

当 Ubuntu 配置将要管理上述资源时，在线 preflight 必须只读检测 `/etc/netplan/*.yaml` ownership：

- 存在 active Netplan ownership 时，在写文件、enable/reload service 或其他 provider mutation
  前失败。
- 诊断列出 host、冲突来源和受影响的 DebianForm declaration。
- 不提供自动接管或 override；管理员必须在 DebianForm 之外准备 native-networkd 主机。
- arbitrary script 的命令文本不参与推断；这部分始终由配置作者负责。

## 20-case Ubuntu 矩阵

以下 20 个 cases 已作为独立 Ubuntu 阻断矩阵运行。`native-networkd image` 表示测试 harness
在 DebianForm 运行前完成外部准备，不是 DebianForm 自动迁移。

| Case | Ubuntu fixture | 分类/实现 Loop |
| --- | --- | --- |
| `apt-source` | official image | APT shared，#47 |
| `apt-virtual-package` | official image | package shared，#47 |
| `bbr` | official image | kernel shared，#48 |
| `component-inputs` | official image | component shared，#48 |
| `docker-compose` | official image | distro package + shared Compose，#47/#48 |
| `docker-daemon` | official image | distro package + shared daemon，#47/#48 |
| `docker-engine` | official image | Ubuntu official Docker repo，#47 |
| `files` | official image | shared，#48 |
| `hostname` | official image | system shared，#48 |
| `multi-directory` | official image | parser/files shared，#48 |
| `nftables` | official image | nftables shared，#48 |
| `script-on-change` | official image | operation shared，#48 |
| `shadowsocks-rust` | official image | artifact/systemd shared，#48 |
| `shared-script-networkd` | native-networkd image | ownership guard + networkd，#49 |
| `source-build` | official image | build/package shared，#47/#48 |
| `system-settings` | official image | hostname/timezone/locale，#48 |
| `systemd-extensions` | official image | timer/resolved/journald，#48 |
| `systemd-service-unit` | official image | systemd shared，#48 |
| `wireguard` | native-networkd image | ownership guard + two-host networkd，#49 |
| `wireguard-three-host` | native-networkd image | ownership guard + three-host networkd，#49 |

## 交付证据

Ubuntu Preview 的权威实现基线是提交
[`49c741e848bbbcc7c95f1969bf8952d5cc425e7d`](https://github.com/mofelee/debianform/commit/49c741e848bbbcc7c95f1969bf8952d5cc425e7d)。
[CI run 29334736664](https://github.com/mofelee/debianform/actions/runs/29334736664) 在同一提交上证明：

- Ubuntu 24.04 LTS amd64：`20/20`。
- Debian 12 amd64：`20/20`。
- Debian 13 amd64：`20/20`。
- `Ubuntu 24.04 target matrix gate` 和 `Managed target matrix gate` 均成功。
- 连同 unit job 共 `63/63`。

这是 24.04 首次交付时的历史基线。当前四目标门禁以[支持矩阵](support-matrix.zh.md)记录的提交
`0211ab2c98d674182dc91a9af7bd887dc91e5539` 和 CI run `29418825778` 为准，其中 24.04 与
26.04 分别使用独立的 `20/20` 矩阵和 gate。

Preview 表示成熟度，不允许合并已知 Ubuntu 回归，也不等同于 Beta。未来提升 Beta 至少要求
持续阻断 CI、release verification 证据、已解决高风险用户反馈，以及显式 support-tier 决策。
