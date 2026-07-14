# 04. IR 数据模型与资源边界

本章解释 `internal/core/ir` 的职责。IR 是 DebianForm 的领域层模型：它已经脱离原始 HCL，但还不是
provider 操作，也不是 state。

## 数据流位置

```text
parser.Config
  -> merge.CompileWithOptions
  -> ir.Program
  -> graph.Compile
```

IR 的上游是 parser/merge，下游是 graph。它的任务是表达“用户希望每台 host 成为什么样子”。

## Program

`ir.Program` 是编译后的完整配置：

- `Hosts`：每台目标主机的期望状态。
- `Variables`：变量定义元数据。
- `Components`：component 模板元数据。

`Program` 不包含 plan action。某个资源到底是 `create`、`update` 还是 `no-op`，必须等 engine 读取
state 和 observed 状态后才能知道。

## HostSpec

`HostSpec` 是 IR 的中心结构。它包含：

- `Name`
- `Source`
- `Facts`
- `SSH`
- `State`
- `System`
- `Kernel`
- `Packages`
- `APT`
- `Files`
- `Secrets`
- `Directories`
- `Groups`
- `Users`
- `Systemd`
- `Services`
- `Nftables`
- `Docker`
- `Components`

这些字段按领域组织，而不是按底层命令组织。例如 `APTSpec` 表达 repository 和 source file，
但不直接说要运行哪条 `apt-get update`；`SystemdSpec` 表达 unit 内容，但不直接说什么时候
`daemon-reload`。这些执行细节属于 graph/provider。

## SourceRef

几乎所有 spec 都带 `SourceRef`：

- `File`
- `Line`
- `Path`

它用于：

- 编译错误定位。
- lifecycle 错误定位。
- plan change 中显示 source。
- golden 测试稳定断言。

新增 IR 字段时，只要它来自用户配置，就应该尽量保留 source。

## LifecycleSpec

`LifecycleSpec` 当前核心字段是 `PreventDestroy`。它在两个地方发挥作用：

- graph node 上保留 lifecycle。
- engine 计算 destroy/delete 时检查 `prevent_destroy`。

注意：lifecycle 是资源语义，不是 provider 命令细节。provider 不应该自行决定绕过
`prevent_destroy`。

## Host facts

DSL 中目标平台 facts 写作 `platform.distribution` / `platform.version` /
`platform.architecture` / `platform.codename`。IR 的 `HostFacts`
当前仍通过 `System` 字段保存探测结果，沿用现有 provider/state facts schema：

- hostname
- distribution
- version
- architecture
- codename
- detected_at

facts 可以来自用户声明，也可以由在线模式发现后注入。IR 持有 facts 的目的是让后续 graph 和 component
实例化可以基于稳定字段决策，而不是到处调用 SSH。

## Domain spec 与 provider resource 的差异

IR 的 domain spec 不等于 graph node：

- 一个 `APTRepositorySpec` 可能展开成 signing key file、repository source file 和 apt cache refresh operation。
- 一个 `SystemdUnit` 可能展开成 file-like resource，并触发 daemon reload 或 service restart。
- `DockerSpec` 会展开出 package、repository、daemon config、service、compose plugin 等多个节点。
- 一个 component instance 会携带多组领域资源和 artifact 资源。

这种分层让用户 DSL 保持简洁，也让 provider 实现可以共享低阶资源模型。

## ContentSummary

文件类资源常见字段里有 `ContentSummary`。它用于在不直接暴露内容的情况下记录摘要信息，例如内容长度、
hash 或来源。具体脱敏策略在 graph、plan 和 state 层继续执行。

原则是：IR 可以承载必要内容用于后续执行，但任何会序列化到公开输出的路径都必须经过脱敏检查。

## SSHSpec 和 StateSpec

`SSHSpec` 表达连接目标：

- `host`
- `port`
- `user`
- `identity_file`

`StateSpec` 表达远端 state 和 lock 位置：

- `path`
- `lock_path`

它们属于 host 级配置，在线模式第一阶段编译必须能得到这些字段，否则无法连接主机发现 facts。

## Components in HostSpec

`HostSpec.Components` 是已经实例化到 host 上的 component instance 输出。它保留 component name，
并携带该实例产生的各类资源 spec。

graph 层会用 component prefix 生成地址，例如：

```text
host.<host>.components.<instance>....
```

因此 component instance name 是资源地址稳定性的一部分。

## 设计边界

- IR 应该稳定、可 JSON 序列化、适合 golden 测试。
- IR 不应该混入 provider command preview、SSH 命令、远端 observed 状态。
- IR 可以包含 desired content，但要清楚哪些下游输出会脱敏。
- graph 地址稳定性依赖 IR 字段，改字段默认值或排序要谨慎。

## 修改检查清单

- 新增 IR 字段：同步 JSON tag、merge build、graph compile、golden。
- 改 HostSpec 默认值：检查 offline plan、online plan、state digest 是否受影响。
- 新增 domain spec：先定义用户语义，再决定 graph node 如何展开。
- 改 source 传播：检查 plan 文本、JSON 和错误信息。
- 改 facts 字段：更新 facts discovery、state facts 持久化和依赖 facts 的编译逻辑。
