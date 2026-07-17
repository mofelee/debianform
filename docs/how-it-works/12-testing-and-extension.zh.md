# 12. 测试体系与新增能力清单

<p align="right"><a href="12-testing-and-extension.md">English</a> | <strong>简体中文</strong></p>

本章解释 DebianForm 的测试分层，以及新增一个 domain block 或 provider resource 时应该改哪些地方。

## 测试分层

项目测试大致分为：

- parser 单元测试：守护 HCL、变量、表达式和值模型。
- merge 单元测试：守护 profile 合并、component input、IR 编译和校验。
- graph golden：守护 IR 到 resource graph 的展开。
- plan golden：守护 plan document/text/diff/action。
- engine 单元测试：守护 state、action、apply、SSH backend、facts。
- CLI 测试：守护命令入口、flags、inspect、redaction。
- libvirt integration：守护真实 Debian/Ubuntu 目标主机行为。
- source build integration：守护源码构建类 component。

不要只靠一种测试证明跨层能力。DSL 新能力通常至少需要 parser/merge、graph、plan/engine 三层证据。

## Golden 数据目录

`internal/core/testdata` 下的主要目录：

- `fixtures`：输入 `.dbf.hcl` 和变量文件。
- `hostspec`：编译后 IR host spec golden。
- `graph`：resource graph golden。
- `plan`：plan JSON/text golden。
- `parser`：parser 相关 golden。
- `invalid`：应失败的配置样例。

golden 不是负担，它们是架构边界的快照。改动 golden 前要确认差异是期望语义，而不是意外地址或脱敏变化。

## CLI 测试

`cmd/dbf/*_test.go` 覆盖：

- 命令分发。
- 默认文件选择和 `-f`。
- inspect 输出。
- plan 输出。
- redaction matrix。

CLI 测试适合覆盖用户可见行为和组合路径，尤其是变量优先级、敏感输入和输出格式。

## Engine/provider 测试

`internal/core/engine/*_test.go` 适合覆盖：

- `Compare` action 矩阵。
- apply state 更新。
- orphan destroy/forget。
- lock/read/write。
- facts discovery。
- SSH runner 参数。
- provider 对 fake runner 的命令行为。

真实远端命令尽量通过 fake runner 检查脚本内容和 observed 返回；需要验证系统真实行为时再用 libvirt。

## Libvirt integration

`test/integration/libvirt` 覆盖真实 Debian/Ubuntu 目标主机上的 apply/check 流程。case 通常包含：

- `*.dbf.hcl`
- `*.check.sh`
- 可选 `*.drift.sh`
- 多 host case 文件。

适合放到这里的场景：

- systemd、apt、docker、network、kernel 等本地 fake 很难证明的行为。
- 多 host 依赖。
- apply 后 check/drift 的真实闭环。

## 新增 domain block 的步骤

新增一个用户 DSL domain block，通常要改：

1. parser：识别 block/attribute，输出 `parser.Value` 或专用结构。
2. IR：新增 spec 类型和 `HostSpec` 字段。
3. merge：从 raw value 构建 spec，设置默认值、source、lifecycle、summary。
4. validate：检查必填、枚举、路径、引用关系。
5. graph：把 spec 展成 node/operation，定义稳定 address。
6. provider：如果需要新 kind，实现 Plan/Apply/Destroy。
7. plan/state：确认 diff、sanitize、digest 是否足够。
8. docs：更新用户文档、支持矩阵和本系列相关章节。
9. tests：补 parser/merge/graph/plan/engine/CLI/integration。

如果新 block 只是语法糖，尽量在 merge 或 graph 展成已有 provider kind，避免无谓新增 provider。

## 新增 provider resource 的步骤

新增 provider resource kind，通常要改：

1. graph：生成 node，确定 `Kind`、`ProviderType`、`Desired`、`ProviderPayload`。
2. provider：在 Plan/Apply/Destroy switch 中注册。
3. provider plan：定义 observed 和 action 判断。
4. provider apply：实现 create/update/delete 幂等命令。
5. provider destroy：实现 orphan managed 删除。
6. state：确认 desired/observed sanitize 和 digest。
7. schedule：必要时更新 `SafeParallelKind`。
8. operation：如果需要 reload/restart/update，生成 operation 并实现 RunOperation。
9. tests：fake runner 单元测试、graph golden、plan golden、必要时 libvirt。

## 新增 component 能力的步骤

component 能力横跨 parser、merge、graph 和 provider。检查：

- component 模板语法。
- input 类型、默认值、validation、sensitive。
- artifact source/extract/build/install 字段。
- facts 依赖，例如 architecture。
- component instance address prefix。
- source build 或 binary install 的 provider 行为。
- redaction，尤其是 sensitive component input。

组件很容易影响地址和 state，新增字段时要特别关注 golden diff。

## 测试选择建议

- 纯语法错误：parser test。
- 类型归一化或默认值：merge test + hostspec golden。
- 资源展开或依赖：graph golden + schedule test。
- plan 展示：plan golden。
- action 判断：engine/provider unit test。
- 远端 shell 命令：provider fake runner test。
- 真实系统效果：libvirt integration。
- secret 风险：redaction matrix。

## 完成定义

一个能力不能只做到“plan 看起来对”。完成至少意味着：

- validate 能发现错误配置。
- offline plan 能给出合理资源形状，或者明确说明为什么需要 runtime facts。
- online plan 能正确区分 create/update/no-op/drift/adopt。
- apply 能幂等执行。
- check 能检测漂移。
- state 不泄漏敏感内容。
- docs 和 tests 覆盖用户可见行为与维护者边界。

## 修改检查清单

- 是否改了地址、digest、JSON 格式或 state schema。
- 是否影响 sensitive/ephemeral/content_write_only。
- 是否需要新增 operation 或依赖。
- 是否补了 host filter、多 host 或并发场景。
- 是否有发行版相关真实行为需要对应目标的 libvirt 验证。
- 是否更新了支持矩阵、CLI 文档或本系列教程。
