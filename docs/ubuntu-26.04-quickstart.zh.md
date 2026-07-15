# Ubuntu 26.04 Preview Quickstart

本文档是在 Ubuntu 26.04 LTS (`resolute`) amd64 Server 上完成第一次
`validate/plan/apply/check` 的最短路径。该目标当前为 **Preview**，不是 Beta、GA 或生产就绪
承诺；Debian 13 amd64 仍是 DebianForm 的默认和最高优先级目标。

支持范围不包括 Ubuntu 26.04 arm64、桌面环境、NetworkManager 管理、Snap、PPA、Ubuntu Pro、
cloud-init 生命周期、sudo/become、默认 `ubuntu` 用户连接或 Ubuntu 24.04 到 26.04 的原地
升级。DebianForm 继续使用同一个 `dbf` CLI、`*.dbf.hcl` DSL 和 state，不存在 UbuntuForm。

## 1. 安装 CLI

本文档以当前 `main` 为准。包含 Ubuntu 26.04 Preview 的 release notes 发布前，应从当前源码
构建：

```bash
git clone https://github.com/mofelee/debianform.git
cd debianform
make build
export PATH="$PWD:$PATH"
dbf version
```

正式 release 的 notes 明确列出 Ubuntu 26.04 LTS amd64 Preview 后，可以使用 Homebrew：

```bash
brew install mofelee/debianform/dbf
dbf version
```

或使用安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
dbf version
```

## 2. 准备目标主机

准备一台全新的低风险 Ubuntu Server 26.04 LTS amd64 VM。当前管理连接必须是 root SSH；
DebianForm 不会以默认 `ubuntu` 用户登录后再 sudo。在控制机的 `~/.ssh/config` 中配置稳定别名：

```sshconfig
Host ubuntu26_preview
  HostName 192.0.2.26
  User root
  IdentityFile ~/.ssh/id_ed25519
```

确认 SSH 和完整目标 tuple：

```bash
ssh ubuntu26_preview 'set -eu; . /etc/os-release; printf "%s %s %s\n" "$ID" "$VERSION_ID" "${VERSION_CODENAME:-$UBUNTU_CODENAME}"; dpkg --print-architecture'
```

预期包含 `ubuntu 26.04 resolute` 和 `amd64`。其他 tuple 会在 provider observation 或 mutation
前被拒绝。

## 3. 校验示例和离线 plan

仓库中的 [`examples/ubuntu-26.04-preview.dbf.hcl`](../examples/ubuntu-26.04-preview.dbf.hcl)
显式声明离线 plan 所需的完整 platform tuple：

```hcl
platform {
  distribution = "ubuntu"
  version      = "26.04"
  architecture = "amd64"
  codename     = "resolute"
}
```

先在本地校验并预览：

```bash
dbf validate -f examples/ubuntu-26.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-26.04-preview.dbf.hcl --offline
```

`validate` 不连接目标主机。`plan --offline` 不读取远端 facts；Ubuntu 配置只要依赖发行版分派，
就必须显式提供 `distribution`、`version`、`architecture` 和 `codename`，不能从 `resolute`
推断发行版。

## 4. 在线 plan、apply、no-op 和 check

确认本地预览只包含 `/etc/debianform-ubuntu26-preview.txt` 后执行：

```bash
dbf plan -f examples/ubuntu-26.04-preview.dbf.hcl
dbf apply -f examples/ubuntu-26.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-26.04-preview.dbf.hcl
dbf check -f examples/ubuntu-26.04-preview.dbf.hcl
```

临时测试环境可使用 `dbf apply ... --auto-approve`。第一次在线 plan 应显示一个 file create；
apply 后的第二次 plan 应为 no-op，`check` 应返回 0。在线命令会探测远端四个 platform facts，
并在 provider observation 或 mutation 前拒绝声明与探测不一致的目标。

## 5. APT 和 Docker 发行版边界

Ubuntu 26.04 使用 `resolute` archive/security suite。Docker official source 必须由已验证 tuple
选择 `https://download.docker.com/linux/ubuntu` 和 `resolute` suite；DebianForm 不会回退到
Ubuntu 24.04 的 `noble` repository、package 或 fixture。显式自定义 repository URL 也不会被
自动跨发行版改写。

## 6. Network ownership 边界

这个示例不声明 `systemd.networkd`，也不管理 `/etc/systemd/network/` 下的文件，因此不会触发
network ownership preflight。DebianForm 不生成、不读取 Netplan YAML 内容、不修改或删除
Netplan YAML，也不调用 `netplan`；普通 Ubuntu 主机可以继续由 Netplan 管理现有网络。

如果配置声明结构化 `systemd.networkd` 或管理 `/etc/systemd/network/` 下的 raw file，目标机
必须先由运维人员在 DebianForm 之外准备为稳定、重启后仍可 SSH 的 native-networkd 主机。
DebianForm 只读检查 Netplan ownership；发现冲突会在任何网络写入、service enable 或 reload
前失败。它不会自动迁移、接管或删除 Netplan 配置。

## 7. Preview 边界

- Ubuntu 26.04 LTS amd64 有独立的 20-case 阻断矩阵和
  `Ubuntu 26.04 target matrix gate`；Preview 代表成熟度，不表示 CI 回归可以忽略。
- Ubuntu 24.04 LTS amd64 是另一条独立验证的 Preview 路径，不能互相借用矩阵结果。
- Ubuntu 26.04 arm64、桌面/NetworkManager、Netplan 管理、sudo/become、默认 `ubuntu` 用户、
  Snap、PPA、Ubuntu Pro、cloud-init 生命周期和原地升级仍为 Unsupported。
- 新平台问题应附完整 tuple、`dbf version`、最小脱敏配置和 plan/check 输出；不要公开 SSH key、
  token、state 明文或内部主机信息。

完整能力和排除项见[支持矩阵](support-matrix.zh.md)、
[平台支持策略](platform-support-strategy.zh.md)与
[Ubuntu 26.04 支持契约](ubuntu-26.04-support-contract.zh.md)。

## 文档验证记录

2026-07-15 使用实现基线
[`0211ab2c98d674182dc91a9af7bd887dc91e5539`](https://github.com/mofelee/debianform/commit/0211ab2c98d674182dc91a9af7bd887dc91e5539)
构建的 CLI、本次新增示例和 Ubuntu 官方 released image
`ubuntu-26.04-server-cloudimg-amd64.img` 创建全新 VM；image
SHA-256 为 `0826c5005ebc70edcfc4519e5d65eca766782f16426231c4c3e92b811ba8df0b`。目标事实为
`ubuntu/26.04/resolute/amd64`。

本页示例的 `validate`、offline plan、online plan、apply、第二次 online plan 和 check 均通过。
第一次 online plan 为 `1 create`；apply 后的 plan 与 check 均为
`0 create, 0 update, 0 delete, 1 no-op, 0 operations`。`/etc/netplan/50-cloud-init.yaml` 的
SHA-256 在执行前后保持
`e2aabf4fd72d957351ea7150be9d477e3bafb4654906198da9840a58bdba5e50`，未创建 networkd 配置。
验证结束后，测试 domain、disk、seed、console、NVRAM、远端临时目录、临时 SSH config 和
known-hosts artifact 均已清理。

同一实现基线的 [CI run 29418825778](https://github.com/mofelee/debianform/actions/runs/29418825778)
还证明 Debian 12、Debian 13、Ubuntu 24.04 和 Ubuntu 26.04 各 `20/20`，三个 aggregate gates
成功，连同 unit 共 `84/84` jobs。
