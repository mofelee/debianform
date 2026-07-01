# apply SSH 调试器需求文档

本文描述 `dbf apply` 的交互式 SSH 调试器需求。目标是让用户像使用 GDB 一样观察和控制
DebianForm 在远端主机上执行的每一次 SSH 调用，支持单步执行、一次执行多步、继续执行、
查看脚本详情、查看远端输出，以及失败后重试。

该功能面向排查和学习 DebianForm apply 行为，不是日常 apply 的默认交互方式。

## 背景

DebianForm 的常规 `apply` 流程会先生成在线 plan，用户确认后再真正修改远端主机。执行时，
CLI 会通过进度日志展示当前 host、资源地址、动作和摘要，但不会展示每一次 SSH 调用的完整
脚本，也不会允许用户在两个远端调用之间暂停。

当前 apply 的内部流程大致是：

```text
parse config
  -> discover runtime facts
  -> compile program
  -> compile resource graph
  -> online plan
  -> print plan
  -> confirm
  -> lock state
  -> persist facts
  -> re-plan
  -> read state
  -> execute resource/operation waves
  -> write state after successful steps
  -> unlock state
```

其中许多阶段都会通过 SSH 在远端执行脚本：

- runtime facts discovery 会读取 hostname、Debian architecture、codename。
- online plan 会读取远端 state 和 observed 状态，例如文件 hash、package 状态、service 状态。
- apply 会运行 provider 生成的修改脚本，例如写文件、安装包、重启服务、执行 component script。
- apply 成功后会写回远端 state。

用户希望新增一种调试模式，能清楚看到每一步到底执行了什么远端命令，并能像 GDB 一样：

- 一次执行 1 个远端调用。
- 一次执行 5 个或 10 个远端调用。
- 继续执行到结束。
- 在失败时查看上下文并重试。

## 目标

- 为 `dbf apply` 增加交互式 SSH 调试模式。
- 调试模式覆盖 apply 相关全流程中的远端 SSH 调用，包括 facts discovery、state lock/read/write、
  online plan probes、apply resource、operation、operation output read 和 unlock。
- 每次远端调用执行前，展示 host、阶段、来源、完整远端脚本或命令。
- 每次远端调用执行后，展示 exit 结果、耗时、stdout、stderr。
- 支持 GDB 风格命令：`step`、`next N`、`continue`、`list/show`、`retry`、`quit`、`help`。
- 支持一次执行多个远端调用，例如 `next 5`、`next 10`。
- 失败后停住，允许查看详情、重试或退出。
- 调试输出走 stderr，保持 stdout 仍用于 plan/apply 正文输出。
- 调试模式下强制串行执行，保证交互顺序和输出顺序可理解。

## 非目标

- 不把 shell 脚本内部的每一行改造成独立可暂停的步骤。
- 不在第一版实现断点表达式，例如按 host、resource address、script pattern 设置 breakpoint。
- 不在第一版实现 watchpoint 或变量检查。
- 不在第一版实现跳过失败远端调用后继续执行。
- 不改变普通 `dbf apply` 的默认行为。
- 不改变 `dbf plan --debug` 的现有含义。
- 不修改 plan JSON schema、HTML plan schema 或 state 文件格式。

## 名词定义

### 远端调用

调试器里的“一步”定义为一次 `engine.Runner` 调用：

- `Run(ctx, host, script)`
- `RunInput(ctx, host, remoteCommand, input)`
- `RunCommand(ctx, host, remoteCommand)`

这些调用最终都会通过 SSH 在远端主机执行命令。调试模式按这些调用暂停，而不是按 shell 脚本
中的单行命令暂停。

### 调试步

一个调试步对应一个远端调用。它包含：

- 调用序号。
- host。
- 阶段，例如 `discover facts`、`plan inspect`、`state write`、`apply resource`。
- 资源地址或 operation 地址，如有。
- 资源动作和摘要，如有。
- runner 方法：`Run`、`RunInput` 或 `RunCommand`。
- 远端命令或脚本。
- stdin payload，如有。
- 执行结果：exit 状态、耗时、stdout、stderr、错误信息。

### 调试会话

一次 `dbf apply --debug` 运行就是一个调试会话。会话从第一次远端 SSH 调用前开始，到 apply
完成、失败退出、用户 quit 或上下文取消为止。

