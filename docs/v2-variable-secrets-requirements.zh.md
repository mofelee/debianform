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

## 实施 Loop

以下 loop 把本文档拆成可合并闭环，格式对齐 `docs/v2-implementation-plan.zh.md`。每个
loop 都应能独立提交、独立测试，并保持现有示例和集成测试继续通过。

状态约定：

- [x] 已完成
- [ ] 未完成

总体原则：

- [ ] 每个 loop 都必须形成代码、测试、示例或 fixture、文档和验收输入的闭环。
- [ ] parser、CLI、HostSpec、ResourceGraph、plan/state、provider 和 runner 的边界变更
      应分轮推进；除非本轮目标明确要求，不在同一轮同时改多个安全边界。
- [ ] golden fixture 只更新本 loop 直接影响的内容。
- [ ] 文档中的新增语法至少有一个对应测试、fixture 或 CLI smoke。
- [ ] 涉及 sensitive、ephemeral、write-only 或 runner payload 的 loop 必须增加“不泄露
      明文”的负向断言。
- [ ] `make test` 必须通过；如果某类集成测试暂时无法运行，需要在实现记录中说明原因。

### 当前基线

- [x] v2 已有 `secrets.file`，并要求 plan/state 不写 secret 明文。
- [x] component input 已支持 `sensitive` 标记和派生 file/unit content 脱敏。
- [x] plan/state 已有 sensitive content 摘要或 redaction 机制。
- [ ] 顶层 `variable` 尚未实现。
- [ ] 外部赋值来源、`ephemeral`、write-only provider payload 和 redacted runner channel
      尚未完整打通。

### Loop 0：现状基线和回归护栏

目标：在动 variable 前，先固定当前 secret/sensitive 行为，避免后续 parser、IR 或
provider 重构让已有安全边界退化。

范围：

- `secrets.file`
- `files.file sensitive = true`
- component sensitive input 派生的 file、systemd unit 和 service environment
- HostSpec JSON、plan text/JSON、state JSON 和普通日志的泄漏断言

暂不做：

- 顶层 `variable`
- CLI `-var`/`-var-file`
- `ephemeral`
- write-only provider payload
- runner API 改造

代码：

- [x] 整理现有 sensitive/secret 测试 fixture，形成后续 loop 可复用的泄漏断言 helper。
- [x] 明确当前允许出现的非 secret 元数据，例如非 state 调试输出里的 `source_path`。
- [x] 对当前不能立刻收口的元数据泄漏点加 TODO，并写清楚后续 loop 负责关闭。

测试：

- [x] `secrets.file` 明文不进入 state 和 plan。
- [x] `files.file sensitive = true` 明文不进入 HostSpec JSON、plan 和 state。
- [x] sensitive component input 派生的 file/unit content 不进入 HostSpec JSON、plan 和
      state。
- [x] 普通 non-sensitive file 仍保留可读 text diff。

示例/文档：

- [x] 保持现有示例语法不变。
- [x] 在本文档记录当前 baseline 和已知 TODO，不新增用户可见语法。

验收：

- [x] 不改变用户语法和 plan/state 格式。
- [x] `make test` 通过。

Loop 0 实现记录：

- 新增 `internal/v2/testassert.NoSecretLeak`，统一检查当前 placeholder secret
  明文不会进入 HostSpec JSON、ResourceGraph desired、plan text/JSON/HTML、state JSON。
- 复用现有 `v2-foundation`、`v2-files-plan-preview`、`v2-component-inputs` fixture，并新增
  `internal/v2/testdata/fixtures/v2-sensitive-service-environment.dbf.hcl` 覆盖 structured
  `systemd.service_unit.environment` 的 sensitive 传播。
- 当前允许的非明文元数据：plan/state 可保留 `content_sha256`、`content_bytes` 等摘要；
  非 sensitive 的普通 file 仍输出可读 text diff。
