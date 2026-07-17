<p align="right">
  <a href="beta-feedback-triage.md">English</a> | <strong>简体中文</strong>
</p>

# DebianForm Beta Feedback 和 Triage 流程

本文档定义 public beta 阶段的用户反馈入口、issue triage 流程、优先级和关闭条件。
安全漏洞不走公开 issue；请按 [SECURITY.zh-CN.md](../SECURITY.zh-CN.md) 使用 GitHub Security Advisories。

## 反馈入口

公开 beta 反馈使用 GitHub Issues：

- Beta 体验、文档问题、采用阻塞点：使用 `Beta feedback` issue template。
- 可复现 bug：使用 `Bug report` issue template。
- 安全漏洞：使用 GitHub Security Advisories，不要开公开 issue。

反馈中不要包含：

- SSH private key、API token、password、WireGuard private key。
- 未脱敏的 private hostname、公网 IP、客户名或内部路径。
- 未检查过的完整 state、plan、shell history 或 CI log。

推荐先附带：

- `dbf version` 输出。
- 控制机 OS/arch。
- 目标主机 distribution、version、architecture 和 codename。
- 安装方式：Homebrew、curl installer、source build 或本地开发构建。
- 执行的命令：`validate`、`plan`、`apply`、`check`、`fmt`、inspect。
- 最小可复现 `.dbf.hcl` 片段，移除 secrets。
- 期望结果、实际结果和完整错误文本。

## Issue 标签

| Label | 含义 |
| --- | --- |
| `needs-triage` | 新 issue 默认状态，尚未确认范围和优先级。 |
| `beta-feedback` | beta 使用体验、采用阻塞点或非 bug 反馈。 |
| `bug` | 可复现的行为缺陷。 |
| `docs` | 文档缺口、错误或示例问题。 |
| `release` | 安装、升级、发布 artifact、Homebrew、curl installer。 |
| `integration` | Debian 目标主机、libvirt case、provider apply/check 行为。 |
| `security` | 非漏洞但和安全模型、secret 处理、权限边界有关；漏洞仍走 advisory。 |
| `priority/p0` | 阻塞 beta 主路径或可能导致数据破坏、安全泄漏、错误 apply。 |
| `priority/p1` | 影响常见 beta 用户，但有明确绕过方式。 |
| `priority/p2` | 文档、体验、边界说明或后续优化。 |
| `needs-info` | 需要报告者补充复现信息。 |
| `accepted` | 已确认需要处理。 |
| `known-issue` | 已确认限制或缺陷，会写入 release notes 或 support matrix。 |

## Triage 步骤

1. 确认是否涉及安全漏洞。若是，回复让报告者转到 GitHub Security Advisories，并关闭公开 issue。
2. 检查 issue 是否包含版本、环境、命令、配置片段和错误输出。
3. 如果缺少复现信息，添加 `needs-info`，列出需要补充的最小信息。
4. 判断类型：bug、beta feedback、docs、release、integration 或 security-boundary。
5. 判断优先级：`priority/p0`、`priority/p1`、`priority/p2`。
6. 对可复现 bug，尝试转成最小 fixture、unit test 或 libvirt integration case。
7. 如果属于已知限制，把 issue 标为 `known-issue`，并确认是否需要更新 support matrix、
   release notes template、operations runbook 或 README。
8. 接受处理时添加 `accepted`，并在 issue 中写清楚下一步。

## 优先级标准

`priority/p0`：

- 可能泄露 secret 到 plan、state、log、error、debug output 或 shell command preview。
- 可能对未声明资源执行 destructive apply。
- state lock、state 写入或 check drift 行为破坏主路径。
- 安装产物不可用，或 release artifact 校验失败。
- Quickstart 主路径在受支持 Debian 13 amd64 目标上不可执行。

`priority/p1`：

- 常见 DSL 使用场景失败，但存在可接受绕过方式。
- Docker/Compose、APT、systemd、nftables 等 beta 主路径出现可复现问题。
- 文档示例和实际 CLI 行为不一致。
- 错误信息缺少足够上下文，导致用户难以恢复。

`priority/p2`：

- 非阻塞文档补充。
- 设计建议、语法糖、长期 stable 策略讨论。
- 需要真实环境或多个 release 才能验证的趋势类问题。

## 响应和关闭

- `priority/p0`：优先确认影响范围，并尽快给出 workaround、回滚建议或修复计划。
- `priority/p1`：确认复现路径，安排到最近可执行 loop。
- `priority/p2`：收敛为文档、后续计划或设计讨论。

关闭 issue 前至少满足一个条件：

- 已合并修复，并说明验证命令。
- 已更新文档或 support matrix，并说明当前边界。
- 因缺少信息添加 `needs-info` 后长期无回复。
- 确认不是 DebianForm 问题，并给出原因。
- 安全问题已转入 private advisory。

## 进入 Known Issues

以下情况需要同步到 release notes 或 support matrix：

- 用户可能在正常 beta 使用中遇到的限制。
- 无法在本 release 修复但有明确影响面的 bug。
- 平台验证为 `manual/best-effort`。
- 与 state、plan JSON、secret redaction、root-only SSH 或 release artifact 相关的边界。

每次发布前，检查所有 `known-issue` issue，确认 release notes 的 `Known Issues` 部分同步。