## CLI 入口

新增选项：

```bash
dbf apply --debug
```

示例：

```bash
dbf apply -f examples/bbr.dbf.hcl --debug
dbf apply -f base.dbf.hcl -f host.dbf.hcl --host web1 --debug
dbf apply --debug --auto-approve
```

`--debug` 在不同子命令中有不同含义：

- `dbf plan --debug`：显示 plan 的内部 provider address。
- `dbf apply --debug`：进入交互式 SSH 调试器。

`check`、`validate` 不支持 `--debug`。以下命令应报错：

```bash
dbf check --debug
dbf validate --debug
```

错误信息建议：

```text
--debug is only supported for plan and apply
```

## 与现有 `plan --debug` 的关系

现有 `dbf plan --debug` 只用于 plan 输出，作用是显示内部 provider address，便于排查资源图和
provider 映射。它不进入交互会话，也不改变执行行为。

新增 `dbf apply --debug` 是交互式远端调用调试器。二者复用同一个 flag 名称，但语义按子命令区分：

- `plan --debug`：增强 plan 展示。
- `apply --debug`：控制 apply 期间每一次 SSH 调用。

文档和 help 文案必须明确这一点，避免用户以为 `apply --debug` 只是增加 provider address 输出。

## 高风险输出策略

`apply --debug` 是显式高风险调试模式。调试器会展示远端调用细节，并允许用户展开完整 payload
和输出。这些内容可能包含 secret。

默认展示规则：

- remote command 和 shell script 通常较短，默认完整显示。
- `RunInput` 的 stdin payload 如果是短文本，默认完整显示。
- stdin payload 如果过长或不是可打印 UTF-8，默认只显示摘要，并提示用户输入 `show stdin` 展开。
- stdout/stderr 如果较短，默认完整显示。
- stdout/stderr 如果过长或不是可打印 UTF-8，默认只显示摘要，并提示用户输入 `show stdout` 或
  `show stderr` 展开。

这可能包含：

- secret file 内容。
- sensitive variable 值。
- SSH authorized key。
- component script body。
- state JSON 内容。
- 远端命令输出中的敏感数据。

启动调试会话时必须在 stderr 打印醒目的风险提示：

```text
dbf debugger: WARNING: apply --debug can print remote scripts, stdin payloads,
stdout, and stderr. Expanded output may contain secrets. Do not paste debugger
logs into issues, CI artifacts, or shared chat without review.
```

第一版不再设计额外的 `--debug-sensitive` 或环境变量确认。用户选择 `apply --debug` 即表示接受
调试输出风险；对于长内容和二进制内容，调试器仍应先给摘要和展开提示，而不是自动刷屏。

普通 `plan`、普通 `apply`、state 持久化、plan JSON/HTML 的脱敏行为必须保持不变。只有
显式 `apply --debug` 的 stderr 调试输出允许暴露原始内容。

## 覆盖范围

调试器应覆盖 apply 命令生命周期中的所有远端 SSH 调用。

### 覆盖的阶段

| 阶段 | 是否暂停 | 说明 |
| --- | --- | --- |
| facts discovery | 是 | 展示收集 hostname、architecture、codename 的脚本和输出。 |
| state lock | 是 | 展示获取 lock 的脚本和输出。 |
| state read | 是 | 展示读取 state 的脚本和输出。 |
| state write | 是 | 展示写 state 的脚本和 payload。 |
| online plan inspect | 是 | 展示 plan 阶段读取 observed 状态的脚本和输出。 |
| apply resource | 是 | 展示真正修改资源的脚本。 |
| run operation | 是 | 展示 operation 命令或 component script。 |
| operation output read | 是 | 展示读取 script outputs 的脚本和输出。 |
| state unlock | 自动执行 | 展示 unlock 调用，但不要求用户确认，以避免 quit 或失败后遗留 lock。 |

`state unlock` 是唯一例外。它仍应完整打印脚本和结果，但在 defer 清理路径中自动执行，不进入
prompt 等待用户输入。

### 不覆盖的本地阶段

这些阶段不属于远端 SSH 调用，不进入调试步：

- HCL parse。
- variable load。
- component merge。
- ResourceGraph compile。
- plan document rendering。
- 用户确认 apply 的普通确认问题。

## 并发规则

调试模式必须保证远端调用顺序稳定、输出不交错。因此：

