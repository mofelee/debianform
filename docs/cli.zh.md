# DebianForm CLI

本文档说明 `dbf` 命令行工具的主要功能、可用选项和常见用法。

`dbf` 读取 `.dbf.hcl` 配置文件，用于校验、预览、应用和检查 Debian 主机配置。

第一次使用建议先走 [quickstart](quickstart.zh.md)，它覆盖准备 root SSH 测试主机、
写第一份配置、`validate`、在线 `plan`、`apply`、再次 `plan` no-op 和 `check`。
Stale lock、apply 中途失败、drift 恢复和常见故障排查见
[operations runbook](operations-runbook.zh.md)。

## 基本规则

默认情况下，`dbf` 会读取当前目录中所有 `*.dbf.hcl` 文件，并按文件名排序后合并处理。
如果只想读取一个或多个明确指定的文件或目录，使用可重复的 `-f path`：

```bash
dbf validate -f examples/bbr.dbf.hcl
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

命令失败时会向 stderr 输出 `dbf: ...` 错误信息，并返回非零退出码。配置中的
deprecated component input 会输出 warning，但 warning 本身不会改变退出码。

## 命令总览

```text
dbf validate [-f path ...] [-var name=value] [-var-file path] [--host name]
dbf plan     [-f path ...] [-var name=value] [-var-file path] [--host name] [--format text|json] [--html file] [--debug] [--color auto|always|never] [--offline]
dbf apply    [-f path ...] [-var name=value] [-var-file path] [--host name] [--color auto|always|never] [--parallel n] [--lock-timeout duration] [--auto-approve] [--debug]
dbf check    [-f path ...] [-var name=value] [-var-file path] [--host name] [--color auto|always|never] [--lock-timeout duration]
dbf fmt      [-f path ...]
dbf variable inspect [-f path ...] [-var name=value] [-var-file path]
dbf component inspect [-f path ...] component_name
dbf version
dbf --version
dbf -version
dbf help
```

## 通用配置选择

多数读取配置的命令都支持：

| 选项 | 适用命令 | 说明 |
| --- | --- | --- |
| `-f path` | `validate`、`plan`、`apply`、`check`、`fmt`、`variable inspect`、`component inspect` | 可重复。`path` 可以是文件或目录；目录展开为直属 `*.dbf.hcl`。不传时读取当前目录所有 `*.dbf.hcl`。 |
| `--host name` | `validate`、`plan`、`apply`、`check` | 只处理指定 host。host 不存在时命令失败。 |
| `-var name=value` | `validate`、`plan`、`apply`、`check`、`variable inspect` | 可重复；设置顶层 `variable` 的值。 |
| `-var-file path` | `validate`、`plan`、`apply`、`check`、`variable inspect` | 可重复；从 `.dbfvars` 或 `.dbfvars.json` 文件加载变量值。 |
| `--color auto\|always\|never` | `plan`、`apply`、`check` | 控制文本输出颜色；JSON、HTML 和持久化日志不使用 ANSI。 |

传入文件时，`-f` 精确读取该文件；传入目录时，`-f` 读取该目录直属 `*.dbf.hcl` 文件并按文件名排序，
不会递归读取子目录。多个 `-f` 会按命令行出现顺序展开和解析，重复文件只保留第一次出现。

```bash
dbf validate -f ../shared -f .
dbf plan -f ../shared/base.dbf.hcl -f ./hosts/prod.dbf.hcl --offline
```

变量值来源按低到高优先级合并：

1. 环境变量 `DBF_VAR_name=value`。未知变量会被忽略，便于共享 shell 环境。
2. 参与配置文件所在目录的 `debianform.dbfvars`、`debianform.dbfvars.json`。
3. 参与配置文件所在目录按文件名排序的 `*.auto.dbfvars`、`*.auto.dbfvars.json`。
4. 命令行中按顺序出现的 `-var-file path`。
5. 命令行中按顺序出现的 `-var name=value`。

后面的来源会覆盖前面的同名变量。多个目录参与加载时，自动变量文件按配置文件所在目录首次出现顺序加载；
后出现目录中的自动变量可以覆盖前面目录的同名变量。

`-var` 的 `value` 按变量类型解析：`string` 保留原始字符串，`number`、`bool`、`list`、`map`、
`object`、`tuple` 等类型使用 HCL/JSON 字面值。对已声明为 `sensitive = true` 的变量，
`-var token=@path` 会从文件读取，`-var token=@-` 会从 stdin 读取，
`-var token=env:NAME` 会从环境变量读取；错误信息和 plan/state 不会泄露敏感来源和值。

`host "<name>"` 默认通过 `ssh <name>` 连接，管理用户为 root。推荐把 `HostName`、
`User`、`IdentityFile`、`ProxyJump`、端口等连接细节放在 `~/.ssh/config`。只有需要在
配置内覆盖连接名、端口、identity file 或 state 路径时，才写 `ssh` 或 `state` block。

默认 state 路径为：

```text
/var/lib/debianform/state/<host>.json
/var/lock/debianform/state/<host>.lock
```

## validate

`validate` 在本地解析配置、合并 profile/host/component，并校验生成的 HostSpec。
它不会连接远端主机，适合在提交前或 CI 中做基础配置检查。

```bash
dbf validate
dbf validate -f examples/bbr.dbf.hcl
dbf validate -f examples/bird2.dbf.hcl --host router1
dbf validate -f internal/core/testdata/fixtures/variable-cli.dbf.hcl \
  -var-file internal/core/testdata/fixtures/variable-prod.dbfvars \
  -var environment=staging
