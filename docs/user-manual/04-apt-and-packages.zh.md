# 04. 安装软件包和配置 APT 源

本章演示两类常见 APT 运维任务：

- 安装 Debian 官方源中的软件包。
- 管理一个 deb822 `.sources` 文件，并在漂移后自动修复。

本章示例已在 Debian 13 amd64 测试主机上验证通过。示例会安装 `jq`，需要测试主机可以访问 Debian
软件源。

## 创建工作目录

```bash
mkdir -p debianform-manual/04-apt-and-packages
cd debianform-manual/04-apt-and-packages
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/04-state.json"
    lock_path = "/var/lock/debianform/manual/04-state.lock"
  }

  apt {
    source_file "manual-disabled" {
      path = "/etc/apt/sources.list.d/debianform-manual-disabled.sources"

      content = <<-EOF
        Types: deb
        URIs: https://deb.debian.org/debian
        Suites: trixie
        Components: main
        Enabled: no
        Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
      EOF

      on_destroy = "restore"
    }
  }

  packages {
    install = ["jq"]
  }
}
```

这里的 APT source 文件写了 `Enabled: no`。它用于演示 DebianForm 如何管理 source 文件和触发
`apt-get update`，但不会实际启用重复的软件源。

`on_destroy = "restore"` 表示如果以后不再管理这个 source 文件，DebianForm 会尽量恢复它接管前的内容。
如果接管前文件不存在，则删除它。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

离线 plan 应该显示 2 个 create 和 1 个 operation：

```text
Summary: 2 create, 0 update, 0 delete, 0 no-op, 1 operations
```

operation 是：

```text
! host.manual1.apt.cache_refresh
  refresh apt package cache
  command: apt-get update
```

APT source 文件变化会触发一次 cache refresh，然后 package 安装可以使用最新缓存。

## 验证结果

检查 `jq`：

```bash
ssh manual1 'jq --version'
```

预期类似：

```text
jq-1.7
```

查看 DebianForm 管理的 source 文件：

```bash
ssh manual1 'sed -n "1,8p" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

预期：

```text
Types: deb
URIs: https://deb.debian.org/debian
Suites: trixie
Components: main
Enabled: no
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
```

## 制造 source 文件漂移

手动把 `Enabled: no` 改成 `Enabled: yes`：

```bash
ssh manual1 'sed -i "s/Enabled:.*/Enabled: yes/" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

运行：

```bash
dbf check
```

预期失败，并显示 source file 要 update，同时会再次触发 apt cache refresh：

```text
Summary: 0 create, 1 update, 0 delete, 1 no-op, 1 operations
dbf: remote state does not match v2 configuration
```

`check` 只报告漂移，不会运行 `apt-get update` 或修改文件。

## 修复漂移

运行：

```bash
dbf apply --auto-approve
dbf check
```

修复后应显示：

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 2 no-op, 0 operations
```

确认文件被修回：

```bash
ssh manual1 'grep -F "Enabled: no" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

## 本章完整命令

```bash
mkdir -p debianform-manual/04-apt-and-packages
cd debianform-manual/04-apt-and-packages

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/04-state.json"
    lock_path = "/var/lock/debianform/manual/04-state.lock"
  }

  apt {
    source_file "manual-disabled" {
      path = "/etc/apt/sources.list.d/debianform-manual-disabled.sources"

      content = <<-EOT
        Types: deb
        URIs: https://deb.debian.org/debian
        Suites: trixie
        Components: main
        Enabled: no
        Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
      EOT

      on_destroy = "restore"
    }
  }

  packages {
    install = ["jq"]
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'jq --version'
ssh manual1 'sed -n "1,8p" /etc/apt/sources.list.d/debianform-manual-disabled.sources'

ssh manual1 'sed -i "s/Enabled:.*/Enabled: yes/" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F "Enabled: no" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

## 清理

如果想卸载本章安装的软件包并删除 source 文件：

```bash
ssh manual1 'rm -f /etc/apt/sources.list.d/debianform-manual-disabled.sources /var/lib/debianform/manual/04-state.json /var/lock/debianform/manual/04-state.lock; rm -rf /var/lock/debianform/manual/04-state.lock.d; DEBIAN_FRONTEND=noninteractive apt-get remove -y jq libjq1 libonig5'
```

继续后续章节时不需要清理。
