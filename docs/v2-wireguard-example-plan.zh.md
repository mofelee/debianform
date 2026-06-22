# DebianForm v2 WireGuard Example Plan

本文档记录新增 WireGuard 示例和双主机集成测试的执行计划。目标不是只验证单机
`wg-quick` 能启动，而是必须在测试中创建两台主机，并证明它们通过 WireGuard 隧道互通。

## 目标

- 新增一个基于当前已实现 DSL 的 WireGuard 示例。
- 集成测试必须创建两台 Debian VM。
- 两台 VM 必须在同一个 libvirt NAT 网络内启动，并通过 WireGuard 建立点对点隧道。
- 测试必须验证双向连通性，而不是只检查服务 active。
- WireGuard private key 不作为普通明文配置提交到用户示例中。
- 当前阶段不引入尚未实现的 `systemd.networkd` DSL。

## 非目标

- 不实现完整 mesh、hub-and-spoke 或动态 peer discovery。
- 不实现 systemd-networkd 的 `.netdev` / `.network` 领域 DSL。
- 不把 production private key 写进 `examples/`。
- 不把 WireGuard key 生成作为 DebianForm core 功能；测试 fixture 可以使用测试专用 key。
- 不要求 WireGuard endpoint 暴露到宿主机或公网；只验证两台 VM 内部互通。

## 当前约束

- 当前 libvirt integration runner 一次只创建一台 VM，并只替换一个 `__DBF_VM_IP__`。
- WireGuard 互通测试需要两台 VM，所以必须先扩展 runner 或新增双主机 runner。
- 当前可用 DSL 已能覆盖第一版 WireGuard：
  - `packages.install` 安装 `wireguard-tools`、`iproute2`、`iputils-ping`。
  - `directories.directory` 创建 `/etc/wireguard`。
  - `secrets.file` 管理 `/etc/wireguard/wg0.conf`，避免 plan/state 输出私钥。
  - `services.service "wg-quick@wg0"` 管理 `wg-quick@wg0.service`。
- 仓库已有 `examples/v2-systemd-networkd-wireguard.dbf.hcl` 是 design-only 草案；本轮不基于
  `systemd.networkd` 实现，避免示例依赖未实现语法。

## 推荐实现方式

第一版使用 `wg-quick@wg0`：

- 每台主机安装 `wireguard-tools`。
- 每台主机通过 `secrets.file` 写入完整 `/etc/wireguard/wg0.conf`。
- `wg0.conf` 包含：
  - `[Interface] Address = 10.80.0.1/30` 或 `10.80.0.2/30`
  - `[Interface] ListenPort = 51820`
  - `[Interface] PrivateKey = ...`
  - `[Peer] PublicKey = ...`
  - `[Peer] AllowedIPs = 10.80.0.x/32`
  - `[Peer] Endpoint = <peer-libvirt-ip>:51820`
  - `[Peer] PersistentKeepalive = 25`
- 使用 `services.service "wg-quick@wg0"` 启动并 enable 服务。

说明：

- `wg-quick` 的配置格式需要 inline `PrivateKey`；它不像 systemd-networkd WireGuard
  `.netdev` 那样支持 `PrivateKeyFile`。因此第一版把整个 `wg0.conf` 当作 secret 文件管理。
- 用户示例中不提交真实 secret 文件，只引用 `examples/secrets/wg-a.conf` 和
  `examples/secrets/wg-b.conf`，并在注释中说明这些文件应由用户本地生成。
- 集成测试可以提交测试专用 `wg0.conf` fixture，或由测试准备脚本生成到 case workdir；这些 key
  只能用于测试，不能作为生产示例。

## 阶段 1: 用户示例

新增示例文件：

```text
examples/v2-wireguard-wgquick.dbf.hcl
```

示例结构：

- 定义 component `wireguard_wgquick`。
- component inputs：
  - `config_source`: 本地 secret config 文件路径。
- component 资源：
  - `packages.install = ["wireguard-tools", "iproute2", "iputils-ping"]`
  - `/etc/wireguard` directory。
  - `secrets.file "/etc/wireguard/wg0.conf"`，`source = input.config_source`，`mode = "0600"`。
  - `services.service "wg-quick@wg0"`，`enabled = true`，`state = "running"`。
