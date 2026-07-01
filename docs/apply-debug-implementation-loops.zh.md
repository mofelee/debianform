# apply --debug 实施循环拆分

本文档把 [apply SSH 调试器需求文档](apply-debugger-requirements.zh.md) 拆成可验证的开发 loop。
每个 loop 都必须形成一个可合并闭环：

- 本轮能力能独立运行或通过 fake runner 验证。
- 单元测试覆盖本轮新增语义。
- 文档和 help 文案在能力暴露时同步更新。
- `go test ./...` 通过；必要时补 `go vet ./...`。

状态约定：

- `[ ]` 未完成
- `[x]` 已完成

## 当前基线

- [x] `Runner` 已统一封装远端调用：`Run`、`RunInput`、`RunCommand`。
- [x] `apply` 会先打印 online plan，确认后再进入真正执行。
- [x] `Engine.Apply` 内部会重新 plan，并在每个资源成功后写 state。
- [x] `plan --debug` 已显示内部 `provider_address`。
- [x] progress 日志已能显示 host、动作、资源地址和摘要。
- [x] 普通 plan/state 输出已有敏感值脱敏约束。

## Loop 1: CLI 入口和语义边界

目标：让 `dbf apply --debug` 成为合法入口，同时保持 `plan --debug` 的既有语义。

范围：

- `plan --debug` 继续只显示 provider address。
- `apply --debug` 进入调试模式，但本轮可以先只打印“调试模式已启用”的占位信息。
- `check --debug`、`validate --debug` 报错。
- `apply --debug --parallel 2` 报错；`--parallel 1` 可接受。
- usage/help 文案说明 `--debug` 在 `plan` 和 `apply` 中含义不同。

暂不做：

- 不拦截 SSH 调用。
- 不实现交互 prompt。
- 不打印脚本或 payload。

代码：

- [ ] 调整 `cmd/dbf/main.go` 的 flag 校验，让 `--debug` 同时支持 `plan` 和 `apply`。
- [ ] 在 apply 分支增加 debug mode 选项传递结构，但不改变普通 apply 行为。
- [ ] 调试模式下强制 `parallel <= 1`。
- [ ] usage 文本更新为 `dbf apply ... [--debug]`。

测试：

- [ ] CLI 测试覆盖 `apply --debug` 可解析。
- [ ] CLI 测试覆盖 `plan --debug` 仍输出 `provider_address`。
- [ ] CLI 测试覆盖 `check --debug`、`validate --debug` 报错。
- [ ] CLI 测试覆盖 `apply --debug --parallel 2` 报错。

验收：

```bash
go test ./cmd/dbf
go test ./...
```

## Loop 2: DebugRunner 骨架和非交互记录模式

目标：实现包装 `Runner` 的 `DebugRunner`，先以非交互方式记录并打印远端调用详情。

范围：

- `DebugRunner` 实现 `Runner` 接口。
- 拦截 `Run`、`RunCommand`、`RunInput`。
- 每次调用前打印序号、host、runner 方法、remote command/script。
- 每次调用后打印成功/失败、耗时、stdout/stderr 摘要。
- `RunInput` 读取 input 到内存后再交给 inner runner。
- 调试输出写 stderr。

暂不做：

- 不实现 prompt。
- 不实现 `step` / `next` / `continue`。
- 不实现 retry。
- 不接入真实 apply 全流程；可先用 fake runner 单测。

代码：

- [ ] 新增 `internal/core/engine/debug_runner.go` 或等价文件。
- [ ] 新增 `DebugSession`，负责序号、输出 writer、结果格式化。
- [ ] 实现短文本 payload/stdout/stderr 的完整展示。
- [ ] 空 stdout/stderr 显示 `<empty>`。
- [ ] 为 `RunInput` 保留 payload bytes，确保执行和后续展示一致。

测试：

- [ ] fake runner 单测覆盖 `Run`、`RunCommand`、`RunInput` 都被转发。
- [ ] 单测断言输出包含 host、runner、script/command、stdout、stderr。
- [ ] 单测断言 `RunInput` 的 payload 被传给 inner runner 且可打印。
- [ ] 单测断言错误会返回原错误，并打印 failed 结果。

验收：

```bash
go test ./internal/core/engine -run Debug
go test ./...
```

## Loop 3: 长内容和二进制输出策略

目标：实现友好的内容展示策略，避免长 payload 或二进制内容自动刷屏。

范围：

- 短的可打印 UTF-8 文本默认完整显示。
- 长文本默认显示 length、sha256、行数、预览和展开提示。
- 非 UTF-8 或明显控制字符内容默认显示 binary 摘要和展开提示。
- 实现 `show stdin`、`show stdout`、`show stderr` 的底层格式化能力，先通过单测直接调用。
- 二进制展开使用 hex dump，不向终端写原始 bytes。

