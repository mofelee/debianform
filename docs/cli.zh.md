# DebianForm CLI

本文档说明 `dbf` 命令行工具的主要功能、可用选项和常见用法。

`dbf` 读取 `.dbf.hcl` 配置文件，用于校验、预览、应用和检查 Debian 主机配置。

第一次使用建议先走 [quickstart](quickstart.zh.md)，它覆盖准备 root SSH 测试主机、
写第一份配置、`validate`、在线 `plan`、`apply`、再次 `plan` no-op 和 `check`。
Stale lock、apply 中途失败、drift 恢复和常见故障排查见
[operations runbook](operations-runbook.zh.md)。

## 基本规则

默认情况下，`dbf` 会读取当前目录中所有 `*.dbf.hcl` 文件，并按文件名排序后合并处理。
如果只想读取一个或多个明确指定的文件，使用可重复的 `-f file`：

```bash
dbf validate -f examples/v2-bbr.dbf.hcl
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

命令失败时会向 stderr 输出 `dbf: ...` 错误信息，并返回非零退出码。配置中的
deprecated component input 会输出 warning，但 warning 本身不会改变退出码。

## 命令总览

```text
dbf validate [-f file ...] [--host name]
dbf plan     [-f file ...] [--host name] [--format text|json] [--html file] [--debug] [--offline]
dbf apply    [-f file ...] [--host name] [--parallel n] [--lock-timeout duration] [--auto-approve]
dbf check    [-f file ...] [--host name] [--lock-timeout duration]
dbf fmt      [-f file ...]
dbf variable inspect [-f file ...] [-var name=value] [-var-file path]
dbf component inspect [-f file ...] component_name
dbf version
dbf --version
dbf -version
dbf help
```

## 通用配置选择

多数读取配置的命令都支持：

| 选项 | 适用命令 | 说明 |
| --- | --- | --- |
| `-f file` | `validate`、`plan`、`apply`、`check`、`fmt`、`variable inspect`、`component inspect` | 可重复。传入一个或多个 `-f` 时，只读取显式指定的文件；不传时读取当前目录所有 `*.dbf.hcl`。 |
| `--host name` | `validate`、`plan`、`apply`、`check` | 只处理指定 host。host 不存在时命令失败。 |

`-f` 不会读取目录，也不会自动加载同目录的其他 `.dbf.hcl` 文件；它表示“精确使用这些显式指定的文件”，并按命令行出现顺序解析。

## validate

`validate` 在本地解析配置、合并 profile/host/component，并校验生成的 HostSpec。
它不会连接远端主机，适合在提交前或 CI 中做基础配置检查。

```bash
dbf validate
dbf validate -f examples/v2-bbr.dbf.hcl
dbf validate -f examples/v2-bird2.dbf.hcl --host router1
```

成功时输出类似：

```text
v2 configuration is valid: 1 host(s)
```

选项：

| 选项 | 说明 |
| --- | --- |
| `-f file` | 可重复；只读取显式指定的文件。 |
| `--host name` | 只校验指定 host。 |

## plan

`plan` 生成配置变更预览。默认模式会通过 SSH 连接目标主机，探测 runtime facts、读取远端
state，并对比 observed state。纯本地预览使用 `--offline`。

```bash
dbf plan -f examples/v2-bbr.dbf.hcl --offline
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
dbf plan -f examples/v2-bbr.dbf.hcl --format json --offline
```

生成静态 HTML plan：

```bash
dbf plan -f examples/v2-files-plan-preview.dbf.hcl --html plan.html --offline
```

调试 provider address：

```bash
dbf plan -f examples/v2-bbr.dbf.hcl --format json --debug --offline
```

选项：

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `-f file` | 当前目录所有 `*.dbf.hcl` | 可重复；只读取显式指定的文件。 |
| `--host name` | 空 | 只为指定 host 生成 plan。 |
| `--format text\|json` | `text` | 输出文本或 JSON。JSON 格式见 [plan format](plan-format.md)。 |
| `--html file` | 空 | 将 plan 写成静态 HTML 文件。只能用于 `plan`，且不能和 `--format json` 同时使用。 |
| `--debug` | `false` | 在 plan 输出中显示内部 provider address。只能用于 `plan`。 |
| `--offline` | `false` | 不进行 SSH、state 和 runtime facts 探测，只做本地 plan 预览。只能用于 `plan`。 |

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

```bash
dbf apply -f examples/v2-bbr.dbf.hcl
```

默认情况下，如果 plan 中存在变更或 operation，`apply` 会要求确认：

```text
Apply these changes? Type yes to continue:
```

跳过确认：

```bash
dbf apply -f examples/v2-bbr.dbf.hcl --auto-approve
```

多 host 并发应用：

```bash
dbf apply --parallel 4 --auto-approve
```

选项：

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `-f file` | 当前目录所有 `*.dbf.hcl` | 可重复；只读取显式指定的文件。 |
| `--host name` | 空 | 只应用指定 host。 |
| `--parallel n` | `1` | 最多同时 apply 的 host 数量；必须大于等于 1，只能用于 `apply`。 |
| `--lock-timeout duration` | `5m` | 等待远端 state lock 的最长时间。使用 Go duration 格式，例如 `30s`、`2m`、`10m`。 |
| `--auto-approve` | `false` | 跳过交互确认。 |

如果 plan 没有任何变更和 operation，`apply` 会输出 plan 后直接结束，不会执行确认步骤。
每台 host 内部仍按 ResourceGraph 的确定性顺序串行执行。

## check

`check` 生成在线 plan 并用于检测远端状态是否偏离配置。它不会应用变更。

```bash
dbf check -f examples/v2-bbr.dbf.hcl
dbf check -f examples/v2-bbr.dbf.hcl --host bbr1
```

选项：

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `-f file` | 当前目录所有 `*.dbf.hcl` | 可重复；只读取显式指定的文件。 |
| `--host name` | 空 | 只检查指定 host。 |
| `--lock-timeout duration` | `5m` | 等待远端 state lock 的最长时间。 |

当远端状态与配置不一致，存在 create、update、delete、destroy 或 operation 时，
`check` 返回非零退出码，并输出：

```text
dbf: remote state does not match v2 configuration
```

当前错误文本仍保留历史格式名；语义是远端状态和当前配置不一致。

`check` 不支持 `--offline`，因为它必须读取远端事实和状态。

## fmt

`fmt` 使用 HCL formatter 原地格式化配置文件。

```bash
dbf fmt
dbf fmt -f examples/v2-bbr.dbf.hcl
```

输出示例：

```text
formatted 1 file(s)
```

选项：

| 选项 | 说明 |
| --- | --- |
| `-f file` | 可重复；只格式化显式指定的文件。不传时格式化当前目录所有 `*.dbf.hcl`。 |

`fmt` 会改写文件内容。对已格式化的文件再次运行会输出 `formatted 0 file(s)`。

## component inspect

`component inspect` 输出 component 的公开 input API，格式为 JSON。它适合用于查看
component 需要哪些参数、参数类型、默认值和说明。

```bash
dbf component inspect -f examples/v2-component-inputs.dbf.hcl reverse_proxy
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
| `-f file` | 可重复；只读取显式指定的文件。不传时读取当前目录所有 `*.dbf.hcl`。 |
| `component_name` | 要检查的 component 名称，必填且只能传一个。 |