- `--debug` 下远端 SSH 调用强制串行。
- facts discovery 并发固定为 1。
- engine apply options 使用 `Parallel: 1` 和 `PerHostParallel: 1`。
- 如果用户同时传入 `--debug --parallel N` 且 `N > 1`，命令应报错。

建议错误信息：

```text
--debug cannot be combined with --parallel greater than 1
```

如果用户传入 `--parallel 1`，可以接受。

## 调试器交互

调试器提示符：

```text
(dbfdbg)
```

每个远端调用执行前打印调用详情，然后等待命令。

### 支持命令

| 命令 | 别名 | 说明 |
| --- | --- | --- |
| `step` | `s` | 执行当前远端调用，然后停在下一个远端调用前。 |
| `next N` | `n N` | 连续执行 N 个远端调用，然后停住。N 必须大于等于 1。 |
| `next` | `n` | 等价于 `next 1`。 |
| `continue` | `c` | 后续不再暂停，直到失败或 apply 完成；仍打印每个调用详情和结果。 |
| `list` | `l` | 重新打印当前远端调用详情。 |
| `show` | 无 | 重新打印当前远端调用详情；失败后也打印上次 stdout/stderr。 |
| `show stdin` | 无 | 展开当前调用的完整 stdin；长文本按文本打印，二进制按 hex dump 打印。 |
| `show stdout` | 无 | 展开上一次执行结果的完整 stdout；长文本按文本打印，二进制按 hex dump 打印。 |
| `show stderr` | 无 | 展开上一次执行结果的完整 stderr；长文本按文本打印，二进制按 hex dump 打印。 |
| `retry` | `r` | 仅失败后可用，重新执行当前失败调用。 |
| `quit` | `q` | 取消 apply，触发必要 cleanup/unlock。 |
| `help` | `h` | 打印调试器命令帮助。 |

空回车重复上一个执行命令。会话刚开始时，空回车等价于 `step`。

### 命令解析规则

- 命令大小写不敏感。
- 前后空白忽略。
- `next N` 中 `N` 必须是十进制正整数。
- 未知命令不执行远端调用，只打印帮助提示并继续等待输入。
- 在非失败状态输入 `retry` 应提示 `retry is only available after a failed remote call`。
- 对不存在的内容输入展开命令，例如没有 stdin 时输入 `show stdin`，应提示 `current call has no stdin`。

## 调用详情展示

每个远端调用执行前，stderr 输出建议格式：

```text
dbf debugger: #12 before remote call
  phase: apply resource
  host: web1
  address: host.web1.files.file["/etc/app.conf"]
  action: update
  summary: update file /etc/app.conf
  runner: RunInput
  remote command:
----- BEGIN remote command -----
set -eu
dest='/etc/app.conf'
mkdir -p "$(dirname "$dest")"
tmp="$(mktemp "${dest}.dbf-tmp.XXXXXX")"
trap 'rm -f -- "$tmp"' EXIT
cat > "$tmp"
install -o 'root' -g 'root' -m '0644' "$tmp" "$dest"
----- END remote command -----
  stdin: text, length=128, sha256=...
----- BEGIN stdin -----
...
----- END stdin -----
(dbfdbg)
```

字段规则：

- `phase` 必填。
- `host` 必填。
- `address` 有资源或 operation 上下文时显示，否则省略。
- `action` 有资源 action 时显示。
- `summary` 有摘要时显示。
- `runner` 必填，值为 `Run`、`RunInput`、`RunCommand`。
- `remote command` 或 `script` 必填。
- `stdin` 仅 `RunInput` 显示。

### 长内容和二进制展示

调试器要避免自动把终端刷爆。对于 stdin、stdout、stderr，展示策略如下：

- 短的可打印 UTF-8 文本：默认完整显示。
- 长的可打印 UTF-8 文本：默认显示摘要、前若干行预览和展开提示。
- 非 UTF-8 或包含明显控制字符的内容：默认显示摘要和展开提示，不直接打印原始 bytes。
- 用户明确输入 `show stdin`、`show stdout` 或 `show stderr` 后，才展开完整内容。
- 展开二进制内容时使用 hex dump，不直接向终端写原始 bytes。

建议默认阈值：

- 文本超过 8 KiB 或超过 200 行即视为长内容。
- 二进制内容总是先摘要，不自动展开。

