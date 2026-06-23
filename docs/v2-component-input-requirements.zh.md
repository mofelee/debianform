# DebianForm v2 component input 需求文档

本文档定义 v2 `component` 内 `input` 的目标能力。目标是把 component input
从当前的简单参数声明，提升为接近 Terraform `variable` 的组件接口契约，特别支持
`list(object({...}))` 这类结构化参数。

参照对象为 Terraform 官方文档中的 `variable` block 和 type constraints。DebianForm
不做 Terraform 兼容层；这里只借鉴它成熟的接口设计，并按 DebianForm 的 component、
HostSpec、plan/state 和敏感数据模型做取舍。

## 背景

当前 DebianForm v2 `component input` 已支持：

```hcl
component "app" {
  input "listen_addr" {
    type      = string
    default   = "127.0.0.1:8080"
    sensitive = false
  }
}
```

当前已支持的类型范围：

```text
string
number
bool
any
list(T)
set(T)
map(T)
object({ ... })
tuple([ ... ])
optional(T)
optional(T, default)
```

其中 `optional(...)` 只能用于 object attribute。`description`、`nullable`、
strict object schema、optional default、`validation`、sensitive 派生传播和
`deprecated` warning 已实现；`ephemeral` 仍保留为未来能力。

真实部署组件通常需要结构化接口。例如：

- 多个监听端口，每个端口有 `name`、`port`、`protocol`、`tls`。
- 多个 upstream，每个 upstream 有 `host`、`port`、`weight`。
- 每个用户包含 uid、shell、groups、authorized_keys。
- 每个服务实例包含 environment、volumes、healthcheck 等嵌套对象。

这些场景需要 `list(object({...}))`、`map(object({...}))`、嵌套 object、optional
attribute、nullable 和 validation。

## 目标

- `input` 成为 component 的稳定公开 API。
- 支持 Terraform 风格的类型约束语法，包括 primitive、collection、structural
  types。
- 支持 `list(object({...}))` 和嵌套 `object`，作为第一优先级。
- 支持 object attribute 默认值和可选字段，减少 component 调用方样板代码。
- 支持 `description`，让 component 可以生成可读接口说明。
- 支持 `validation`，在 component 展开前拦截错误输入。
- 支持 `nullable`，明确 `null` 与省略值的语义。
- 保留并增强 `sensitive`，避免 input value 和由它派生的内容泄漏到 plan/state/log。
- 保持现有 `type/default/sensitive` input 配置兼容。
- 所有错误必须指向用户 DSL 的 source path，并能定位到嵌套字段或列表下标。

## 非目标

- 不实现 Terraform root module variable 的外部赋值来源，如 `TF_VAR_*`、`.tfvars`、
  CLI `-var`。
- 不实现 Terraform module system；DebianForm 的输入只服务于 `component`。
- 不提供 `array` 类型关键字；数组语义使用 `list(T)` 或 `tuple([...])` 表达。
- 第一阶段不要求完整复制 Terraform 的自动类型转换和宽松 object 转换。
- 第一阶段不支持 `ephemeral` 的完整语义，除非 plan/state 和 provider write-only
  参数已经具备不落盘能力。
- 不允许 input 默认值依赖 `target`、远端 runtime facts 或另一个 input。

## Terraform 对照

Terraform `variable` block 支持这些配置项：

```hcl
variable "name" {
  type        = <TYPE>
  default     = <DEFAULT_VALUE>
  description = "<DESCRIPTION>"
  sensitive   = <true|false>
  nullable    = <true|false>
  ephemeral   = <true|false>

  validation {
    condition     = <EXPRESSION>
    error_message = "<ERROR_MESSAGE>"
  }
}
```

DebianForm `input` 应采用下列子集和取舍：

