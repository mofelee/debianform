# 02. 管理文件、目录和漂移修复

本章在第 1 章基础上继续管理目录和多个文件，并演示一次真实漂移：手动修改远端文件后，
`dbf check` 会失败，`dbf apply` 会把文件修回配置声明的状态。

本章示例已在 Debian 13 amd64 测试主机上验证通过。

## 创建工作目录

```bash
mkdir -p debianform-manual/02-files-and-drift
cd debianform-manual/02-files-and-drift
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/02-state.json"
    lock_path = "/var/lock/debianform/manual/02-state.lock"
  }

  directories {
    directory "/etc/debianform-manual/app" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/app/app.env" {
      owner   = "root"
      group   = "root"
      mode    = "0640"
      content = <<-EOF
        APP_ENV=prod
        APP_PORT=8080
      EOF
    }

    file "/etc/debianform-manual/app/banner.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by DebianForm\n"
    }
  }
}
```

这份配置声明：

- 一个目录 `/etc/debianform-manual/app`。
- 一个较敏感的环境文件 `app.env`，权限是 `0640`。
- 一个普通文本文件 `banner.txt`，权限是 `0644`。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

离线 plan 应该显示 3 个 create：

```text
Summary: 3 create, 0 update, 0 delete, 0 no-op, 0 operations
```

验证远端文件：

```bash
ssh manual1 'find /etc/debianform-manual/app -maxdepth 1 -type f -printf "%f:%m:%u:%g\n" | sort'
```

预期：

```text
app.env:640:root:root
banner.txt:644:root:root
```

`stat`/`find` 显示的权限通常没有前导 `0`，所以 `0640` 会显示为 `640`。

## 制造一次漂移

现在假设有人手动改了远端环境文件：

```bash
ssh manual1 'printf "APP_ENV=dev\nAPP_PORT=9090\n" > /etc/debianform-manual/app/app.env'
```

运行：

```bash
dbf check
```

这次命令应该失败，末尾类似：

```text
Summary: 0 create, 1 update, 0 delete, 2 no-op, 0 operations
dbf: remote state does not match v2 configuration
```

`check` 不会修复主机。它只检查远端状态是否和配置一致，并在发现差异时返回非零退出码。

## 查看漂移内容

`check` 输出里会显示要把 `app.env` 修回配置中的内容：

```text
~ host.manual1.files.file["/etc/debianform-manual/app/app.env"]
  update file /etc/debianform-manual/app/app.env
```

你还会看到 observed `sha256` 和 desired `summary.sha256` 不同。对于普通文件，DebianForm 用内容 hash、
owner、group 和 mode 判断是否漂移。

## 修复漂移

运行：

```bash
dbf apply --auto-approve
dbf check
```

修复后 `check` 应该显示：

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 3 no-op, 0 operations
```

确认文件内容已恢复：

```bash
ssh manual1 'cat /etc/debianform-manual/app/app.env'
```

预期：

```text
APP_ENV=prod
APP_PORT=8080
```

## 本章完整命令

```bash
mkdir -p debianform-manual/02-files-and-drift
cd debianform-manual/02-files-and-drift

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/02-state.json"
    lock_path = "/var/lock/debianform/manual/02-state.lock"
  }

  directories {
    directory "/etc/debianform-manual/app" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/app/app.env" {
      owner   = "root"
      group   = "root"
      mode    = "0640"
      content = <<-EOT
        APP_ENV=prod
        APP_PORT=8080
      EOT
    }

    file "/etc/debianform-manual/app/banner.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by DebianForm\n"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'find /etc/debianform-manual/app -maxdepth 1 -type f -printf "%f:%m:%u:%g\n" | sort'

ssh manual1 'printf "APP_ENV=dev\nAPP_PORT=9090\n" > /etc/debianform-manual/app/app.env'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'cat /etc/debianform-manual/app/app.env'
```

## 清理

```bash
ssh manual1 'rm -rf /etc/debianform-manual/app /var/lib/debianform/manual/02-state.json /var/lock/debianform/manual/02-state.lock /var/lock/debianform/manual/02-state.lock.d'
```

继续后续章节时不需要清理。
