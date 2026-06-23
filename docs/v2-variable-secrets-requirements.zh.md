# DebianForm v2 variable 与敏感数据需求文档

本文档定义 v2 顶层 `variable`、敏感值传播、运行时 secret 注入，以及它们与现有
`secrets.file` 的关系。目标是把 secret 的“来源”和“落到目标机的资源”拆开，避免
继续把本地 secret 文件路径绑定为唯一输入方式。

Terraform 是主要参照对象，但 DebianForm 不实现 Terraform 兼容层。这里借鉴它的
`variable` block、类型约束、赋值来源优先级，以及把 `sensitive`、`ephemeral` 和
write-only provider 参数拆开的设计，并按 DebianForm 的 HostSpec、ResourceGraph、
plan/state 和 SSH runner 做取舍。

这里的 `variable` 不能只是为了 secret 做的窄接口。它应成为 DebianForm program 的
通用外部输入机制，用来参数化 host/profile/component 中的环境差异、部署尺寸、功能开关、
版本、目标路径和运行时凭据。secret 只是其中一个高风险使用场景。

## 背景

当前 DebianForm v2 已有两类相关能力：

- `secrets.file`：声明一个目标机上的敏感文件，从本地 `source` 读取内容，默认
  `mode = "0600"`，plan/state 不写明文。
- `sensitive = true`：component input 或部分资源内容的敏感标记，用于 plan/state
  脱敏，并传播到由该值派生的 file/unit content。

这解决了“不要把 secret 明文写进 plan/state”的一部分问题，但语义仍然不完整：

- secret 来源被绑定为本地文件路径，无法直接从 stdin、CI secret、env、Vault、SOPS
  或其他运行时来源注入。
- `secrets.file` 同时表达“这是敏感输入来源”和“这是目标机文件资源”，边界不清楚。
- `sensitive` 主要是展示和 state 脱敏语义，不等价于“不进入 HostSpec/graph/plan/state”。
- apply 阶段仍可能通过 provider payload、runner command、debug log 或临时文件泄露。
- state 中保存 sha256/bytes 摘要可用于 drift/no-op，但对低熵 secret 可能形成可离线
  猜测的指纹。

更成熟的方向是：

```text
variable 定义值如何进入 DebianForm
sensitive 定义值如何展示和传播
ephemeral 定义值是否允许持久化
write-only 定义值是否只进入 provider apply 通道
files.file 定义目标机文件资源
```

## 目标

- 支持顶层 `variable`，用于 host/profile/component 配置中的外部输入。
- `variable` 是通用配置参数，不只服务于 secret；普通非敏感配置也应使用同一机制。
- 支持接近 Terraform 的 variable block 字段：`type`、`default`、`description`、
  `validation`、`sensitive`、`nullable`、`ephemeral`、`const`、`deprecated`。
- 支持与 component input 一致的结构化类型能力，包括 `list(object(...))`、嵌套
  `object`、`map(...)`、`set(...)`、`tuple(...)` 和 `optional(...)`。
- 支持多种外部赋值来源：CLI `-var`、`-var-file`、自动加载的 var file、环境变量、
  prompt/stdin，以及本地文件内容读取。
- 定义稳定、可解释的 variable 赋值优先级。
- 支持运行时注入 secret，避免要求用户在配置目录旁长期保留本地 secret 文件。
- 明确区分 `sensitive`、`ephemeral` 和 write-only 三种语义。
- 敏感值进入 `files.file.content`、`systemd.unit.content` 等字段时，自动传播
  sensitive 标记。
- ephemeral 值不得进入 HostSpec JSON、ResourceGraph JSON、plan JSON、state JSON、
  golden debug 输出或普通日志。
- write-only 值只允许进入 provider apply 通道，不得进入 desired/state/diff。
- `files.file` 能完整表达当前 `secrets.file` 的目标机文件部署能力。
- `secrets.file` 保留为过渡语法糖，之后可 deprecated，再视迁移情况删除。
- 所有敏感数据相关错误必须指向用户 DSL source path。

## 非目标

- 不实现 Terraform module system。
- 不要求完全兼容 Terraform `.tfvars` 文件格式、文件名约定或完整 CLI 行为；DebianForm
  可以采用 `.dbfvars`，但语义应同样清楚。