| Terraform variable | DebianForm input | 阶段 | 说明 |
| --- | --- | --- | --- |
| `type` | `type` | 必须 | DebianForm 继续要求显式类型，不采用 Terraform 的省略即 `any`。 |
| `default` | `default` | 必须 | 无 default 时 input 必填；有 default 时可省略。 |
| `description` | `description` | 必须 | 文档元数据，不影响编译。 |
| `validation` | `validation` | 必须 | 展开 component 前执行。 |
| `sensitive` | `sensitive` | 必须 | 需要从只隐藏 input value 扩展为敏感性传播。 |
| `nullable` | `nullable` | 必须 | 默认 `true`，可设为 `false` 禁止顶层 null。 |
| `ephemeral` | 保留字段 | 暂缓 | 只有具备不落盘和 write-only provider 语义后才能安全支持。 |

DebianForm 额外建议支持：

| DebianForm input | 阶段 | 说明 |
| --- | --- | --- |
| `deprecated` | 应支持 | 用于 component API 演进；调用方显式传入时 validate/plan 输出 warning。 |

## 用户语法

### 基本语法

```hcl
component "app" {
  input "listen_addr" {
    type        = string
    description = "Address and port used by the application listener."
    default     = "127.0.0.1:8080"
    nullable    = false
  }
}
```

规则：

- `input` block 必须有且只有一个 label。
- 同一 component 内 input label 必须唯一。
- `type` 必填。
- `description` 可选，但推荐所有公开 component 都写。
- `default` 可选；没有 default 时实例必须传值。
- `nullable` 可选，默认 `true`。
- `sensitive` 可选，默认 `false`。
- `deprecated` 可选，值为非空字符串。
- `validation` block 可重复。

### `list(object(...))`

```hcl
component "reverse_proxy" {
  input "listeners" {
    type = list(object({
      name     = string
      port     = number
      protocol = optional(string, "http")
      tls      = optional(bool, false)

      upstreams = list(object({
        host   = string
        port   = number
        weight = optional(number, 1)
      }))

      headers = optional(map(string), {})
    }))

    description = "Listener definitions exposed by this reverse proxy."
    default     = []
    nullable    = false

    validation {
      condition = alltrue([
        for listener in input.listeners :
        listener.port >= 1 && listener.port <= 65535
      ])
      error_message = "Each listener.port must be between 1 and 65535."
    }
  }

  files {
    file "/etc/reverse-proxy/listeners.json" {
      mode    = "0644"
      content = jsonencode(input.listeners)
    }
  }
}
```

调用：

```hcl
host "edge1" {
  component "proxy" {
    source = component.reverse_proxy

    inputs = {
      listeners = [
        {
          name = "public-http"
          port = 80
          upstreams = [
            { host = "127.0.0.1", port = 8080 },
          ]
        },
        {
          name     = "public-https"
          port     = 443
          protocol = "http"
          tls      = true
          upstreams = [
            { host = "127.0.0.1", port = 8443, weight = 10 },
          ]
          headers = {
            X-Forwarded-Proto = "https"
          }
        },
      ]
    }
  }
}
```

归一化后，第一项应自动补齐：

```hcl
{
  name     = "public-http"
  port     = 80
  protocol = "http"
  tls      = false
  upstreams = [
    { host = "127.0.0.1", port = 8080, weight = 1 },
  ]
  headers = {}
}
```

### `map(object(...))`

```hcl
component "users" {
  input "accounts" {
    type = map(object({
      uid                 = optional(number)
      shell               = optional(string, "/bin/bash")
      groups              = optional(list(string), [])
      ssh_authorized_keys = optional(list(string), [])
    }))

    default     = {}
    nullable    = false
    description = "Managed Unix accounts keyed by username."
  }
}
```

调用：

```hcl
inputs = {
  accounts = {
    deploy = {
      groups = ["docker"]
      ssh_authorized_keys = [
        "ssh-ed25519 AAAA...",
      ]
    }
  }
}
```

### 敏感输入

```hcl
component "service" {
  input "environment" {
    type        = map(string)
    sensitive   = true
    description = "Environment variables containing credentials."
    default     = {}
  }
}
```

