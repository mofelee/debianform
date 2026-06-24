# DebianForm 系统如何工作

这个目录存放面向后续开发者的系统工作原理系列教程。目标不是重复用户手册，而是解释
`dbf` 从读取 `.dbf.hcl` 到生成计划、执行变更、写入 state 的内部链路，以及每个包的职责边界。

建议按下面顺序阅读。每篇教程应包含：

- 用户可见行为：这个阶段解决什么问题。
- 核心代码入口：从哪些函数开始读。
- 数据结构：输入、输出和关键中间表示。
- 设计约束：哪些边界不能随意跨越。
- 修改指南：新增能力时应该改哪些地方、补哪些测试。

## 系列目录

### 任务列表

- [x] [01. 全局架构与命令生命周期](01-architecture.zh.md)
- [x] [02. HCL 解析、变量和值模型](02-parser-values.zh.md)
- [x] [03. Profile、Host 与 Component 如何编译成 IR](03-merge-compile.zh.md)
- [x] [04. IR 数据模型与资源边界](04-ir-model.zh.md)
- [x] [05. ResourceGraph 如何展开资源和依赖](05-resource-graph.zh.md)
- [x] [06. Plan 文档、Diff 与输出格式](06-plan-output.zh.md)
- [x] [07. 在线 Engine：读取状态、观测现实、计算动作](07-engine-plan.zh.md)
- [x] [08. Apply 执行模型、锁和调度](08-apply-scheduler.zh.md)
- [x] [09. Backend、SSH 与远端 State](09-backend-state.zh.md)
- [x] [10. Native Provider 与资源实现](10-native-provider.zh.md)
- [x] [11. Secrets、Sensitive 与脱敏链路](11-redaction.zh.md)
- [x] [12. 测试体系与新增能力清单](12-testing-and-extension.zh.md)

后续如果这个系列继续新增章节，先把章节加入上面的任务列表，再补正文文件。

### [01. 全局架构与命令生命周期](01-architecture.zh.md)

从 `cmd/dbf/main.go` 开始，解释 `validate`、`plan`、`apply`、`check`、`fmt`、`component inspect`
和 `variable inspect` 如何被调度。重点说明离线 plan 和在线 plan 的分叉，以及一条典型命令
的端到端数据流：

```text
CLI flags -> parser.Config -> ir.Program -> graph.ResourceGraph -> engine.Plan -> plan.Document
```

需要覆盖的代码入口：

- `run`
- `runConfigCommand`
- `runV2ConfigCommand`
- `parseV2ConfigWithExternalValues`

### [02. HCL 解析、变量和值模型](02-parser-values.zh.md)

解释 `internal/v2/parser` 如何读取多个 `.dbf.hcl` 文件，如何分阶段处理 `locals`、`variable`
和顶层 block，以及变量值来源的优先级。重点说明 `.dbfvars`、`.auto.dbfvars`、环境变量
`DBF_VAR_`、命令行 `-var` 和 `-var-file` 如何进入解析过程。

需要覆盖的代码入口：

- `parser.ParseFilesWithOptions`
- `parseLocals`
- `parseVariables`
- `parseTopLevel`
- `resolveVariableValues`
- `ParseVariableFile`

### [03. Profile、Host 与 Component 如何编译成 IR](03-merge-compile.zh.md)

解释 `internal/v2/merge` 如何把解析后的配置合并成 `internal/v2/ir.Program`。重点说明
profile import、host 覆盖、component 模板、component input 校验、assert 和 runtime facts
在编译阶段的关系。

需要覆盖的代码入口：

- `merge.CompileWithOptions`
- `resolveProfile`
- `Merge`
- `buildHostSpec`
- `instantiateComponents`
- `validateHostSpec`
- `evaluateAssertions`

### [04. IR 数据模型与资源边界](04-ir-model.zh.md)

围绕 `internal/v2/ir/types.go` 解释 `Program`、`HostSpec` 以及各类 domain spec 的职责。
重点说明 IR 是领域层结构，不直接等同于 provider 操作；它应该表达用户意图和稳定语义，
而不是远端命令细节。

需要覆盖的代码入口：

- `ir.Program`
- `ir.HostSpec`
- `ir.SourceRef`
- `ir.LifecycleSpec`
- 各 domain spec：`APTSpec`、`FileSpec`、`SystemdSpec`、`DockerSpec` 等

### [05. ResourceGraph 如何展开资源和依赖](05-resource-graph.zh.md)