- 不实现 HCP Terraform workspace、variable set、remote run 等平台能力。
- 不承诺第一阶段支持所有 secret backend，例如 Vault、AWS Secrets Manager、GCP Secret
  Manager。
- 不承诺解决目标机 at-rest secret 问题。如果服务要求文件，secret 最终仍会写入目标机
  磁盘或 tmpfs。
- 不把 `sensitive = true` 解释成“不落盘”。`sensitive` 只负责脱敏和传播。
- 不允许 ephemeral 值参与会影响资源地址、集合 key、排序、依赖图结构的表达式。

## 与 Terraform variable 的差异

Terraform 的 `variable` 是 module 参数：root module 可以从 CLI、环境变量、`.tfvars`
和 HCP Terraform workspace 接收值，child module 由父 module 通过 `module` block 传值。
DebianForm 目前没有 Terraform module system，所以 v2 `variable` 应先定义为 program
级输入：同一个 `.dbf.hcl` program 中的 host/profile/component 都可以通过 `var.<name>`
引用它。

DebianForm 应尽量对齐 Terraform 的强能力：

| 能力 | Terraform | DebianForm 目标 |
| --- | --- | --- |
| 引用语法 | `var.name` | 使用 `var.name`，避免另造概念。 |
| 类型约束 | primitive、collection、structural、`any` | 与 component input 使用同一套 type system。 |
| 默认值 | `default` 可选；无 default 时必填 | 同样支持；默认值不得依赖 runtime facts 或其他 variable。 |
| 校验 | `validation` block | 同样支持，错误定位到用户 DSL source path。 |
| 脱敏 | `sensitive` 隐藏 CLI/UI 输出，但普通 sensitive 仍可进 state | 同样脱敏；并要求 taint 传播到 DebianForm resource content。 |
| 不持久化 | `ephemeral` 不进 plan/state，且可引用位置受限 | 同样支持；额外要求不进 HostSpec/ResourceGraph/debug JSON。 |
| write-only | provider resource 参数声明 write-only | DebianForm 需要 provider apply payload 边界，不把值写回 state/diff。 |
| early eval | `const` 可用于 init 等早期阶段 | 可保留 `const`，用于未来 imports、backend、plugin/source 等早期解析场景。 |
| 废弃提示 | `deprecated` | 同样支持，便于长期维护配置接口。 |
| 赋值来源 | CLI、var-file、auto tfvars、env、workspace | 支持 CLI、dbfvars、auto dbfvars、env、prompt/stdin；不做 workspace。 |
| 文件内容输入 | Terraform CLI 没有通用 `@path` 语义 | DebianForm 应增加 `@path`/`@-`，作为部署工具的实用扩展。 |

关键差异：

- Terraform 的 variable 是 module API；DebianForm 的 variable 是 program API。component
  仍保留自己的 `input` 作为组件 API，但两者应共享类型系统、validation、sensitive 和
  ephemeral 传播实现。
- Terraform 的 `sensitive` 只保证显示层脱敏，不保证不进入 state；DebianForm 必须保持
  同样清晰的语义，不能把 `sensitive` 偷偷解释成 ephemeral。
- DebianForm 有 HostSpec 和 ResourceGraph 两层内部产物，因此 ephemeral 的禁止落盘范围
  比 Terraform 的 plan/state 更宽。
- DebianForm 通过 SSH runner 写目标机，write-only 的安全边界必须覆盖 command preview、
  stdout/stderr、临时文件和 provider payload。

## 能力分级

为了避免实现成弱版 variable，能力按三层定义。

### Core

Core 是第一版 variable 必须具备的通用配置能力：

- `var.<name>` 引用。
- `type`、`default`、`description`、`nullable`。
- primitive、collection、structural type constraints。
- 外部赋值：`-var`、`-var-file`、环境变量。
- required variable 校验。
- `validation` block。
- source path 清晰的错误信息。

### Secure

Secure 是 secret 场景必须具备的能力：

- `sensitive` 脱敏和表达式传播。
- `ephemeral` 传播和持久化禁止。
- write-only provider payload。
- redacted runner channel。
- `@path`、`@-`、prompt/stdin 输入。
- `content_version` 或同类非敏感版本触发机制。