- 已知 TODO：`ResourceGraph.Node.ProviderPayload` 仍是 provider apply 输入通道，可能携带
  sensitive content；本轮测试只把 `Node.Desired` 作为 plan/state 可见 desired 边界。该
  payload 应在 write-only provider payload 和 redacted runner channel loop 中收口。
- 已知 TODO：native runner 的实际 apply script 仍可能包含 base64 编码后的文件内容；本轮
  不改 runner API，后续 redacted runner channel loop 负责避免 command preview、日志和
  错误输出泄漏。

### Loop 1：解析顶层 variable 声明

目标：让 parser 和 IR 认识顶层 `variable` block，但不接入外部赋值，也不允许
`var.<name>` 引用。

范围：

- 顶层 `variable "name" { ... }`
- `type`
- `default`
- `description`
- `nullable`
- `sensitive`
- `ephemeral`
- `const`
- `deprecated`
- `validation` metadata

暂不做：

- `var.<name>` 表达式求值
- CLI/env/var file 赋值
- validation 执行
- `const` early-eval 行为
- sensitive/ephemeral 传播

代码：

- [x] IR 增加 `Program.Variables map[string]VariableSpec` 或等价结构。
- [x] parser 支持顶层 `variable` block，且必须有且只有一个 label。
- [x] variable label 在同一 program 内必须唯一。
- [x] 复用 component input 的 type parser、type normalization 和 validation block parser。
- [x] `default` 按 type constraint 做归一化，但只保存 metadata，不注入表达式上下文。
- [x] HostSpec debug 输出可展示 non-sensitive variable metadata；sensitive default 必须
      redacted。

测试：

- [x] parser 单测覆盖 primitive、`list(object(...))`、`map(...)`、`set(...)`、
      `tuple(...)` 和 optional object attribute。
- [x] parser 负例覆盖重复 label、错误 label 数量、未知字段、错误 type expression。
- [x] default 归一化测试覆盖 object optional default、nullable 和类型不匹配。
- [x] sensitive default 的 JSON/debug 输出不包含明文。

示例/文档：

- [x] 增加一个只声明 variable 的最小 fixture。
- [x] 文档注明本轮只接受声明，不允许引用。

验收：

- [x] `dbf validate` 可以接受只声明 variable 但未引用的配置。
- [x] 现有 component input 行为和 golden 不变化。
- [x] `make test` 通过。

Loop 1 实现记录：

- `parser.Config` 现在包含顶层 `Variables`，`variable "name"` 解析 `type`、`default`、
  `description`、`nullable`、`sensitive`、`ephemeral`、`const`、`deprecated` 和重复
  `validation` metadata。
- `ir.Program` 现在包含 `Variables map[string]VariableSpec`。本轮只导出声明 metadata，不把
  variable default 注入 `var` 求值上下文，也不执行 variable validation。
- `default` 在 merge 阶段复用 component input 的结构化 type normalization；object
  `optional(...)` 默认值会写入 IR metadata。
- sensitive variable default 在 `Program` JSON 中输出为 `"<sensitive>"`。
- 新增 `internal/v2/testdata/fixtures/v2-variable-declarations.dbf.hcl` 作为只声明 variable
  的验收输入；`dbf validate` 可接受该 fixture。

### Loop 2：变量求值上下文和 `var.<name>`

目标：让 host/profile/component 配置可以引用 variable default，打通最小 program-level
input 语义，但仍不处理 CLI 覆盖。

范围：

- `var.<name>` namespace
- required variable 检查
- variable default 求值
- default 的 source location 和错误路径
- ordinary non-sensitive variable 在 HostSpec/ResourceGraph 中的稳定输出

暂不做：

- CLI/env/var file 外部赋值
- variable validation 执行
- `@path`/`@-`
- sensitive 自动 taint
- ephemeral 持久化限制

代码：

