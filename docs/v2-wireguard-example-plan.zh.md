# DebianForm v2 WireGuard Example Plan

本文档记录当前 WireGuard 示例和双主机集成测试的实现方向。WireGuard 由
`systemd-networkd` 原生 `.netdev` / `.network` 文件管理，不安装 `wireguard-tools`，
也不使用 `wg-quick`。

## 目标

- 示例使用 `systemd.networkd` DSL 生成原生 networkd 配置。
- WireGuard private key 通过 `secrets.file` 写入 `/etc/wireguard/private.key`。
- private key 文件使用 `root:systemd-network 0640`，因为 Debian 上
  `systemd-networkd.service` 以 `systemd-network` 用户运行。
- `.netdev` 使用 `PrivateKeyFile`，不允许 inline `PrivateKey`。
- 集成测试创建两台 Debian VM，并验证 `wg0` 双向 tunnel ping。

## 当前实现

- `systemd { networkd { ... } }` 支持：
  - `netdev "<label>"` -> `/etc/systemd/network/<label>.netdev`
  - `network "<label>"` -> `/etc/systemd/network/<label>.network`
  - `wireguard_peer "<label>"` -> repeated `[WireGuardPeer]` sections
- networkd section 使用接近原生的 map/list 结构：
  - 标量生成 `Key=value`
  - list 生成重复 `Key=value`
- graph 编译为 file-like resources，并在配置变化后运行：
  - `systemctl start systemd-networkd.service`
  - `networkctl reload`
  - 对 absent netdev 额外执行 `ip link delete <Name> ... || true`

## 示例

主要示例：

```text
examples/v2-wireguard-networkd.dbf.hcl
examples/v2-systemd-networkd-wireguard.dbf.hcl
```

这两个示例展示同一套 networkd 原生写法。真实 validate/apply 前需在本地准备：

```text
examples/secrets/wg-a.key
examples/secrets/wg-b.key
```

`examples/secrets/` 被 gitignore，不能提交生产私钥。

## 集成测试

case 路径：

```text
test/integration/libvirt/cases/wireguard/
```

拓扑：

- VM A libvirt IP：`__DBF_WG_A_VM_IP__`
- VM B libvirt IP：`__DBF_WG_B_VM_IP__`
- WireGuard A tunnel IP：`10.80.0.1/30`
- WireGuard B tunnel IP：`10.80.0.2/30`
- UDP port：`51820`

测试步骤：

- Step 1：写入 private key、`.netdev`、`.network`。
- Step 2：启动/reload networkd，验证 `wg0` 地址和双向 ping。
- Step 3：删除 A 的 `.netdev` 制造 drift，验证 `dbf check` 失败并由 apply 修复。
- Step 4：把 networkd 文件声明为 absent，删除 `wg0`，从 state 移除 networkd
  file resources，但保留 private key。
- Step 5：移除 component，销毁 private key、networkd 文件和 state resources。

断言重点：

- 不安装 `wireguard-tools`。
- 不出现 `wg-quick@wg0.service`。
- state 不包含测试 private key 明文。
- A/B tunnel ping 双向成功。
- drift 可以被检测和修复。

## 验证命令

```bash
go test ./internal/v2/parser ./internal/v2/merge ./internal/v2/graph ./internal/v2/plan ./internal/v2/engine ./cmd/dbf
make test-integration-layout
make test-integration-case CASE=wireguard
```
