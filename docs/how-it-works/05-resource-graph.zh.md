# 05. ResourceGraph 如何展开资源和依赖

<p align="right"><a href="05-resource-graph.md">English</a> | <strong>简体中文</strong></p>

本章解释 `internal/core/graph` 如何把 `ir.Program` 展开成 `ResourceGraph`。graph 是 plan 和 apply 的
共同输入，它定义资源地址、provider payload、依赖关系和 operation 触发关系。

## 数据流

```text
ir.Program
  -> graph.Compile
  -> compileHost
  -> ResourceGraph{Nodes, Operations}
  -> Validate / Waves / ActiveWaves
```

graph 仍然不连接远端主机。它只根据 IR 生成 desired resource 和静态依赖。

## ResourceGraph

`ResourceGraph` 包含两类对象：

- `Nodes`：可被 provider plan/apply/destroy 的资源节点。
- `Operations`：由资源变更触发的一次性操作，例如刷新 apt cache 或重启服务。

资源节点和 operation 都有稳定地址。地址是 state、plan、依赖、测试 golden 的共同身份，不能轻易改。

## Node

`graph.Node` 的关键字段：

- `Host`：所属 host。
- `Address`：DebianForm 层资源地址。
- `Kind`：资源类型，例如 `file`、`package`、`systemd_unit`。
- `Summary`：面向 plan 的简短说明。
- `Source`：用户配置来源。
- `Lifecycle`：例如 `prevent_destroy`。
- `Desired`：DebianForm 层 desired 值。
- `ProviderType`：provider 类型。
- `ProviderAddress`：provider 低阶地址，主要用于 debug。
- `ProviderPayload`：真正传给 provider 的 payload。
- `DependsOn`：依赖的 graph address。

大多数节点的 `Desired` 和 `ProviderPayload` 一致，但有些资源会不同。比如用户层的抽象资源可能需要
转成 file-like provider payload。

## Operation

`graph.Operation` 的关键字段：

- `Host`：明确的执行目标；调度和 provider 不从 address 反解析 host。
- `Address`
- `Action`
- `Summary`
- `DependsOn`
- `TriggeredBy`
- `CommandPreview`
- `Source`

operation 不进入 state。它只在 plan 中显示，并在 apply 中按触发条件执行。

`TriggeredBy` 表示哪些资源发生 `create`、`update`、`delete` 时需要运行该 operation。
`DependsOn` 表示调度顺序，例如必须等某个文件写完后再运行 reload。

## compileHost 的职责

`compileHost` 是 graph 展开的主要函数。它会为 host 的每个领域 spec 生成 node 和 operation。

典型展开：

- kernel module -> `kernel_module` node。
- sysctl -> `sysctl` node，必要时依赖 `tcp_bbr` module。
- apt repository -> signing key node、source file node、apt cache refresh operation。
- package -> `package` node，必要时依赖 repository/cache。
- files/secrets/systemd/nftables/networkd -> file-like node。
- group/user/membership/authorized key -> identity 和权限相关 node。
- service -> `service` node，可能依赖 systemd unit。
- docker -> 多个 repository、package、daemon、service、compose 相关节点。
- component -> 带 component prefix 的 artifact 和领域资源节点。

`compileHost` 会先建立一些地址索引，例如 group、user、systemd unit、repository 的地址映射。
这些索引用于给后续节点补依赖。

## 地址稳定性

地址通常形如：

```text
host.<host>.<domain>.<kind>[<quoted-key>]
host.<host>.components.<component>.<domain>.<kind>[<quoted-key>]
```

地址要满足：

- 同一个 desired resource 每次编译地址一致。
- 不同 resource 不能冲突。
- 适合作为 state key。
- 适合人读和错误定位。

修改地址会导致旧 state 中的资源变成 orphan，从而触发 destroy/forget 行为。除非明确做兼容迁移，
否则不要随意改地址格式。

## 依赖关系

`DependsOn` 用于保证执行顺序。例子：

- sysctl BBR 依赖 `tcp_bbr` module。
- apt repository source 依赖 signing key。
- package 依赖 apt cache refresh 或 repository。
- service 依赖对应 unit file。
- operation 依赖触发它的资源。

graph 只表达静态依赖。某个依赖是否真的需要执行，由 engine 根据 active plan 决定。

## Graph validate 和调度

`ResourceGraph.Validate` 会调用 `scheduleEntries` 和 `validateAcyclic`：

- 检查地址非空。
- 检查地址唯一。
- 检查 `DependsOn` 指向已知地址。
- 检查 `TriggeredBy` 指向已知地址。
- 检查依赖无环。

`Waves` 返回完整资源图的拓扑 waves。

`ActiveWaves(active)` 只对本次需要执行的地址调度。它会忽略未 active 的依赖，用于 apply 中只执行
有变化的节点和被触发的 operation。

## SafeParallelKind

`SafeParallelKind` 标记哪些资源适合在同一 host 上并行执行。当前 file-like、directory 和 component
artifact 类资源更容易并行；package、user、group、service 等默认更保守。

apply 调度会结合：

- 全局 `--parallel`。
- 每 host 并发槽。
- `SafeParallelKind`。

graph 只给出资源类型的并行安全提示，真正调度在 engine。

## 敏感 graph JSON

`Node.MarshalJSON` 会在节点是 content write only 或 sensitive 时清掉 `ProviderPayload`。

原因是 graph 可能用于测试或调试输出，provider payload 往往包含实际 `content`、`source_path` 等执行数据。
即使 IR 和 in-memory graph 需要内容来执行，也不能让 JSON 序列化默认泄漏。

## 设计边界

- graph 可以了解资源展开和依赖关系。
- graph 不应该读取 state 或 observed 状态。
- graph 不应该决定 action。
- graph 可以生成 command preview，但不应该执行命令。
- graph address 是稳定接口，改动要按兼容性处理。

## 修改检查清单

- 新增 resource kind：定义 address、kind、desired、provider type/payload、source、lifecycle。
- 新增依赖：补 graph validate 和 schedule 测试，避免环和未知依赖。
- 新增 operation：确认 `TriggeredBy`、`DependsOn`、plan 展示和 provider `RunOperation`。
- 改地址：检查 state orphan 后果，必要时设计迁移。
- 新增敏感 payload：确认 `Node.MarshalJSON`、plan diff、state sanitize 不泄漏。
