# DebianForm 安全模型

本文档说明 DebianForm public beta 阶段的安全边界、secret 处理和漏洞响应流程。它不是
生产加固清单；它用于明确当前工具会做什么、不会做什么，以及用户在低风险主机上试用前需要
自行控制的风险。

## 执行模型

DebianForm 是一个通过 SSH 管理 Debian 主机的配置工具。当前唯一支持的管理连接模型是
root SSH：

- `ssh.user` 只能省略或设置为 `"root"`。
- 省略 `ssh.user` 时仍按 root 连接。
- 不支持 sudo、become、sudoers 管理或非 root 管理连接。
- `plan`、`apply` 和 `check` 在线模式都会通过 SSH 读取目标主机事实、state 和 observed
  状态。

选择 root-only 的原因是当前资源会写入 `/etc`、`/usr/local`、systemd、APT、nftables、
内核参数和 `/var/lib/debianform` state。用非 root 管理同一范围会引入大量提权和排障
分支，容易让主路径不可靠。

## 权限边界

DebianForm 管理连接拥有目标主机 root 权限，因此安全假设是：

- 只对低风险测试主机或已评估的受控主机运行 `apply`。
- 每次真实 `apply` 前先运行 online `plan`，并确认变更范围。
- 控制机、CI runner、SSH key 和配置仓库都属于同一个信任边界。
- 不把未审阅的第三方 `.dbf.hcl` 配置直接 apply 到真实主机。
- 不在同一个 state path 上并发运行多个 `apply`；state lock 用于防止同 host 并发写入。

服务本身可以低权限运行。例如 `systemd.service_unit.user/group` 可以让目标服务使用
非 root 用户运行；这只影响服务进程权限，不改变 DebianForm 管理连接必须是 root 的边界。

当前不承诺：

- 细粒度最小权限管理连接。
- sudo/become 支持。
- 多租户控制平面隔离。
- 对恶意配置文件的沙箱执行。
- 对目标机已被入侵时的完整取证或修复。

## Secret 处理

DebianForm 的目标是避免把 secret 明文写入 plan、state、普通日志和 release/debug 产物。
当前相关语义如下：

- `files.file sensitive = true`：普通文件资源按敏感内容处理。
- `secrets.file`：兼容层，等价于敏感文件部署语义；新配置优先使用
  `variable + files.file sensitive = true`。
- sensitive variable 或 component input 派生出的 file/unit content 会继承敏感标记。
- plan text、plan JSON、HTML plan、state 和 HostSpec/ResourceGraph debug 输出只应保存
  hash、bytes、changed 等摘要，不输出明文。
- ephemeral variable 不应写入 HostSpec、ResourceGraph、plan、state、cache、golden fixture
  或普通日志。
- write-only 值只能进入 provider apply 通道，不应进入 desired/state/diff。

用户仍需要注意：

- `sensitive` 不表示目标主机不落盘。若服务需要文件，secret 最终仍会写到目标主机文件系统。
- state 中的 sha256/bytes 摘要可用于 drift/no-op 判断，但对低熵 secret 可能形成可猜测指纹。
- shell history、CI log、外部命令输出、用户自定义脚本和第三方服务日志不由 DebianForm 完全控制。
- 不要把真实 secret、SSH private key、WireGuard private key、token 或 `.env` 提交到仓库。
- 公开 issue、反馈和日志粘贴前必须自行脱敏。

## State 和 Lock

state 默认位于：

```text
/var/lib/debianform/state/<host>.json
```

lock 默认位于：

```text
/var/lock/debianform/state/<host>.lock
```

state 保存 resource ownership、脱敏 desired 摘要和 observed 摘要。它不应保存：

- secret content。
- sensitive component input 明文。
- 由 sensitive input 派生的 file/unit content 明文。
- SSH 私钥。
- 命令日志。
- lock lease token 之外的运行期细节。

state 使用原子写入；`apply` 每成功执行一个资源节点后立即写回 state。中途失败时，state
只应包含已经成功的节点。恢复流程见 [operations runbook](operations-runbook.zh.md)。

## 供应链和安装安全

每个公开 release 应包含 tarball、checksum、cosign keyless bundle、SBOM 和 GitHub
provenance attestation。安装或升级前建议：

- 从 GitHub Release、Homebrew tap 或官方 install script 获取产物。
- 校验 `checksums.txt`。
- 校验 cosign keyless bundle。
- 校验 GitHub provenance attestation。
- 先在低风险目标上运行 `validate`、online `plan` 和 `check`。

`.deb` 包和 apt repository 当前尚未发布；相关渠道出现前不应视为官方安装路径。

## 漏洞响应

安全漏洞不要开公开 issue。请使用 GitHub Security Advisories：

```text
https://github.com/mofelee/debianform/security/advisories/new
```

建议报告包含：

- 受影响版本和 commit。
- 控制机 OS/arch。
- 目标主机 Debian 版本和架构。
- 最小复现配置，移除 secret。
- 影响范围：secret 泄露、错误 destructive apply、权限边界绕过、release artifact 校验失败等。
- 已知 workaround 或是否已经公开。

维护响应流程：

1. 在 advisory thread 中确认收到报告。
2. 判断影响范围和优先级，避免在公开 issue 中暴露细节。
3. 准备修复、回归测试和 release notes。
4. 对已发布版本，优先发布新的修复 tag；不复用已有 tag。
5. 必要时更新 support matrix、known issues、operations runbook 或 compatibility policy。

security-relevant P0 包括：

- secret 出现在 plan、state、log、error、debug output 或 shell command preview。
- 对未声明资源执行 destructive apply。
- state lock 或 state 写入导致错误 ownership 或错误 no-op。
- release artifact checksum、signature、attestation 或 installer 路径被破坏。

## 反馈边界

普通 bug 和 beta 体验反馈走 GitHub Issues；安全漏洞走 advisory。公开反馈中不要包含：

- SSH private key、API token、password、WireGuard private key。
- 未脱敏的 private hostname、公网 IP、客户名或内部路径。
- 未检查过的完整 state、plan、shell history 或 CI log。

公开反馈流程见 [beta feedback and triage](beta-feedback-triage.zh.md)。