### Ergonomic

Ergonomic 是让 variable 足够好用的能力：

- `.dbfvars` 和 `.auto.dbfvars`。
- JSON var file。
- env 前缀，例如 `DBF_VAR_name`。
- complex value 在 CLI/env 中可用 JSON 表达。
- `deprecated` warning。
- `const` early-eval 变量。
- `dbf variable inspect` 或等价命令，用于列出变量接口、类型、默认值、是否敏感和说明。

## 术语

### sensitive

`sensitive` 是展示和传播语义：

- CLI、plan text、plan JSON、HostSpec debug 输出和错误上下文不得打印明文。
- 引用 sensitive 值得到的新值默认继续 sensitive。
- sensitive 值可以进入 state，除非同时是 ephemeral 或 write-only。
- 对非 ephemeral 的 sensitive content，state 可以保存受控摘要，用于 drift/no-op。

### ephemeral

`ephemeral` 是持久化语义：

- 值只在当前进程运行期存在。
- 不写入 HostSpec、ResourceGraph、plan、state、cache、golden fixture 或普通日志。
- 不能用于决定资源地址、map key、set key、count-like 结构、依赖边和删除策略。
- 只能传入允许运行时求值的字段，例如 write-only provider 参数或明确支持 ephemeral 的
  resource argument。

### write-only

`write-only` 是 provider 边界语义：

- 值可以在 apply 时交给 provider。
- provider 不得把该值写回 desired、observed、state 或 plan。
- plan 阶段不得要求读取该值才能生成非敏感 diff。
- 资源更新应由非敏感触发字段决定，例如 `content_version`、`secret_version` 或受控
  digest。

## 用户语法

### 顶层 variable

```hcl
variable "wg_private_key" {
  type        = string
  description = "WireGuard private key for this host."
  sensitive   = true
  ephemeral   = true
  nullable    = false
}
```

规则：

- `variable` 是顶层 block，必须有且只有一个 label。
- variable label 在同一 program 内必须唯一。
- `type` 必填。
- `description` 可选，但公开配置推荐填写。
- `default` 可选；没有 default 时必须由外部赋值。
- `nullable` 可选，默认 `true`。
- `sensitive` 可选，默认 `false`。
- `ephemeral` 可选，默认 `false`。
- `const` 可选，默认 `false`。
- `deprecated` 可选，值为非空字符串。
- `validation` block 可重复。

完整形态：

```hcl
variable "name" {
  type        = <TYPE>
  default     = <DEFAULT_VALUE>
  description = "<DESCRIPTION>"
  sensitive   = <true|false>
  nullable    = <true|false>
  ephemeral   = <true|false>
  const       = <true|false>
  deprecated  = "<MESSAGE>"

  validation {
    condition     = <EXPRESSION>
    error_message = "<MESSAGE>"
  }
}
```

### 普通配置变量

```hcl
variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "prod"
  nullable    = false

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "listeners" {
  type = list(object({
    name = string
    port = number
    tls  = optional(bool, false)
  }))

  default  = []
  nullable = false
}
```

这些变量不是 secret，也不应走 secret-only 特例。它们应和 component input 一样参与类型
检查、默认值填充、validation 和 source path 错误定位。

### 外部赋值

第一阶段至少支持：

```bash
dbf plan  -f site.dbf.hcl -var wg_private_key=@/run/secrets/wg.key
dbf apply -f site.dbf.hcl -var wg_private_key=@/run/secrets/wg.key
dbf apply -f site.dbf.hcl -var-file prod.dbfvars
```

建议语义：

- `name=value` 表示字面值。
- `name=@path` 表示从本地文件读取，读取结果作为 value，不把 path 当作资源语义写入
  HostSpec/graph/state。
- `name=@-` 表示从 stdin 读取。
- `-var-file` 支持 HCL 风格 key/value；后续可支持 JSON。
- CLI 参数中的 sensitive value 不得出现在错误、debug log 或 shell command preview 中。

后续可增加：

```bash
dbf apply -var-file secrets.dbfvars
dbf apply -var wg_private_key=env:WG_PRIVATE_KEY
dbf apply -var wg_private_key=cmd:pass-show-wireguard
```