- [x] evaluator 增加只读 `var` namespace。
- [x] variable final value 先由 default 生成；无 default 且无外部值时报错。
- [x] 只允许引用已声明 variable。
- [x] variable default 不得引用 `var`、`input`、`target`、`path` 或 runtime facts。
- [x] host/profile/component 展开时使用同一份归一化后的 variable value。
- [x] 错误信息指向 variable 声明或引用处的 source path。

测试：

- [x] `files.file.content = var.message` 可以生成预期 HostSpec。
- [x] `system.hostname = var.hostname` 可以生效。
- [x] component body 可以读取 `var.<name>`。
- [x] 引用不存在的 variable 报错。
- [x] required variable 未赋值报错。
- [x] default 依赖 runtime facts、`input` 或其他 `var` 报错。

示例/文档：

- [x] 增加一个只靠 default 参数化普通文件内容或 hostname 的 fixture。
- [x] 文档说明 DebianForm variable 是 program API，不是 component input 的替代品。

验收：

- [x] 只靠 default 的普通 variable 可用于 host/profile/component 展开。
- [x] `dbf validate`、`dbf plan --offline` 对新增 fixture 成功。
- [x] `make test` 通过。

Loop 2 实现记录：

- parser 现在先解析 `locals` 和顶层 `variable` 声明，再解析 host/profile/component；最终
  variable default 被归一化后作为只读 `var` namespace 注入普通表达式求值。
- 本轮没有外部赋值来源；没有 `default` 的 variable 在 parse 阶段报 required error。
- variable default 只允许读取 `local.*` 和常量表达式；读取 `var`、`path`、`input`、
  `target` 等 namespace 会报 source path 清晰的错误。
- 新增 `internal/v2/testdata/fixtures/v2-variable-defaults.dbf.hcl`，覆盖 host、profile 和
  component body 同时读取同一份 `var.<name>` default。
- 本轮仍不执行 variable validation，也不把 `sensitive` 传播进表达式结果；这些留给后续
  validation 和 sensitive propagation loops。

### Loop 3：CLI `-var` 字面值

目标：实现最小外部赋值入口，让用户可以从命令行覆盖普通配置参数；本轮不读取文件、
stdin 或 secret backend。

范围：

- `dbf validate -var name=value`
- `dbf plan -var name=value`
- `dbf apply -var name=value`
- repeated `-var`
- CLI value 按 variable type conversion

暂不做：

- `-var-file`
- 自动 var file
- 环境变量
- `@path`/`@-`
- prompt
- `env:`/`cmd:` secret source

代码：

- [x] CLI 支持重复 `-var name=value`。
- [x] `-var` 优先级高于 default。
- [x] 重复 `-var` 按出现顺序后者覆盖前者。
- [x] 未声明 variable 通过 `-var` 传入时报错。
- [x] string/number/bool 按目标 type 解析；复杂类型第一版可要求 JSON 字符串。
- [x] sensitive variable 的 CLI value 不进入错误上下文和 debug log。

测试：

- [x] string、number、bool、list、object 赋值成功。
- [x] `-var` 覆盖 default。
- [x] 类型不匹配报错并指向 variable name。
- [x] 重复 `-var` 后者覆盖前者。
- [x] 未声明 variable 报错。
- [x] sensitive CLI value 不出现在错误输出和 snapshot。

示例/文档：

- [x] 增加 `examples` 或 CLI smoke fixture，展示 `-var env=prod`。
- [x] 文档明确复杂值第一版的 CLI 编码规则。

验收：

- [x] 用户可以用 `dbf validate/plan/apply -var env=prod` 参数化普通配置。
- [x] `make test` 通过。

实现记录：

- `dbf validate`、`dbf plan`、`dbf apply` 现在都接受可重复的 `-var name=value`。
  parser 会在默认值前应用这些外部值，同名变量以后出现的值为准。
- CLI 字面值按声明的 variable type 解释：`string` 保留原始字符串，`number`/`bool`
  使用 HCL 字面值解析，`list`/`map`/`object`/`tuple`/`set`/`any` 可使用 JSON
  字符串，例如 `-var ports=[80,443]` 或 `-var labels={"tier":"frontend"}`。