- 定义两个 host：
  - `wg-a`
  - `wg-b`
- 两个 host 分别挂载同一个 component，但传入不同 `config_source`。

验收标准：

- 示例展示两台 host 的写法。
- 示例注释明确 private key 不应提交。
- 示例不依赖 `systemd.networkd` 未实现语法。
- 如果示例引用 ignored secrets，则不要把它加入 runnable example golden；只做解析或文档示例。

## 阶段 2: 双 VM integration runner

当前 `run-case.sh` 是单 VM runner。WireGuard case 需要以下能力：

- 在一个 case 内创建两台 VM。
- 两台 VM 连接到同一个临时 libvirt NAT network。
- 为两台 VM 写入 SSH config alias，例如：
  - `wg-a`
  - `wg-b`
- 支持在 numbered `*.dbf.hcl` 中替换多个 placeholder：
  - `__DBF_WG_A_SSH_HOST__`
  - `__DBF_WG_B_SSH_HOST__`
  - `__DBF_WG_A_VM_IP__`
  - `__DBF_WG_B_VM_IP__`
- 保留现有单 VM case 行为，避免破坏 `files`、`nftables`、`bbr` 等已有测试。

推荐实现：

- 新增 `test/integration/libvirt/run-two-host-case.sh`，不要把单 VM runner 改成复杂通用框架。
- `run.sh` 根据 case 目录中的 marker 文件选择 runner：
  - 默认：`run-case.sh`
  - 存在 `two-host.case`：`run-two-host-case.sh`
- `run-two-host-case.sh` 复用现有 runner 的大部分逻辑，但创建两个 domain：
  - `dbf-v2-wireguard-a-...`
  - `dbf-v2-wireguard-b-...`
- 两台 VM 使用同一个 libvirt network。
- cleanup 必须分别 destroy/undefine 两台 VM，并删除同一个 network。

验收标准：

- 旧的单 VM integration case 不需要改配置即可继续通过 layout check。
- WireGuard case 可以拿到两台 VM 的 SSH alias 和 libvirt IP。
- 任一 VM 创建失败时，cleanup 仍能清理已创建的 VM/network。

## 阶段 3: WireGuard integration case

新增 case：

```text
test/integration/libvirt/cases/wireguard/
```

文件：

- `two-host.case`
- `1.dbf.hcl`
- `1.check.sh`
- `2.dbf.hcl`
- `2.check.sh`
- `3.dbf.hcl`
- `3.drift.sh`
- `3.check.sh`
- `4.dbf.hcl`
- `4.check.sh`
- `5.dbf.hcl`
- `5.check.sh`
- `secrets/wg-a.conf`
- `secrets/wg-b.conf`

拓扑：

- VM A libvirt IP：`__DBF_WG_A_VM_IP__`
- VM B libvirt IP：`__DBF_WG_B_VM_IP__`
- WireGuard A tunnel IP：`10.80.0.1/30`
- WireGuard B tunnel IP：`10.80.0.2/30`
- UDP port：`51820`
- A peer endpoint：`__DBF_WG_B_VM_IP__:51820`
- B peer endpoint：`__DBF_WG_A_VM_IP__:51820`

Step 1 desired state：

- 两台 host 都安装 WireGuard。
- 两台 host 都写入 `/etc/wireguard/wg0.conf`。
- 两台 host 都保持 `wg-quick@wg0.service` stopped/disabled。

Step 1 checks：

- A/B package installed。
- A/B secret config mode 为 `0600 root root`。
- A/B service inactive。
- A/B state 都记录 secret、package resources。
- A/B state 不输出 `PrivateKey` 明文。

Step 2 desired state：

- 两台 host 保持同一份 WireGuard 配置。
- 两台 host 都 enable/start `wg-quick@wg0.service`。

Step 2 checks：

- A/B 都有 `wg0` interface 和预期 tunnel IP。
- A/B 都显示 `wg show wg0` 有 peer。
- A ping `10.80.0.2` 成功。
- B ping `10.80.0.1` 成功。
- A/B state 都记录 secret、package、service resources。
- A/B state 不输出 `PrivateKey` 明文。