要求：

- `input.environment` 本身在 HostSpec、plan、state、debug 输出中必须隐藏。
- 任何由 `input.environment` 派生的表达式也必须带敏感标记。
- 如果敏感值最终写入 `files.file.content`，plan/state 只能保存摘要，不能保存明文。
- 如果敏感值最终写入非敏感字段且当前实现无法传播敏感性，validate 必须报错或给出
  明确 warning；不能静默泄漏。

### 废弃输入

```hcl
component "app" {
  input "listen_addr" {
    type       = string
    deprecated = "Use listeners instead."
    default    = "127.0.0.1:8080"
  }

  input "listeners" {
    type    = list(object({ port = number }))
    default = []
  }
}
```

如果调用方显式传入 `listen_addr`，`dbf validate` 和 `dbf plan` 应输出 warning，但不阻止
执行。只使用 default 不应触发 deprecated warning，避免旧组件默认值污染输出。

## 类型系统

### 支持的类型表达式

第一阶段必须支持：

```text
string
number
bool
any
list(T)
set(T)
map(T)
object({ name = T, other = optional(T), third = optional(T, DEFAULT) })
tuple([T1, T2, ...])
```

其中 `T` 可以递归嵌套。

示例：

```hcl
type = list(object({
  name = string
  tags = optional(map(string), {})
  peers = optional(list(object({
    public_key  = string
    allowed_ips = list(string)
  })), [])
}))
```

### 不支持的类型表达式

```text
array(T)
list
map
set
object
tuple
optional(T, dynamic_default_expression)
```

说明：

- 不提供 `array`。用户应写 `list(T)`。
- 不推荐 Terraform 的裸 `list`、裸 `map`、裸 `set` shorthand。DebianForm 应要求完整
  element type，减少接口含糊。
- `optional` 只能出现在 `object({ ... })` 的 attribute 类型位置。
- `optional(T, DEFAULT)` 的 default 必须是可在 parse/compile 阶段求值的静态值，且必须
  能转换为 `T`。

### `any`

`any` 应作为逃生口，而不是默认类型。

规则：

- `type = any` 允许任意可序列化 HCL 值。
- `list(any)`、`map(any)` 允许元素类型由输入值决定。
- 如果 component 内部访问 `any` 的字段或下标，错误会延迟到表达式求值阶段；因此公开
  component 不应优先使用 `any`。
- `any` 值必须仍然能生成 JSON/HostSpec 表示；函数值、未知值和不能序列化的值不允许。

### object 字段规则

DebianForm 应采用严格 object schema：

- 必填字段缺失时报错。
- 字段类型不匹配时报错。
- 未声明字段默认报错，而不是像 Terraform 转换 object 时静默丢弃。
- 如果未来需要兼容宽松模式，应显式新增 `additional_attributes = true` 或类似机制；
  不应把静默丢弃作为默认行为。

原因：DebianForm 面向主机目标状态，输入 typo 如果被静默忽略，可能导致错误配置进入
plan/apply。

### optional object attributes

`optional(T)`：

- 调用方省略该字段时，归一化值为 `null`。
- 调用方提供该字段时，值必须能转换为 `T`。

`optional(T, DEFAULT)`：

- 调用方省略该字段时，使用 `DEFAULT`。
- 调用方显式传 `null` 时：
  - 如果字段类型允许 null，则保留 null。
  - 如果后续需要 Terraform 完全一致行为，可再决定是否用 default 覆盖 null。

第一阶段建议保持简单规则：省略才触发 optional default，显式 null 不触发 default。

### null 和 nullable

`nullable` 控制 input 顶层值：

- `nullable = true`：允许调用方显式传 `null`。
- `nullable = false`：调用方传 `null` 必须报错。
- 默认值为 `true`，与 Terraform 保持一致。
- 对 collection/object 内部的 null，按其嵌套类型和 optional 规则判断；顶层
  `nullable = false` 不递归禁止内部 null。

