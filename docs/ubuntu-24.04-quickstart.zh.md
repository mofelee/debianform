# Ubuntu 24.04 Preview Quickstart

本文档是在 Ubuntu 24.04 LTS (`noble`) amd64 Server 上完成第一次
`validate/plan/apply/check` 的最短路径。该目标当前为 **Preview**，不是 Beta、GA 或生产就绪
承诺；Debian 13 amd64 仍是 DebianForm 的默认和最高优先级目标。

当前 Ubuntu 支持范围不包括 Ubuntu 22.04/26.04、arm64、桌面环境、Snap、PPA、Ubuntu Pro、
cloud-init 生命周期、sudo/become 或默认 `ubuntu` 用户连接。DebianForm 继续使用同一个 `dbf`
CLI、`*.dbf.hcl` DSL 和 state，不存在 UbuntuForm。

## 1. 安装 CLI

本文档以当前 `main` 为准。`v0.6.0` 及更早 release 不包含 Ubuntu Preview；首个 release 的
notes 明确列出 Ubuntu 24.04 LTS amd64 Preview 后，才能直接使用 Homebrew 或安装脚本：

```bash
brew install mofelee/debianform/dbf
dbf version
```

也可以使用安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
dbf version
```

在该 release 发布前，从当前源码构建并把仓库根目录中的 `dbf` 加入本次 shell 的 `PATH`：

```bash
git clone https://github.com/mofelee/debianform.git
cd debianform
make build
export PATH="$PWD:$PATH"
dbf version
```

## 2. 准备目标主机

准备一台全新的低风险 Ubuntu Server 24.04 LTS amd64 VM。当前管理连接必须是 root SSH；
DebianForm 不会以默认 `ubuntu` 用户登录后再 sudo。先在控制机的 `~/.ssh/config` 中配置稳定
别名：

```sshconfig
Host ubuntu24_preview
  HostName 192.0.2.24
  User root
  IdentityFile ~/.ssh/id_ed25519
```

确认 SSH 和目标 tuple：

```bash
ssh ubuntu24_preview 'set -eu; . /etc/os-release; printf "%s %s %s\n" "$ID" "$VERSION_ID" "$VERSION_CODENAME"; dpkg --print-architecture'
```

预期包含 `ubuntu 24.04 noble` 和 `amd64`。其他 tuple 会在在线 facts 校验阶段被拒绝。

## 3. 校验示例和离线 plan

仓库中的 [`examples/ubuntu-24.04-preview.dbf.hcl`](../examples/ubuntu-24.04-preview.dbf.hcl)
显式声明离线 plan 所需的完整 platform tuple：

```hcl
platform {
  distribution = "ubuntu"
  version      = "24.04"
  architecture = "amd64"
  codename     = "noble"
}
```

先在本地校验并预览：

```bash
dbf validate -f examples/ubuntu-24.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-24.04-preview.dbf.hcl --offline
```

`validate` 不连接目标主机。`plan --offline` 不读取远端 facts，因此 Ubuntu 配置只要依赖发行版
分派，就必须显式提供 `distribution`、`version`、`architecture` 和 `codename`；不能从
`noble` 推断发行版。

## 4. 在线 plan、apply、no-op 和 check

确认本地预览只包含 `/etc/debianform-ubuntu-preview.txt` 后执行：

```bash
dbf plan -f examples/ubuntu-24.04-preview.dbf.hcl
dbf apply -f examples/ubuntu-24.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-24.04-preview.dbf.hcl
dbf check -f examples/ubuntu-24.04-preview.dbf.hcl
```

临时测试环境可使用 `dbf apply ... --auto-approve`。第一次在线 plan 应显示一个 file create；
apply 后的第二次 plan 应为 no-op，`check` 应返回 0。在线命令会探测远端四个 platform facts，
并在 provider observation 或 mutation 前拒绝声明与探测不一致的目标。

## 5. Network ownership 边界

这个示例不声明 `systemd.networkd`，也不管理 `/etc/systemd/network/` 下的文件，因此不会触发
网络 ownership preflight。DebianForm 不生成、不读取 Netplan YAML 内容、不修改或删除 Netplan
YAML，也不调用 `netplan`；普通 Ubuntu 主机可以继续由 Netplan 管理现有网络。

如果配置声明结构化 `systemd.networkd` 或管理 `/etc/systemd/network/` 下的 raw file，目标机
必须先由运维人员在 DebianForm 之外准备为稳定、重启后仍可 SSH 的 native-networkd 主机。
DebianForm 只读检查 `/etc/netplan/*.yaml` 是否仍存在 active ownership；发现冲突会在任何网络
写入、service enable 或 reload 前失败。它不会自动迁移、接管或删除 Netplan 配置。

## 6. Preview 边界

- Ubuntu 24.04 LTS amd64 当前有独立的 20-case 阻断矩阵，但 Preview 仍表示用户反馈和 release
  验证积累少于 Beta。
- Docker official source 会根据已验证的 `platform.distribution` 选择
  `https://download.docker.com/linux/ubuntu`；自定义 Debian mirror URL 不会自动改写为 Ubuntu。
- 新平台问题应附上完整 tuple、`dbf version`、最小脱敏配置和 plan/check 输出；不要公开 SSH
  key、token、state 明文或内部主机信息。

完整能力和排除项见 [支持矩阵](support-matrix.zh.md) 与
[Ubuntu 24.04 支持契约](ubuntu-24.04-support-contract.zh.md)。

## 文档验证记录

2026-07-14 使用实现基线
[`49c741e848bbbcc7c95f1969bf8952d5cc425e7d`](https://github.com/mofelee/debianform/commit/49c741e848bbbcc7c95f1969bf8952d5cc425e7d)
和 Ubuntu 官方 `noble-server-cloudimg-amd64.img` 创建全新 VM；image SHA256 为
`ffe6203da54deeb6db5d2a98a83f9ec8e55f149d3f7ba622e1abe5fa966ee3d6`。本页示例的
`validate`、offline plan、online plan、apply、no-op plan 和 check 均通过；最终 summary 为
`0 create, 0 update, 0 delete, 1 no-op, 0 operations`。测试 domain、disk、seed、console log 和
临时 SSH 配置已清理。