- 未声明变量会报错；sensitive variable 的非法 CLI value 会返回脱敏错误，不包含原始
  value。
- 新增 `internal/v2/testdata/fixtures/v2-variable-cli.dbf.hcl` 和 CLI smoke，覆盖
  `-var environment=prod`、复杂值、重复覆盖、未声明变量和 sensitive 错误脱敏。

### Loop 4：var file、auto var file 和环境变量

目标：让 variable 成为可用的通用配置输入，而不是只能依赖命令行字符串。

范围：

- `-var-file path`
- HCL 风格 `.dbfvars`
- JSON var file
- `*.auto.dbfvars`
- `debianform.dbfvars`
- `DBF_VAR_<name>`
- 赋值优先级

暂不做：

- `@path`/`@-`
- prompt
- `env:`/`cmd:` source
- workspace 或远端 variable set
- secret backend 集成

代码：

- [x] CLI 支持重复 `-var-file path`。
- [x] 支持 HCL 风格 var file：顶层 `name = value`。
- [x] 支持 JSON var file，至少覆盖对象顶层 key/value。
- [x] 自动加载 `*.auto.dbfvars`、`*.auto.dbfvars.json`、`debianform.dbfvars` 和
      `debianform.dbfvars.json`。
- [x] 支持环境变量 `DBF_VAR_<name>`。
- [x] 固定优先级：`-var` > `-var-file` > auto/default var files > env > default。
- [x] CLI/var-file 传入未声明变量时报错；env 未声明变量忽略。

测试：

- [x] var file 中 string/list/object 正确解析。
- [x] 多个 `-var-file` 后者覆盖前者。
- [x] auto var file 按文件名字典序加载。
- [x] env 低于 var file 和 CLI。
- [x] var file 未声明变量报错。
- [x] env 未声明变量忽略。
- [ ] sensitive value 不进入错误输出、HostSpec、plan 或 state 明文。

示例/文档：

- [x] 增加 `prod.dbfvars` 或等价 fixture。
- [x] 文档写清楚优先级和未声明变量处理规则。

验收：

- [x] 普通部署参数可以完全从 var file/env 注入。
- [x] `dbf validate/plan/apply` 对 CLI、var file 和 env 的行为一致。
- [x] `make test` 通过。

实现记录：

- `dbf validate`、`dbf plan`、`dbf apply` 和 `dbf check` 现在都接受可重复的
  `-var-file path`，格式支持 HCL 顶层属性和 JSON 顶层对象。
- 自动 var file 从配置文件所在目录加载：先 `debianform.dbfvars`、再
  `debianform.dbfvars.json`，随后按路径字典序加载 `*.auto.dbfvars` 和
  `*.auto.dbfvars.json`。自动加载要求本次配置文件来自同一目录。
- 环境变量使用 `DBF_VAR_<name>`，按 CLI 字面值规则解析；未声明环境变量会忽略。
- 总优先级由低到高为：`DBF_VAR_*`、`debianform.dbfvars*`、`*.auto.dbfvars*`、
  重复 `-var-file`、重复 `-var`。同一层级内后出现的值覆盖先出现的值。
- CLI 和 var file 的未声明变量会报错；env 未声明变量忽略，避免 CI 环境污染导致失败。
- sensitive 值的完整 HostSpec、plan 和 state 脱敏传播仍留在 Loop 6。

### Loop 5：validation、nullable、deprecated 和 inspect

目标：把 variable 做成稳定公开接口，而不是裸 map；用户可以校验、文档化和检查 program
需要哪些外部输入。

范围：

- variable `validation`
- `nullable = false`
- `deprecated`
- warning 聚合
- `dbf variable inspect` 或等价 inspect 命令

暂不做：

- `ephemeral`
- write-only
- `const` early-eval
- secret source 读取
- external secret backend

代码：