示例：

```hcl
input "config" {
  type = object({
    path = string
    mode = optional(string)
  })
  nullable = false
}
```

这里 `config = null` 非法，但 `config.mode = null` 可以合法。

### 类型转换

目标是尽量接近 Terraform 的 cty conversion，但需要有清晰边界。

必须支持：

- tuple/list literal 转换为 `list(T)`，并逐项转换为 `T`。
- object/map literal 转换为 `map(T)`，并逐项转换为 `T`。
- object literal 转换为 `object({...})`，严格检查 schema。
- list/tuple 转换为 `tuple([...])`，长度必须一致。
- `set(T)` 归一化为确定性顺序，HostSpec/plan 输出必须稳定。

第一阶段可以不支持，或只在确认无歧义时支持：

- string、number、bool 之间的 Terraform 式自动互转。
- object/map 之间的宽松转换。
- set 与 list/tuple 的完全互转。

如果实现选择不支持某个 Terraform 自动转换，错误信息必须说明 DebianForm 需要显式类型。

## validation

### 语法

```hcl
input "listeners" {
  type = list(object({
    name = string
    port = number
  }))

  validation {
    condition = alltrue([
      for listener in input.listeners :
      listener.port >= 1 && listener.port <= 65535
    ])
    error_message = "Each listener.port must be between 1 and 65535."
  }
}
```

规则：

- `validation` block 不能有 label。
- 每个 validation 必须包含 `condition` 和 `error_message`。
- `condition` 必须求值为 bool。
- `error_message` 必须是非空字符串。
- validation 可重复，按源码顺序执行。
- validation 在 default、optional default、type conversion 和 nullable 检查之后执行。
- validation 失败时 `validate`、`plan`、`apply` 都必须停止。

### validation 可见上下文

第一阶段 validation 只允许访问：

```text
input.<current_input_name>
```

例如在 `input "listeners"` 的 validation 中，允许：

```hcl
input.listeners
```

不允许：

```hcl
target.system.codename
input.other_input
local.some_value
file("...")
templatefile("...", {})
```

原因：input validation 应描述该 input 自身契约，必须离线、纯函数、稳定可复现。

后续如果需要跨 input 校验，应新增 component 级 `assert` 或 `validation`，而不是让单个
input validation 隐式依赖其他 input。

### validation 函数

当前实现支持一组纯函数：

```text
length
contains
startswith
endswith
regex
can
alltrue
anytrue
distinct
sort
keys
values
flatten
toset
tonumber
tostring
tobool
```

可暂缓：

```text
cidrhost
cidrnetmask
cidrsubnet
jsondecode
yamldecode
```

禁止：

```text
file
templatefile
env
外部命令
网络访问
```

## component body 中的 input 访问

component 展开时，`input` 对象必须包含所有归一化后的 input 值：

- 调用方传入值优先。
- 未传值且有 `default` 时使用 default。
- object optional 字段按规则补齐。
- 类型转换完成。
- `nullable` 检查完成。
- validation 已通过。

component body 可以访问嵌套字段：

```hcl
content = input.listeners[0].upstreams[0].host
```

可以使用 for expression 生成结构：

```hcl
content = jsonencode([
  for listener in input.listeners : {
    name = listener.name
    port = listener.port
  }
])
```

这要求 expression evaluator 把 `input` 注入为结构化 cty value，而不是提前压成字符串。

## IR 需求

### ComponentInputSpec

建议中间表达扩展为：

```go
type ComponentInputSpec struct {
    Name        string
    Type        ComponentInputTypeSpec
    TypeExpr    string
    Description string
    Default     *Value
    Sensitive   bool
    Nullable    bool
    Deprecated  string
    Validations []ComponentInputValidationSpec
    Source      SourceRef
}

type ComponentInputValidationSpec struct {
    ConditionSource SourceRef
    Message         string
    MessageSource   SourceRef
}
```