如果 input 设置了 `sensitive = true`，且默认值存在，输出中的默认值会显示为
`"<sensitive>"`，不会泄露明文。

## variable inspect

`variable inspect` 输出顶层 variable 的公开输入 API，格式为 JSON。它适合用于查看配置
需要哪些外部变量、变量类型、默认值、nullable/sensitive/ephemeral/deprecated 标记和说明。

```bash
dbf variable inspect -f examples/v2-variable-secret-file.dbf.hcl
```

选项：

| 选项 | 说明 |
| --- | --- |
| `-f file` | 可重复；只读取显式指定的文件。不传时读取当前目录所有 `*.dbf.hcl`。 |
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
| `--html` | `plan` | 不能和 `--format json` 同时使用。 |
| `--debug` | `plan` | 用于 plan 调试输出。 |
| `--offline` | `plan` | 离线 plan 预览。 |
| `--parallel` | `apply` | 控制多 host apply 并发。 |
| `--auto-approve` | `apply` | 跳过 apply 确认。 |
| `--lock-timeout` | `apply`、`check` | 在线执行等待远端 state lock 的超时时间。 |

无效组合会直接失败，例如：

```bash
dbf plan -f examples/v2-bbr.dbf.hcl --parallel 2
dbf check -f examples/v2-bbr.dbf.hcl --offline
dbf plan -f examples/v2-bbr.dbf.hcl --html plan.html --format json
```

## 常见工作流

本地检查配置：

```bash
dbf validate -f examples/v2-bbr.dbf.hcl
```

本地预览可运行示例：

```bash
dbf plan -f examples/v2-bbr.dbf.hcl --offline
```

输出机器可读 plan：

```bash
dbf plan -f examples/v2-bbr.dbf.hcl --format json --offline
```

应用到远端主机：

```bash
dbf apply -f examples/v2-bbr.dbf.hcl --auto-approve
```

检查远端是否漂移：

```bash
dbf check -f examples/v2-bbr.dbf.hcl
```

格式化当前目录配置：

```bash
dbf fmt
```

查看 component 输入：

```bash
dbf component inspect -f examples/v2-component-inputs.dbf.hcl reverse_proxy
```
