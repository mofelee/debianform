<p align="right">
  <a href="compatibility-policy.md">English</a> | <strong>简体中文</strong>
</p>

# DebianForm 兼容性和迁移政策

本文档定义 DebianForm 的 DSL、CLI、state schema 和 plan JSON 格式兼容性规则。项目当前仍处于
public preview / beta 阶段；这里的政策用于约束后续发布，并说明进入 stable 前需要达到的门槛。

## 适用范围

本政策覆盖：

- 用户可见 CLI 命令、参数和退出码语义。
- `.dbf.hcl` DSL、默认值、validation 和 resource address。
- 远端 state 文件 schema、state 迁移和回滚边界。
- `dbf plan --format json` 输出格式。
- release notes 中的 breaking changes、migration notes 和 known issues。

不覆盖：

- Go 包内部 API。
- 未在 support matrix 标记为 Beta/Compat 的 Design-only fixture。
- `--debug` 输出中的内部 provider address 和诊断细节。
- 未承诺支持的外部服务行为，例如第三方 APT 源、registry、Homebrew tap 服务可用性。

## 发布阶段

### Public Beta

beta 阶段允许破坏性变更，但必须满足：

- 破坏性 CLI、DSL、state 或 plan JSON 变更必须写入 release notes 的 `Breaking Changes`。
- 需要用户操作的变更必须写入 `Migration Notes`，包括配置修改、state 处理和回滚限制。
- `CHANGELOG.md` 必须同步记录用户可见影响。
- 不能在 patch 风格 beta tag 中静默改变已有资源的危险行为，例如从 no-op 变成 destroy。

beta 阶段的目标是尽快收敛兼容边界；不承诺旧 beta 版本长期维护。

### Stable

stable 之后遵循 semver：

- patch 版本只能包含兼容修复、安全修复、文档更新和非破坏性增强。
- minor 版本可以增加 DSL、plan JSON 字段、state 字段和新 provider 能力。
- 破坏性 CLI、DSL、state 或 plan JSON 变更只能进入 minor 版本，并且必须有迁移说明。
- 安全修复优先进入最新 stable patch；是否 backport 到旧 minor 由维护者按风险决定。

## DSL 兼容性

兼容变更包括：

- 新增可选 block、attribute、enum 值或 provider 能力。
- 为已有 block 新增默认值明确、不会改变现有配置含义的字段。
- 增加 warning、deprecation 诊断或更具体的错误信息。
- 修复原本错误的 validation，只要不让已合法且安全的配置突然失败。

破坏性变更包括：

- 移除或重命名已有 block、attribute、enum 值或函数。
- 修改默认值，导致同一配置产生不同远端资源或不同 destroy/update 行为。
- 修改稳定 resource address，导致已有 state 无法关联远端资源。
- 把原本合法的配置改为错误，除非原行为有安全风险或会造成明显错误配置。
- 把 compat 写法移除，而没有至少一个 minor 周期的 deprecation warning 和迁移路径。

弃用流程：

1. 在 release notes 中标记 deprecated，并在 CLI 输出 warning。
2. 文档给出替代写法和迁移步骤。
3. 至少保留到下一个 minor 版本；beta 阶段可以缩短，但必须在 release notes 中说明。
4. 移除时按 breaking change 处理。

## CLI 兼容性

兼容变更包括：

- 新增命令或 flag。
- 对成功输出增加非结构化说明，但不改变机器可读 JSON 格式。
- 对错误信息增加更多上下文。

破坏性变更包括：

- 删除或重命名命令、flag。
- 改变已记录命令的退出码语义。
- 改变 `apply`、`check`、lock 或 confirmation 的安全语义。
- 改变 `-f`、`--host`、`--offline`、`--format json` 等主路径参数含义。

## State Schema 迁移政策

当前 state 顶层 `version` 为 `2`。state 文件是远端事实和 ownership 的安全边界，
迁移必须保守。

兼容 state 变更包括：

- 新增可忽略字段。
- 新增 resource record 的摘要字段。
- 对 observed 摘要增加信息，但不改变 ownership。
- 修复脱敏摘要字段，只要不需要读取旧明文。

破坏性 state 变更包括：

- 修改顶层 `version`。
- 删除、重命名或改变 `resources` key/address 语义。
- 改变 `ownership` 含义。
- 改变 desired digest 的计算方式，导致大量无意义 drift 或 destroy。
- 需要重写 state 才能继续 apply/check。

迁移规则：

- CLI 读取 state 时必须检测 `version`。
- 对未知较新版本必须拒绝 apply，并提示用户升级 CLI；不能尝试写回。
- 对旧版本如果有自动迁移器，必须先备份原 state，再原子写入迁移结果。
- 自动迁移不得写入 secret 明文，不得降低文件权限，不得改变 lock 路径。
- 如果不能安全自动迁移，必须失败并给出手工步骤。
- release notes 的 `Migration Notes` 必须说明迁移前检查、备份、回滚和失败恢复。

回滚边界：

- 已被新版本迁移过的 state 不保证旧 CLI 可读取。
- 需要支持回滚时，release notes 必须明确要求保留迁移前 state 备份。
- patch 版本不得引入需要不可逆 state 迁移的变更。

## Plan JSON Format 兼容性

`dbf plan --format json` 当前格式版本是 `debianform.plan.alpha1`。

兼容变更包括：

- 新增顶层字段、change 字段、operation 字段或 diagnostic 字段。
- 新增 action/kind 枚举值，但必须在 release notes 中说明。
- 对 summary 增加字段。
- 增加更精确的 source、diagnostic 或 non-debug metadata。

破坏性变更包括：

- 修改 `format_version` 语义但不改版本字符串。
- 删除或重命名已有字段。
- 改变字段类型。
- 改变 `changes`、`operations`、`diagnostics` 的基本结构。
- 在普通 JSON 输出中加入 debug-only provider address。
- 泄露 sensitive 明文。

消费者建议：

- 必须读取并校验 `format_version`。
- 对未知字段应忽略。
- 对未知 action/kind 应保守处理为需要人工检查。
- 不应依赖 object key 顺序。
- 不应解析 text renderer 输出作为机器接口。

格式版本规则：

- 非破坏性新增字段不需要修改 `format_version`。
- 破坏性格式变更必须修改 `format_version`。
- stable 后，破坏性 plan JSON 格式变更只能进入 minor 版本，并必须在 release notes 中列出。

## Release Gate

每次 release 前必须检查：

- `CHANGELOG.md` 是否列出用户可见兼容性影响。
- release notes 是否保留 `Compatibility`、`Breaking Changes` 和 `Migration Notes`。
- 破坏性 DSL/state/plan JSON 变更是否只进入允许的版本线。
- state migration 是否有测试、备份策略和失败恢复说明。
- plan JSON 格式变更是否更新 `docs/plan-format.md`。
- support matrix 是否反映新增、弃用或不支持的能力。

stable/GA 之前还必须满足：

- 至少多个正式 release 没有未说明的破坏性 DSL/state/plan JSON 变更。
- 真实用户或真实主机反馈没有阻塞性兼容问题。
- state migration 和 plan JSON policy 已在 release process 中作为 gate 执行。