`TypeExpr` 是规范化后的用户可读类型字符串，如：

```text
list(object({name=string,port=number,tls=optional(bool,false)}))
```

`ComponentInputTypeSpec` 是机器可读 schema，不能只保存字符串，否则难以生成文档、
错误路径和后续 tooling。

### ComponentInputTypeSpec

建议结构：

```go
type ComponentInputTypeSpec struct {
    Kind       string
    Element    *ComponentInputTypeSpec
    Attributes map[string]ComponentObjectAttributeSpec
    Tuple      []ComponentInputTypeSpec
}

type ComponentObjectAttributeSpec struct {
    Type     ComponentInputTypeSpec
    Optional bool
    Default  *Value
}
```

`Kind` 取值：

```text
string
number
bool
any
list
set
map
object
tuple
```

### ComponentInstanceSpec

`ComponentInstanceSpec.InputValues` 必须保存归一化后的值，而不是原始调用方输入。

敏感值规则：

- `sensitive = true` 的 input，在 JSON HostSpec 中显示为 `"<sensitive>"` 或 omit。
- 如果实现需要 provider 阶段使用真实值，真实值只能保存在内存中的 compiled program，
  不能写入 plan/state JSON。
- `ephemeral` 未来启用时，必须从 HostSpec JSON、plan JSON、state JSON 全部 omit。

### SourceRef

嵌套错误必须能指向精确路径：

```text
component.reverse_proxy.input["listeners"].default[0].upstreams[1].port
host.edge1.component["proxy"].inputs["listeners"][0].upstreams[0].host
```

错误示例：

```text
examples/app.dbf.hcl:42: host.edge1.component["proxy"].inputs["listeners"][0].port:
component.reverse_proxy input "listeners" expected number at .port, got string
```

## plan/state 需求

- plan 输出不应把 input metadata 当作资源变化。
- component instance 的 `input_values` 只用于解释和调试。
- 非敏感 input 可以显示归一化值。
- 敏感 input 必须 redacted。
- 由敏感 input 派生出的 file content、unit content、secret source 等必须继承敏感性。
- state 不能保存敏感明文。
- 如果目前某些 provider 必须保存内容才能 diff，应保存 hash、长度、mode、owner 等摘要。
- `description`、`deprecated`、type schema 属于 component template metadata，不应写入每个
  host state。

## CLI 和 UX 需求

### validate

`dbf validate` 必须检查：

- input block 属性是否合法。
- type expression 是否可解析。
- default 是否能转换为 type。
- optional default 是否能转换为对应字段 type。
- nullable=false 时 default 不能是 null。
- validation block 是否完整。
- validation condition 是否能求值为 bool。
- component instance 是否遗漏必填 input。
- component instance 是否传入未知 input。
- component instance input 是否能转换为声明类型。
- component instance input 是否通过 validation。

### plan/apply

`dbf plan` 和 `dbf apply` 复用 validate 的所有 input 检查。任何 input 错误都必须在
ResourceGraph 生成前失败。

### inspect 或 docs 输出

后续可以增加：

```bash
dbf component inspect -f app.dbf.hcl reverse_proxy
```

输出：

```text
component.reverse_proxy inputs

listeners
  type: list(object({ ... }))
  default: []
  nullable: false
  sensitive: false
  description: Listener definitions exposed by this reverse proxy.
```

这不是第一阶段必须项，但 `description` 和 type schema 设计应支持它。

## 实现建议

### 解析类型表达式

不要用字符串拼接解析类型。应基于 HCL AST 解析：

- `hcl.AbsTraversalForExpr` 识别 primitive 和 `any`。
- `hclsyntax.FunctionCallExpr` 识别 `list(T)`、`map(T)`、`set(T)`、`object({...})`、
  `tuple([...])`、`optional(...)`。
- `object` 参数必须是 object constructor。
- `tuple` 参数必须是 tuple/list constructor。
- `optional` 只能在 object attribute 类型位置出现。

### 内部值模型