暂不做：

- 不实现交互 prompt 命令解析。
- 不在真实 apply 中等待用户输入。

代码：

- [ ] 增加 payload 分类函数：短文本、长文本、二进制。
- [ ] 增加 sha256、byte length、line count 计算。
- [ ] 增加 preview 生成逻辑，建议阈值为 8 KiB 或 200 行。
- [ ] 增加完整展开渲染函数，二进制输出 hex dump。

测试：

- [ ] 短文本默认完整显示。
- [ ] 超过阈值的长文本默认只显示摘要和 preview。
- [ ] 非 UTF-8 payload 默认只显示 binary 摘要。
- [ ] `show stdin` 的渲染函数可输出完整文本或完整 hex dump。
- [ ] stdout/stderr 使用同一策略。

验收：

```bash
go test ./internal/core/engine -run 'Debug|Payload'
go test ./...
```

## Loop 4: 交互 prompt、step/next/continue/show/quit

目标：实现 GDB 风格交互控制，但暂不做失败 retry。

范围：

- 调试 prompt 为 `(dbfdbg)`。
- 支持 `step` / `s`。
- 支持 `next N` / `n N`，例如 `next 5`、`n 10`。
- 支持 `continue` / `c`。
- 支持 `list` / `l` / `show` 重新打印当前调用详情。
- 支持 `show stdin`、`show stdout`、`show stderr` 展开内容。
- 支持 `quit` / `q` 取消 apply。
- 空回车初始等价于 `step`，之后重复上一个执行命令。

暂不做：

- 不实现失败后 retry。
- 不区分 cleanup 调用自动执行；下一轮处理。
- 不接入所有 apply 阶段；先用 DebugRunner 单测验证。

代码：

- [ ] `DebugSession` 增加 input reader。
- [ ] 实现命令解析和状态机。
- [ ] `BeforeCall` 根据当前模式决定是否等待 prompt。
- [ ] `continue` 模式仍打印调用详情和结果，但不等待输入。
- [ ] `quit` 返回明确取消错误，例如 `apply cancelled by debugger`。

测试：

- [ ] fake stdin 输入 `step`，只执行一个调用后下一次继续等待。
- [ ] fake stdin 输入 `next 5`，连续放行 5 个调用。
- [ ] fake stdin 输入 `continue`，后续调用不再等待输入。
- [ ] fake stdin 输入 `show stdin`，展开当前 payload 但不执行调用。
- [ ] fake stdin 输入 `quit`，返回取消错误且不执行当前调用。
- [ ] 未知命令不执行调用，并打印帮助提示。

验收：

```bash
go test ./internal/core/engine -run Debug
go test ./...
```

## Loop 5: 接入 apply 全流程

目标：让 `dbf apply --debug` 真正包裹 SSH runner，并覆盖 facts、online plan、lock、re-plan、apply、state write。

范围：

- `loadOnlineProgramWithProgress` 或其调用方支持传入 debug-wrapped runner。
- facts discovery 使用 debug runner。
- online plan 使用 debug runner。
- apply 的 lock/read/write/re-plan/provider apply/operation 使用 debug runner。
- 调试输出走 stderr。
- 调试模式下 facts discovery、engine plan/apply 并发全部固定为 1。
- 启动时打印高风险 warning。

暂不做：

- 不精细标注 phase/address；下一轮做。
- 不实现 retry。
- cleanup/unlock 可先跟普通调用一样进入 prompt；下一轮修正。

代码：

- [ ] 在 CLI apply 分支创建 `DebugSession`。
- [ ] 用 `DebugRunner` 包装 `SSHRunner`，同时保留底层 `SSHRunner.Close`。
- [ ] 调试模式传入 `factsParallel=1`。
- [ ] 调试模式传入 `coreengine.Options{Parallel: 1, PerHostParallel: 1}`。
- [ ] 确保普通 `apply` 未启用 debug 时无行为变化。

测试：

- [ ] CLI/fake runner 测试验证 `apply --debug` 的 stderr 出现调试 warning。
- [ ] 测试验证 debug 模式下 stdout 仍是 plan/apply 正文。
- [ ] 测试验证 debug 模式设置并发为 1。
- [ ] 测试验证普通 apply 不输出调试内容。

验收：

```bash
go test ./cmd/dbf
go test ./internal/core/engine
go test ./...
```

可手工验证：

```bash
dbf apply -f examples/bbr.dbf.hcl --debug --auto-approve
```

## Loop 6: 远端调用上下文标注

目标：让每个调试步显示清楚来源阶段、资源地址、动作和摘要。

范围：

- 标注 facts discovery：`discover facts`。
- 标注 SSH backend：`state lock`、`state read`、`state write`、`state unlock`。
- 标注 provider plan：`plan inspect`。
- 标注 resource apply/destroy：`apply resource`。
- 标注 operation：`run operation`。
- 标注 operation output read：`operation output read`。
- 没有上下文时显示 `phase: remote call`。

