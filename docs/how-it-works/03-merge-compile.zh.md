# 03. Profile、Host 与 Component 如何编译成 IR

<p align="right"><a href="03-merge-compile.md">English</a> | <strong>简体中文</strong></p>

本章解释 `internal/core/merge` 如何把 parser 输出的原始配置编译成 `internal/core/ir.Program`。
这个阶段是 DebianForm 从“配置语法树”进入“领域模型”的边界。

## 数据流

```text
parser.Config
  -> merge.CompileWithOptions
  -> variables/component templates
  -> profiles + host body merge
  -> host facts injection
  -> component instantiation
  -> validate + assert
  -> ir.Program
```

主入口是 `merge.CompileWithOptions`。它接收 `CompileOptions`，用于控制 host 过滤、facts 注入、
component 是否跳过，以及是否只验证 runtime templates。

## CompileOptions

关键选项：

- `HostFilter`：只编译指定 host。
- `HostFacts`：在线模式发现的主机 facts，按 host name 注入。
- `SkipComponents`：第一阶段在线编译使用，避免还没发现 facts 时实例化 component。
- `ValidateRuntimeTemplates`：`validate` 使用，验证 component 模板但不做真实实例化。
- `Warnings`：收集非致命告警，例如弃用能力。

这些选项让同一套编译逻辑服务于 `validate`、离线 plan、在线 plan 和 apply。

## Program 顶层结构

`ir.Program` 包含：

- `Hosts []HostSpec`
- `Variables map[string]VariableSpec`
- `Components map[string]ComponentTemplateSpec`

`Variables` 和 `Components` 是公开元数据，既给 inspect 命令使用，也让后续阶段保留必要上下文。
真正会展开成 graph node 的主要是 `Hosts`。

## Profile 合并

host 和 profile 的关系在 compile 阶段处理。大致流程：

1. 对 host 建立一个空 map value。
2. 按 host 的 `Imports` 顺序解析每个 profile。
3. 把 profile body 叠加到当前 raw value。
4. 最后把 host 自己的 body 叠加上去。
5. 收集 profile assert 和 host assert。

`resolveProfile` 带有 cache 和 visiting 标记，用来避免重复解析和检测循环引用。

合并函数是 `Merge(base, overlay)`。它理解 parser 的 modifier：

- 默认 map 合并。
- `force()` 覆盖整个值。
- `before()` / `after()` 控制 list 合并顺序。
- `unset()` 删除或清空对应值。

因此 profile 不是简单复制，而是带有有意识的覆盖语义。

## HostSpec 构建

`buildHostSpec` 把合并后的 `parser.Value` 转成强类型 `ir.HostSpec`。这里会处理：

- 默认 SSH host/user/state path。
- system、kernel、packages、apt、files、secrets、directories。
- groups、users、systemd、services、nftables、docker。
- lifecycle、source、content summary 等元数据。

这一阶段会把用户 DSL 的便捷写法归一化成 IR，例如把 block label、默认 owner/group/mode、ensure
默认值等写成明确字段。

## Runtime facts

在线模式会先编译一次基础 program，用它建立 SSH runner 并发现 facts。第二次编译时通过
`CompileOptions.HostFacts` 注入 facts。

`applyHostFacts` 会把 facts 写入 `HostSpec.Facts` 和相关 system 字段。依赖运行期 facts 的配置，
例如按架构选择 component artifact 或 APT suite，如果离线缺少事实，会在编译或 graph 阶段失败。

## Component 模板和实例化

component 在 parser 层保留模板 body、input 定义和 artifact 定义。merge 层做两类事情：

1. `componentTemplateSpecs`：把 component 模板元数据编译成 `ir.ComponentTemplateSpec`。
2. `instantiateComponents`：把 host 上的 component instance 和 inputs 实例化成 host 下的资源 spec。

输入会经过：

- 类型归一化。
- required/default/nullability 校验。
- sensitive 标记传播。
- validation block 校验。
- deprecated warning。

component 实例化后产生的用户、组、文件、systemd、component artifact 等会挂在 `HostSpec.Components`
里。graph 层会把 host 自身资源和 component 资源一起展开。

## 变量和 component input 公开元数据

`variableSpecs` 和 `componentTemplateSpecs` 会把变量/component input 的定义转成 IR：

- type/type expression/type spec。
- description/default。
- sensitive/nullable/ephemeral/const/deprecated。
- validations。
- source。

如果 default 是 sensitive，inspect 输出会在 CLI 层进一步替换为 `"<sensitive>"`。

## Assertions

断言逻辑在 `internal/core/merge/assert.go`。compile 收集 profile 和 host assert 后，在 `HostSpec`
构建和 facts/component 处理后执行 `evaluateAssertions`。

断言面对的是已经归一化后的 host spec，而不是原始 HCL。这样 assert 能验证最终语义，例如导入 profile
和 host override 之后的结果。

## 校验

`validateHostSpec` 是 IR 出口前的守门点。它检查 HostSpec 是否满足领域约束，例如必填字段、路径、
重复身份、ensure 值、引用关系等。

parser 层只保证语法和局部形状；跨资源、跨 block 的语义应该在这里或 graph validate 中完成。

## 设计边界

- merge 可以理解 DebianForm 的领域语义。
- merge 不应该读取远端 state，也不应该执行命令。
- merge 输出的 IR 应该稳定表达用户意图，不应该包含 provider 的 shell 细节。
- component 实例化应该在 merge 完成，因为 graph 层需要看到完整 host resources。

## 修改检查清单

- 新增 DSL 字段：更新 parser 读取、merge build、IR 类型、validate 和 golden。
- 新增 profile 合并语义：补 `merge.Merge` 单元测试，确认 source 和 modifier 行为。
- 新增 component input 类型或校验：补 component inspect、merge validation 和 sensitive 用例。
- 新增依赖 runtime facts 的能力：确认离线错误、在线 facts 注入和 state facts 持久化都合理。
- 修改 assert 上下文：补 assert 正反测试，避免断言看到未归一化结构。