现有 `parser.Value` 可以表达 string/bool/number/list/map/null，但缺少：

- 显式 type。
- object schema 归一化后的字段默认值。
- sensitive mark。
- set 与 tuple 的区分。

建议：

- 类型检查和 conversion 层使用 `cty.Type` / `cty.Value`。
- 保留 `parser.Value` 作为 HostSpec/IR JSON 的稳定值表示。
- 在 `parser.Value` 上增加 `Sensitive bool`，或引入 wrapper 保存 marks。
- 从 cty 转回 `parser.Value` 时保留 redaction/summary 所需信息。

### 转换顺序

对每个 component instance：

```text
1. 收集调用方 inputs。
2. 检查未知 input。
3. 对每个声明 input：
   a. 调用方有值则取调用方值。
   b. 调用方无值且有 default 则取 default。
   c. 调用方无值且无 default 则报 required。
   d. 顶层 null + nullable=false 则报错。
   e. 按 type 进行 conversion 和 object optional default 填充。
   f. 执行 validation blocks。
   g. 标记 sensitive。
4. 构造归一化 input object。
5. 用归一化 input object 展开 component body。
```

### 敏感性传播

Terraform 会让引用 sensitive variable 的表达式继续保持 sensitive。DebianForm 也应采用
类似策略。

当前实现：

- `input.foo.sensitive = true` 时，`input.foo` 是 sensitive cty value。
- 表达式求值后，如果任一输入带 sensitive mark，结果也带 sensitive mark。
- `parser.Value` 保存 sensitive mark。
- file 和 systemd unit 内容会根据 sensitive mark 自动转为敏感资源。
- HostSpec JSON、plan JSON/text 和 state JSON 根据 sensitive mark 选择摘要或 redaction。

## 错误信息要求

错误信息应包含：

- 文件。
- 行号。
- DebianForm source path。
- component template 名称。
- input 名称。
- 嵌套字段路径。
- 期望类型。
- 实际类型。

示例：

```text
examples/proxy.dbf.hcl:37: host.edge1.component["proxy"].inputs["listeners"][0].upstreams[0].port:
component.reverse_proxy input "listeners" expected number at .upstreams[0].port, got string
```

validation 失败示例：

```text
examples/proxy.dbf.hcl:51: component.reverse_proxy.input["listeners"].validation[0]:
validation failed for input "listeners": Each listener.port must be between 1 and 65535.
```

deprecated warning 示例：

```text
warning: examples/app.dbf.hcl:20: host.web1.component["app"].inputs["listen_addr"]:
component.app input "listen_addr" is deprecated: Use listeners instead.
```

## 兼容性

现有配置继续合法：

```hcl
input "repo_uri" {
  type = string
}

input "packages" {
  type    = list(string)
  default = []
}

input "labels" {
  type    = map(string)
  default = {}
}
```

新增字段不会改变旧 input 行为：

```hcl
input "repo_uri" {
  type        = string
  description = "APT repository URI."
}
```

破坏性变化必须避免：

- `type` 继续必填。
- `list(string)` 和 `map(string)` 语义不变。
- `sensitive = true` 至少继续 redacts `input_values`。
- 旧错误 path 尽量保持可识别。

可能的行为变化：

- 如果引入 Terraform 式 primitive conversion，`default = 123` 对 `type = string` 可能从
  失败变成 `"123"`。第一阶段建议保持严格，避免隐式变化。
- 如果引入 strict object schema，带多余字段的 object 会报错；这是新类型能力，不影响
  旧配置。

## 测试要求

### parser 单测

- 支持 `description`、`nullable`、`deprecated`。
- 支持重复 `validation` block。
- 拒绝未知 input 属性。
- 拒绝 nested block，除 `validation` 外。
- 解析 primitive、`any`、`list(T)`、`set(T)`、`map(T)`。
- 解析 `object({ name = string, enabled = optional(bool, true) })`。
- 解析 `list(object({ ... }))`。
- 拒绝 `array(string)`。
- 拒绝裸 `list`、裸 `map`、裸 `set`。
- 拒绝 object 外部的 `optional(...)`。