解释 `internal/v2/graph` 如何把一个 host 的 IR 展开为可计划、可执行的 resource node 和
operation。重点说明 address 命名、provider payload、依赖关系、operation trigger，以及
敏感内容如何在 graph JSON 中避免泄漏。

需要覆盖的代码入口：

- `graph.Compile`
- `compileHost`
- `ResourceGraph.Validate`
- `Node.MarshalJSON`
- 各 domain node builder

### [06. Plan 文档、Diff 与输出格式](06-plan-output.zh.md)

解释 `internal/v2/plan` 如何把离线 graph 或在线 engine plan 渲染成人类可读文本、JSON 和
HTML。重点说明 `plan.Document` 的格式版本、summary 统计、diff tree、敏感内容摘要和
`--debug` provider address。

需要覆盖的代码入口：

- `plan.New`
- `engine.Plan.Document`
- `BuildDiff`
- `PrintText`
- `PrintJSON`
- `PrintHTML`

### [07. 在线 Engine：读取状态、观测现实、计算动作](07-engine-plan.zh.md)

解释 `internal/v2/engine` 如何在在线模式中读取远端 state，调用 provider 观测实际状态，
然后把 desired、prior state 和 observed reality 比较成 action。重点说明 `create`、`update`、
`delete`、`adopt`、`forget`、`destroy`、`no-op` 的语义。

需要覆盖的代码入口：

- `Engine.Plan`
- `Compare`
- `orphanSteps`
- `operationSteps`
- `Plan.Document`

### [08. Apply 执行模型、锁和调度](08-apply-scheduler.zh.md)

解释 `apply` 为什么会先重新生成在线 plan，如何获取每台 host 的 lock，如何按依赖 wave
执行 resource step 和 operation step，以及执行完成后如何更新 state。重点说明
`--parallel`、每主机并发、失败后的部分执行语义和重复 apply 的幂等预期。

需要覆盖的代码入口：

- `Engine.Apply`
- `executionWaves`
- `runExecutionWaves`
- `applyStep`
- `runOperation`
- `persistHostFacts`

### [09. Backend、SSH 与远端 State](09-backend-state.zh.md)

解释 `internal/v2/engine` 的 backend 抽象和 SSH 实现如何读写远端 state、获取 lock、执行命令、
上传内容，并说明 `internal/v2/state` 的格式、normalize、desired digest 和 ownership。

需要覆盖的代码入口：

- `Backend`
- `NewSSHBackend`
- `Runner`
- `state.State`
- `state.Resource`
- `state.Normalize`
- `state.DesiredDigest`

### [10. Native Provider 与资源实现](10-native-provider.zh.md)

解释 native provider 如何把 graph node 的 provider payload 转成 Debian 主机上的实际操作。
重点说明每种 provider type 的 plan/apply/destroy 边界、observed 值、desired digest 和
低阶命令预览。

需要覆盖的代码入口：

- `Provider`
- `NewNativeProvider`
- `Provider.Plan`
- `Provider.Apply`
- `Provider.Destroy`
- `Provider.RunOperation`

### [11. Secrets、Sensitive 与脱敏链路](11-redaction.zh.md)

解释 sensitive 数据从 parser、IR、graph、plan、state、HTML/JSON 输出到测试断言的完整脱敏链路。
重点说明 `content_write_only`、secret file、sensitive variable/component input 的差异，
以及哪些结构绝不能把明文序列化出去。

需要覆盖的代码入口：

- `Node.MarshalJSON`
- `plan.BuildDiff`
- `cmd/dbf/redaction_matrix_test.go`
- `internal/v2/testassert/secrets.go`
- sensitive variable 和 component input 的编译逻辑

### [12. 测试体系与新增能力清单](12-testing-and-extension.zh.md)

解释单元测试、golden test、CLI test、source build test 和 libvirt integration test 各自守护的
边界。最后给出“新增一个 domain block 或 provider resource”时的代码修改清单和测试清单。

需要覆盖的路径：

- `cmd/dbf/*_test.go`
- `internal/v2/parser/*_test.go`
- `internal/v2/merge/*_test.go`
- `internal/v2/graph/*_test.go`
- `internal/v2/plan/*_test.go`
- `internal/v2/engine/*_test.go`
- `internal/v2/testdata/**`
- `test/integration/libvirt/**`

## 写作约定

- 文件名使用两位序号和中文主题，例如 `01-architecture.zh.md`。
- 每篇先给一张数据流，再进入代码入口。
- 引用代码时使用稳定函数名和包路径，避免复制大段源码。
- 面向维护者解释设计原因，不面向终端用户解释命令怎么用。
- 每篇结尾提供“修改这个阶段时的检查清单”。
