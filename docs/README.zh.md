# DebianForm Docs

这里是 DebianForm 的文档入口。第一次使用从 Quickstart 开始；日常查命令看 CLI 手册；
排障看 Operations Runbook。

## 开始使用

- [Quickstart](quickstart.zh.md)：从安装到第一次 `apply/check` 的最短路径。
- [用户手册](user-manual/README.zh.md)：由浅入深的可运行教程，覆盖常见运维任务。
- [CLI 手册](cli.zh.md)：`validate`、`plan`、`apply`、`check`、`fmt` 和 inspect 命令。
- [DSL Reference](dsl-reference.zh.md)：`.dbf.hcl` 已实现指令、字段、默认值、限制和可测试示例。
- [真实部署模板](realistic-deployment-example.zh.md)：一个低权限 systemd app 的完整小例子。
- [README asciinema 演示录制指南](readme-asciinema-demo.zh.md)：重新生成 GitHub 首页终端演示。

## 日常查阅

- [支持矩阵](support-matrix.zh.md)：当前支持的平台、配置 block、resource 类型和示例状态。
- [安全模型](security-model.zh.md)：root SSH、secret 脱敏、state/lock 和漏洞响应边界。
- [兼容性政策](compatibility-policy.zh.md)：beta/stable 的 CLI、DSL、state 和 plan JSON 兼容规则。
- [系统如何工作](how-it-works/README.zh.md)：面向后续开发者的内部架构和实现链路系列教程。
- [Plan JSON 格式](plan-format.md)：`dbf plan --format json` 的结构化输出。
- [State 格式](state.md)：远端 state、lock、ownership 和脱敏规则。
- [systemd service units](systemd-service-units.md)：纯文本 unit 和结构化 `service_unit` 写法。
- [CLI 颜色输出和日志策略](cli-color-output-policy.zh.md)：终端颜色、日志颜色、CI 和 JSON 输出边界。
- [删除行为提示设计稿](delete-behavior-diagnostics-plan.zh.md)：plan/apply 删除提示、颜色和行为矩阵。

## 运维排障

- [Operations Runbook](operations-runbook.zh.md)：stale lock、apply 中断、drift、资源恢复和常见错误。
- [平台支持策略](platform-support-strategy.zh.md)：Debian 版本、架构和支持等级提升条件。

## 发布维护

- [Release 快速操作手册](release-quick-runbook.zh.md)：日常发布步骤。
- [Release 流程](release-process.zh.md)：发布产物、release gate、安装和升级流程。
- [Release notes 模板](release-notes-template.md)：GitHub Release 正文模板。
- [Release 自动化计划](release-automation-plan.zh.md)：发布 workflow 的实现记录。
- [Linux Homebrew 验证策略](linux-homebrew-verification-policy.zh.md)：Linux Homebrew best-effort 边界。
- [APT repository 可行性评估](apt-repository-feasibility.zh.md)：`.deb` 和 APT 仓库的后续路径。
- [Beta feedback triage](beta-feedback-triage.zh.md)：beta 反馈入口和 triage 规则。

## 归档设计稿

历史需求文档、实施计划和过期检查清单已移动到
[`archive/legacy-design/`](archive/legacy-design/)。这些文档只用于追溯设计背景，不作为当前用户入口
或能力承诺。
