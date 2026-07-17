# 11. Secrets、Sensitive 与脱敏链路

<p align="right"><a href="11-redaction.md">English</a> | <strong>简体中文</strong></p>

本章解释 DebianForm 如何在 parser、IR、graph、plan、state 和 provider 执行中处理敏感数据。
这条链路必须整体理解，因为任何一层放松都可能泄漏 secret。

## 敏感数据来源

常见来源：

- `secret` file。
- `files.file`、nftables 等资源显式设置的 `sensitive = true`。
- sensitive content 自动传播到 files、systemd unit、APT source/signing key 或 nftables
  等文件类资源的标记。
- `content_write_only = true`。
- sensitive variable。
- ephemeral variable。
- sensitive component input。
- `-var @path`、`-var env:NAME`、stdin 等外部输入。

这些概念相关但不等同。维护时要分清“可执行时需要明文”和“可输出/持久化时不能有明文”。

## Parser 层

parser 使用 cty mark 和 `parser.Value` 字段传播：

- `SensitiveMark`
- `EphemeralMark`
- `Value.Sensitive`
- `Value.Ephemeral`

变量声明中 `sensitive = true` 会让归一化后的变量值带 sensitive。`ephemeral = true` 会让值带 ephemeral。

parser 还会限制 ephemeral 用在 map key 或 set element 中，因为这些位置会影响稳定身份和输出。

## CLI 变量来源保护

CLI 支持从文件、stdin、环境变量读取变量。对 sensitive variable：

- 读取失败时错误信息会隐藏敏感 source path。
- 解析失败时不会把原始值写进错误。
- inspect 默认值会显示 `"<sensitive>"`。

这部分主要在 `cmd/dbf/main.go` 和 parser variable 逻辑中完成。

## IR 层

IR 可能仍然携带执行所需内容，包括文件 content。IR 本身不是脱敏边界。

但是 IR 中的字段必须保留足够元数据，例如：

- `Sensitive`
- `ContentWriteOnly`
- `ContentSummary`
- source

后续 graph、plan、state 依赖这些字段决定如何展示和持久化。

APT source、APT signing key 和 nftables 的 content 编译路径会在把 `parser.Value` 转为
Go 字符串前检查 sensitive/ephemeral 元数据：sensitive 标记继续传播；这三类资源尚未实现
ephemeral write-only 语义，因此在编译期拒绝该值。不能只保存字符串值，否则这些独立
文件类资源会绕过下游脱敏。

## Graph 层

graph node 有两套内容：

- `Desired`
- `ProviderPayload`

provider payload 可能包含真实执行内容。`Node.MarshalJSON` 对 content write only 或 sensitive node 会清空
`ProviderPayload`，避免 graph JSON 泄漏。

注意：这只保护 JSON 序列化；内存中的 graph 仍需要 payload 执行。
节点的 `Desired` 也必须先按 sensitive/write-only 语义移除明文并保留必要摘要；不能只依赖
`Node.MarshalJSON` 隐藏 `ProviderPayload`。

## Plan 层

plan diff 必须做到：

- 普通文本可以展示 hunk。
- sensitive 内容只展示摘要。
- write-only 内容不能展示明文。
- HTML 和 JSON 与 text 一样不能泄漏。

`BuildDiff` 和相关格式化函数承担这部分职责。新增 diff kind 或输出格式时，都要跑 redaction 测试。

## State 层

state 是持久化边界，必须脱敏。`state.SanitizeDesired` 会：

- 删除 `content`，保存 `content_sha256` 和 `content_bytes`。
- 对 sensitive desired 删除 `source_path` 和 `summary`。

`DesiredDigest` 基于 sanitize 后的 desired 计算，因此能检测内容变化，但 state 不保存明文内容。

observed 也经过 `SanitizeObserved`。

## Provider 执行层

provider 有时必须把明文传给远端，例如写文件。原则：

- 优先通过 stdin 或安全 heredoc/base64 传输。
- 不要把 secret 拼进 shell 命令行或 command preview。
- 错误信息、stdout、stderr 不应把 write-only payload 纳入返回 observed。
- command preview 不能包含敏感明文，因为它会显示在 plan 和 operation 中。

redaction matrix 里专门测试 native provider command preview、错误、stdout/stderr 和 observed。

## Redaction regression matrix

`cmd/dbf/redaction_matrix_test.go` 覆盖多条输出路径：

- plan text stdout。
- plan JSON stdout。
- plan HTML artifact。
- hostspec JSON。
- resource graph desired JSON。
- state JSON。
- native provider command preview 和 error。
- native provider stdout/stderr。
- APT source、APT signing key 和 nftables content 的 sensitive 输出矩阵。

`internal/core/testassert/secrets.go` 维护一组哨兵 secret 字符串。`NoSecretLeak` 会检查输出中不包含这些值。
上述三类 content 的 fail-closed ephemeral 用例位于 merge 回归测试中。

新增敏感路径时，优先把它纳入 matrix。

## Ephemeral 的特殊性

ephemeral 值比 sensitive 更严格：它通常不应进入持久化结构。当前实现通过 mark、key 限制、state sanitize
和 redaction test 防止泄漏。

`files.file.content` 具备 write-only provider payload 和非敏感 `content_version` 触发字段。
APT source、APT signing key 和 nftables content 当前没有这套语义，因此会在编译期拒绝
ephemeral 值。

如果新增能力允许 ephemeral 影响资源内容，要检查：

- 它是否会进入 resource address。
- 它是否会进入 state desired/observed。
- 它是否会出现在 error、plan、command preview。

## 设计边界

- sensitive 标记不是“可以忽略输出检查”的借口，而是必须驱动输出脱敏。
- state、plan、JSON、HTML、错误信息都是泄漏面。
- provider 可以短暂持有明文用于执行，但不能把明文写入持久化或展示结构。
- redaction 测试应覆盖真实路径，不只测单个 helper。

## 修改检查清单

- 新增 secret/sensitive 输入：补 parser/merge 标记传播测试。
- 新增输出路径：加入 redaction matrix。
- 新增 state 字段：确认 sanitize 后不含明文和路径泄漏。
- 新增 provider 命令：确认命令、错误、stdout/stderr 不含 secret。
- 新增 HTML/JSON 字段：用哨兵 secret 做端到端检查。
