# DebianForm CLI 颜色输出和日志策略

<p align="right"><a href="cli-color-output-policy.md">English</a> | <strong>简体中文</strong></p>

本文档记录 CLI 颜色输出和日志颜色的需求讨论。结论：颜色适合用于面向人的终端输出，
尤其是 `plan` / `apply` 中需要快速识别风险的内容；不适合写入结构化输出、持久化日志或默认 CI 输出。

## 背景

DebianForm 的输出同时服务两类场景：

- 人在终端里阅读 `plan`、`apply` 和错误提示。
- 程序、CI 或审计流程读取 JSON、日志文件和命令输出。

颜色能帮助用户快速识别危险动作，但 ANSI 颜色码会污染机器可读输出和日志文件。因此颜色必须是展示层能力，
不能成为语义本身。

## 目标

- 让人类可读的终端输出更容易区分 create、update、delete、warning、error 和高风险动作。
- 让删除行为提示可以按风险分类显示颜色。
- 保持 JSON、state、持久化日志和默认 CI 输出无 ANSI 颜色码。
- 支持 `NO_COLOR` 和显式颜色控制参数。
- 确保无颜色环境下仍能完整理解输出语义。

## 非目标

- 不给所有日志行统一染色。
- 不把颜色写入 plan JSON、state、debug log 或远端日志文件。
- 不让颜色成为判断行为的唯一依据。
- 不在 secret、sensitive value 或 command preview 上引入新的泄露风险。

## 输出类型策略

| 输出位置 | 默认是否加颜色 | 原因 | 要求 |
| --- | --- | --- | --- |
| `dbf plan` 文本输出 | 是，仅 TTY | 用户需要快速识别变更类型和删除风险。 | 同时显示文本分类，例如 `delete behavior: destructive`。 |
| `dbf apply` 执行前计划 | 是，仅 TTY | 与 `plan` 保持一致，执行前风险最重要。 | 颜色规则与 `plan` 一致。 |
| `dbf apply` 执行过程进度 | 谨慎，仅 TTY | 过多颜色会降低可读性。 | 只给 warning、error、危险删除、需要人工注意的 note 加色。 |
| `dbf apply` 执行后 summary | 是，仅 TTY | 帮助复核实际执行结果。 | 如果有删除动作，保留删除行为图例。 |
| `dbf check` 文本输出 | 可以，仅 TTY | drift、error、warning 适合用颜色辅助扫描。 | drift 类型必须有文字说明。 |
| `dbf validate` 文本输出 | 可以，仅 TTY | warning/error 可以加色。 | 成功输出不需要大量颜色。 |
| `--format json` | 否 | 机器可读输出不能包含 ANSI。 | 使用结构化字段表达风险和分类。 |
| HTML plan | 是，使用 CSS | HTML 本身是展示格式。 | 颜色来自 CSS class，不混入 ANSI。 |
| 持久化日志文件 | 否 | 便于检索、审计和复制。 | 默认禁止 ANSI；如未来支持必须显式开启。 |
| CI 非 TTY 输出 | 否 | 默认应保持稳定、可 grep。 | 可通过显式参数强制开启。 |
| debug log | 否 | debug log 重点是诊断和脱敏。 | 不引入 ANSI，不泄露 sensitive 内容。 |

## 颜色控制接口

建议提供统一参数：

```text
--color=auto|always|never
```

语义：

- `auto`：默认值。只有 stdout/stderr 是 TTY 且没有禁用颜色时启用。
- `always`：强制输出颜色，适合用户明确要保留彩色 CI log 的场景。
- `never`：强制禁用颜色。

环境变量：

- `NO_COLOR` 存在且非空时，`auto` 模式禁用颜色。
- `TERM=dumb` 时，`auto` 模式禁用颜色。
- 如果未来支持 `CLICOLOR` / `CLICOLOR_FORCE`，必须在 CLI 手册中定义与 `--color` 的优先级。

优先级建议：

1. 显式 `--color=never` 禁用颜色。
2. 显式 `--color=always` 启用颜色。
3. `--color=auto` 下遵守 `NO_COLOR`、`TERM=dumb` 和 TTY 检测。

## 颜色语义

颜色只用于辅助扫描，必须和文本语义绑定。

| 语义 | 建议颜色 | 示例 |
| --- | --- | --- |
| create / add | 绿色 | 新建资源、adopt 成功。 |
| update / change | 黄色 | 内容或属性变更。 |
| delete / destroy | 按删除行为分类 | 不再简单统一红色。 |
| forget | 灰色 | 解除管理但不修改远端。 |
| restore-original | 蓝色 | 恢复 state 中保存的原内容。 |
| remove-managed-artifact | 黄色 | 删除 DebianForm 写入的文件或产物。 |
| destructive | 红色 | 可能删除数据、账号、包、目录或停止服务。 |
| external-side-effect | 紫色 | 会触发 reload/restart/activate/update 等额外动作。 |
| warning | 黄色 | 配置可执行但需要注意。 |
| error | 红色 | 命令失败或配置非法。 |
| note / hint | 青色或默认色 | 例如重新登录后 group membership 才生效。 |

