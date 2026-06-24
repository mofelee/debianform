# 08. Apply 执行模型、锁和调度

本章解释 `Engine.Apply` 如何执行变更。apply 是 DebianForm 中唯一会修改远端主机和远端 state 的核心路径。

## 数据流

```text
program + resourceGraph
  -> Backend.Lock(each host)
  -> persistHostFacts
  -> Engine.Plan
  -> Backend.Read(current state)
  -> executionWaves
  -> runExecutionWaves
  -> Provider.Apply/Destroy/RunOperation
  -> Backend.Write(state)
```

CLI 层会先打印一次在线 plan 给用户确认；`Engine.Apply` 内部仍会重新 plan。

## 为什么 apply 重新 plan

用户确认前打印的 plan 和真正执行之间可能发生变化：

- 其他进程修改了远端 state。
- 人手动改了主机。
- 上一次 apply 半途失败后又恢复了一部分资源。

`Engine.Apply` 获取 lock 后重新读取 state 和 observed 状态，可以让执行基于更接近当前现实的数据。

## Lock 顺序

`Engine.Apply` 会先对目标 host 调 `Backend.Lock`。当前 SSH backend 使用远端 lock path 和 lock dir：

- 成功创建 lock dir 后写 lock 文件。
- lock 包含 owner、pid、token、expires_at。
- 超时 stale lock 可以接管。
- unlock 时检查 token，避免误删别人的 lock。

lock 是 host 级别，不是资源级别。并发 apply 同一 host 会被挡住。

## Facts 持久化

`persistHostFacts` 会把已发现的 host facts 写入 state。这样 state 中保留最近一次在线发现的：

- hostname
- architecture
- codename
- detected_at

它在真正 plan 和执行前发生。写入 facts 不代表资源已经执行，只是保存运行期上下文。

## Execution waves

`executionWaves(resourceGraph, plan)` 会把本次 active step 和 operation 转成按依赖分组的 waves。

它处理两类地址：

- graph 中仍存在的 active step/operation：交给 `ResourceGraph.ActiveWaves` 拓扑排序。
- state orphan step：不在当前 graph 中，先放到最前面的 orphan wave。

这样 apply 既能删除/忘记孤儿资源，也能按当前资源图依赖执行正常变更。

## ActiveWaves 的含义

`ActiveWaves` 只调度本次要执行的地址。未变化的依赖不会被执行，但如果一个 active 节点依赖另一个
active 节点，会保证顺序。

例如：

- repository source 和 apt cache refresh 都 active，则 source 先执行。
- package active 但 repository 没变化，则 package 不会等待一个未 active 的 repository step。

## 并发控制

`runExecutionWaves` 使用两层 semaphore：

- 全局并发：`opts.Parallel`，对应 CLI `--parallel`。
- 每 host 并发：`opts.PerHostParallel`，当前默认 1。

每个 execution item 还会根据 `SafeParallelKind` 决定占用多少 host slots：

- safe parallel 资源占 1 个 host slot。
- 非 safe parallel 资源占满该 host 的所有 slots，相当于同 host 串行。

在当前默认每 host parallel 为 1 的情况下，同一 host 上实际仍偏串行；全局并发主要用于多 host。

## 失败传播

每个 wave 内的 runnable item 并发执行。执行后：

- 失败地址记录到 `failed`。
- 后续 wave 中依赖失败地址的 item 会被标记 blocked，不再执行。
- `runExecutionWaves` 返回第一个错误。

已经成功执行的资源不会回滚。DebianForm 依赖 state 和 observed 状态，让下一次 `plan/apply` 继续修复。

## Resource step 执行

`executeResourceStep` 根据 action 决定 provider 调用：

- `create`、`update`、`delete` -> `Provider.Apply`
- `destroy` -> `Provider.Destroy`
- `adopt` -> 不改远端，只写 state
- `forget` -> 只删 state
- `no-op` -> 不做事

成功后更新 state：

- `create`、`update`、`adopt` 写入 resource state。
- `delete`、`destroy`、`forget` 删除 resource state。
- `no-op` 不写。

每个资源成功后立即 `Backend.Write`，不是整次 apply 结束后统一写。这让中途失败后的 state 尽量反映已完成进度。

## Provider payload

执行前会通过 `providerStep` 把 node 转成 provider node。一般情况下 provider 使用 `ProviderPayload`
作为 desired。

例外是 content write only：为了避免把 write-only content 放入可持久化 desired，执行时保留原 node。

## Operation 执行

operation 由 `Provider.RunOperation` 执行。operation 成功或失败不会写入 state，因为它不是资源。

operation 的幂等性和安全性由生成它的 graph 逻辑和 provider 实现共同保证。例如 apt cache refresh、
daemon reload、service restart 都应当可重复运行。

## State 写入内容

`resourceStateForStep` 写入：

- host
- kind
- provider type/address
- ownership
- lifecycle
- sanitized desired
- desired digest
- sanitized observed
- updated_at
- order

desired digest 用未脱敏 desired 计算，但 state 中保存的是 sanitize 后的 desired。这样既能比较内容变化，
又不保存明文 content。

## 设计边界

- apply 不做回滚；失败后靠下一次 plan/apply 收敛。
- lock 是 host 级互斥，不能替代 provider 内部命令的幂等性。
- state 是进度记录，不是远端现实的唯一来源。
- provider action 必须可重复执行，适应部分成功后的重试。

## 修改检查清单

- 新增 action：更新 `executeResourceStep` 和 state 更新逻辑。
- 新增 operation：确认依赖、触发、provider `RunOperation` 和失败行为。
- 改并发：补 schedule/engine 测试，特别是同 host 非 safe parallel 资源。
- 改 state 写入：确认 sanitize、digest、serial、updated_at 和中途失败语义。
- 改 lock：补 SSH backend 测试，并确认 stale lock 和 token mismatch 行为。
