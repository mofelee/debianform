# script / on_change 实施计划

<p align="right"><a href="script-on-change-implementation-plan.md">English</a> | <strong>简体中文</strong></p>

本文档把 [script / on_change 需求文档](script-on-change-requirements.zh.md) 拆成可执行
开发循环。每个 loop 都必须形成一个可合并闭环：

- 代码路径可运行。
- 单元测试和必要 golden 覆盖本轮语义。
- 至少一个 fixture 或示例能验证本轮能力。
- 文档同步更新。
- `make test` 通过。

状态约定：

- `[x]` 已完成
- `[ ]` 未完成

## 当前基线

- [x] host 是最终执行单元，plan/apply/check 都按 host 执行。
- [x] profile 可被 profile/host import，按现有 merge 规则合并。
- [x] component 支持 typed input，并在 host 挂载后展开。
- [x] ResourceGraph 已支持 operation、`TriggeredBy` 和依赖调度。
- [x] plan text/json/html 已能展示 operation 和 `triggered_by`。
- [x] provider 已支持运行 operation 命令。
- [x] component 内已支持 `script` block 的 DSL 解析和 HostSpec 编译。
- [x] `files.file` 已支持 `on_change` 的 DSL 解析和 HostSpec 编译。
- [x] engine 已支持脚本 `mode` 的 once/each 触发上下文。
- [x] component `script.outputs` 已支持脚本生成文件的 hash 记录和漂移触发。

## Loop 1: DSL 解析和 IR 骨架

目标：让 component 内可以声明 `script`，`files.file` 可以引用同 component 内的 script；
本轮只打通 validate/compile，不生成 operation。

范围：

- `component` 内新增 `script "<name>" { ... }`。
- `files.file` 新增 `on_change = script.<name>`。
- `script` 字段支持 `mode`、`interpreter`、`outputs`、`run`、`content`、`commands`。
- `run`、`content`、`commands` 互斥且必须三选一。
- `mode` 仅允许 `"once"` / `"each"`，默认 `"once"`。
- `interpreter` 默认 `["/bin/sh", "-eu"]`，必须是非空 string list。

暂不做：

- 不在 `host` 或 `profile` 支持 `script`。
- 不生成 ResourceGraph operation。
- 不执行脚本。
- 不把 host 的 script 引用作为 component input。

代码：

- [x] parser 支持 component-local 和程序根部的 `script` block。
- [x] parser 增加 `files.file.on_change` 的 HCL traversal 解析。
- [x] IR 增加 `ComponentScriptSpec`，挂到 `ComponentInstanceSpec`。
- [x] IR 在 `ManagedFile` 上增加 `OnChange`，保存 script 名称和 source。
- [x] merge/buildComponentSpec 编译 component script 和 file on_change。
- [x] validate 解析 `on_change` 到 component-local 或 host-scoped 根声明身份。
- [x] 更新 HostSpec JSON/golden，确保 script 元数据稳定输出且不泄漏敏感内容。

测试：

- [x] parser 单测覆盖 component `script`、`on_change = script.reload`。
- [x] parser/merge 负例覆盖 host/profile 内声明 script、未知 script、非法 traversal。
- [x] merge 单测覆盖互斥执行体、非法 mode、空 interpreter。
- [x] fixture 覆盖一个 component 文件引用 script 的 HostSpec。

文档：

- [x] 将需求文档中本轮已实现部分同步到 DSL Reference，标记为已支持语法。

验收：

- [x] `dbf validate` 可以接受包含 component script/on_change 的配置。
- [x] 未知 script 引用报错并指向 `files.file.on_change`。
- [x] `make test` 成功。

## Loop 2: ResourceGraph operation 生成

目标：从 component `script` 生成 ResourceGraph operation，并让 plan 能看到哪些文件会触发脚本。

范围：

- 为每个被引用的 component script 生成 operation。
- operation 地址使用 component instance 作用域：

```text
host.<host>.components.<instance>.script["<name>"]
```

- operation `TriggeredBy` 包含引用该 script 的 component file 节点。
- operation `DependsOn` 至少包含 `TriggeredBy` 中的 file 节点。
- `CommandPreview` 使用短文本，例如 `script reload (once)`。

暂不做：

- 不执行真实脚本。
- 不支持 `each` 拆分运行；本轮先按现有 operation 模型展示一次。

代码：

- [x] graph 编译 component file 时收集 script triggers。
- [x] graph 为 script 生成 operation，挂载 source、summary、mode、preview。
- [x] graph validate 确认 operation trigger 地址存在。
- [x] plan text/json/html 能展示 script operation 和 triggered_by。
- [x] ResourceGraph JSON 不展开完整脚本执行体，避免 plan 噪声和泄漏面扩大。

测试：

- [x] graph 单测覆盖 component script operation 地址、依赖和 triggered_by。
- [x] plan golden 覆盖 script operation 的 text/json 输出。
- [x] 负例覆盖没有任何 file 引用的 script 不生成 operation。

文档：

- [x] 更新需求文档中的 Plan 展示为实际输出格式。
- [x] DSL Reference 增加最小可运行示例，使用 `dbf plan --offline` 验证地址形状。

验收：

- [x] `dbf plan --offline` 可看到 script operation。
- [x] operation 只显示短 preview，不展开完整脚本内容。
- [x] `make test` 成功。

## Loop 3: 执行载荷和 NativeProvider 运行（已实现）