- [x] variable `validation` 在最终赋值和 type conversion 后执行。
- [x] validation 复用 component input 的纯函数集合和错误定位机制。
- [x] `nullable = false` 禁止最终值为 null。
- [x] `deprecated` 只在变量被显式赋值时 warning；只使用 default 不 warning。
- [x] warning 聚合方式与 validate/plan/apply 现有 warning 输出保持一致。
- [x] 增加 `dbf variable inspect -f <file>`，输出 name、type、default、nullable、
      sensitive、ephemeral、description 和 deprecated。
- [x] inspect 对 sensitive default 显示 `<sensitive>`。

测试：

- [x] validation 成功和失败。
- [x] validation condition 非 bool 报错。
- [x] validation 不能读取 `target`、`input`、`path` 或未允许的 namespace。
- [x] nullable false 拦截 null。
- [x] deprecated 显式赋值产生 warning，default 不产生 warning。
- [x] inspect golden 覆盖复杂类型、default redaction 和 deprecated message。

示例/文档：

- [x] 示例 variable 增加 environment validation。
- [x] README 或本文档增加 `dbf variable inspect` 输出样例。

验收：

- [x] variable 可以作为公开配置接口被文档化和检查。
- [x] validation failure 指向 variable source path。
- [x] `make test` 通过。

实现记录：

- variable validation 在最终赋值、类型归一化和 optional/default 填充之后执行，使用与
  component input validation 相同的纯函数集合。
- validation 条件只能读取当前变量的 `var.<name>`；读取 `target`、`input`、`path` 或其他
  variable 会报错，失败错误指向 `variable["name"].validation[i]`。
- `nullable = false` 会拦截最终值为 `null` 的 default、CLI、var file 或 env 赋值。
- `deprecated` 只在变量被外部显式赋值时产生 warning，沿用 validate/plan/apply 的
  warning 输出格式；只使用 default 不 warning。
- 新增 `dbf variable inspect -f <file>`，可配合 `-var`/`-var-file` 查看最终变量接口。
  示例输出：

```json
{
  "variables": [
    {
      "name": "environment",
      "type": "string",
      "default": "prod",
      "nullable": false,
      "sensitive": false,
      "ephemeral": false,
      "deprecated": "Use deployment_environment instead.",
      "description": "Deployment environment."
    },
    {
      "name": "token",
      "type": "string",
      "default": "<sensitive>",
      "nullable": true,
      "sensitive": true,
      "ephemeral": false
    }
  ]
}
```

### Loop 6：sensitive 传播到资源内容

目标：补齐 variable source 到表达式结果和资源 payload 的 sensitive taint 传播。`sensitive`
在本轮仍只表示展示和 state 脱敏，不表示不落盘。

范围：

- variable value 的 sensitive mark
- 表达式结果 sensitive 聚合
- `files.file.content`
- `systemd.unit.content`
- structured service unit environment
- CLI、inspect、错误上下文和 debug 输出 redaction

暂不做：

- `ephemeral`
- write-only provider payload
- `@path`/`@-`
- runner 通道改造
- 低熵 secret digest 策略调整

代码：

- [ ] variable 注入 evaluator 时携带 `Sensitive` mark。
- [ ] `jsonencode`、模板字符串、map/list/object 等表达式结果聚合 sensitive mark。
- [ ] `files.file.content` 引用 sensitive 值时自动设置 file sensitive。
- [ ] `systemd.unit.content` 和 service environment 引用 sensitive 值时自动设置 unit 或
      environment sensitive。
- [ ] 用户显式 `sensitive = false` 不能覆盖表达式传播出来的 sensitive mark。
- [ ] CLI/inspect/错误上下文不得打印 sensitive 明文。

测试：

- [ ] sensitive variable 写入 file 后，plan/state/HostSpec 不出现明文。
- [ ] sensitive variable 经 `jsonencode`、模板字符串、map/list/object 传播后仍 sensitive。
- [ ] 用户显式 `sensitive = false` 不能覆盖传播结果。
- [ ] 普通 non-sensitive variable 仍显示 text diff。
- [ ] 错误输出和 warning 中不包含 sensitive 明文。