这些来源必须遵守同一套 sensitive/ephemeral 语义。

赋值优先级建议从高到低：

1. CLI `-var`，按出现顺序后者覆盖前者。
2. CLI `-var-file`，按出现顺序后者覆盖前者。
3. 自动加载的 `*.auto.dbfvars` 或 `*.auto.dbfvars.json`，按文件名字典序。
4. `debianform.dbfvars` 或 `debianform.dbfvars.json`。
5. 环境变量，例如 `DBF_VAR_<name>`。
6. variable `default`。

未声明变量的处理：

- CLI `-var` 传入未声明变量应报错。
- var file 中的未声明变量应 warning 或 error；建议第一版直接 error，避免拼写错误静默生效。
- 环境变量中的未声明变量可以忽略，避免污染 CI 环境导致失败。

### 用 variable 写敏感文件

```hcl
variable "wg_private_key" {
  type      = string
  sensitive = true
  ephemeral = true
  nullable  = false
}

host "wg-a" {
  files {
    file "/etc/wireguard/private.key" {
      content = var.wg_private_key
      owner   = "root"
      group   = "systemd-network"
      mode    = "0640"
    }
  }
}
```

要求：

- `file.content` 引用了 sensitive 值时，该 file 自动 `sensitive = true`。
- `file.content` 引用了 ephemeral 值时，该 content 不得进入 desired/state/plan。
- 目标机仍会写入 `/etc/wireguard/private.key`。这是资源语义，不应被误描述为
  “secret 不落盘”。

### 显式版本触发

ephemeral/write-only 内容不进入 state 后，系统无法仅靠 state 中的明文或摘要判断内容
是否变化。用户应显式提供非敏感版本：

```hcl
variable "app_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "app_token_version" {
  type     = string
  nullable = false
}

files {
  file "/etc/app/token" {
    content         = var.app_token
    content_version = var.app_token_version
    mode            = "0600"
  }
}
```

规则：

- `content_version` 不敏感，可进入 desired/state/diff。
- `content_version` 变化触发 update。
- 如果 write-only content 没有 version，plan 可以保守显示 `replace/update required`，但
  apply 后不能保证下一次 no-op。

## 与 `secrets.file` 的关系

当前语法：

```hcl
secrets {
  file "/etc/app/token" {
    source = "secrets/app-token"
    owner  = "root"
    group  = "root"
    mode   = "0600"
  }
}
```

应逐步等价为：

```hcl
variable "app_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

files {
  file "/etc/app/token" {
    content = var.app_token
    owner   = "root"
    group   = "root"
    mode    = "0600"
  }
}
```

迁移策略：

- 第一阶段保留 `secrets.file`，不改变现有配置行为。
- 第二阶段将 `secrets.file` 编译为 `files.file` 的敏感语法糖，内部复用同一条 file
  provider 路径。
- 第三阶段给 `secrets.file` 增加 deprecation warning，提示用户改用
  `variable + files.file`。
- 删除 `secrets.file` 只能在 `files.file` 支持 sensitive/ephemeral/write-only 后进行。

保留 `secrets.file` 的短期价值：

- 简单本地文件部署仍然方便。
- 现有示例和集成测试不用立即破坏。
- 在 write-only runner 完成前，`secrets.file` 可继续提供已验证的最小安全边界。

## 编译与 IR 要求

### Parser

- 解析顶层 `variable` block。
- 支持 `type`、`default`、`description`、`nullable`、`sensitive`、`ephemeral`、
  `validation`。
- `var.<name>` 表达式可在 host/profile/component 中引用。
- 未赋值且无 default 的 variable 必须报错。
- 外部赋值必须按 type constraint 进行转换和校验。

### Value

- Value 需要同时携带：
  - `Sensitive bool`
  - `Ephemeral bool`
  - `WriteOnly bool` 或等价的 provider-only 标记
- 表达式求值时：
  - 任一输入 sensitive，结果 sensitive。
  - 任一输入 ephemeral，结果 ephemeral。
  - write-only 值不得参与普通表达式，除非目标字段允许 write-only。

### HostSpec