删除行为颜色以 `docs/delete-behavior-diagnostics-plan.zh.md` 为准。通用 action 颜色不能覆盖删除行为颜色，
否则所有删除都会被误读为同一种风险。

## 日志颜色边界

这里的“log”需要区分两种：

- 终端进度日志：`apply` 过程中打印给用户看的短行状态。
- 持久化或可转存日志：CI log、debug log、文件日志、远端命令输出。

终端进度日志可以少量使用颜色，但只能给风险和结果状态加色，例如 warning、error、destructive delete。
普通执行行、远端命令输出、debug 信息不应该默认加色。

持久化或可转存日志默认不加色。原因是：

- ANSI 会影响 grep、diff、审计和 issue 复制。
- 日志可能被其他系统二次解析。
- 颜色不提供新的机器语义。
- 颜色码可能让 secret 脱敏、错误定位和 golden 测试更复杂。

## 实现要求

- 颜色应集中在 CLI renderer 或 terminal writer 层处理，provider、engine、plan JSON 不应拼 ANSI 字符串。
- 结构化 plan 应保留行为字段，例如 `action`、`delete_behavior`、`delete_risk`，再由 renderer 决定颜色。
- 测试 golden 默认使用无颜色输出。
- 颜色测试应只覆盖 renderer，避免让大量业务测试依赖 ANSI。
- 文本输出在禁用颜色时必须仍然包含符号、分类名和说明。
- HTML plan 使用 CSS class 表示颜色语义，不复用 ANSI 逻辑。

## 可验证实现 Loop

### Loop 1：CLI 颜色控制基础（已实现）

范围：

- 新增 `--color=auto|always|never`。
- `auto` 支持 TTY 检测、`NO_COLOR` 和 `TERM=dumb`。
- 文本 plan renderer 通过选项决定是否输出 ANSI。
- JSON、HTML、state、debug log 不引入 ANSI。

验收：

- `dbf plan --offline --color=always` 的文本输出包含 ANSI。
- `dbf plan --offline --color=never` 的文本输出不包含 ANSI。
- `NO_COLOR=1 dbf plan --offline` 的默认 `auto` 输出不包含 ANSI。
- `dbf plan --offline --format json --color=always` 输出仍是无 ANSI JSON。
- 原有无颜色 golden 不需要因为默认 `auto` 在非 TTY 中启用颜色而变化。

### Loop 2：删除行为结构化字段（已实现）

范围：

- 在 `plan.Change` 中加入 `delete_behavior`、`delete_notes`、`delete_risk`。
- 只在 delete/destroy/forget 类动作中输出。
- 先覆盖 BBR/sysctl、apt source file keep/restore、file、directory、package、systemd/nftables、operation side effect 等核心路径。

验收：

- BBR 删除 plan JSON 包含 `delete_behavior = "remove-managed-artifact"`。
- apt source file keep/restore 在 JSON 中分类不同。
- 非删除动作不输出删除行为字段。
- sensitive 内容不进入 `delete_notes`。

### Loop 3：文本和 HTML 删除行为提示（已实现）

范围：

- 文本 plan/apply 删除项显示 `delete behavior` 和 `note`。
- 有删除项时底部显示图例和文档路径。
- HTML plan 增加删除行为 badge 和图例。

验收：

- BBR 删除文本 plan 明确提示只删除 sysctl 持久化文件，不恢复运行时值。
- destructive、external-side-effect、restore-original、forget 在文本和 HTML 中可区分。
- 无删除动作时不显示删除行为图例。

### Loop 4：apply/check 进度颜色边界（部分已实现）

范围：

- `apply`/`check` 的计划输出复用 plan renderer 颜色选项。
- 进度日志默认不大量加色；只允许 warning/error/高风险提示后续单独接入。

验收：

- `apply --color=never` 的计划输出无 ANSI。
- `apply --color=always` 的计划输出有 ANSI。
- stderr 进度日志默认无 ANSI，避免污染 CI/debug log。

当前状态：

- 计划输出已复用 `--color`。
- 进度日志保持无 ANSI。
- warning/error/高风险进度提示是否加色仍留待后续单独设计。

## 验收标准

- TTY 下 `dbf plan` 能用颜色区分 create/update/delete 行。
- 删除项按删除行为分类显示颜色，并显示文本分类。
- `NO_COLOR=1 dbf plan` 不输出 ANSI 颜色码。
- `dbf plan --format json` 不输出 ANSI 颜色码。
- 非 TTY 或 CI 默认输出不包含 ANSI 颜色码。
- `dbf apply` 的普通进度行不过度使用颜色，warning/error/危险删除有明确颜色和文字提示。
- 持久化日志、debug log、state 和 plan JSON 不包含 ANSI 颜色码。

## 待定问题

- `apply` 执行过程中的远端命令 stdout/stderr 是否应完全原样透传，还是由 DebianForm 统一去除 ANSI。
- Windows 终端支持是否需要额外检测，还是当前阶段只按 Unix TTY 处理。