示例/文档：

- [ ] 增加 sensitive variable 派生 file 的 fixture。
- [ ] 文档明确 `sensitive` 不是 `ephemeral`，目标机文件仍会写入磁盘或 tmpfs。

验收：

- [ ] `sensitive` 可安全用于脱敏和摘要展示。
- [ ] 本轮不引入“不持久化”语义。
- [ ] `make test` 通过。

### Loop 7：`@path`、`@-` 和运行时输入

目标：让 secret 可以从运行时来源注入，不要求长期存在于配置目录旁的 `secrets/` 文件。

范围：

- `-var name=@path`
- `-var name=@-`
- 可选 `-var name=env:ENV_NAME`
- stdin/prompt 输入路径
- sensitive source path redaction

暂不做：

- `cmd:` source
- SOPS/age/Vault 等 secret backend
- `ephemeral`
- write-only provider payload
- runner 通道改造

代码：

- [ ] CLI `-var name=@path` 读取本地文件内容作为 value。
- [ ] CLI `-var name=@-` 从 stdin 读取。
- [ ] 可选支持 `-var name=env:ENV_NAME`，从指定环境变量读取 value。
- [ ] 对 sensitive variable，读取路径和值都不得进入 HostSpec、ResourceGraph、plan、
      state、debug log 或错误输出。
- [ ] 缺失文件、权限错误和 stdin 读取错误不能回显部分 secret。

测试：

- [ ] `@path` 内容进入目标 file content。
- [ ] `@-` 可以从测试 stdin 注入。
- [ ] `env:ENV_NAME` 行为若实现，覆盖存在、缺失和空值。
- [ ] 缺失文件报错但不打印路径之外的敏感内容；sensitive source path 按规则 redacted。
- [ ] plan/state 不包含注入的 secret 明文或本地 secret 路径。

示例/文档：

- [ ] 增加 `variable + files.file + -var secret=@path` fixture 或 CLI smoke。
- [ ] 文档说明 `@path` 是输入来源，不是目标资源 source path。

验收：

- [ ] 用户可以用 `variable + files.file` 替代简单 `secrets.file` 来源。
- [ ] `make test` 通过。

### Loop 8：ephemeral 值传播和结构性字段限制

目标：实现“不进入持久化产物”的值级语义，并在编译阶段阻止 ephemeral 值影响资源结构。

范围：

- variable value 的 `Ephemeral` mark
- 表达式结果 ephemeral 聚合
- HostSpec/ResourceGraph/plan/state 序列化边界
- ephemeral 可用字段白名单
- 结构性字段限制

暂不做：

- provider write-only payload 完整改造
- runner redaction 改造
- `secrets.file` 迁移
- secret backend 集成

代码：

- [ ] Value 增加 `Ephemeral` mark，并在表达式求值中传播。
- [ ] HostSpec JSON、ResourceGraph JSON、plan JSON、state JSON 和 golden debug 输出禁止
      包含 ephemeral 明文。
- [ ] ephemeral 值只能进入明确支持 runtime-only/write-only 的字段。
- [ ] resource label、path、owner、group、mode、ensure、lifecycle、map/set key、
      depends_on 等结构性字段禁止 ephemeral。
- [ ] 编译错误必须指向产生 ephemeral 值的引用位置和目标字段。

测试：

- [ ] ephemeral variable 用于 `files.file.content` 可以通过到本轮定义的 runtime-only
      边界。
- [ ] ephemeral variable 用于 `files.file.path` 报错。
- [ ] ephemeral variable 用于 map key/set key 报错。
- [ ] ephemeral variable 用于 depends_on/lifecycle 报错。
- [ ] HostSpec/ResourceGraph/plan/state/golden 中不包含 ephemeral 明文。