- HostSpec 不得包含 ephemeral 明文。
- HostSpec 可以保留非敏感 variable 的归一化结果。
- HostSpec debug JSON 对 sensitive 非 ephemeral 值必须脱敏。
- HostSpec 中的资源字段需要能表达“该字段运行时才可用”，避免用空字符串或
  `"<sensitive>"` 伪装真实值。

### ResourceGraph

- `Node.Desired` 只保存可持久化 desired。
- `Node.ProviderPayload` 或新的运行时 payload 结构可以携带 apply 所需值，但输出 JSON
  时必须默认脱敏或完全省略。
- 需要区分：
  - plan 可见 desired
  - provider apply payload
  - state persisted desired

## Plan 与 State 要求

### Plan

- sensitive diff 不显示明文。
- ephemeral 值不进入 plan JSON。
- write-only 字段在 plan 中显示为 `<write-only>` 或只显示版本变化。
- 如果缺少可持久化版本，plan 应明确显示该资源无法可靠判断 no-op，而不是伪装成
  精确 diff。

### State

- state 不得保存 ephemeral 明文。
- state 不得保存 write-only 明文。
- sensitive 非 ephemeral content 可以保存摘要，但应明确这是 drift/no-op 辅助信息。
- 对低熵 secret，推荐使用用户显式 `content_version`，避免把可离线猜测的 sha256 长期
  写入 state。
- `source_path` 对 sensitive/ephemeral 来源默认不写入 state。

## Provider 与 Runner 要求

- provider API 需要显式接收 write-only payload。
- write-only payload 不得被 `ProviderPlan`、`Observed`、state resource 或 debug log 返回。
- SSH runner 应支持 redacted command/payload：
  - command preview 不包含 secret。
  - 日志不包含 secret。
  - 错误消息不回显 secret。
- 写入文件时，优先使用不会把 secret 拼进可见 shell command 的通道，例如 stdin、
  sftp/scp 或受控临时文件。
- 临时文件应使用严格权限，并在失败路径尽量清理。

## 校验规则

- ephemeral 值不得用于 resource address、map key、set key、label、depends_on、
  lifecycle、owner/group/path 等结构性字段。
- write-only 值不得用于 plan 阶段必须比较的普通 desired 字段。
- `files.file.content` 可接受 sensitive/ephemeral 值。
- `files.file.path`、`owner`、`group`、`mode`、`ensure` 不接受 ephemeral 值。
- sensitive 值进入非敏感输出字段时，系统应自动 taint，而不是要求用户手写
  `sensitive = true`。
- 如果用户显式设置 `sensitive = false`，但 content 引用了 sensitive 值，应以传播结果为准。

## 实施阶段

### 阶段一：运行时 variable

- 支持顶层 `variable`。
- 支持 `-var name=value`、`-var name=@path`、`-var name=@-`。
- 支持 type/default/nullable/validation/sensitive。
- sensitive 传播到 file/unit content。
- 不支持 ephemeral 时，文档必须明确 sensitive 仍可能进入内部 desired。

### 阶段二：ephemeral 与 write-only

- Value 增加 ephemeral 传播。
- HostSpec/graph/plan/state 禁止保存 ephemeral。
- provider API 增加 write-only payload。
- runner 增加 redacted payload 通道。
- `files.file.content` 支持 ephemeral/write-only。

### 阶段三：`secrets.file` 语法糖化

- `secrets.file` 内部编译到 `files.file` 敏感路径。
- 新示例优先使用 `variable + files.file`。
- 文档标记 `secrets.file` 为兼容层。

### 阶段四：废弃评估

- 当所有示例、集成测试和用户迁移路径稳定后，给 `secrets.file` 增加 warning。
- 至少一个 minor 周期后再考虑删除。

## 未决问题

- 是否提供 `.dbfvars` 文件格式，还是只支持 CLI 和 stdin。
- 是否内置 SOPS/age，还是只提供 `cmd:`/stdin 接口交给外部工具。
- state 中是否默认保存 sensitive content sha256，还是要求用户显式 opt-in。
- `content_version` 应使用统一字段名，还是每个资源定义自己的 version 字段。
- plan 在缺少 write-only version 时是每次显示 update，还是要求报错。
