# script / on_change 需求文档

<p align="right"><a href="script-on-change-requirements.md">English</a> | <strong>简体中文</strong></p>

本文描述 `script` 指令和 `files.file.on_change` 的设计目标。当前已实现 DSL 解析、
validate、HostSpec 编译、ResourceGraph/plan operation 展示、apply 阶段脚本执行、
`mode = "once"` / `"each"` 触发语义，以及运行时触发上下文注入。已支持语法见
[DSL Reference](dsl-reference.zh.md)。分阶段实施计划见
[script / on_change 实施计划](script-on-change-implementation-plan.zh.md)。

## 核心概念

DebianForm 的执行模型以 `host` 为中心：

- `host` 是最终执行单元。`plan`、`apply`、`check` 都以 host 为目标；每台 host
  有自己的 SSH 连接、远端 state 和 lock。
- `profile` 是不可传参的基础配置片段，用来沉淀可复用的主机配置。profile 可以被
  profile 或 host import；被 import 的内容先合并，当前 profile/host 后合并并覆盖
  同名字段。
- `component` 是可传参的复用部署单元，用来封装一组资源、公开 typed input，并可选择
  声明 artifact 下载、构建和安装。component 只有被 host 挂载后才会展开；它没有 host
  的完整语义，也不会独立执行。

`script` / `on_change` 属于 component 的内部运行生命周期：component 自己声明
"我的文件变了以后，应该怎么让自己生效"。它不属于 profile，也不建议作为 host 的通用
回调机制。

这里的"运行生命周期"与现有 `lifecycle { prevent_destroy = true }` 不同：后者是资源
删除保护，前者是资源变更后的操作钩子。

## 目标

- 让 component 能封装配置文件和对应的 reload/restart/activate 操作。
- 让 component 调用者通过 input 调整行为，而不是传入外部 script 引用。
- 保持 host/profile/component 边界清晰：host 组合，profile 复用基础配置，component
  负责自己的内部生效逻辑。
- 让 plan 能清楚展示哪些文件变更会触发哪些操作。
- 避免脚本内容泄漏 sensitive 文件内容或让 plan 输出变得过长。

## 非目标

- 第一版不支持在 `host` 或 `profile` 内声明 `script`。
- 第一版不支持 `host` 把 `script` 引用作为 input 传给 component。
- 第一版不支持一个 `files.file` 绑定多个 `on_change`。
- 第一版不把 `script` 作为独立可 apply 的顶层资源。

## DSL 设计

`script` 出现在 `component` 内，与 `files`、`services` 等 block 同级。

```hcl
component "app" {
  input "service_name" {
    type = string
  }

  script "reload" {
    mode = "once"
    run  = "systemctl reload ${input.service_name}.service"
  }

  files {
    file "/etc/app/config.yaml" {
      content   = "..."
      on_change = script.reload
    }
  }
}
```

`files.file.on_change` 只接受一个 script 引用。一个文件的最终变更只触发一个
on-change 操作；如果需要多步行为，脚本内部负责组织。

## script 字段

`script "<name>"` 支持这些字段：

| 字段 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `mode` | 否 | `"once"` | `"once"` 或 `"each"`。 |
| `interpreter` | 否 | `["/bin/sh", "-eu"]` | 执行脚本内容的解释器和参数。 |
| `outputs` | 否 | `[]` | 脚本生成的普通文件绝对路径列表，用于 check/plan 漂移检测。 |
| `run` | 三选一 | 无 | 单条 shell 命令字符串。 |
| `content` | 三选一 | 无 | 多行脚本文本。 |
| `commands` | 三选一 | 无 | 命令矩阵，例如 `[["systemctl", "reload", "app.service"]]`。 |

`run`、`content`、`commands` 互斥，必须且只能选择一个。

建议语义：

- `run` 会被包装成脚本文本执行。
- `content` 原样交给 `interpreter` 执行。
- `commands` 由 DebianForm 安全拼接成脚本文本，每个内层 list 表示一条命令及其参数。

## 运行模式

`mode = "once"`：

- 同一轮 apply 中，如果多个文件触发同一个 script，该 script 只运行一次。
- 适合 `systemctl reload app.service`、`systemctl restart app.service` 这类合并操作。

`mode = "each"`：

- 同一轮 apply 中，每个触发文件各运行一次。
- 适合脚本需要按文件路径处理的场景，例如对每个生成文件单独执行校验或导入。

脚本执行失败时 apply 失败。失败语义沿用现有 operation 的严格模型，不做 warn-only。

## 输出文件

