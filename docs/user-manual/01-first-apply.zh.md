# 01. 准备测试主机并完成第一次 apply/check

本章完成 DebianForm 的最小闭环：连接一台 Debian 测试主机，写一个文件，执行 `validate`、
`plan`、`apply` 和 `check`。

本章示例已在 Debian 13 amd64 测试主机上验证通过。

## 前提

准备一台可以丢弃的 Debian 13 amd64 主机。DebianForm 当前需要 root SSH，因为它会写 `/etc`、
管理 systemd、安装包，并在远端写 state/lock。

先在控制机的 `~/.ssh/config` 中配置一个稳定别名。本手册统一使用 `manual1`：

```sshconfig
Host manual1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
```

如果测试机在跳板机后面，可以加 `ProxyJump`：

```sshconfig
Host manual1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump jump-host
```

确认 SSH 能工作：

```bash
ssh manual1 'cat /etc/debian_version && dpkg --print-architecture && id -u'
```

预期输出类似：

```text
13.5
amd64
0
```

`id -u` 必须是 `0`。

## 创建工作目录

每一章都建议使用独立目录。这样可以直接复制本章命令，不会和其他章节的文件混在一起。

```bash
mkdir -p debianform-manual/01-first-apply
cd debianform-manual/01-first-apply
```

## 写第一份配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/01-state.json"
    lock_path = "/var/lock/debianform/manual/01-state.lock"
  }

  files {
    file "/etc/debianform-manual/hello.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "hello from DebianForm\n"
    }
  }
}
```

这份配置做三件事：

- 管理 host `manual1`。
- 为本章使用独立 state 文件，避免和其他章节互相影响。
- 在远端写 `/etc/debianform-manual/hello.txt`。

`files.file` 会自动创建父目录，所以不需要额外声明 `/etc/debianform-manual`。

## 本地校验

先运行：

```bash
dbf validate
```

预期：

```text
v2 configuration is valid: 1 host(s)
```

`validate` 只解析和校验配置，不连接主机。

## 离线 plan

运行：

```bash
dbf plan --offline
```

你应该看到 1 个 create：

```text
Summary: 1 create, 0 update, 0 delete, 0 no-op, 0 operations
```

离线 plan 不连接主机，也不读取远端 state。它适合先看资源地址和大致变更形状。

## 在线 plan

运行：

```bash
dbf plan
```

第一次运行时，远端文件还不存在，所以仍然是 1 个 create：

```text
Summary: 1 create, 0 update, 0 delete, 0 no-op, 0 operations
```

在线 plan 会通过 SSH 读取主机 facts、远端 state 和实际文件状态。因此你会在 diff 中看到类似
`exists: false`、`sha256: ""` 的 observed 信息。

## 执行 apply

本手册为了方便复制，使用 `--auto-approve` 跳过交互确认：

```bash
dbf apply --auto-approve
```

成功后末尾会看到：

```text
apply complete
```

`apply` 会先打印计划，再获取远端 lock，重新计算在线 plan，然后执行变更并写入 state。

## 再次 plan 和 check

运行：

```bash
dbf plan
dbf check
```

两条命令都应该显示：

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 1 no-op, 0 operations
```

这说明配置、远端 state 和主机实际状态一致。

## 用 SSH 验证远端结果

运行：

```bash
ssh manual1 'cat /etc/debianform-manual/hello.txt && stat -c %a:%U:%G /etc/debianform-manual/hello.txt'
```

预期：

```text
hello from DebianForm
644:root:root
```

## 本章完整命令

下面是本章可以一次复制运行的版本：

```bash
mkdir -p debianform-manual/01-first-apply
cd debianform-manual/01-first-apply

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/01-state.json"
    lock_path = "/var/lock/debianform/manual/01-state.lock"
  }

  files {
    file "/etc/debianform-manual/hello.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "hello from DebianForm\n"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf plan
dbf apply --auto-approve
dbf plan
dbf check
ssh manual1 'cat /etc/debianform-manual/hello.txt && stat -c %a:%U:%G /etc/debianform-manual/hello.txt'
```

## 清理

如果只是想清理本章创建的远端文件，可以运行：

```bash
ssh manual1 'rm -rf /etc/debianform-manual /var/lib/debianform/manual/01-state.json /var/lock/debianform/manual/01-state.lock /var/lock/debianform/manual/01-state.lock.d'
```

后续章节会使用自己的 state path。继续阅读时不需要清理本章。
