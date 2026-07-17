<p align="right">
  <a href="quickstart.md">English</a> | <strong>简体中文</strong>
</p>

# DebianForm Quickstart

本文档是一条从零到第一次 apply/check 的最短路径，面向最高优先级的低风险 Debian 13
amd64 测试主机。Debian 12 amd64 也属于 Beta 支持范围；DebianForm 仍处于 public preview /
beta 阶段，不要把第一轮试用目标设为生产主机。
Ubuntu 24.04 和 26.04 LTS amd64 的独立 Preview 路径分别见
[Ubuntu 24.04 Preview Quickstart](ubuntu-24.04-quickstart.zh.md)与
[Ubuntu 26.04 Preview Quickstart](ubuntu-26.04-quickstart.zh.md)。

## 1. 安装 CLI

macOS 或 Linux 上可以使用 Homebrew：

```bash
brew install mofelee/debianform/dbf
dbf version
```

也可以使用安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
dbf version
```

## 2. 准备目标主机

准备一台低风险 Debian 13 amd64 主机，并在控制机的 `~/.ssh/config` 里给它一个稳定名字。
针对 Debian 12 bookworm amd64 的纯本地 platform assertion/offline smoke，见
[`examples/debian12-amd64.dbf.hcl`](../examples/debian12-amd64.dbf.hcl)。

这里必须使用 root：DebianForm 需要安装包、写 `/etc`、管理 systemd，并在
`/var/lib/debianform` 和 `/var/lock/debianform` 写 state/lock。当前不支持 sudo、become
或非 root 管理连接；SSH config 中的用户应为 root。

```sshconfig
Host server1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
```

先确认普通 `ssh` 可以工作。DebianForm 默认把 `host "server1"` 当作 `ssh server1`
使用，因此连接细节应该优先放在 SSH config 中：

```bash
ssh server1 'cat /etc/debian_version && uname -m'
```

## 3. 写第一份配置

创建一个配置目录并进入目录。这个目录里先只放一份 `site.dbf.hcl`：

```bash
mkdir debianform-demo
cd debianform-demo
```

新建 `site.dbf.hcl`：

```hcl
host "server1" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires the tcp_bbr kernel module."
  }
}
```

这份配置没有写 `ssh` 和 `state`：

- `host "server1"` 默认通过 `ssh server1` 连接。
- state 默认写到目标主机的 `/var/lib/debianform/state/server1.json`。
- lock 默认写到目标主机的 `/var/lock/debianform/state/server1.lock`。

它也不需要写 `platform`：在线 `plan` / `apply` / `check` 会从真实主机探测 distribution、
version、architecture 和 codename。只有离线预览依赖这些 facts，或像 Debian 12 smoke 那样
专门断言平台时才显式声明。Ubuntu 的发行版分派离线 plan 需要完整四元组。

只有需要覆盖连接名、端口、identity file 或 state 路径时，才写 `ssh` 或 `state` block。

下面的命令都不写 `-f`。不加 `-f` 时，`dbf` 会读取当前工作目录下所有 `*.dbf.hcl`
文件，并按文件名排序后合并处理。当前目录只有 `site.dbf.hcl`，所以命令可以保持简洁。

## 4. 本地校验

`validate` 只解析和校验配置，不连接目标主机：

```bash
dbf validate
```

预期输出类似：

```text
configuration is valid: 1 host(s)
```

当前成功信息仍保留历史格式名；看到这个输出即表示当前配置校验通过。

## 5. 本地预览 plan

`--offline` 不连接目标主机，适合先检查资源地址和变更形状：

```bash
dbf plan --offline
```

预期会看到 `tcp_bbr` module 和两个 sysctl 资源的 create 计划。

## 6. 在线 plan

在线 plan 会通过 SSH 读取 runtime facts、远端 state 和 observed 状态：

```bash
dbf plan
```

第一次运行通常会看到 create/update 计划。此时还不会修改目标主机。

## 7. Apply

确认 plan 符合预期后执行：

```bash
dbf apply
```

如果用于 CI 或临时测试环境，可以跳过交互确认：

```bash
dbf apply --auto-approve
```

apply 会先生成未持锁的在线 preview，获取目标主机 state lock 后重新读取 state/observed 状态，再打印
实际执行计划。交互模式下，实际计划发生变化会再次要求确认；获准后才按资源图顺序执行并写入 state。

## 8. 验证 no-op 和 check

apply 成功后，重新运行在线 plan：

```bash
dbf plan
```

预期 summary 中 create/update/delete/operations 都为 0。

最后运行 check：

```bash
dbf check
```

目标主机与配置一致时，`check` 返回 0；如果有人手动修改了远端状态，`check` 会输出 plan
并返回非零。

## 常见失败

更完整的恢复步骤见 [operations runbook](operations-runbook.zh.md)。

- `ssh: connect ...`：先用普通 `ssh server1` 排查网络、SSH config、密钥和 root 登录权限。
- `offline plan cannot resolve runtime facts`：当前配置依赖远端 facts。改用在线 plan，
  或在 fixture 中显式声明所需的 `platform.distribution` / `platform.version` /
  `platform.architecture` / `platform.codename`。
- `remote state does not match configuration`：`check` 检测到 drift 或尚未 apply 的
  变更。先读 plan，再决定 apply、修正配置或恢复远端状态。