### merge/compiler 单测

- 缺失必填 input 报错。
- 未知 input 报错。
- default 被使用并归一化。
- `nullable=false` 拒绝 null。
- `nullable=true` 接受顶层 null。
- object 必填字段缺失报错。
- object 多余字段报错。
- optional 字段省略后填充 null 或 default。
- `list(object(...))` 嵌套字段类型错误能指向下标。
- validation 成功。
- validation 失败。
- validation condition 非 bool 报错。
- deprecated input 显式传值产生 warning。
- sensitive input 在 HostSpec JSON 中 redacted。

### golden 测试

- 增加一个 runnable fixture：`examples/v2-component-inputs.dbf.hcl`。
- golden HostSpec 覆盖 `list(object(...))` 归一化结果。
- golden plan 确认敏感 input 不泄漏。

### 集成测试

第一阶段不需要 libvirt 集成测试覆盖所有类型；但至少应有一个 component 使用
`list(object(...))` 生成实际文件，并在 VM 中验证文件内容符合归一化结果。

## 分阶段计划

### Phase 1: 类型和 description（已实现）

- 新增 type parser，支持 primitive、`any`、`list`、`set`、`map`、`object`、`tuple`、
  `optional`。
- 新增 `description`。
- 新增 `nullable`。
- 新增 strict object validation。
- 新增 optional object attributes。
- 默认保持 strict primitive typing，不做 string/number/bool 自动互转。
- 更新 IR、HostSpec golden 和 parser/merge 测试。

### Phase 2: validation（已实现）

- 支持 input `validation` block。
- 注入 `input.<current>` validation context。
- 增加纯函数集合。
- 确保 validation 在 component body 展开前执行。
- 增加错误 path 和 source location。

### Phase 3: sensitive propagation（已实现）

- 为 cty/parser.Value 增加 sensitive mark。
- 表达式求值传播 sensitive。
- file/unit/service/environment 等 provider payload 尊重 sensitive mark。
- plan/state 只输出摘要或 redaction。

### Phase 4: deprecated 和 tooling（已实现）

- 支持 `deprecated` warning。
- 增加 `dbf component inspect`。
- README 和 examples 展示 component input API。

### Phase 5: ephemeral 评估

只有满足以下条件后才实现：

- plan JSON 不保存 ephemeral value。
- state JSON 不保存 ephemeral value。
- provider 支持 write-only 或 runtime-only 参数。
- 任何引用 ephemeral input 的表达式也能被标记为 ephemeral。
- 不能把 ephemeral 派生值写入需要 diff/state 明文的资源。

## 验收标准

以下配置必须通过 validate，并在 HostSpec 中产生归一化后的结构化输入：

```hcl
component "demo" {
  input "listeners" {
    type = list(object({
      name = string
      port = number
      tls  = optional(bool, false)
      tags = optional(map(string), {})
    }))

    description = "Listeners exposed by demo."
    default     = []
    nullable    = false
  }
}
```

以下调用必须合法：

```hcl
inputs = {
  listeners = [
    {
      name = "http"
      port = 80
    },
  ]
}
```

归一化结果必须包含：

```hcl
listeners = [
  {
    name = "http"
    port = 80
    tls  = false
    tags = {}
  },
]
```

以下调用必须报错，并指向 `.listeners[0].port`：

```hcl
inputs = {
  listeners = [
    {
      name = "http"
      port = "eighty"
    },
  ]
}
```

以下类型必须报错，并提示使用 `list(T)`：

```hcl
input "ports" {
  type = array(number)
}
```

## 参考资料

- Terraform variable block reference:
  <https://developer.hashicorp.com/terraform/language/block/variable>
- Terraform type constraints:
  <https://developer.hashicorp.com/terraform/language/expressions/type-constraints>