暂不做：

- 不改变 provider 的脚本内容。
- 不拆分 shell 行级步骤。

代码：

- [ ] 新增 `RemoteCallContext` 和 `WithRemoteCallContext` / getter。
- [ ] 在 facts、backend、engine execute、provider plan/apply 关键入口注入上下文。
- [ ] `DebugRunner` 从 context 读取并打印 phase/address/action/summary。
- [ ] state unlock 设置 cleanup 标志，为下一轮自动放行准备。

测试：

- [ ] 单测覆盖 context 注入后调试输出包含 phase/address/action/summary。
- [ ] 单测覆盖无 context 时 fallback 为 `remote call`。
- [ ] 单测覆盖 state read/write/lock/unlock phase。
- [ ] 单测覆盖 apply resource 和 operation phase。

验收：

```bash
go test ./internal/core/engine -run 'Debug|RemoteCallContext'
go test ./...
```

## Loop 7: cleanup 自动执行和失败 retry

目标：完善失败和退出路径，避免用户 quit 后遗留 state lock，并支持失败调用重试。

范围：

- `state unlock` 这类 cleanup 调用完整打印但不进入 prompt。
- 用户 `quit` 后不再执行普通调用，但 cleanup 调用仍尽力执行。
- 远端调用失败后进入 `(dbfdbg failed)`。
- failed prompt 支持 `show`、`show stdin`、`show stdout`、`show stderr`、`retry`、`quit`、`help`。
- `retry` 重跑同一个远端调用，使用同一 script/stdin payload。
- 失败状态下不允许 `step`、`next`、`continue`。

暂不做：

- 不支持 skip。
- 不支持 breakpoint。

代码：

- [ ] `DebugRunner` 对失败调用保存 last call 和 last result。
- [ ] 失败后进入 retry loop。
- [ ] `RunInput` retry 使用内存中的同一 payload。
- [ ] cleanup 调用绕过 prompt，但仍打印详情和结果。
- [ ] quit/cancel 后 cleanup 仍可执行。

测试：

- [ ] fake runner 第一次失败、retry 成功。
- [ ] fake runner retry 仍使用同一 stdin payload。
- [ ] 失败状态输入 `step` / `next` / `continue` 不执行调用。
- [ ] quit 后普通调用不执行，cleanup 调用仍执行。
- [ ] cleanup 调用不等待 prompt。

验收：

```bash
go test ./internal/core/engine -run Debug
go test ./...
```

## Loop 8: 文档、真实场景验收和收口

目标：把能力收口成用户可用的完整交付，更新用户文档，并做至少一个端到端验证。

范围：

- 更新 CLI 文档。
- 更新 operations runbook。
- 更新 apply scheduler how-it-works。
- 为 `apply --debug` 增加一个真实或 fake integration 场景。
- 确认调试模式不破坏现有敏感值默认脱敏测试。

暂不做：

- 不实现 breakpoint。
- 不实现行级 shell 调试。
- 不实现文件日志输出。

代码：

- [ ] `docs/cli.zh.md` 记录 `apply --debug`。
- [ ] `docs/operations-runbook.zh.md` 增加调试 apply 的排障用法。
- [ ] `docs/how-it-works/08-apply-scheduler.zh.md` 补充 debug runner 在 apply 流程中的位置。
- [ ] 如果需要，README 或用户手册增加一段简短入口说明。

测试：

- [ ] `make test`。
- [ ] `go vet ./...`。
- [ ] 若环境允许，选一个 libvirt case 用 `--debug --auto-approve` 和 `continue` 跑通。
- [ ] 手工验证长文本和二进制 payload 不自动刷屏，`show stdin` 可展开。

验收：

```bash
make test
go vet ./...
test -z "$(gofmt -l $(git ls-files '*.go'))"
```

可选真实验证：

```bash
dbf apply -f examples/bbr.dbf.hcl --debug --auto-approve
```

在 prompt 输入：

```text
next 5
continue
```

期望：

- 调试器从 facts discovery 开始展示远端调用。
- `next 5` 后停在第 6 个远端调用前。
- `continue` 后不再暂停。
- apply 成功后没有遗留 state lock。

## 后续增强候选

这些能力不进入第一版 loop：

- breakpoint：按 host、phase、address、command substring 停住。
- `until phase/address`：连续执行到指定阶段或资源。
- `set print stdin off`：会话内临时关闭 stdin 展示。
- `set print stdout limit N`：动态调整长输出阈值。
- `save log path`：调试输出写文件。
- `skip`：跳过失败调用继续执行，需要非常明确的 state 风险提示。
- 行级 shell 调试：需要 provider 脚本结构化重构。