`outputs` 用于把脚本副作用纳入 DebianForm 状态模型。每个 output 必须是绝对路径，并且脚本执行后
必须存在为普通文件。DebianForm 会在脚本成功后记录 output 的 SHA256；后续 check/plan 如果发现
output 缺失、变成目录、hash 与上次记录不一致，或脚本声明本身变化，会重新触发该 script。

`outputs` 不表示 DebianForm 直接写入这些文件，也不在移除声明时删除远端 output；它只定义脚本输出
的检查和重新收敛边界。

## 运行时上下文

运行脚本时，DebianForm 注入环境变量，避免用户必须把触发源硬编码到脚本中：

| 环境变量 | 说明 |
| --- | --- |
| `DBF_SCRIPT_NAME` | script 名称。 |
| `DBF_COMPONENT_NAME` | component instance 名称。 |
| `DBF_TRIGGER_ADDRESS` | 当前触发资源地址；`each` 模式下总是单个地址。 |
| `DBF_TRIGGER_PATH` | 当前触发文件路径；`each` 模式下总是单个路径。 |
| `DBF_TRIGGER_ADDRESSES` | `once` 模式下的触发地址列表，换行分隔。 |
| `DBF_TRIGGER_PATHS` | `once` 模式下的触发路径列表，换行分隔。 |

component 的 input 在编译 component 时已经参与表达式求值，因此常见定制应通过 input 完成。

## 完整示例

下面示例展示 component 自己封装配置文件、systemd unit、service 和 reload 行为。host 只
负责挂载 component 并传入普通 input。

```hcl
component "managed_app" {
  input "service_name" {
    type = string
  }

  input "listen_addr" {
    type    = string
    default = "127.0.0.1:8080"
  }

  script "reload" {
    mode = "once"
    run  = "systemctl reload ${input.service_name}.service"
  }

  files {
    file "/etc/managed-app/config.env" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = "LISTEN_ADDR=${input.listen_addr}\n"

      on_change = script.reload
    }
  }

  systemd {
    service_unit "managed-app" {
      description = "Managed app"
      run         = ["/usr/local/bin/managed-app", "--config", "/etc/managed-app/config.env"]
      restart     = "always"
    }
  }

  services {
    service "managed-app" {
      enabled = true
      state   = "running"
    }
  }
}

host "app1" {
  component "app" {
    source = component.managed_app

    inputs = {
      service_name = "managed-app"
      listen_addr  = "127.0.0.1:9000"
    }
  }
}
```

如果用户希望不同环境使用 restart 而不是 reload，应优先把行为做成普通 input：

```hcl
component "managed_app" {
  input "service_action" {
    type    = string
    default = "reload"

    validation {
      condition     = contains(["reload", "restart"], input.service_action)
      error_message = "service_action must be reload or restart."
    }
  }

  input "service_name" {
    type = string
  }

  script "apply_config" {
    mode = "once"
    run  = "systemctl ${input.service_action} ${input.service_name}.service"
  }

  files {
    file "/etc/managed-app/config.env" {
      content   = "..."
      on_change = script.apply_config
    }
  }
}
```

## 编译与执行模型

建议编译链路：

1. host/profile 先按现有规则 merge，得到 host 基础配置。
2. host 挂载 component，并为每个 component instance 求值 input。
3. component 内的 `script` 和 `files.file.on_change` 在 instance 作用域内解析。
4. ResourceGraph 为 file 节点和 script operation 建立依赖边。
5. 在线 plan 时，只有实际 create/update/delete 的 file 会触发对应 operation。
6. apply 时，先应用文件变更，再按依赖顺序执行触发的 script operation。

operation 地址建议包含 component instance，避免同一个 component 多次挂载时互相冲突：

```text
host.app1.components.app.script["reload"]
```

## Plan 展示

plan 当前展示短摘要，不直接展开完整脚本内容：

```text
  ! host.app1.components.app.script["reload"]
    run component script reload
    triggered_by: host.app1.components.app.files.file["/etc/managed-app/config.env"]
    command: script reload (once)
```

当前 ResourceGraph/plan 只输出短 `command_preview`。完整脚本执行载荷只保存在进程内的
operation 内部字段中，用于 apply 执行，不属于 plan text/json/html 的公共接口。

## 待确认问题

- `commands` 是否作为第一版能力落地，还是先只支持 `run` / `content`。
- 是否需要跨 component 合并同类操作，例如多个 component 都触发同一个 nginx reload。
- `mode = "once"` 多触发时，环境变量列表是否足够，是否需要 JSON stdin 作为后续扩展。
