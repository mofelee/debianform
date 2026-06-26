# 02. HCL 解析、变量和值模型

本章解释 `internal/core/parser` 如何把 `.dbf.hcl`、变量文件、环境变量和 CLI 变量转换成
`parser.Config`。这个阶段只负责读配置和表达式求值，不负责把配置变成最终资源。

## 数据流

```text
files
  -> hclparse.ParseHCLFile
  -> parseLocals
  -> parseVariables
  -> resolveVariableValues
  -> parseTopLevel
  -> parser.Config
```

`ParseFilesWithOptions` 是主入口。它有两个关键选项：

- `AllowMissingVariables`：允许变量没有最终值，常用于 inspect 或第一阶段变量声明解析。
- `SkipTopLevel`：只解析 `locals` 和 `variable`，跳过 `profile`、`host`、`component`。

## 为什么分阶段解析

parser 先解析所有文件的 `locals`，再解析所有 `variable`，再解析顶层 block。原因是顶层 block 可能引用
`local.*` 和 `var.*`，而变量值又可能来自配置文件之外。

完整解析流程：

1. 读取所有 HCL 文件，保存 `file + hclsyntax.Body`。
2. 遍历所有文件解析 `locals`。
3. 遍历所有文件解析 `variable` 声明。
4. 用外部变量值和默认值解析出 `cfg.VariableValues`。
5. 如果没有 `SkipTopLevel`，解析 `profile`、`host`、`component`。

这个顺序意味着多个文件共享同一个变量和 locals 命名空间；重复定义会报错。

## `parser.Config`

`parser.Config` 是 parser 的输出：

- `Files`：本次读取的配置文件列表。
- `Locals`：所有 `locals` 计算后的值。
- `Variables`：变量声明。
- `VariableValues`：归一化后的变量最终值。
- `ExplicitVariableValues`：哪些变量来自外部显式赋值。
- `Profiles`：原始 profile 定义。
- `Hosts`：原始 host 定义。
- `Components`：原始 component 模板定义。

注意：这里的 host/profile/component 仍然接近用户配置形态，还没有应用 profile import，也没有展开成
`ir.HostSpec`。

## 值模型

parser 不直接把 HCL 值散落成 Go 基本类型，而是统一使用 `parser.Value`：

- `KindNull`
- `KindString`
- `KindBool`
- `KindNumber`
- `KindList`
- `KindMap`

`Value` 还携带：

- `Source`：文件、行号、路径，用于错误定位和后续 plan source。
- `Modifier`：`force`、`before`、`after`、`unset`。
- `Sensitive`：值包含敏感标记。
- `Ephemeral`：值包含 ephemeral 标记。

这些元数据是后续 merge、脱敏、错误信息和测试断言的重要基础。

## 表达式求值

`evalValue` 是普通属性和值表达式的核心入口。它支持：

- list 和 map 字面量。
- 标量表达式。
- `local.*` 和 `var.*`。
- `path.module`。
- 函数：`file`、`jsonencode`、`templatefile`、`toset`。
- modifier 函数：`force()`、`before()`、`after()`、`unset()`。

`evalValue` 最终会通过 cty 转回 `parser.Value`。如果 cty value 有 sensitive 或 ephemeral mark，
这些 mark 会保留在 `Value` 上。

两个重要约束：

- unknown value 不支持。
- map key 和 set element 不能使用 ephemeral 值，因为它们会进入地址或稳定身份。

## 变量来源与优先级

CLI 层收集外部变量值，parser 层负责按变量声明解析和归一化。实际追加顺序在
`collectExternalVariableValues`：

```text
DBF_VAR_*
  -> 每个参与配置目录的 debianform.dbfvars / debianform.dbfvars.json
  -> 每个参与配置目录的 *.auto.dbfvars / *.auto.dbfvars.json
  -> -var-file
  -> -var
```

目录顺序来自配置文件所在目录的首次出现顺序。`resolveVariableValues` 按这个外部列表逐项处理，
同名变量后出现的值会覆盖先出现的值。

如果变量没有显式值：

- 有默认值时，使用默认值。
- 没默认值且不允许 missing 时，报 required 错误。
- 没默认值但 `AllowMissingVariables` 为 true 时，跳过。

## 变量文件

`ParseVariableFile` 支持两种格式：

- `.json`：必须是 JSON object。
- 其他后缀：按 HCL attributes 解析，不允许 block。

HCL var file 中的值会经过 `evalValue`，所以可以使用受支持的函数和字面量。JSON var file 会保留 JSON
number 的字符串形态，避免过早丢失数字表示。

## CLI `-var` 的特殊来源

`-var name=value` 一般把 value 当作字符串、HCL 表达式或 JSON 解析，取决于变量类型和内容。

当变量已声明且 value 以特殊前缀开头时：

- `@path`：从本地文件读取变量值。
- `@-`：从 stdin 读取。
- `env:NAME`：从环境变量读取。

如果变量是 sensitive，读取失败或解析失败时错误信息会隐藏源路径细节，避免泄漏。

## 顶层 block

`parseTopLevel` 只接受：

- `profile`
- `host`
- `component`

`locals` 和 `variable` 已经在前面阶段处理。其他顶层 block 都会报 `unknown top-level block`。

这里 parser 只检查语法和局部结构，例如 block label 数量、重复定义、支持的 attribute/block。
跨 profile、component、host 的语义合并放在 merge 层。

## SourceRef 的作用

`ir.SourceRef` 在 parser 阶段大量产生，即使它定义在 `internal/core/ir` 包里。它记录：

- `File`
- `Line`
- `Path`

后续阶段会把 source 带到 IR、graph node、plan change 和 lifecycle 错误中。新增字段时要优先维护
source，否则维护者很难从 plan 或错误回到用户配置。

## 设计边界

- parser 可以理解 HCL 和 DebianForm DSL 的语法形状。
- parser 不应该理解远端状态、provider 命令、state 文件格式。
- parser 可以标记 sensitive/ephemeral，但不负责最终 plan/state 脱敏策略。
- parser 输出的 host/profile/component 是中间结构，不是执行计划。

## 修改检查清单

- 新增表达式函数：补 `eval.go` 测试，确认 sensitive/ephemeral mark 是否正确传播。
- 新增变量类型或约束：补 normalize 和 var file/CLI 解析测试。
- 新增顶层 block：更新 `parseTopLevel`、merge 编译入口和 docs。
- 修改变量优先级：补 CLI 层测试，并明确 `.dbfvars`、auto、`-var-file`、`-var` 的覆盖关系。
- 修改 SourceRef：确认错误信息、plan source 和 golden 文件都同步更新。
