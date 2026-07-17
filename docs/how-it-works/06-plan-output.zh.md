# 06. Plan 文档、Diff 与输出格式

<p align="right"><a href="06-plan-output.md">English</a> | <strong>简体中文</strong></p>

本章解释 `internal/core/plan` 如何把资源图或 engine plan 转成用户可读和机器可读输出。

## 两种 plan 来源

DebianForm 有两条 plan document 生成路径：

```text
offline:
  graph.ResourceGraph -> plan.New -> plan.Document

online:
  engine.Plan -> engine.Plan.Document -> plan.Document
```

离线 plan 不知道远端现实，所以 `plan.New` 会把所有 graph node 当作 `create`。

在线 plan 已经由 engine 算出 action，所以 `engine.Plan.Document` 会保留 `create`、`update`、
`delete`、`adopt`、`forget`、`destroy`、`no-op` 等动作。

## Document 格式

`plan.Document` 的关键字段：

- `FormatVersion`：当前是 `debianform.plan.alpha1`。
- `GeneratedAt`：UTC RFC3339 时间。
- `Command`：本次命令的 file/host 上下文。
- `Summary`：create/update/delete/no-op/operations 统计。
- `Changes`：资源变更列表。
- `Operations`：将运行的 operation 列表。
- `Diagnostics`：诊断信息，目前默认空列表。

这个结构是 `dbf plan --format json` 的对外接口，字段变更要谨慎。

## Change

`plan.Change` 表示一个资源动作：

- `Host`
- `Address`
- `Action`
- `Summary`
- `Source`
- `ProviderAddress`
- `DeleteBehavior`
- `DeleteNotes`
- `DeleteRisk`
- `Diff`
- `LowLevelActions`

`ProviderAddress` 只有在 `--debug` 时输出，用于维护 provider 映射，不是普通用户入口。
Change 和 Operation 都携带显式 `Host`，供 JSON 消费者及 HTML host filter 使用，不依赖 address 字符串解析。
删除行为字段只在 `delete`、`destroy`、`forget` 类动作中输出，用于让 JSON、文本和 HTML renderer
复用同一份删除风险语义。

## Diff tree

`DiffNode` 是递归结构，用来表达值变化：

- `Path`
- `Kind`
- `Action`
- `Sensitive`
- `Before`
- `After`
- `BeforeSummary`
- `AfterSummary`
- `Children`
- `Hunks`

`BuildDiff(action, before, after)` 是核心入口。它会把 map/list/scalar/text/sensitive 内容组织成树。

文本内容会生成 hunk，便于 plan 文本和 HTML 展示文件内容变化。敏感内容则只展示摘要，不展示明文。

## Summary 统计

离线 `plan.New` 的 summary 很简单：

- 每个 node 都是 create。
- operation 数量来自 graph operations。
- update/delete/no-op 为 0。

在线 `engine.summarize` 会按 step action 统计：

- `create` -> create。
- `update` 和 `adopt` -> update。
- `delete`、`destroy`、`forget` -> delete。
- `no-op` -> no-op。
- operation step 数量 -> operations。

这里的 summary 是展示层统计，不直接决定 apply 行为。

## Text 输出

`PrintText` 输出：

- `Plan:` 标题。
- 每个 change 的动作符号和地址。
- summary 和 source。
- debug provider address。
- diff 子节点。
- operation 的触发和命令预览。
- 最终 summary。

动作符号：

- `+`：create/adopt。
- `~`：update。
- `-`：delete/destroy/forget。
- `=`：no-op。
- `!`：operation。

## JSON 输出

`PrintJSON` 直接对 `Document` 做缩进 JSON 编码。它是机器可读接口，使用方可能依赖字段名和 action 字符串。

改 JSON 结构前要检查：

- `docs/plan-format.md`
- CLI golden 或单元测试。
- redaction matrix。
- 下游自动化兼容性。

## HTML 输出

`PrintHTML` 使用内置 template 渲染静态 HTML。它和 text/JSON 使用同一个 `Document`，不重新计算 plan。

因此 HTML 的正确性依赖：

- `Document` 中的 diff 和 source 已经正确。
- template 不绕过脱敏字段。
- 文件写入逻辑只在 CLI `writePlanHTML` 里处理路径和目录创建。

## Source 展示

plan source 来自 parser 阶段产生并一路传递的 `SourceRef`。如果新增资源时没有正确设置 source，
plan 仍可工作，但用户和维护者无法定位资源来自哪个配置位置。

## 设计边界

- plan 层负责展示，不负责发现远端状态。
- plan 层不应该调用 provider 或 backend。
- plan diff 可以比较 before/after，但 before/after 应该由 engine 或 graph 传入。
- plan 层必须保守处理 sensitive/content write only。

## 修改检查清单

- 改 `Document` JSON：同步 `docs/plan-format.md` 和测试。
- 改 text 输出：更新 CLI/golden，并检查中文/英文用户文档是否受影响。
- 改 diff 算法：补 scalar、map、list、text、sensitive 用例。
- 新增 action：更新 action symbol、summary、engine、provider 和 docs。
- 改 HTML：生成示例检查敏感字段不泄漏。
