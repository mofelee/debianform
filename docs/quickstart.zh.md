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

准备一台低风险 Debian 13 amd64 主机，并确认控制机可以用 root SSH key 登录。
DebianForm 当前不支持 sudo、become 或非 root 管理连接。

```bash
export DBF_HOST=192.0.2.10
ssh root@"$DBF_HOST" 'cat /etc/debian_version && uname -m'
```

如果 SSH 需要指定私钥，先确认普通 `ssh` 可以工作，再把同一个私钥写入配置中的
`ssh.identity_file`。

## 3. 写第一份配置

新建 `site.dbf.hcl`：

```hcl
host "server1" {
  ssh {
    host = "192.0.2.10"
    user = "root"
    # identity_file = "~/.ssh/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform/state/server1.json"
    lock_path = "/var/lock/debianform/state/server1.lock"
  }

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

把 `ssh.host` 改成目标主机地址。`state.path` 和 `state.lock_path` 是目标主机上的
DebianForm state 与 lock 文件路径。

## 4. 本地校验

`validate` 只解析和校验配置，不连接目标主机：

```bash
dbf validate -f site.dbf.hcl
```

预期输出类似：

```text
v2 configuration is valid: 1 host(s)
```

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

- `ssh: connect ...`：先用普通 `ssh root@host` 排查网络、密钥和 root 登录权限。
- `offline plan cannot resolve runtime facts`：当前配置依赖远端 facts。改用在线 plan，
  或在 fixture 中显式声明 `system.architecture` / `system.codename`。
- `remote state does not match v2 configuration`：`check` 检测到 drift 或尚未 apply 的
  变更。先读 plan，再决定 apply、修正配置或恢复远端状态。