```

成功时输出类似：

```text
configuration is valid: 1 host(s)
```

选项：

| 选项 | 说明 |
| --- | --- |
| `-f path` | 可重复；读取显式指定的文件，或目录直属 `*.dbf.hcl`。 |
| `--host name` | 只校验指定 host。 |
| `-var name=value` | 可重复；设置顶层变量。 |
| `-var-file path` | 可重复；加载变量文件。 |

## plan

`plan` 生成配置变更预览。默认模式会通过 SSH 连接目标主机，探测 runtime facts、读取远端
state，并对比 observed state。纯本地预览使用 `--offline`。
在线 `plan` 会把 facts/state/observed 探测进度写到 stderr；plan 正文仍写到 stdout。

```bash
dbf plan -f examples/bbr.dbf.hcl --offline
```

文本输出示例：

```text
Plan:
  + host.bbr1.kernel.module["tcp_bbr"]
    create kernel module tcp_bbr

Summary: 3 create, 0 update, 0 delete, 0 no-op, 0 operations
```

输出 JSON：

```bash
dbf plan -f examples/bbr.dbf.hcl --format json --offline
```

生成静态 HTML plan：

```bash
dbf plan -f examples/files-plan-preview.dbf.hcl --html plan.html --offline
```

调试 provider address：

```bash
dbf plan -f examples/bbr.dbf.hcl --format json --debug --offline
```

选项：

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `-f path` | 当前目录所有 `*.dbf.hcl` | 可重复；读取显式指定的文件，或目录直属 `*.dbf.hcl`。 |
| `--host name` | 空 | 只为指定 host 生成 plan。 |
| `--format text\|json` | `text` | 输出文本或 JSON。JSON 格式见 [plan format](plan-format.md)。 |
| `--html file` | 空 | 将 plan 写成静态 HTML 文件。只能用于 `plan`，且不能和显式 `--format` 同时使用。 |
| `--debug` | `false` | 在 plan 输出中显示内部 provider address。`apply --debug` 是不同语义，见 apply 章节。 |
| `--offline` | `false` | 不进行 SSH、state 和 runtime facts 探测，只做本地 plan 预览。只能用于 `plan`。 |
| `-var name=value` | 空 | 可重复；设置顶层变量。 |
| `-var-file path` | 空 | 可重复；加载变量文件。 |

注意事项：

- 在线 `plan` 需要配置中的 host 可以通过 root SSH 连接，并且远端是受支持的 Debian 系统。
  DebianForm 不支持 sudo、become 或非 root 管理连接；`ssh.user` 只能省略或设为
  `"root"`。
- `--offline` 不能解析依赖远端 runtime facts 的表达式，除非配置中已经声明了匹配的
  system facts。
- `--html` 输出目录不存在时会自动创建。

## apply

`apply` 先生成在线 plan，再按 plan 修改远端主机。它会使用远端 state lock，避免同一
host 上多个 apply 并发写 state。
执行期间会向 stderr 输出当前 host、资源地址、动作和长步骤心跳；stdout 仍保留 plan 输出。

```bash
dbf apply -f examples/bbr.dbf.hcl
```

默认情况下，如果 plan 中存在变更或 operation，`apply` 会要求确认：

```text
Apply these changes? Type yes to continue:
```

跳过确认：

```bash
dbf apply -f examples/bbr.dbf.hcl --auto-approve
```

多 host 并发应用：

```bash
dbf apply --parallel 4 --auto-approve
```

未显式设置 `--parallel` 时，在线 SSH 阶段默认最多同时处理 4 台 host。

调试远端 SSH 调用：

```bash
dbf apply -f examples/bbr.dbf.hcl --debug --auto-approve
```

`apply --debug` 会进入交互式 SSH 调试器，从 facts discovery 开始拦截每一次远端调用，并把
调试信息写到 stderr。它会显示 host、phase、资源或 operation address、action、summary、
远端脚本或命令、stdin 摘要、stdout/stderr 摘要和执行结果。短文本默认完整显示；长文本和二进制
payload 只显示长度、sha256、预览和展开提示，需要显式输入 `show stdin`、`show stdout` 或
`show stderr` 才会展开。

调试器命令：

```text
step
next 5
continue
show
show stdin
retry
quit
help
```

远端调用失败后会进入 `(dbfdbg failed)`，可用 `show` 查看失败上下文，用 `retry` 重跑同一个
远端调用，或用 `quit` 取消 apply。`quit` 后 state unlock 这类 cleanup 调用仍会尽力执行。

`apply --debug` 是高风险模式：展开输出可能包含 secret、远端脚本、stdin payload、stdout 或
stderr。建议只在排障时使用，不要把完整调试日志粘贴到公开 issue。调试模式下远端调用强制串行；
`--debug --parallel 2` 这类组合会失败，`--parallel 1` 可接受。

选项：

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `-f path` | 当前目录所有 `*.dbf.hcl` | 可重复；读取显式指定的文件，或目录直属 `*.dbf.hcl`。 |
| `--host name` | 空 | 只应用指定 host。 |
| `--parallel n` | `4` | 最多同时进行远端 SSH 阶段的 host 数量，包括 facts discovery、lock/read state、inspect 和 apply；必须大于等于 1，只能用于 `apply`。 |
| `--lock-timeout duration` | `5m` | 等待远端 state lock 的最长时间。使用 Go duration 格式，例如 `30s`、`2m`、`10m`。 |
| `--auto-approve` | `false` | 跳过交互确认。 |
| `--debug` | `false` | 进入交互式 SSH 调试器。只能用于 `apply`；会强制串行，不能和 `--parallel` 大于 1 同时使用。 |
| `-var name=value` | 空 | 可重复；设置顶层变量。 |
| `-var-file path` | 空 | 可重复；加载变量文件。 |

如果 plan 没有任何变更和 operation，`apply` 会输出 plan 后直接结束，不会执行确认步骤。
每台 host 内部仍按 ResourceGraph 的确定性顺序串行执行。
进度日志不会输出资源 desired 内容或远端命令 stdout，因此不会破坏 JSON/text plan 消费。

## check

`check` 生成在线 plan 并用于检测远端状态是否偏离配置。它不会应用变更。
和在线 `plan` 一样，探测进度会写到 stderr，检查结果 plan 写到 stdout。

```bash
dbf check -f examples/bbr.dbf.hcl
dbf check -f examples/bbr.dbf.hcl --host bbr1
```

选项：

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `-f path` | 当前目录所有 `*.dbf.hcl` | 可重复；读取显式指定的文件，或目录直属 `*.dbf.hcl`。 |
| `--host name` | 空 | 只检查指定 host。 |
| `--lock-timeout duration` | `5m` | 等待远端 state lock 的最长时间。 |
| `-var name=value` | 空 | 可重复；设置顶层变量。 |
| `-var-file path` | 空 | 可重复；加载变量文件。 |

当远端状态与配置不一致，存在 create、update、delete、destroy 或 operation 时，
`check` 返回非零退出码，并输出：

```text
dbf: remote state does not match configuration
```

当前错误文本仍保留历史格式名；语义是远端状态和当前配置不一致。

`check` 不支持 `--offline`，因为它必须读取远端事实和状态。

## fmt

`fmt` 使用 HCL formatter 原地格式化配置文件。

```bash
dbf fmt
dbf fmt -f examples/bbr.dbf.hcl
```

输出示例：

```text
formatted 1 file(s)
```

选项：

| 选项 | 说明 |
| --- | --- |
| `-f path` | 可重复；格式化显式指定的文件，或目录直属 `*.dbf.hcl`。不传时格式化当前目录所有 `*.dbf.hcl`。 |

`fmt` 会改写文件内容。对已格式化的文件再次运行会输出 `formatted 0 file(s)`。

## component inspect

`component inspect` 输出 component 的公开 input API，格式为 JSON。它适合用于查看
component 需要哪些参数、参数类型、默认值和说明。

```bash
dbf component inspect -f examples/component-inputs.dbf.hcl reverse_proxy
```

输出结构示例：

```json
{
  "name": "proxy",
  "inputs": [
    {
      "name": "listeners",
      "type": "list(object({name=string,port=number}))",
      "default": [],
      "nullable": false,
      "sensitive": false,
      "deprecated": "Use endpoints instead.",
      "description": "Listeners."
    },
    {
      "name": "token",
      "type": "string",
      "default": "<sensitive>",
      "nullable": true,
      "sensitive": true
    }
  ]
}
```

选项和参数：

| 选项/参数 | 说明 |
| --- | --- |
| `-f path` | 可重复；读取显式指定的文件，或目录直属 `*.dbf.hcl`。不传时读取当前目录所有 `*.dbf.hcl`。 |
| `component_name` | 要检查的 component 名称，必填且只能传一个。 |

如果 input 设置了 `sensitive = true`，且默认值存在，输出中的默认值会显示为
`"<sensitive>"`，不会泄露明文。

## variable inspect

`variable inspect` 输出顶层 variable 的公开输入 API，格式为 JSON。它适合用于查看配置
需要哪些外部变量、变量类型、默认值、nullable/sensitive/ephemeral/deprecated 标记和说明。

```bash
dbf variable inspect -f examples/variable-secret-file.dbf.hcl
```

选项：

| 选项 | 说明 |
| --- | --- |
| `-f path` | 可重复；读取显式指定的文件，或目录直属 `*.dbf.hcl`。不传时读取当前目录所有 `*.dbf.hcl`。 |
| `-var name=value` | 可重复；为 inspect 时的变量求值提供字面值。 |
| `-var-file path` | 可重复；从 `.dbfvars` 或 `.dbfvars.json` 文件加载变量值。 |

如果 variable 设置了 `sensitive = true`，输出中的默认值会显示为 `"<sensitive>"`。

## version

查看版本信息：

```bash
dbf version
```

`version` 输出详细构建信息：

```text
dbf dev
commit: unknown
built: unknown
go: go1.x
platform: linux/amd64
```

只输出一行版本号：

```bash
dbf --version
dbf -version
```

输出示例：

```text
dbf dev
```

## help

显示内置帮助：

```bash
dbf help
dbf --help
dbf -h
```

不带任何参数运行 `dbf` 也会显示帮助并返回成功。

## 选项限制

以下选项只在特定命令中有实际作用。无效组合会失败；少数无意义组合可能会被解析但不会影响结果，
应按下表使用。

| 选项 | 仅适用命令 | 说明 |
| --- | --- | --- |
| `--format` | `plan` | `validate`、`apply`、`check` 不支持结构化输出。 |
| `--html` | `plan` | 不能和显式 `--format` 同时使用。 |
| `--debug` | `plan`、`apply` | `plan` 中显示内部 provider address；`apply` 中进入交互式 SSH 调试器。 |
| `--color` | `plan`、`apply`、`check` | `auto` 只在 TTY 且未设置 `NO_COLOR` / `TERM=dumb` 时启用；`always` 强制启用；`never` 禁用。 |
| `--offline` | `plan` | 离线 plan 预览。 |
| `--parallel` | `apply` | 控制多 host 在线 SSH 阶段并发。 |
| `--auto-approve` | `apply` | 跳过 apply 确认。 |
| `--lock-timeout` | `apply`、`check` | 在线执行等待远端 state lock 的超时时间。 |

无效组合会直接失败，例如：

```bash
dbf plan -f examples/bbr.dbf.hcl --parallel 2
dbf check -f examples/bbr.dbf.hcl --offline
dbf plan -f examples/bbr.dbf.hcl --html plan.html --format json
dbf apply -f examples/bbr.dbf.hcl --debug --parallel 2
```

## 常见工作流

本地检查配置：

```bash
dbf validate -f examples/bbr.dbf.hcl
```

本地预览可运行示例：

```bash
dbf plan -f examples/bbr.dbf.hcl --offline
```

输出机器可读 plan：

```bash
dbf plan -f examples/bbr.dbf.hcl --format json --offline
```

应用到远端主机：

```bash
dbf apply -f examples/bbr.dbf.hcl --auto-approve
```

检查远端是否漂移：

```bash
dbf check -f examples/bbr.dbf.hcl
```

格式化当前目录配置：

```bash
dbf fmt
```

查看 component 输入：

```bash
dbf component inspect -f examples/component-inputs.dbf.hcl reverse_proxy
```
