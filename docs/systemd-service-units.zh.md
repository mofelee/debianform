# systemd service units

<p align="right"><a href="systemd-service-units.md">English</a> | <strong>简体中文</strong></p>

DebianForm 支持两种 `.service` unit 写法：

- `systemd.unit`：纯文本写入完整 systemd unit 文件。
- `systemd.service_unit`：结构化描述常见服务，DebianForm 生成 `.service` unit 文件。

两者最终都会编译成同一种 `SystemdUnit`，写入 `/etc/systemd/system/*.service`，
并在内容变化后触发 `systemctl daemon-reload`。服务是否开机启动、当前是否运行，仍然由
`services.service` 管理。

## 纯文本写法

纯文本写法适合需要完整控制 unit 内容的场景，例如复杂的 `ExecStartPre=`、
`CapabilityBoundingSet=`、多个 drop-in 尚未抽象的指令，或需要管理非 service 类型的
unit。

```hcl
systemd {
  unit "myapp.service" {
    content = <<-EOF
      [Unit]
      Description=My App
      Wants=network-online.target
      After=network-online.target

      [Service]
      WorkingDirectory=/var/lib/myapp
      Environment=MYAPP_ENV=production
      ExecStart=/usr/local/bin/myapp --config /etc/myapp/config.yaml
      Restart=always
      RestartSec=5s

      [Install]
      WantedBy=multi-user.target
    EOF
  }
}

services {
  service "myapp" {
    enabled = true
    state   = "running"
  }
}
```

如果只是想用纯文本写一个 `.service`，也可以使用 `service_unit` 的文本模式；
label 会自动补 `.service`：

```hcl
systemd {
  service_unit "myapp" {
    content = <<-EOF
      [Service]
      ExecStart=/usr/local/bin/myapp --config /etc/myapp/config.yaml
    EOF
  }
}
```

## 结构化写法

结构化写法适合常见长驻服务。它减少重复样板，并保留和 `services.service` 的清晰分工：
`service_unit` 只生成 unit 文件，`services.service` 只管理 enabled/running 状态。

```hcl
systemd {
  service_unit "myapp" {
    description = "My App"

    run = [
      "/usr/local/bin/myapp",
      "--config",
      "/etc/myapp/config.yaml",
    ]

    working_dir   = "/var/lib/myapp"
    restart       = "always"
    restart_delay = "5s"

    wants = ["network-online.target"]
    after = ["network-online.target"]

    environment = {
      MYAPP_ENV = "production"
    }

    stdout = "journal"
    stderr = "journal"
  }
}

services {
  service "myapp" {
    enabled = true
    state   = "running"
  }
}
```

上面的结构化写法会生成等价的 `/etc/systemd/system/myapp.service`：

```ini
[Unit]
Description=My App
Wants=network-online.target
After=network-online.target

[Service]
WorkingDirectory=/var/lib/myapp
Environment=MYAPP_ENV=production
ExecStart=/usr/local/bin/myapp --config /etc/myapp/config.yaml
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

## 对比

| 能力 | `systemd.unit` 纯文本 | `systemd.service_unit` 结构化 |
| --- | --- | --- |
| unit 类型 | 任意 unit 文件名 | `.service`，label 可省略 `.service` |
| 内容控制 | 完全手写 | DebianForm 根据字段生成 |
| 常见服务样板 | 需要手写 | 内置 `run`、环境、工作目录、重启、日志、依赖 |
| 不常见 systemd 指令 | 直接写 | 暂未覆盖时应改用纯文本 |
| 文件元数据 | `owner`、`group`、`mode` | `owner`、`file_group`、`mode` |
| 服务运行状态 | 配合 `services.service` | 配合 `services.service` |

`service_unit` 结构化字段当前覆盖：

- `description`
- `run`
- `type`
- `user`
- `group`
- `working_dir`
- `environment`
- `restart`
- `restart_delay`
- `wants`
- `after`
- `wanted_by`
- `stdout`
- `stderr`

`wanted_by` 默认是 `["multi-user.target"]`，这样 `services.service.enabled = true`
可以直接启用服务。需要只生成无 install section 的 unit 时，可以显式设置
`wanted_by = []`。

完整示例见 `examples/systemd-service-unit.dbf.hcl`。
