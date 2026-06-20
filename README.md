# DebianForm

DebianForm 是面向 Debian 主机的 v2 配置工具。

当前仓库以 v2 为唯一主线：

- v2 用户语法记录在 `docs/`，设计夹具位于 `examples/v2-*.dbf.hcl`。
- `dbf validate` 和 `dbf plan` 可解析 profile/host 合并，生成 HostSpec、
  ResourceGraph 和 plan。
- `dbf apply` 和 `dbf check` 已接入 v2 SSH 执行路径、v2 state 和 observed 检测。
- libvirt 集成测试位于 `test/integration/libvirt/`，用于在 Debian 13 VM 中验证 v2。

v2 用户层只写 `host`、`profile` 和领域块，不暴露旧式低阶资源语法。

## v2 设计文档

- [v2 requirements](docs/v2-requirements.md)
- [v2 IR requirements](docs/v2-ir-requirements.zh.md)
- [v2 plan format](docs/v2-plan-format.md)
- [v2 implementation plan](docs/v2-implementation-plan.zh.md)

## v2 示例

`examples/` 中的文件是 v2 示例和设计夹具。当前已支持以下示例通过 v2 validate，
并可生成 plan：

- `examples/v2-bbr.dbf.hcl`
- `examples/v2-profile-merge.dbf.hcl`
- `examples/v2-systemd-service.dbf.hcl`
- `examples/v2-user-group.dbf.hcl`

其他示例仍为 design-only fixture：

- `examples/v2-fleet.dbf.hcl`
- `examples/v2-apt-repository.dbf.hcl`
- `examples/v2-bird2.dbf.hcl`
- `examples/v2-component-binary.dbf.hcl`
- `examples/v2-nftables.dbf.hcl`
- `examples/v2-plan-preview.dbf.hcl`
- `examples/v2-systemd-networkd-wireguard.dbf.hcl`

BBR v2 plan 示例：

```bash
dbf plan -f examples/v2-bbr.dbf.hcl
```

```text
Plan:
  + host.bbr1.kernel.module["tcp_bbr"]
    create kernel module tcp_bbr
  + host.bbr1.kernel.sysctl["net.core.default_qdisc"]
    create sysctl net.core.default_qdisc
  + host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]
    create sysctl net.ipv4.tcp_congestion_control

Summary: 3 create, 0 update, 0 delete, 0 no-op, 0 operations
```

结构化 plan 可用：

```bash
dbf plan -f examples/v2-bbr.dbf.hcl --format json
```

静态 HTML preview 可用：

```bash
dbf plan -f examples/v2-bbr.dbf.hcl --html plan.html
```

配置格式化会原地改写目标 HCL 文件：

```bash
dbf fmt -f examples/v2-bbr.dbf.hcl
```

基础系统配置示例：

```hcl
host "service1" {
  files {
    file "/etc/myapp/config.yaml" {
      mode    = "0644"
      content = "listen: 127.0.0.1:8080\n"
    }
  }

  systemd {
    unit "myapp.service" {
      content = <<-EOF
        [Service]
        ExecStart=/usr/local/bin/myapp --config /etc/myapp/config.yaml
      EOF
    }
  }

  services {
    service "myapp" {
      enabled = true
      state   = "running"
    }
  }
}
```

`files.file sensitive = true` 和 `secrets.file` 不会在 plan 中输出明文；plan 只输出
hash、长度等摘要。`services.service.state` 支持 `running`、`stopped`、`restarted`
和 `reloaded`。

## 集成测试

libvirt 集成测试会启动全新的 Debian 13 cloud VM，并执行 v2 `validate`、`apply` 和
`check`：

```bash
make test-integration-layout
make test-integration-case CASE=files
make test-integration
```

## 开发

```bash
make build
make test
make update-golden
```

`make test` 运行 v2 Go 测试。
`make update-golden` 使用 `UPDATE_GOLDEN=1` 刷新 snapshot/golden 测试输出。