Step 3 drift：

- 在 A 上停止 `wg-quick@wg0.service`，或替换 A 的 `/etc/wireguard/wg0.conf` 为 drift 内容。
- `dbf check -f 3.dbf.hcl` 必须失败。
- `dbf apply -f 3.dbf.hcl` 修复 drift。

Step 3 checks：

- A/B `wg-quick@wg0.service` 都 active。
- A/B tunnel ping 仍双向成功。
- 被修改的 config hash 已恢复到 desired。

Step 4 desired state：

- 两台 host 仍保留 WireGuard component 和 `/etc/wireguard/wg0.conf`。
- 两台 host 都 disable/stop `wg-quick@wg0.service`。
- 这一步专门避免从 running state 直接 destroy 时，`wg0.conf` 先被删除导致 `wg-quick down`
  缺失配置文件。

Step 4 checks：

- A/B `wg-quick@wg0.service` inactive/disabled。
- A/B `wg0` interface 不存在。
- A/B `/etc/wireguard/wg0.conf` 仍存在且权限正确。
- A/B state 仍记录 stopped service desired state，且不输出 `PrivateKey` 明文。

Step 5 destroy：

- 两台 host 都移除 WireGuard component。
- DebianForm destroy 远端 `/etc/wireguard/wg0.conf`。
- state 最终没有 managed resources。

Step 5 checks：

- A/B `wg0` interface 不存在。
- A/B `/etc/wireguard/wg0.conf` 不存在。
- A/B `wg-quick@wg0.service` inactive 或 unit 无实例状态。
- 两台 host 的 integration state 文件都清空或移除。

## 阶段 4: Parser、layout 和文档接入

- 如果新增用户示例进入 runnable examples，需要补 parser golden。
- 如果用户示例依赖 ignored secrets，则不要加入 runnable examples；改为在 README 中标注它是
  deployment example，需要本地 secret 文件。
- `test/integration/libvirt/validate-cases.sh` 需要识别 `two-host.case`，至少验证 numbered
  config 连续、check hook 存在，并用新 validate 路径校验多 host config。
- `test/integration/libvirt/README.md` 增加运行命令：

```bash
make test-integration-case CASE=wireguard
```

验收标准：

- `dbf validate -f test/integration/libvirt/cases/wireguard/1.dbf.hcl` 通过。
- `make test-integration-layout` 通过。
- WireGuard case 在 libvirt 中创建两台 VM 并完成全部 hooks。

## 测试矩阵

必须运行：

- `go test ./...`
- `make test-integration-layout`
- `make test-integration-case CASE=wireguard`，或在远端 `qemu+ssh` 环境下用等价的
  `$virsh-test-host` 双 VM 手工流程验证。

WireGuard case 必须至少包含这些断言：

- A/B package installed。
- A/B secret config deployed with mode `0600`。
- A/B service active and enabled。
- A/B `wg0` interface has expected tunnel IP。
- A -> B tunnel ping succeeds。
- B -> A tunnel ping succeeds。
- drift can be detected and repaired。
- destroy removes config/interface/state resources.

## 风险和处理

- 两台 VM 的 libvirt IP 是运行时值，不能写死在 repo 中；必须由 runner 替换 placeholder。
- WireGuard private key 是敏感内容；用户示例不能提交生产 key。
- 如果只检查 service active，不能证明隧道可用；必须检查双向 tunnel ping。
- 如果两个 VM 不在同一 libvirt network，endpoint 可能不可达；runner 必须共享 network。
- 如果复用单 VM runner 做复杂扩展，容易影响现有 case；优先新增 two-host runner，降低回归风险。
- 如果使用 `systemd.networkd` 草案语法，会引入未实现 DSL；第一版必须基于现有可运行资源。

## 建议提交拆分

1. 文档计划：新增本文件。
2. Runner：新增双 VM integration runner 和 layout 校验支持。
3. 示例：新增 `examples/v2-wireguard-wgquick.dbf.hcl`。
4. Integration：新增 `wireguard` 双主机 case。
5. 文档接入：更新 README 和 integration README。