长文本摘要示例：

```text
  stdin: text payload, length=24576, sha256=..., 612 lines
  preview:
----- BEGIN stdin preview -----
...
----- END stdin preview -----
  hint: type "show stdin" to print the full payload
```

二进制摘要示例：

```text
  stdin: binary payload, length=128, sha256=...
  hint: type "show stdin" to print a full hex dump
```

二进制展开示例：

```text
(dbfdbg) show stdin
----- BEGIN stdin hex -----
00000000  ...
----- END stdin hex -----
```

## 执行结果展示

每个远端调用执行完成后，stderr 输出建议格式：

```text
dbf debugger: #12 remote call succeeded in 348ms
  stdout:
----- BEGIN stdout -----
...
----- END stdout -----
  stderr:
----- BEGIN stderr -----
...
----- END stderr -----
```

失败时：

```text
dbf debugger: #12 remote call failed in 348ms
  error: remote command on web1 failed: exit status 1: ...
  stdout:
----- BEGIN stdout -----
...
----- END stdout -----
  stderr:
----- BEGIN stderr -----
...
----- END stderr -----
(dbfdbg failed)
```

空 stdout/stderr 也应明确显示为 empty，避免用户误判为漏输出：

```text
  stdout: <empty>
  stderr: <empty>
```

如果 stdout/stderr 是长内容或二进制内容，规则与 stdin 相同：先显示摘要和提示，用户输入
`show stdout` 或 `show stderr` 后再完整展开。

## 失败处理

远端调用失败后，调试器必须暂停。

失败状态下允许：

- `show`：重新打印调用详情和失败输出。
- `show stdin` / `show stdout` / `show stderr`：展开完整 payload 或输出。
- `retry` / `r`：重新执行同一个远端调用。
- `quit` / `q`：退出 apply。
- `help` / `h`：显示帮助。

失败状态下不允许：

- `step`
- `next`
- `continue`

原因是失败调用可能已经部分修改远端主机。跳过该调用继续执行后续步骤，可能让远端状态和
DebianForm state 更难判断。第一版不提供 skip 语义。

如果 retry 成功：

- 打印成功结果。
- 调试器离开失败状态。
- 根据 retry 前的控制模式继续：
  - 如果用户输入的是 `retry`，成功后停在下一个远端调用前。
  - 如果未来扩展支持失败后恢复 continue，可另行设计；第一版不需要。

## 用户取消

用户输入 `quit` 后：

- 当前尚未执行的远端调用不再执行。
- apply 返回非零错误。
- 已经成功执行的远端修改不回滚。
- 已获取的 state lock 必须尽力 unlock。
- unlock 脚本和结果仍打印到 stderr，但不等待 prompt。

建议错误信息：

```text
apply cancelled by debugger
```

如果用户通过 Ctrl-C 中断：

- 尽量沿用现有 context cancellation 行为。
- 已注册的 defer cleanup 仍应运行。
- unlock 仍尽力执行。

## 与 apply 确认的关系

`dbf apply --debug` 仍保留现有 apply 确认流程：

```text
Apply these changes? Type yes to continue:
```

除非用户传入 `--auto-approve`。

调试器覆盖 facts discovery 和 online plan probes，因此会在打印初始 plan 之前就开始暂停。
这是有意设计：用户希望看到“收集了什么信息在远端”。

流程示意：

```text
dbf apply --debug
  -> debugger starts before facts discovery SSH call
  -> facts/probes are stepped
  -> plan is printed
  -> normal apply confirmation
  -> lock/re-plan/apply/state writes are stepped
```

## Runner 设计建议

新增一个包装现有 `Runner` 的调试 runner：

```go
type DebugRunner struct {
    Inner Runner
    Session *DebugSession
}
```

它实现同样的接口：

```go
Run(ctx context.Context, host, script string) (Result, error)
RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error)
RunCommand(ctx context.Context, host, remoteCommand string) (Result, error)
```

`DebugRunner` 负责：

- 构造调试步。
- 执行前交给 `DebugSession.BeforeCall`。
- 调用 inner runner。
- 执行后交给 `DebugSession.AfterCall`。
- 失败后进入 retry loop。

`RunInput` 必须先完整读取 `input` 到内存：

- 用于展示 payload。
- 用于真实执行。
- 用于失败 retry。

