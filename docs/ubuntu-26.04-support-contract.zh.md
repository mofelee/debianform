# Ubuntu 26.04 Preview 实施契约

本文档锁定 issue [#53](https://github.com/mofelee/debianform/issues/53) 的实现边界，供
platform、provider、libvirt、CI、文档和 release review 共同遵守。它是实施合同，不是当前
支持声明：在 [#60](https://github.com/mofelee/debianform/issues/60) 的完整矩阵和
[#61](https://github.com/mofelee/debianform/issues/61) 的发布证据完成前，Ubuntu 26.04 仍按
[支持矩阵](support-matrix.zh.md)标记为 Unsupported。

## 目标和非目标

新增被管理目标固定为 Ubuntu 26.04 LTS (`resolute`) amd64 Server，初始支持等级为 Preview。
本轮扩展已交付的 Ubuntu 24.04 路径，不创建第二套 Ubuntu 实现：

- 继续使用同一个 `dbf` CLI、`*.dbf.hcl` DSL、resource address、plan 格式和远端 state。
- Debian 13 amd64 继续是默认和最高优先级目标；Debian 12/13 和 Ubuntu 24.04 的现有门禁
  名称、覆盖范围和阻断语义不变。
- 继续只允许 root SSH；不增加 sudo、become 或默认 `ubuntu` 用户管理。
- 不管理 Netplan 或 NetworkManager，不生成、读取、修改、删除或迁移 Netplan YAML，也不
  调用 `netplan`。
- 不支持 Ubuntu 26.04 arm64、桌面、Snap、PPA、Ubuntu Pro、cloud-init 生命周期或
  Ubuntu 24.04 到 26.04 的原地升级。
- 不因为 Ubuntu 版本不同而复制 parser、graph、engine 或整套 provider。

## Platform tuple 和失败规则

Ubuntu 26.04 唯一允许的 tuple 是：

```hcl
host "server1" {
  platform {
    distribution = "ubuntu"
    version      = "26.04"
    architecture = "amd64"
    codename     = "resolute"
  }
}
```

| 字段 | 在线事实来源 | 目标值 | 语义 |
| --- | --- | --- | --- |
| `distribution` | `/etc/os-release` 的 `ID` | `ubuntu` | 发行版 identity，不能从 codename 推断。 |
| `version` | `/etc/os-release` 的 `VERSION_ID` | `26.04` | Ubuntu release identity。 |
| `architecture` | `dpkg --print-architecture`，失败时归一化 `uname -m` | `amd64` | 包和 artifact 架构。 |
| `codename` | `VERSION_CODENAME`，兼容读取 `UBUNTU_CODENAME` | `resolute` | repository suite 和 assertion。 |

失败规则：

1. 在线 `plan/apply/check` 以远端探测值为事实来源，显式字段只作为 assertion。
2. `26.04+noble`、`24.04+resolute`、非 amd64 或未知 Ubuntu 版本都必须在 provider observation
   和 mutation 前失败。
3. 失败诊断必须包含 host、字段、声明值、探测值和 source path。
4. `validate` 不连接主机，只检查字段形状和完整 tuple 的 allowlist。
5. 需要发行版分派的离线 plan 必须声明完整四元组；不能从 codename、命令存在性或控制机
   系统推断 Ubuntu 版本。
6. 不允许用 `noble` repository、suite、包或 fixture 静默替代 `resolute`。

## Released image 和 repository 合同

VM 证据必须使用 Ubuntu 官方 released image：

- endpoint：`https://cloud-images.ubuntu.com/releases/26.04/release/`
- image：`ubuntu-26.04-server-cloudimg-amd64.img`
- checksum：同目录 `SHA256SUMS`；每次执行解析并记录实际 digest
- 禁止用 `resolute/current` daily image 作为支持或发布证据

Ubuntu archive 和第三方 repository 的规则：

- baseline 以 Ubuntu 官方 archive/security suite 为事实来源，不能把第三方镜像同步延迟算成
  DebianForm 缺陷。
- Docker 必须实际验证 `https://download.docker.com/linux/ubuntu/dists/resolute/` 的 key、
  suite、architecture、package 和 conflict removal；仅 HTTP endpoint 存在不等于兼容。
- 如果 upstream 未提供可安装的 `resolute` 内容，对应 domain 保持 Unsupported 并阻断完整
  Preview 声明；不得回退到 `noble`。

## 兼容性分类

按[兼容性政策](compatibility-policy.zh.md)，本轮目标是兼容性新增：

- 不新增或修改 DSL 字段；只扩展已有 platform allowlist 的合法 tuple。
- state 顶层版本保持 `2`，不运行 state migration。
- plan JSON 保持 `debianform.plan.alpha1`。
- resource address、ownership、desired digest、destroy/forget 和 lock 语义不变。
- Ubuntu 24.04、Debian 12/13 的合法配置和 repository 输出必须保持兼容。
- 新增的 Ubuntu 版本分支只允许由完整、已验证的 platform tuple 选择。

## Domain 差异矩阵

“共享回归”表示复用当前实现并在 26.04 VM 上重新验证；“版本证据”表示只有 baseline 证明存在
差异时才允许窄分支；“原生 networkd”表示目标必须由管理员在 DebianForm 之外预先移除
Netplan ownership。

| Domain/block | Ubuntu 26.04 分类 | 证据和约束 |
| --- | --- | --- |
| `variable`、`locals`、`profile`、`component`、`host` | 共享回归 | parser/eval/merge 不变；由全量 unit/golden 覆盖。 |
| `ssh` | 共享回归 | root-only；不新增 sudo/become。 |
| `state` | 共享回归 | schema、path、lock、ownership 和 redaction 不变。 |
| `platform` | allowlist 扩展 | 精确接受 `ubuntu/26.04/resolute/amd64`。 |
| `system` | 共享回归 + 版本证据 | hostname、timezone、locale 只按 VM 失败适配。 |
| `kernel` | 共享回归 + 版本证据 | module/sysctl 保持现有语义。 |
| `packages` | 共享回归 + 版本证据 | APT/dpkg；包名差异必须链接 baseline。 |
| `apt` | 共享回归 + 版本证据 | deb822、keyring、refresh、drift 和删除完整验证。 |
| `files`、`secrets`、`directories` | 共享回归 | 路径、权限、ownership 和 redaction 不变。 |
| `groups`、`users` | 共享回归 + 版本证据 | UID/GID、home、shell 和 membership 完整验证。 |
| `systemd.unit`、`systemd.service_unit`、`systemd.timer` | 共享回归 + 版本证据 | unit、enable、state、timer 触发和删除完整验证。 |
| `systemd.resolved`、`systemd.journald` | 共享回归 + 版本证据 | service/path 差异只按 baseline 窄适配。 |
| `systemd.networkd` | 原生 networkd | active Netplan ownership 时必须在 mutation 前拒绝。 |
| `services` | 共享回归 | systemctl 路径不变；networkd 受 ownership preflight 约束。 |
| `nftables` | 共享回归 + 版本证据 | validate、activate、rollback、drift 和删除。 |
| `docker` | distro 共享 + 版本证据 | `linux/ubuntu` + `resolute`；daemon/users/Compose 复用。 |
| `assert` | 共享回归 | `self.platform`/`target.platform` 四字段保持不变。 |

## Resource/provider 差异矩阵

| Resource/provider | Ubuntu 26.04 分类 | 证据和约束 |
| --- | --- | --- |
| `system_hostname`、`system_timezone`、`system_locale` | 共享回归 + 版本证据 | 不改变 address 和 observed 结构。 |
| `kernel_module`、`sysctl` | 共享回归 + 版本证据 | 不按版本预先分叉。 |
| `package` | 共享回归 + 版本证据 | install/remove/virtual package/conflict 来自 baseline。 |
| `apt_signing_key`、APT source `file` | 共享回归 + 版本证据 | create/update/drift/delete、refresh、redaction。 |
| 普通 `file`、`directory` | 共享回归 | ownership、mode、sensitive 和删除不变。 |
| `group`、`user`、`user_group_membership` | 共享回归 | identity、home、membership 和删除不变。 |
| `systemd_unit`、`service` | 共享回归 + 版本证据 | networkd service 受 preflight 约束。 |
| `nftables_file` | 共享回归 + 版本证据 | 校验、激活、rollback 和删除。 |
| `component_artifact` | 共享回归 + 版本证据 | binary/file/archive/CA/source 和 amd64 selection。 |
| `docker_repository` | distro 共享 + 版本证据 | 必须生成 `linux/ubuntu` + `resolute`，无跨版本回退。 |
| `docker_package_conflicts` | 版本证据 | 只从 26.04 baseline 增加包名。 |
| `docker_daemon`、`docker_compose_project` | 共享回归 + 版本证据 | package 就绪后验证 no-op、drift 和删除。 |
| `operation` | 共享回归 | APT refresh、daemon-reload、restart 和 on-change identity 不变。 |

## Network ownership

普通 Ubuntu 26.04 主机可以继续由 Netplan 维护网络，只要 DebianForm 配置不声明结构化
`systemd.networkd` 资源，也不管理 `/etc/systemd/network` 下的文件。非网络操作前后
`/etc/netplan/*` 必须保持不变。

将要管理 networkd 时：

- 只读 preflight 必须覆盖 26.04 的 Netplan 配置、生成文件、renderer 和 service 状态。
- active ownership 在写文件、enable/reload service 或其他 mutation 前失败。
- 诊断列出 host、ownership 证据、受影响 declaration 和外部准备要求。
- 不提供自动接管、override 或 Netplan YAML；测试 harness 的 native-networkd 准备不是产品能力。
- arbitrary script 内容不参与 ownership 推断，由配置作者负责。

## 20-case 26.04 baseline 和验收映射

“released image”表示保留镜像原有 Netplan ownership；“native-networkd image”表示 harness 在
DebianForm 运行前完成外部准备。

| Case | Fixture | 预期分类 | 证据 Loop |
| --- | --- | --- | --- |
| `apt-source` | released image | APT shared + resolute suite | #56/#57 |
| `apt-virtual-package` | released image | package/virtual package shared | #56/#57 |
| `bbr` | released image | kernel shared | #56/#58 |
| `component-inputs` | released image | component shared | #56/#58 |
| `docker-compose` | released image | package + shared Compose | #56/#57/#58 |
| `docker-daemon` | released image | package + shared daemon | #56/#57/#58 |
| `docker-engine` | released image | Docker `resolute` repository | #56/#57 |
| `files` | released image | file/directory/redaction shared | #56/#58 |
| `hostname` | released image | system hostname shared | #56/#58 |
| `multi-directory` | released image | merge/files shared | #56/#58 |
| `nftables` | released image | nftables shared | #56/#58 |
| `script-on-change` | released image | operation shared | #56/#58 |
| `shadowsocks-rust` | released image | artifact/systemd shared | #56/#58 |
| `shared-script-networkd` | native-networkd image | ownership + reload dedup | #56/#59 |
| `source-build` | released image | source/package/CA shared | #56/#57/#58 |
| `system-settings` | released image | hostname/timezone/locale | #56/#58 |
| `systemd-extensions` | released image | timer/resolved/journald | #56/#58 |
| `systemd-service-unit` | released image | structured systemd shared | #56/#58 |
| `wireguard` | native-networkd image | two-host networkd | #56/#59 |
| `wireguard-three-host` | native-networkd image | three-host networkd | #56/#59 |

每个 case 必须覆盖其已配置的 `validate`、target fact assertion、online JSON plan、drift
rejection、`apply`、JSON no-op plan、`check`、case assertion 和 cleanup。#56 先记录首个失败阶段和
归属，不在 baseline commit 中顺手修 provider；#57-#59 只修有证据的差异。

## CI 和支持声明 gate

- 新增独立命名的 20-case Ubuntu 26.04 matrix 和 `Ubuntu 26.04 target matrix gate`。
- 不重命名或弱化现有 `Managed target matrix gate` 与 `Ubuntu 24.04 target matrix gate`。
- 支持声明要求同一提交上 Debian 12、Debian 13、Ubuntu 24.04、Ubuntu 26.04 各 20 个 case，
  即 80 个 target-case 结果全部绿色。
- 记录 exact commit、CI run URL、released image digest 和成功/失败后的 hypervisor cleanup。
- Preview 代表成熟度，不代表允许合并已知回归；声明完成后 26.04 gate 是 release blocker。

## Preview 提升和关闭条件

完成一次绿色矩阵只允许发布 Preview。提升到 Beta 仍需持续阻断 CI、released image 更新后的
重复验证、release 验证、无未解决高风险反馈和显式支持等级决策。

关闭 #53 前，#54-#61 必须逐项提供当前远端证据；不能用 Ubuntu 24.04 的成功结果推导
26.04，也不能用 HTTP endpoint 存在替代真实 package、apply、no-op、check 和 drift 证据。
