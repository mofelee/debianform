# DebianForm WireGuard Example Plan

<p align="right"><a href="wireguard-example-plan.md">English</a> | <strong>简体中文</strong></p>

本文档记录当前 WireGuard 示例和双主机集成测试的实现方向。WireGuard 由
`systemd-networkd` 原生 `.netdev` / `.network` 文件管理，不安装 `wireguard-tools`，
也不使用 `wg-quick`。

## 目标

- 示例使用 `systemd.networkd` DSL 生成原生 networkd 配置。
- 示例通过结构化 component input 暴露 `interface.route_table`，默认值为
  `"off"`；这会生成 `RouteTable=off`，禁止 networkd 根据 peer `AllowedIPs`
  自动写入系统路由表，路由由用户显式管理。
- component 必须可以复用到同一台机器的多个 WireGuard interface。接口名、networkd
  文件路径和 private key 路径不能固定为 `wg0` 或 `/etc/wireguard/private.key`。
- component 必须支持一个 interface 下配置多个 peer。调用方可以用稳定 label 标识 peer，
  component 展开后生成多个 `[WireGuardPeer]` section。
- WireGuard private key 通过 `secrets.file` 写入
  `/etc/wireguard/${input.interface.name}.key`。
- private key 文件使用 `root:systemd-network 0640`，因为 Debian 上
  `systemd-networkd.service` 以 `systemd-network` 用户运行。
- `.netdev` 使用 `PrivateKeyFile`，不允许 inline `PrivateKey`。
- 集成测试创建两台 Debian VM，并验证 `wg0` 双向 tunnel ping。

## 复用需求

原先示例把 `netdev "10-wg0"`、`network "20-wg0"`、`Name = "wg0"` 和
`/etc/wireguard/private.key` 绑定在一起。这样一台机器如果需要连接两个 WireGuard
网络，就必须复制 component 或手写 networkd 配置；同一 host 上挂载两次 component
还会争用相同 private key 路径和相同 networkd block label。

新的 component API 使用：

- `interface.name` 作为真实 WireGuard interface 名，例如 `wg-prod`、`wg-backup`。
- `path = "/etc/systemd/network/10-${input.interface.name}.netdev"` 和
  `path = "/etc/systemd/network/20-${input.interface.name}.network"` 显式决定文件名。
- `secrets.file "private_key" { path = "/etc/wireguard/${input.interface.name}.key" }`
  作为 private key 目标路径，避免多个 component instance 覆盖同一个 key。
- `peers = map(object(...))` 暴露多 peer 输入。map key 是 peer label，用于稳定
  `[WireGuardPeer]` 生成顺序和错误定位。

调用方示例：

```hcl
component "wg_prod" {
  source = component.wireguard_networkd

  inputs = {
    private_key_source = "secrets/edge1-prod.key"
    interface = {
      name    = "wg-prod"
      address = "10.80.0.10/24"
    }
    peers = {
      server_a = {
        public_key  = "<server-a-public-key>"
        allowed_ips = ["10.80.0.1/32", "10.10.0.0/16"]
        endpoint    = "server-a.example.net:51820"
      }
      server_b = {
        public_key  = "<server-b-public-key>"
        allowed_ips = ["10.80.0.2/32", "10.20.0.0/16"]
        endpoint    = "server-b.example.net:51820"
      }
    }
  }
}
```

## 实现 loop

### Loop 1: networkd peer map

让 `systemd.networkd.netdev` 支持 `wireguard_peer = <map>` 属性。这个属性和现有
`wireguard_peer "<label>" { ... }` block 使用同一份内部表示：map key 是 peer label，
map value 是 networkd section。保留 block 写法，避免破坏已有配置。

验收点：

- `wireguard_peer = { server = { PublicKey = "...", AllowedIPs = [...] } }` 可以编译。
- 现有 `wireguard_peer "server" { ... }` 继续可用。
- 同一个 `netdev` 中不能同时用属性和 block 定义同名 peer。
- inline `PresharedKey` 仍被拒绝，必须使用 `PresharedKeyFile`。

### Loop 2: reusable component example

升级 `examples/wireguard-networkd.dbf.hcl`：

- `peer` input 改为 `peers = map(object(...))`。
- component 内部把 `input.peers` 转换为 networkd `WireGuardPeer` section map。
- `netdev` / `network` block label 改为通用 label，并用 `path` 绑定到
  `input.interface.name`。
- private key 写入 `/etc/wireguard/${input.interface.name}.key`。
- 示例展示至少一个 host 有多个 peers，另一个 host 仍可只配置一个 peer。

### Loop 3: duplicate example cleanup

`examples/wireguard-networkd.dbf.hcl` 和
`examples/systemd-networkd-wireguard.dbf.hcl` 原先内容完全相同。保留
`examples/wireguard-networkd.dbf.hcl` 作为唯一可运行 WireGuard component 示例，
删除重复文件，并更新 README、support matrix 和本文档引用。

## 当前实现

- `systemd { networkd { ... } }` 支持：
  - `netdev "<label>"` -> `/etc/systemd/network/<label>.netdev`
  - `network "<label>"` -> `/etc/systemd/network/<label>.network`
  - `wireguard_peer "<label>"` 或 `wireguard_peer = { ... }` -> repeated
    `[WireGuardPeer]` sections
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
examples/wireguard-networkd.dbf.hcl
```

真实 validate/apply 前需在本地准备：

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
go test ./internal/core/parser ./internal/core/merge ./internal/core/graph ./internal/core/plan ./internal/core/engine ./cmd/dbf
make test-integration-layout
make test-integration-case CASE=wireguard
```
