# 05. 管理 systemd service unit 和服务状态

本章演示如何用 DebianForm 部署一个低权限 systemd 服务：创建运行用户、写 worker 脚本、
生成 `.service` unit，启用并启动服务。随后手动停止服务，使用 `check/apply` 检测并修复漂移。

本章示例已在 Debian 13 amd64 测试主机上验证通过。

## 创建工作目录

```bash
mkdir -p debianform-manual/05-systemd-service
cd debianform-manual/05-systemd-service
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/05-state.json"
    lock_path = "/var/lock/debianform/manual/05-state.lock"
  }

  groups {
    group "manualsvc" {
      system = true
    }
  }

  users {
    user "manualsvc" {
      system = true
      group  = "manualsvc"
      home   = "/var/lib/manualsvc"
      shell  = "/usr/sbin/nologin"
    }
  }

  directories {
    directory "/var/lib/manualsvc" {
      owner = "manualsvc"
      group = "manualsvc"
      mode  = "0750"
    }
  }

  files {
    file "/usr/local/bin/manualsvc-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOF
        #!/usr/bin/env sh
        set -eu
        while :; do
          date -Is >> /var/lib/manualsvc/heartbeat.log
          sleep 10
        done
      EOF
    }
  }

  systemd {
    service_unit "manualsvc" {
      description = "DebianForm manual service"
      run         = ["/usr/local/bin/manualsvc-worker"]
      user        = "manualsvc"
      group       = "manualsvc"
      working_dir = "/var/lib/manualsvc"
      restart     = "always"
      stdout      = "journal"
      stderr      = "journal"
    }
  }

  services {
    service "manualsvc" {
      enabled = true
      state   = "running"
    }
  }
}
```

这份配置会生成 `/etc/systemd/system/manualsvc.service`，并让服务使用 `manualsvc` 用户运行。
worker 每 10 秒向 `/var/lib/manualsvc/heartbeat.log` 写一行时间。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

离线 plan 会包含 6 个资源和 1 个 operation：

```text
Summary: 6 create, 0 update, 0 delete, 0 no-op, 1 operations
```

operation 是 systemd daemon reload。unit 文件变化后，DebianForm 会运行：

```text
systemctl daemon-reload
```

## 验证服务

运行：

```bash
ssh manual1 'sleep 1; systemctl is-active manualsvc.service; systemctl is-enabled manualsvc.service; test -s /var/lib/manualsvc/heartbeat.log && tail -n 1 /var/lib/manualsvc/heartbeat.log'
```

预期类似：

```text
active
enabled
2026-06-24T04:50:09+00:00
```

这说明服务已启动、已设为开机启动，并且 worker 正在写 heartbeat。

## 制造服务漂移

手动停止服务：

```bash
ssh manual1 'systemctl stop manualsvc.service'
```

运行：

```bash
dbf check
```

预期失败，并显示 service 需要重新 start：

```text
~ host.manual1.services.service["manualsvc"]
  start service manualsvc.service

Summary: 0 create, 1 update, 0 delete, 5 no-op, 0 operations
dbf: remote state does not match configuration
```

`check` 仍然只检测，不会启动服务。

## 修复服务漂移

运行：

```bash
dbf apply --auto-approve
dbf check
```

修复后应显示：

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 6 no-op, 0 operations
```

确认服务恢复：

```bash
ssh manual1 'systemctl is-active manualsvc.service'
```

预期：

```text
active
```

## 本章完整命令

```bash
mkdir -p debianform-manual/05-systemd-service
cd debianform-manual/05-systemd-service

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/05-state.json"
    lock_path = "/var/lock/debianform/manual/05-state.lock"
  }

  groups {
    group "manualsvc" {
      system = true
    }
  }

  users {
    user "manualsvc" {
      system = true
      group  = "manualsvc"
      home   = "/var/lib/manualsvc"
      shell  = "/usr/sbin/nologin"
    }
  }

  directories {
    directory "/var/lib/manualsvc" {
      owner = "manualsvc"
      group = "manualsvc"
      mode  = "0750"
    }
  }

  files {
    file "/usr/local/bin/manualsvc-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOT
        #!/usr/bin/env sh
        set -eu
        while :; do
          date -Is >> /var/lib/manualsvc/heartbeat.log
          sleep 10
        done
      EOT
    }
  }

  systemd {
    service_unit "manualsvc" {
      description = "DebianForm manual service"
      run         = ["/usr/local/bin/manualsvc-worker"]
      user        = "manualsvc"
      group       = "manualsvc"
      working_dir = "/var/lib/manualsvc"
      restart     = "always"
      stdout      = "journal"
      stderr      = "journal"
    }
  }

  services {
    service "manualsvc" {
      enabled = true
      state   = "running"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'sleep 1; systemctl is-active manualsvc.service; systemctl is-enabled manualsvc.service; test -s /var/lib/manualsvc/heartbeat.log && tail -n 1 /var/lib/manualsvc/heartbeat.log'

ssh manual1 'systemctl stop manualsvc.service'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'systemctl is-active manualsvc.service'
```

## 清理

```bash
ssh manual1 'systemctl disable --now manualsvc.service 2>/dev/null || true; rm -f /etc/systemd/system/manualsvc.service /usr/local/bin/manualsvc-worker; systemctl daemon-reload; userdel manualsvc 2>/dev/null || true; groupdel manualsvc 2>/dev/null || true; rm -rf /var/lib/manualsvc /var/lib/debianform/manual/05-state.json /var/lock/debianform/manual/05-state.lock /var/lock/debianform/manual/05-state.lock.d'
```

继续后续章节时不需要清理。
