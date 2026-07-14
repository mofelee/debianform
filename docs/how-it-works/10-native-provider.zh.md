# 10. Native Provider 与资源实现

本章解释 `NativeProvider` 如何把 graph node 转成受支持目标主机上的实际观测和修改。
provider 是最接近操作系统的一层。

## 数据流

```text
graph.Node + prior state
  -> NativeProvider.Plan
  -> ProviderPlan{Action, Observed, Ownership}

engine.Step
  -> NativeProvider.Apply / Destroy
  -> observed map
```

provider 不读取 HCL，也不写 state。它只通过 `Runner` 观察和修改远端主机。

## Provider 接口

`Provider` 定义：

- `Plan(ctx, node, prior)`：观测远端资源，返回建议 action。
- `Apply(ctx, step)`：执行 create/update/delete 类动作，返回 observed。
- `Destroy(ctx, step)`：销毁 orphan managed 资源。
- `RunOperation(ctx, operation)`：执行 graph operation。

engine 负责根据这些结果组织全局 plan 和写 state。

## Dispatch by Kind

`NativeProvider.Plan` 和 `Apply` 都按 `node.Kind` 分发。当前覆盖的资源包括：

- file-like：`file`、`secret`、`systemd_unit`、`nftables_file`、`networkd_netdev`、`networkd_network`。
- APT：`apt_source_file`、`apt_signing_key`、`package`。
- component artifact：download、build、binary、file、archive、CA certificate。
- system：directory、kernel module、sysctl。
- identity：group、user、group membership、authorized key。
- service。
- docker package conflicts 和 compose project。

新增 kind 时必须同时考虑 Plan、Apply、Destroy 和 graph 生成。

## Plan 的职责

provider plan 的核心是读取当前主机状态。例如：

- file-like 读取 path 是否存在、类型、sha256、owner、group、mode。
- package 检查 dpkg 状态。
- service 检查 enabled/running。
- group/user 检查系统数据库。
- docker compose project 检查 compose 输出或期望标记。

然后 provider 返回 `ProviderPlan`。很多资源最终遵循 engine `Compare` 的 desired/prior/observed digest
逻辑，但 file-like 等资源会直接根据文件 sha、权限和 write-only 规则判断。

## File-like 资源

`planFileLike` 是最重要的模式之一：

- 如果 ensure absent 且文件存在 -> `delete`。
- 如果 ensure absent 且文件不存在 -> no-op 或 absent in sync。
- 如果文件不存在 -> `create`。
- 如果内容、owner、group、mode 不一致 -> `update`。
- 否则 -> in sync。

`content_write_only` 特殊处理：provider 不读取远端内容 sha 来比较明文内容，而是依赖 prior desired digest
和权限/类型观测。这样可以写入内容，但不把内容当作普通可读状态。

`applyFileLike` 会写内容、owner、group、mode。systemd unit 变更后会执行 `systemctl daemon-reload`。

## APT source file 的特殊语义

`apt_source_file` 支持 `on_destroy`：

- `keep`：配置移除或 ensure absent 时偏向 forget，不恢复原文件。
- `restore`：删除时尝试恢复管理前的原始内容。

因此它的 plan/apply/destroy 比普通 file 多了 original content/owner/group/mode 的 observed 处理。

## Destroy

`NativeProvider.Destroy` 使用 prior state 中保存的 desired 来决定如何删除资源。常见行为：

- file-like 删除 path。
- directory 删除目录，但保护空路径和 `/`。
- package 执行 `apt-get remove`。
- service 执行 `systemctl disable --now`。
- docker compose project 把 state 改成 absent 后运行 compose down 类命令。

`Destroy` 只用于 orphan managed resource。普通 ensure absent 的删除通常走 `Apply`。

## RunOperation

`RunOperation` 使用 `operation.Host` 作为远端目标，然后执行 `operation.CommandPreview`。Host 缺失时会
保守失败；address 只作为稳定身份和诊断上下文，不参与 SSH 路由。

这意味着 graph 生成的 command preview 不是纯展示字符串，它也是当前 native operation 的执行命令。
新增 operation 时要确保 command preview 可执行、幂等，并且不包含敏感明文。

## Helper 约定

provider 内部大量 helper 做以下事情：

- shell quoting。
- 读取 path metadata/content。
- 写入文件内容。
- 计算 desired content sha。
- 生成 package/service/docker 命令。
- 归一化 mode。
- 从 desired map 取 string/bool/list。

新增 provider 行为时应复用 helper，避免自己拼接不安全 shell。

## Observed map

provider 返回的 observed 会进入 plan diff 和 state sanitize。observed 应该包含足够判断后续漂移的信息，
但不能包含不该持久化的敏感明文。

如果确实需要保存恢复信息，例如 apt source original content，需要确认该资源的安全边界和 redaction 测试。

## 设计边界

- provider 可以执行远端命令，但不写 state。
- provider 不决定 orphan 策略，那是 engine 的职责。
- provider 不解析 HCL 或 profile。
- provider 返回 action/observed，但全局排序和 operation 触发由 engine/graph 决定。
- provider 命令必须尽量幂等，适应 apply 中途失败后重试。

## 修改检查清单

- 新增 kind：更新 graph node、provider Plan/Apply/Destroy、tests、golden。
- 新增 shell 命令：使用 `shellQuote` 或 stdin，避免注入和 secret 出现在命令行。
- 新增 observed 字段：确认 state sanitize 和 plan diff 不泄漏。
- 新增 operation：确认 command preview 可执行且幂等。
- 改 file-like 逻辑：重点测试 content、write-only、sensitive、mode/owner/group、absent。
