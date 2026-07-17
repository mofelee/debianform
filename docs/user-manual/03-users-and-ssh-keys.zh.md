<p align="right">
  <a href="03-users-and-ssh-keys.md">English</a> | <strong>简体中文</strong>
</p>

# 03. 管理用户、组和 SSH authorized keys

本章演示如何创建系统组、系统用户、用户 home 目录和 SSH authorized key，并演示 authorized key
被手动删除后如何用 `check/apply` 检测和修复。

本章示例已在 Debian 13 amd64 测试主机上验证通过。编写本章时还修复了一个系统问题：当目录的
owner/group 指向同一份配置中新建的用户或组时，资源图现在会正确让目录依赖用户和组，避免 apply
时先 chown 后创建用户。

## 创建工作目录

```bash
mkdir -p debianform-manual/03-users-and-ssh-keys
cd debianform-manual/03-users-and-ssh-keys
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/03-state.json"
    lock_path = "/var/lock/debianform/manual/03-state.lock"
  }

  groups {
    group "manualapp" {
      system = true
    }
  }

  users {
    user "manualapp" {
      system = true
      group  = "manualapp"
      home   = "/var/lib/manualapp"
      shell  = "/usr/sbin/nologin"

      ssh_authorized_keys = [
        "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMANUALUSERKEY000000000000000000000000000000000000 manual@example",
      ]
    }
  }

  directories {
    directory "/var/lib/manualapp" {
      owner = "manualapp"
      group = "manualapp"
      mode  = "0750"
    }
  }
}
```

这份配置声明：

- 系统组 `manualapp`。
- 系统用户 `manualapp`，主组是 `manualapp`。
- home 目录 `/var/lib/manualapp`，归属 `manualapp:manualapp`，权限 `0750`。
- 一条 authorized key。

示例里的 key 是假 key，只用于演示资源管理。实际使用时换成真实公钥。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

离线 plan 应该显示 4 个 create：

```text
Summary: 4 create, 0 update, 0 delete, 0 no-op, 0 operations
```

验证远端状态：

```bash
ssh manual1 'getent passwd manualapp; getent group manualapp; stat -c %a:%U:%G /var/lib/manualapp; cat /var/lib/manualapp/.ssh/authorized_keys'
```

预期类似：

```text
manualapp:x:999:989::/var/lib/manualapp:/usr/sbin/nologin
manualapp:x:989:
750:manualapp:manualapp
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMANUALUSERKEY000000000000000000000000000000000000 manual@example
```

uid/gid 可能不同，取决于测试主机已有系统用户和组。

## 制造 authorized key 漂移

删除远端 authorized key：

```bash
ssh manual1 'sed -i /MANUALUSERKEY/d /var/lib/manualapp/.ssh/authorized_keys'
```

运行：

```bash
dbf check
```

预期失败，并显示要重新添加 authorized key：

```text
+ host.manual1.users.user["manualapp"].ssh_authorized_key["3a13cdc31d27ffb8"]
  add authorized key for manualapp

Summary: 1 create, 0 update, 0 delete, 3 no-op, 0 operations
dbf: remote state does not match configuration
```

authorized key 被建成独立资源，所以只修复 key，不需要重建用户。

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

Summary: 0 create, 0 update, 0 delete, 4 no-op, 0 operations
```

确认 key 回来了：

```bash
ssh manual1 'grep -F manual@example /var/lib/manualapp/.ssh/authorized_keys'
```

## 本章完整命令

```bash
mkdir -p debianform-manual/03-users-and-ssh-keys
cd debianform-manual/03-users-and-ssh-keys

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/03-state.json"
    lock_path = "/var/lock/debianform/manual/03-state.lock"
  }

  groups {
    group "manualapp" {
      system = true
    }
  }

  users {
    user "manualapp" {
      system = true
      group  = "manualapp"
      home   = "/var/lib/manualapp"
      shell  = "/usr/sbin/nologin"

      ssh_authorized_keys = [
        "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMANUALUSERKEY000000000000000000000000000000000000 manual@example",
      ]
    }
  }

  directories {
    directory "/var/lib/manualapp" {
      owner = "manualapp"
      group = "manualapp"
      mode  = "0750"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'getent passwd manualapp; getent group manualapp; stat -c %a:%U:%G /var/lib/manualapp; cat /var/lib/manualapp/.ssh/authorized_keys'

ssh manual1 'sed -i /MANUALUSERKEY/d /var/lib/manualapp/.ssh/authorized_keys'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F manual@example /var/lib/manualapp/.ssh/authorized_keys'
```

## 清理

```bash
ssh manual1 'userdel manualapp 2>/dev/null || true; groupdel manualapp 2>/dev/null || true; rm -rf /var/lib/manualapp /var/lib/debianform/manual/03-state.json /var/lock/debianform/manual/03-state.lock /var/lock/debianform/manual/03-state.lock.d'
```

继续后续章节时不需要清理。
