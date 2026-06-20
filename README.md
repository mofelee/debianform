# DebianForm

DebianForm v2 是对原 v1 原型的破坏式重设计。

当前仓库处于过渡状态：

- v2 是当前设计方向。
- v2 用户语法记录在 `docs/`，设计夹具位于 `examples/v2-*.dbf.hcl`。
- 当前 `dbf` CLI 仍临时使用 `internal/v1/` 中的 legacy v1 执行器构建。
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

- `examples/v2-fleet.dbf.hcl`
- `examples/v2-bbr.dbf.hcl`
- `examples/v2-apt-repository.dbf.hcl`
- `examples/v2-bird2.dbf.hcl`
- `examples/v2-component-binary.dbf.hcl`
- `examples/v2-nftables.dbf.hcl`
- `examples/v2-plan-preview.dbf.hcl`
- `examples/v2-systemd-networkd-wireguard.dbf.hcl`

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
```

`make test` 当前会运行过渡代码库的 Go 测试，包括 legacy v1 包。