目标：让 script operation 在 apply 中真正执行，并保持 plan 展示简洁。

范围：

- ResourceGraph operation 增加内部执行载荷，保存 interpreter、脚本内容、mode。
- provider 执行 script operation 时使用载荷，而不是把完整脚本塞进 `CommandPreview`。
- `run` 包装成脚本文本执行。
- `content` 原样交给 interpreter 执行。
- `commands` 安全拼接成脚本文本执行。
- 脚本非零退出导致 apply 失败。

暂不做：

- 不实现 `each` 拆分；仍按 once 运行一次。
- 不支持 stdin JSON。

代码：

- [x] graph.Operation 增加不在普通 plan 展示中展开的 script payload。
- [x] Operation JSON redaction/省略策略明确，避免脚本文本污染 plan JSON。
- [x] NativeProvider.RunOperation 支持 script payload。
- [x] SSH runner 使用 `RunInput` 或等效方式把脚本文本交给自定义 interpreter。
- [x] commands 模式使用现有 shell quote 规则生成安全脚本文本。

测试：

- [x] NativeProvider 单测覆盖 run/content/commands 三种执行体。
- [x] 失败路径测试覆盖非零退出返回错误。
- [x] redaction 测试覆盖 script payload 不进入 operation preview/plan。
- [x] engine 单测覆盖 script operation 被触发后调用 provider。

文档：

- [x] 更新需求文档，明确执行载荷不属于 plan 公共接口。

验收：

- [x] apply 时脚本能在文件变更后执行。
- [x] plan text/json/html 不展示完整脚本内容。
- [x] `make test` 成功。

## Loop 4: once/each 触发语义和环境变量（已实现）

目标：实现 `mode = "once"` 与 `mode = "each"` 的最终语义，并向脚本注入触发上下文。

范围：

- `once`：同一轮 apply 中同一 script 被多个文件触发时只运行一次。
- `each`：同一轮 apply 中每个实际变更文件各运行一次。
- 注入环境变量：
  - `DBF_SCRIPT_NAME`
  - `DBF_COMPONENT_NAME`
  - `DBF_TRIGGER_ADDRESS`
  - `DBF_TRIGGER_PATH`
  - `DBF_TRIGGER_ADDRESSES`
  - `DBF_TRIGGER_PATHS`
- online plan 和 apply 使用实际 changed 集合决定 operation step。

代码：

- [x] engine.OperationStep 携带实际触发源地址和路径。
- [x] operationSteps 对 `each` 按实际触发源拆分 step。
- [x] execution item 地址对 `each` step 保持唯一，避免同一 operation 多 step 覆盖结果。
- [x] NativeProvider 执行 script 前注入环境变量。
- [x] `once` 多触发时环境变量列表使用换行分隔。
- [x] scheduler 保持脚本在触发文件之后执行。

测试：

- [x] engine 单测覆盖一个 once script 被两个文件触发只运行一次。
- [x] engine 单测覆盖一个 each script 被两个文件触发运行两次。
- [x] NativeProvider 单测覆盖环境变量内容。
- [x] plan JSON/text 覆盖 each 拆分后的稳定地址或展示格式。

文档：

- [x] DSL Reference 记录 once/each 语义和环境变量。
- [x] 用户手册或示例补充 each 的适用场景。

验收：

- [x] once/each 与需求文档一致。
- [x] `make test` 成功。

## Loop 5: 示例、文档和集成验收（已实现）

目标：把能力收口成用户可读、可验证、可维护的完整交付。

范围：

- 增加一个小型 component 示例，展示 config file 变更触发 service reload。
- 增加一个 libvirt integration case，验证真实 Debian 13 上首次 apply、二次 no-op、配置变更触发 reload。
- 更新支持矩阵、DSL Reference、README 或用户手册入口。
- 明确 `script` / `on_change` 已从设计目标变为已实现能力。

代码：

- [x] 新增 `examples/component-script-on-change.dbf.hcl`。
- [x] 新增 `test/integration/libvirt/cases/script-on-change/`。
- [x] 如有必要，更新 inspect 输出，让 component 公开 API 中能看到相关 input。

测试：

- [x] `dbf validate -f examples/component-script-on-change.dbf.hcl` 成功。
- [x] `dbf plan -f examples/component-script-on-change.dbf.hcl --offline` 输出预期 operation。
- [x] `make test` 成功。
- [x] `make test-integration-layout` 成功。
- [x] libvirt case 在 Debian 13 amd64 上通过。

文档：

- [x] DSL Reference 从"设计目标"迁移到"已实现语法"。
- [x] README 可选加入短例子或链接到示例。
- [x] `docs/script-on-change-requirements.zh.md` 标记已实现部分，保留后续问题。
- [x] `docs/README.zh.md` 将链接从"设计中的需求"移动到合适的日常查阅或 DSL 相关位置。

验收：

- [x] 用户可以通过 component 封装文件变更后的 reload/restart 操作。
- [x] host 只需要挂载 component 并传 input，不需要知道内部 script 细节。
- [x] plan/apply/check 输出与现有 operation 风格一致。
- [x] `make test` 和相关集成检查通过。

## 后续扩展

- 跨 component 合并同类操作，例如多个 component 共同触发一次 nginx reload。
- JSON stdin 触发上下文，替代换行环境变量列表。
- 更细的失败策略，例如 warn-only 或重试；第一版不做。
- 更丰富的 script inspect 输出，用于 component 作者调试。
