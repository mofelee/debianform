# systemd service units

<p align="right"><a href="systemd-service-units.md">English</a> | <strong>简体中文</strong></p>

DebianForm 支持两种 `.service` unit 写法：

- `systemd.unit`：纯文本写入完整 systemd unit 文件。
- `systemd.service_unit`：结构化描述常见服务，DebianForm 生成 `.service` unit 文件。

两者最终都会编译成同一种 `SystemdUnit`，写入 `/etc/systemd/system/*.service`，
并在内容变化后触发 `systemctl daemon-reload`。`service_unit` 还可以在 reload 后显式执行
runtime action。服务是否开机启动、当前是否运行，仍然由 `services.service` 管理。

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
    change_action = "restart"

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

## 让运行中的服务应用 unit 变化

`systemctl daemon-reload` 只更新 systemd 读取到的 unit 定义，不会替换已经运行的进程。如果
unit 变化还必须作用到 live service，应在 `service_unit` 上设置 `change_action`：

```hcl
systemd {
  service_unit "myapp" {
    run           = ["/usr/local/bin/myapp", "--config", "/etc/myapp/config.yaml"]
    change_action = "try-restart"
  }
}
```

支持 `restart`、`reload` 和 `try-restart`。DebianForm 会把动作作为独立 operation 显示在
plan 中，并保证以下顺序：

1. 写入发生变化的 unit；
2. 运行 `systemctl daemon-reload`；
3. 仅当服务 active 时运行所选动作；
4. 收敛匹配的 `services.service` 状态。

inactive 服务会保持 stopped。新建 unit 配合 `state = "running"` 时，动作会看到服务仍为
inactive，随后 service resource 只启动一次。动作失败会使 apply 失败且不会记录完成状态，
下一次 apply 会重试。active unit 不支持 reload 时，`reload` 会失败。

## 对比

| 能力 | `systemd.unit` 纯文本 | `systemd.service_unit` 结构化 |
| --- | --- | --- |
| unit 类型 | 任意 unit 文件名 | `.service`，label 可省略 `.service` |
| 内容控制 | 完全手写 | DebianForm 根据字段生成 |
| 常见服务样板 | 需要手写 | 内置 `run`、环境、工作目录、重启、日志、依赖 |
| 不常见 systemd 指令 | 直接写 | 暂未覆盖时应改用纯文本 |
| 文件元数据 | `owner`、`group`、`mode` | `owner`、`file_group`、`mode` |
| 服务运行状态 | 配合 `services.service` | 配合 `services.service` |
| unit 变化后的 runtime action | 不支持 | 可选 `change_action` |

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
- `change_action`（raw `service_unit` 模式也可用）

`wanted_by` 默认是 `["multi-user.target"]`，这样 `services.service.enabled = true`
可以直接启用服务。需要只生成无 install section 的 unit 时，可以显式设置
`wanted_by = []`。

完整示例见 `examples/systemd-service-unit.dbf.hcl`。
