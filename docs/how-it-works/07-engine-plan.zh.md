# 07. 在线 Engine：读取状态、观测现实、计算动作

本章解释 `internal/core/engine.Engine.Plan` 如何在在线模式下计算真实动作。它是 DebianForm 区分
“期望配置”、“远端 state”和“主机现实”的关键层。

## 数据流

```text
ir.Program + graph.ResourceGraph
  -> Backend.Read(host state)
  -> Provider.Plan(node, prior)
  -> Compare(desired, prior, observed)
  -> orphanSteps
  -> operationSteps
  -> engine.Plan
```

`Engine.Plan` 不执行修改；它只读取 state、观测现实并计算步骤。`Engine.Plan` 本身不获取 state lock，
因此普通 `dbf plan` 仍是无锁读取。`dbf check` 使用 `Engine.Check`，先获取所有目标 host 的锁，再在
同一锁周期内运行完整的 `Engine.Plan`。

## Engine 依赖

`Engine` 有两个接口依赖：

- `Backend`：读写 state、获取 lock。
- `Provider`：观测资源、应用资源、删除资源、运行 operation。

在线 plan 只需要：

- `Backend.Read`
- `Provider.Plan`

`Engine.Check` 还需要 `Backend.Lock`，但仍然不会调用 write、apply、destroy 或 run operation。
apply 才会调用这些修改接口。

## State、desired、observed

每个资源动作由三类信息决定：

- desired：graph node 中当前配置希望的状态。
- prior：远端 state 中上次 DebianForm 记录的资源状态。
- observed：provider 从主机实际观测到的状态。

state 不是唯一事实来源。它记录 DebianForm 上次管理时的 desired digest、observed、ownership 等。
provider 仍然要观测主机，才能识别漂移和已有资源。

## Engine.Plan 主流程

`Engine.Plan` 做以下步骤：

1. 校验 backend/provider 非空。
2. 调 `resourceGraph.Validate`。
3. 对每台目标 host 调 `Backend.Read`。
4. 对每个 graph node 调 `Provider.Plan(ctx, node, prior)`。
5. 把非 no-op step 加入 `Steps`。
6. 记录会触发 operation 的 changed address。
7. 调 `orphanSteps` 处理 state 中有、desired 中没有的资源。
8. 调 `operationSteps` 根据 changed address 选择 operation。
9. 排序 step 和 operation。
10. 生成 summary。

`opts.Host` 会在读取 state、遍历 node、处理 orphan 和 operation 时过滤 host。operation 过滤直接比较
`graph.Operation.Host`，不会从 address 前缀推断执行目标。

## Engine.Check 的锁周期

`Engine.Check` 先按 `opts.Host` 选出目标 host，并在读取任何 state 前获取全部 host 的 state lock。
`opts.LockTimeout` 控制锁等待时间。全部锁获取成功后，它调用 `Engine.Plan`；state 读取、逐资源或
逐 host 的 provider 观测都在锁内完成，最后才释放所有锁。

锁租约丢失会取消正在执行的 plan。无论 plan 成功、失败或 context 被取消，engine 都会尝试释放
每个已获取的锁，并把租约或 unlock 错误与 plan 错误一起返回。这个包装只提供一致性边界，不会
持久化 host facts 或写入 state。CLI 为解析配置执行的 runtime facts discovery 发生在调用
`Engine.Check` 之前，因此不在锁周期内；不遵守 DebianForm state lock 的外部进程也不会被阻止。

## ProviderPlan

provider 返回 `ProviderPlan`：

- `Action`
- `Summary`
- `Observed`
- `Ownership`

如果 provider 没有给 action，engine 会视为 `no-op`。如果没有 summary，engine 用节点 summary 作为 fallback。

多数 provider 资源最终会调用 `Compare`，但 provider 也可以为特殊资源提供额外判断。

## Compare 的动作语义

`Compare(node, prior, observed)` 的核心规则：

- desired ensure absent 且 observed exists -> `delete`。
- desired ensure absent 且 observed 不存在 -> `no-op`。
- observed 不存在 -> `create`。
- observed digest 等于 desired digest，且 prior 为空 -> `adopt`。
- observed digest 等于 desired digest，且 prior 存在 -> `no-op`。
- prior 存在且 observed digest 不等于 prior digest -> `update`，summary 是 repair drift。
- 其他 digest 不一致 -> `update`。

`adopt` 表示主机上已有资源与 desired 一致，但 DebianForm state 里没有记录。apply 时会写入 state，
但不执行 provider 修改。

## Orphan 处理

orphan 是 state 中存在、但当前 desired graph 中不存在的资源。`orphanSteps` 会为它们生成：

- `destroy`：默认删除远端资源并删除 state。
- `forget`：只删除 state，不碰远端资源。

会选择 `forget` 的典型情况：

- prior ownership 是 `adopted`。
- `apt_source_file` 的 `on_destroy = keep`。
- directory 已被另一个 desired directory 继续管理。

如果 orphan 要 destroy 且 lifecycle prevent_destroy 为 true，plan 阶段直接报错。

## Operation 触发

`operationSteps` 会遍历 graph operations。只要 operation 的某个 `TriggeredBy` 地址在本次 changed 集合里，
就生成一个 `OperationStep{Action: run}`。

只有 `create`、`update`、`delete` 会触发 operation。`adopt`、`forget`、`destroy`、`no-op` 不在
`triggersOperation` 当前规则内。

这点很重要：operation 表达的是“配置导致的实际资源写入或删除后需要做的动作”，不是 state housekeeping。

## Plan.Document

`engine.Plan.Document` 把 engine step 转成 `plan.Document`。它会：

- 对每个 step 构造 before/after。
- destroy/forget 时从 prior desired 展示 before。
- 使用 `BuildDiff` 生成 diff。
- 为 change 和 operation 携带显式 host 及 operation 信息。
- 使用 engine summary。

如果 `--debug` 开启，它会输出 provider address。prior 中也可能补 provider address。

## 设计边界

- engine 负责 action 语义，不负责 HCL 解析。
- engine 不应该知道每个 provider resource 的 shell 细节。
- provider 负责观测现实，engine 负责把 provider plan 组织成全局 plan。
- orphan 和 lifecycle 规则属于 engine，因为它们依赖 state 和 desired 的关系。

## 修改检查清单

- 新增 action：更新 constants、Compare、summary、document、apply execute、plan 输出。
- 改 drift 判断：补 prior/observed/desired 组合测试。
- 改 orphan 策略：补 state test 和 apply no-op/destroy/forget 用例。
- 改 operation 触发：检查 apt/systemd/service/docker 相关行为。
- 改 host filter：确认 state read、node、orphan、operation 都一致过滤。
