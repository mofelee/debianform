# DebianForm Quickstart

本文档是一条从零到第一次 apply/check 的最短路径，面向低风险 Debian 13 测试主机。
DebianForm 仍处于 public preview / beta 阶段；不要把第一轮试用目标设为生产主机。

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
DebianForm 当前不支持 sudo、become 或非 root 管理连接；SSH config 中的用户应为 root。

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

只有需要覆盖连接名、端口、identity file 或 state 路径时，才写 `ssh` 或 `state` block。

## 4. 本地校验

`validate` 只解析和校验配置，不连接目标主机：

```bash
dbf validate -f site.dbf.hcl
```

预期输出类似：

```text
v2 configuration is valid: 1 host(s)
```

当前成功信息仍保留历史格式名；看到这个输出即表示当前配置校验通过。

## 5. 本地预览 plan

`--offline` 不连接目标主机，适合先检查资源地址和变更形状：

```bash
dbf plan -f site.dbf.hcl --offline
```

预期会看到 `tcp_bbr` module 和两个 sysctl 资源的 create 计划。

## 6. 在线 plan

在线 plan 会通过 SSH 读取 runtime facts、远端 state 和 observed 状态：

```bash
dbf plan -f site.dbf.hcl
```

第一次运行通常会看到 create/update 计划。此时还不会修改目标主机。

## 7. Apply

确认 plan 符合预期后执行：

```bash
dbf apply -f site.dbf.hcl
```

如果用于 CI 或临时测试环境，可以跳过交互确认：

```bash
dbf apply -f site.dbf.hcl --auto-approve
```

apply 会先重新生成在线 plan，获取目标主机 state lock，然后按资源图顺序执行变更并写入
state。

## 8. 验证 no-op 和 check

apply 成功后，重新运行在线 plan：

```bash
dbf plan -f site.dbf.hcl
```

预期 summary 中 create/update/delete/operations 都为 0。

最后运行 check：

```bash
dbf check -f site.dbf.hcl
```

目标主机与配置一致时，`check` 返回 0；如果有人手动修改了远端状态，`check` 会输出 plan
并返回非零。

## 常见失败

更完整的恢复步骤见 [operations runbook](operations-runbook.zh.md)。

- `ssh: connect ...`：先用普通 `ssh server1` 排查网络、SSH config、密钥和 root 登录权限。
- `offline plan cannot resolve runtime facts`：当前配置依赖远端 facts。改用在线 plan，
  或在 fixture 中显式声明 `system.architecture` / `system.codename`。
- `remote state does not match v2 configuration`：`check` 检测到 drift 或尚未 apply 的
  变更。先读 plan，再决定 apply、修正配置或恢复远端状态。