示例/文档：

- [ ] 增加 ephemeral content 的最小 fixture。
- [ ] 文档列出第一版允许和禁止 ephemeral 的字段。

验收：

- [ ] ephemeral 的编译产物安全边界成立。
- [ ] 不支持 ephemeral 的字段全部 fail closed。
- [ ] `make test` 通过。

### Loop 9：write-only provider payload 和版本触发

目标：把 ephemeral content 从 desired/state/diff 中拿掉，只在 apply 时进入 provider，并用
非敏感版本字段触发更新。

范围：

- ResourceGraph persisted desired 和 provider apply payload 分离
- `files.file.content` write-only payload
- `content_version` 或同类非敏感触发字段
- provider plan/state/observed redaction
- no-op/drift 语义

暂不做：

- runner command/log 通道改造
- `secrets.file` 语法糖迁移
- 多 provider 的通用 write-only 注册表

代码：

- [ ] ResourceGraph 明确区分 persisted desired、plan-visible desired 和 provider apply
      payload。
- [ ] `files.file.content` 支持 write-only payload。
- [ ] provider apply 可以拿到 write-only 内容。
- [ ] provider plan、observed、state 和 plan JSON 不能返回 write-only 明文。
- [ ] plan 用 `content_version` 或同类字段判断更新。
- [ ] 固定缺少 `content_version` 时的规则：报错或保守显示 update，不能伪装成精确
      no-op。

测试：

- [ ] write-only content 不进入 `Node.Desired`。
- [ ] apply 能把 write-only content 写到目标文件。
- [ ] state 不包含 write-only content。
- [ ] observed/provider plan 不返回 write-only 明文。
- [ ] 缺少 `content_version` 时行为符合固定规则。
- [ ] `content_version` 变化触发 update，不变时按本轮规则 no-op 或保守提示。

示例/文档：

- [ ] 增加 `variable sensitive+ephemeral + files.file.content + content_version` fixture。
- [ ] 文档说明为什么低熵 secret 推荐使用显式版本而不是长期保存 digest。

验收：

- [ ] 推荐 secret 写法具备可解释的 plan/apply 行为。
- [ ] plan/state/provider 边界不泄露 write-only 明文。
- [ ] `make test` 通过。

### Loop 10：redacted runner 通道

目标：关闭 SSH command preview、stdout/stderr、错误包装和临时文件侧的 secret 泄露风险。

范围：

- runner API
- command preview
- stdin/scp/sftp 或受控临时文件写入
- stdout/stderr redaction
- provider error wrapping
- 临时文件权限和清理

暂不做：

- 新 secret backend
- `secrets.file` deprecation
- 跨 host secret 分发

代码：

- [ ] runner API 支持 redacted payload 或专门的 secret payload 类型。
- [ ] 文件写入优先使用不会把 secret 拼进 shell command 的通道，例如 stdin、sftp/scp 或
      严格权限临时文件。
- [ ] command preview 不包含 secret。
- [ ] stdout/stderr 和错误包装不回显 secret。
- [ ] apply 临时文件使用严格权限，并在成功和失败路径尽量清理。
- [ ] systemd reload、nftables validate/activate 等后续 operation 继续使用原依赖顺序。

测试：

- [ ] fake runner 记录的 command preview 不含 secret。
- [ ] provider error 不含 secret。
- [ ] stdout/stderr redaction 覆盖成功和失败路径。
- [ ] 文件写入仍成功。
- [ ] systemd unit reload 等后续 operation 不受影响。

示例/文档：

- [ ] 文档补充 runner 层 redaction 边界。
- [ ] 如有调试输出示例，确认只展示 redacted preview。

验收：

- [ ] write-only secret 在执行通道中也满足“不进入日志和 preview”。
- [ ] `make test` 通过；相关集成测试记录是否覆盖真实 SSH runner。

### Loop 11：`secrets.file` 语法糖化

