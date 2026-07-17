# 01. 全局架构与命令生命周期

<p align="right"><a href="01-architecture.md">English</a> | <strong>简体中文</strong></p>

本章解释 `dbf` 命令从参数进入，到解析配置、编译 IR、生成资源图、生成计划或执行变更的主流程。
后续章节会分别展开 parser、merge、graph、plan、engine 和 provider。

## 一句话模型

DebianForm 的主链路是把用户声明转换成可观测、可比较、可执行的资源图：

```text
CLI flags
  -> parser.Config
  -> ir.Program
  -> graph.ResourceGraph
  -> engine.Plan 或 plan.Document
  -> provider/backend 对远端主机执行
```

其中 `plan --offline` 不连接主机，直接把资源图视为“全部待创建”；在线 `plan`、`apply` 和
`check` 会通过 SSH 读取 facts、远端 state 和 observed 状态，再计算真实动作。

## 命令入口

主入口在 `cmd/dbf/main.go`：

- `main` 只负责调用 `run(os.Args[1:])`，把错误打印到 stderr 并退出。
- `run` 根据第一个参数分发到 `version`、`fmt`、`component`、`variable` 或配置命令。
- 配置命令包括 `validate`、`plan`、`apply`、`check`，统一进入 `runConfigCommand`。

这种结构让命令分发保持在 CLI 层。parser、merge、graph、engine 都不直接理解命令行参数。

## 配置命令的共同准备

`runConfigCommand` 做三件事：

1. 定义和解析 flags。
2. 用 `configFiles` 决定读取哪些 `.dbf.hcl`。
3. 调用 `runConfigWorkflow` 执行真正的 流程。

`configFiles` 的规则很直接：

- 如果传入一个或多个 `-f`，就按命令行顺序读取这些 source。
- `-f` source 可以是文件或目录；目录展开为直属 `*.dbf.hcl`，按文件名排序，不递归。
- 如果没有 `-f`，就在当前工作目录找所有 `*.dbf.hcl`，按文件名排序。
- 如果找不到配置文件，返回错误。

变量输入也在 CLI 层收集，但实际类型归一化在 parser 层完成。CLI 支持：

- `DBF_VAR_` 环境变量。
- 默认变量文件：`debianform.dbfvars`、`debianform.dbfvars.json`。
- 自动变量文件：`*.auto.dbfvars`、`*.auto.dbfvars.json`。
- 显式 `-var-file`。
- 显式 `-var name=value`。

入口函数是 `parseConfigWithExternalValues`。它先用 `SkipTopLevel` 解析变量声明，再收集外部变量值，
最后把变量值交回 parser 做完整解析。

## `validate`

`validate` 的目标是验证本地配置能不能编译成合法 IR，不连接主机，不读取 state。

数据流：

```text
files + vars
  -> parseConfigWithExternalValues
  -> compileValidationProgram
  -> merge.CompileWithOptions(ValidateRuntimeTemplates: true)
```

`ValidateRuntimeTemplates` 的意义是：component 等运行期模板不真正实例化远端产物，但要尽量验证模板结构、
输入和断言是否成立。它适合本地快速失败。

`validate` 不允许 `--format`，成功时只打印 host 数量。

## `plan --offline`

离线 plan 不连接目标机。它只使用配置里已经声明的 facts 和静态信息生成资源图，然后把每个节点渲染为
`create`。

数据流：

```text
parser.Config
  -> merge.CompileWithOptions
  -> graph.Compile
  -> plan.New
  -> PrintText/PrintJSON/PrintHTML
```

这里不会调用 `engine.Plan`，所以不会读取 state，也不会知道目标机上资源是否已经存在。

如果配置依赖运行期 facts，例如没有声明 `platform.architecture` 却需要按架构选择 component artifact，
离线 plan 会报错并提示改用在线 plan 或显式声明 facts。

## 在线 `plan`

在线 plan 会先连接主机发现 facts，再重新编译程序。

数据流：

```text
parser.Config
  -> compileProgram(SkipComponents: true)
  -> SSHRunner
  -> DiscoverProgramFacts
  -> compileProgram(HostFacts: facts)
  -> graph.Compile
  -> engine.Plan
  -> engine.Plan.Document
```

这里有一个重要的两阶段编译：

1. 第一阶段用 `SkipComponents` 编译出基本 host 和 SSH/state 配置，让系统知道该连哪些主机。
2. 发现 facts 后，第二阶段把 facts 注入编译选项，再实例化 component 和依赖 facts 的资源。

最终的 `engine.Plan` 会读取远端 state，并由 provider 观测实际主机状态。它输出的 action 才是在线语义：
`create`、`update`、`delete`、`adopt`、`forget`、`destroy`、`no-op` 等。

## `check`

`check` 和在线 `plan` 共用前半段流程，也会打印 plan 文本。不同点是：

- 如果 `engine.Plan` 里存在 resource step 或 operation step，`check` 返回错误。
- 如果没有变更，`check` 返回成功。

因此 `check` 是漂移检测命令，不做修改。

## `apply`

`apply` 先生成在线 plan 并打印给用户确认。确认后，它不会直接执行刚才打印出来的旧对象，而是调用
`engine.Apply`。`Engine.Apply` 内部会：

1. 获取目标 host 的 state lock。
2. 再次调用 `Engine.Plan`。
3. 在仍持有 lock 时打印实际执行计划；若它与已确认的 preview 不同，交互模式下再次确认。
4. 获准后持久化已发现 facts。
5. 根据资源图依赖拆成 execution waves。
6. 调 provider 执行 resource step 和 operation。
7. 每个资源 step 成功后写回 state。

重新 plan 是为了让 apply 在持锁之后基于最新 state 和 observed reality 执行。实际执行计划在任何 state
写入或 provider 修改之前展示和批准，避免用户只确认旧 preview、实际却执行了新增动作。

## `fmt`

`fmt` 是一个特殊命令。它会先调用 `loadProgram` 验证配置能解析和编译，然后用
`hclwrite.Format` 重写输入文件。也就是说，格式化不是纯文本操作；语义上无效的配置不会被格式化。

## Inspect 命令

`component inspect` 和 `variable inspect` 都面向机器可读输出：

- `component inspect` 解析配置并编译 component 模板输入定义，输出 JSON。
- `variable inspect` 支持 `AllowMissingVariables` 和 `SkipTopLevel`，用于列出变量定义和默认值。

敏感默认值会显示为 `"<sensitive>"`，避免在 inspect 输出里泄漏。

## 设计边界

- CLI 层负责 flags、文件选择、输出格式和用户确认。
- parser 层负责 HCL、变量、locals、表达式和值模型。
- merge 层负责 profile/component/host 合并和 IR 校验。
- graph 层负责把 IR 展成资源节点和依赖。
- plan 层只负责计划文档和展示。
- engine 层负责在线 state、observed、action、apply 调度。
- provider/backend 层负责和远端主机交互。

新增功能时，不要让某一层越界。例如 provider 不应该解析 HCL，parser 不应该知道 systemd reload 命令，
plan 不应该决定资源是否存在。

## 修改检查清单

- 新增 CLI flag：确认只在 `cmd/dbf/main.go` 消费，并传给对应内部层。
- 新增命令：确认 `run`、usage、测试和文档都更新。
- 改变 plan/apply 行为：确认离线 plan、在线 plan、check、apply 四条路径都考虑过。
- 改变量输入规则：补 CLI test、parser variable test 和 sensitive error 用例。
- 改输出格式：补 text、JSON 或 HTML golden/断言，并检查敏感值不会泄漏。
