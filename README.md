# DebianForm

DebianForm v2 是对原 v1 原型的破坏式重设计。

当前仓库处于过渡状态：

- v2 是当前设计方向。
- v2 用户语法记录在 `docs/`，设计夹具位于 `examples/v2-*.dbf.hcl`。
- `dbf validate` 和 `dbf plan` 已接入 v2 Loop 3 路径，可解析 profile/host 合并、
  生成 HostSpec、ResourceGraph 和 create-only plan；`apply`/`check` 的 v2 路径尚未实现。
- legacy v1 示例、文档和 libvirt 集成测试已归档到 `legacy/v1/`。

新的 v2 工作不要以旧的 `debian_*` 资源语法作为用户模型。它只作为 provider、SSH
执行、远端 state 和 libvirt 测试启动方式的实现参考保留。

## v2 设计文档

- [v2 requirements](docs/v2-requirements.md)
- [v2 IR requirements](docs/v2-ir-requirements.zh.md)
- [v2 plan format](docs/v2-plan-format.md)
- [v2 implementation plan](docs/v2-implementation-plan.zh.md)

## v2 示例

`examples/` 中的文件是 v2 设计夹具，当前还不能由 legacy CLI 执行。
Loop 3 已支持以下示例通过 v2 validate，并可生成 create-only plan：

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

`files.file sensitive = true` 和 `secrets.file` 不会在 plan 中输出明文；当前 plan 只输出
hash、长度等摘要。`services.service.state` 支持 `running`、`stopped`、`restarted`
和 `reloaded`。

## Legacy v1 归档

可复用的 v1 参考材料已移出主文档和主示例路径：

- `legacy/v1/README.md`
- `legacy/v1/examples/`
- `legacy/v1/test/integration/libvirt/`
- `internal/v1/`

libvirt runner 仍可作为参考代码运行：

```bash
make test-legacy-v1-integration-layout
make test-legacy-v1-integration-case CASE=files
make test-legacy-v1-integration
```

## 开发

```bash
make build
make test
make update-golden
```

`make test` 当前会运行过渡代码库的 Go 测试，包括 legacy v1 包。
`make update-golden` 使用 `UPDATE_GOLDEN=1` 刷新 snapshot/golden 测试输出。