目标：把旧 `secrets.file` 迁移到新 file/sensitive/write-only 管线，减少两套实现，同时不
破坏现有 state address。

范围：

- `secrets.file`
- `files.file`
- resource address 兼容
- 路径冲突检测
- state migration 或兼容读取

暂不做：

- 默认输出 deprecation warning
- 删除 `secrets.file`
- 新 secret backend

代码：

- [ ] `secrets.file` 内部编译为敏感 file 资源，或与 `files.file` 共用同一 helper。
- [ ] 保持原 resource address，避免立即破坏 state。
- [ ] `secrets.file` 和 `files.file` 写同一路径仍报错。
- [ ] 新示例优先使用 `variable + files.file`。
- [ ] 文档标记 `secrets.file` 为兼容层。

测试：

- [ ] 旧 `secrets.file` 示例 plan/state 与旧行为兼容。
- [ ] `secrets.file` 和 `files.file` 路径冲突仍报错。
- [ ] state address 不意外变化。
- [ ] `secrets.file` 复用新 sensitive/write-only 泄漏断言。

示例/文档：

- [ ] README 和本文档展示新写法，同时保留旧写法兼容说明。
- [ ] 示例中新增推荐写法，不立即删除旧 fixture。

验收：

- [ ] 新旧 secret 文件路径共用同一安全逻辑。
- [ ] 现有 state 和示例不被破坏。
- [ ] `make test` 通过。

### Loop 12：`secrets.file` 废弃评估和用户收口

目标：在迁移路径稳定后，再决定是否给 `secrets.file` 增加 deprecation warning；删除动作
不属于本轮默认目标。

范围：

- deprecation warning
- warning 兼容开关或迁移周期
- README/docs/examples 收口
- release note 或迁移说明

暂不做：

- 删除 `secrets.file`
- 强制迁移现有 state
- 默认禁用旧语法

代码：

- [ ] 给 `secrets.file` 增加可控 deprecation warning。
- [ ] warning 不改变退出码。
- [ ] 如需要，提供关闭 warning 的兼容开关，或至少保留一个 minor 周期。
- [ ] 示例、README、docs 改为推荐 `variable + files.file`。

测试：

- [ ] 使用 `secrets.file` 输出 warning。
- [ ] warning 不改变 validate/plan/apply 退出码。
- [ ] 兼容开关如实现，需要覆盖开启和关闭。
- [ ] 现有集成测试仍可运行。

示例/文档：

- [ ] README 默认展示新写法。
- [ ] 迁移说明写清楚 `source` 到 `-var @path` 的迁移方式。
- [ ] 本文档更新 `secrets.file` 的最终定位。

验收：

- [ ] `secrets.file` 可以作为兼容层继续存在。
- [ ] 删除旧语法需要另行决策和单独 loop。
- [ ] `make test` 通过。

## 推荐实现顺序

如果只想尽快获得强大的通用 variable，先做：

```text
Loop 0 -> 1 -> 2 -> 3 -> 4 -> 5
```

如果目标是尽快替代 `secrets.file` 的来源能力，继续做：

```text
Loop 6 -> 7
```

如果目标是达到接近 Terraform ephemeral/write-only 的安全边界，继续做：

```text
Loop 8 -> 9 -> 10
```

最后再处理兼容语法：

```text
Loop 11 -> 12
```

不要跳过 Loop 6 直接做 ephemeral/write-only。没有 sensitive 传播和泄漏测试时，后续安全
改动很难判断是否真的收敛。

## 未决问题

- 是否提供 `.dbfvars` 文件格式，还是只支持 CLI 和 stdin。
- 是否内置 SOPS/age，还是只提供 `cmd:`/stdin 接口交给外部工具。
- state 中是否默认保存 sensitive content sha256，还是要求用户显式 opt-in。
- `content_version` 应使用统一字段名，还是每个资源定义自己的 version 字段。
- plan 在缺少 write-only version 时是每次显示 update，还是要求报错。