这会增加内存占用，但 apply 写入 payload 通常是配置文件、state JSON 或脚本文本，第一版可接受。
如未来需要支持超大 payload，可增加大小阈值和临时文件策略。

## 调试上下文标注

为了让用户知道每个 SSH 调用来自哪里，需要给 runner 调用加上下文。

建议在 engine 包内使用 context helper：

```go
type RemoteCallContext struct {
    Phase   string
    Host    string
    Address string
    Action  string
    Summary string
    Cleanup bool
}
```

调用前通过 context 注入：

```go
ctx = WithRemoteCallContext(ctx, RemoteCallContext{
    Phase: "apply resource",
    Host: step.Host,
    Address: step.Address,
    Action: step.Action,
    Summary: step.Summary,
})
```

调试 runner 从 context 中读取该信息。如果没有上下文，仍应显示：

```text
phase: remote call
```

需要标注的主要位置：

- facts discovery：`discover facts`
- SSH backend read：`state read`
- SSH backend write：`state write`
- SSH backend lock：`state lock`
- SSH backend unlock：`state unlock`，并设置 `Cleanup: true`
- provider plan：`plan inspect`
- resource apply/destroy：`apply resource`
- operation run：`run operation`
- operation output read：`operation output read`

如果某个 provider apply 内部会先执行修改脚本，再执行读取验证脚本，这两个 SSH 调用都应进入
调试器。第一个 phase 是 `apply resource`，第二个可以继续使用 `apply resource`，summary 中
保留同一个资源摘要；如果需要更清楚，可额外在 summary 里加 `verify observed state`。

## 输出流

调试器所有内容输出到 stderr：

- 风险提示。
- 调用详情。
- prompt。
- stdout/stderr 结果块。
- 调试器帮助。
- 失败上下文。

普通 plan/apply 正文继续输出到 stdout。

这样可以保留现有脚本对 stdout 的消费方式，也能通过 shell 分开保存：

```bash
dbf apply --debug > apply.out 2> debugger.log
```

## 状态和锁语义

调试器不能改变 DebianForm 的 state 语义：

- 资源成功后仍立即写 state。
- 失败后不回滚已完成资源。
- 下一次 plan/apply 仍依赖 observed 状态继续收敛。
- state lock 仍是 host 级互斥。

调试器 `quit` 或失败退出时，已经成功写入的 state 保留。未成功执行的资源不写入成功 state。

unlock 必须尽力执行，即使用户退出调试器也不能故意保留 lock。

## 示例会话

### 单步执行

```text
$ dbf apply -f examples/bbr.dbf.hcl --debug
dbf debugger: WARNING: apply --debug can print remote scripts, stdin payloads, stdout, and stderr. Expanded output may contain secrets.

dbf debugger: #1 before remote call
  phase: discover facts
  host: bbr1
  runner: Run
  script:
----- BEGIN script -----
set -eu
host_name="$(hostname 2>/dev/null || true)"
...
----- END script -----
(dbfdbg) step
dbf debugger: #1 remote call succeeded in 112ms
  stdout:
----- BEGIN stdout -----
hostname=bbr1
architecture=amd64
codename=trixie
----- END stdout -----
  stderr: <empty>

dbf debugger: #2 before remote call
  phase: state read
  host: bbr1
  runner: Run
  script:
...
(dbfdbg)
```

### 一次执行五步

```text
(dbfdbg) next 5
dbf debugger: #2 remote call succeeded in 23ms
...
dbf debugger: #3 remote call succeeded in 31ms
...
dbf debugger: #4 remote call succeeded in 28ms
...
dbf debugger: #5 remote call succeeded in 44ms
...
dbf debugger: #6 remote call succeeded in 51ms
...

dbf debugger: #7 before remote call
  phase: apply resource
  host: bbr1
  address: host.bbr1.kernel.module["tcp_bbr"]
...
(dbfdbg)
```

### 失败后重试

```text
dbf debugger: #12 remote call failed in 348ms
  error: remote command on web1 failed: exit status 100: ...
  stdout: <empty>
  stderr:
----- BEGIN stderr -----
E: Could not get lock /var/lib/dpkg/lock-frontend
----- END stderr -----
(dbfdbg failed) show
...
(dbfdbg failed) retry
dbf debugger: #12 remote call succeeded in 2.1s
...
dbf debugger: #13 before remote call
...
```

## 测试要求

### DebugRunner 单元测试

覆盖：

- `step` 每次只执行一个远端调用。
- 空回车初始等价于 `step`。
- `next 5` 连续执行 5 个调用后恢复暂停。
- `continue` 后不再等待 prompt，但仍打印每个调用和结果。
- `list` / `show` 不执行远端调用，只重新打印详情。
- `RunInput` 会打印 remote command；短文本 stdin 默认完整显示。
- 长 stdin 默认显示摘要、预览和 `show stdin` 提示。
- 非 UTF-8 stdin 默认显示二进制摘要；`show stdin` 使用 hex dump 展开。
- 长 stdout/stderr 默认显示摘要和展开提示。
- `show stdout` / `show stderr` 能展开完整输出。
- 失败后进入 failed prompt。
- 失败后 `retry` 重新执行同一个调用。
- 失败后 `step` / `next` / `continue` 不允许。
- `quit` 返回明确取消错误。
- cleanup/unlock 调用不等待 prompt。

### CLI 单元测试

覆盖：

- `apply --debug` flag 可以解析并进入调试模式。
- `plan --debug` 保持现有 provider address 输出语义，不进入调试模式。
- `check --debug` 报错。
- `validate --debug` 报错。
- `apply --debug --parallel 2` 报错。
- `apply --debug --parallel 1` 可接受。
- usage 文本包含 `--debug`。
- 调试输出写 stderr，不写 stdout。

### 集成测试建议

在不依赖真实 libvirt 的单元/轻集成测试中，可以使用 fake runner：

- fake runner 记录调用顺序。
- fake runner 返回预设 stdout/stderr。
- fake stdin 模拟用户输入 `step`、`next 2`、`continue`、`retry`、`quit`。

真实 libvirt 集成测试可以后续补充一个小 case：

- 运行 `dbf apply --debug --auto-approve`。
- stdin 输入 `next 100` 或 `continue`。
- 断言命令成功。
- 断言 stderr 包含 facts discovery、state lock、apply resource、state write。

## 文档要求

实现时需要同步更新：

- `docs/cli.zh.md`
- `docs/operations-runbook.zh.md`
- `docs/how-it-works/08-apply-scheduler.zh.md`
- CLI `usage()` 输出

文档必须明确：

- `apply --debug` 是高风险模式，展开 payload 或输出时可能泄露 secret。
- 长文本和二进制 payload 默认只显示摘要和展开提示。
- 调试器的“一步”是一次 SSH 调用，不是 shell 脚本的一行。
- `next N` 可以一次执行多个远端调用。
- 调试模式强制串行，不适合性能测试。
- `plan --debug` 和 `apply --debug` 的区别。

## 验收标准

以下行为应成立：

```bash
dbf apply -f examples/bbr.dbf.hcl --debug
```

- 在第一次 facts discovery SSH 调用前停住。
- 显示完整 facts discovery 脚本。
- `step` 后执行该 SSH 调用，并显示 stdout/stderr。

```text
(dbfdbg) next 5
```

- 连续执行 5 个远端调用。
- 第 5 个调用完成后，在下一个远端调用前停住。

```text
(dbfdbg) continue
```

- 后续不再暂停。
- 仍打印每个远端调用详情和执行结果。

远端调用失败时：

- 调试器停在 failed prompt。
- `show` 能重新展示脚本和输出。
- `retry` 能重新执行同一个远端调用。
- `quit` 能退出 apply 并尽力 unlock。

普通命令不受影响：

```bash
dbf apply -f examples/bbr.dbf.hcl
dbf plan -f examples/bbr.dbf.hcl --debug
```

- `apply` 不进入调试器。
- `plan --debug` 继续只影响 plan 输出。

## 后续扩展

第一版完成后，可以考虑：

- breakpoint：按 host、phase、address、command substring 停住。
- `until phase/address`：连续执行到指定阶段或资源。
- `set print stdin off`：会话中临时关闭 stdin 打印。
- `set print stdout limit N`：限制大输出展示长度。
- `save log path`：把调试输出写入文件。
- `skip`：显式跳过失败调用继续执行，但需要非常清楚地标注 state 风险。
- 按 shell 行级别重构 provider 脚本，实现更细粒度调试。
