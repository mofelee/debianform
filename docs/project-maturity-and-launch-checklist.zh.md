# 项目成熟度与上线清单

本文档用于评估 DebianForm 当前项目成熟度，并整理上线前需要补齐的事项。

## 当前成熟度

当前项目成熟度建议评为 **3/5：beta 初期**。

它已经不是原型项目。当前仓库已经具备：

- v2 CLI 主路径：`validate`、`fmt`、`plan`、`apply`、`check`。
- v2 DSL 的 parser、merge、HostSpec、ResourceGraph、plan、state 和 SSH 执行路径。
- state 文件、远端 lock、observed 检测、drift 检查和 apply 后状态持久化。
- DAG 调度、多 host apply 并发控制和失败传播。
- plan JSON、文本 renderer 和静态 HTML preview。
- golden 测试、race 单测、libvirt Debian 13 VM 集成测试设计。
- README、v2 设计文档、示例和常见错误说明。

这说明项目已经具备完整的核心闭环，适合自用或小范围真实机器试用。

但它还不适合直接作为 stable/GA 面向大规模用户发布。主要缺口不在核心功能是否存在，而在发布流程、支持边界、安全承诺、运维文档和真实用户验证。

## 建议发布定位

推荐先以 **`v0.1.0-beta`** 或 **public preview** 形式发布。

发布说明应明确：

- 目标系统是 Debian 13。
- 被管理目标主机优先支持架构是 amd64。
- `dbf` CLI 发布产物覆盖 Linux/macOS 的 amd64 和 arm64。
- 当前 v2 是唯一主线。
- 旧实验格式已经废弃。
- design-only fixture 不是可运行功能承诺。
- 生产机器使用前必须先运行 `plan`，并建议先在测试主机验证。

暂不建议使用 stable、GA、production-ready 等定位。

## 上线前必须完成

### 1. 明确支持范围

在 README 和 release notes 中写清：

- 支持的 Debian 版本。
- 支持的 CPU 架构。
- 支持的 DSL block 和 resource 类型。
- 不支持或暂不稳定的能力。
- 哪些示例是可运行样例，哪些只是设计 fixture。

最低要求：

- `examples/v2-fleet.dbf.hcl` 等 design-only 文件不能让用户误以为可以直接 apply。
- 所有公开承诺的示例都必须能通过 validate/golden 测试。

### 2. 让 CI 成为发布门槛

发布前 CI 必须稳定通过：

```bash
go vet ./...
go test -race -count=1 ./...
make build
make test-integration-layout
make test-integration
```

GitHub Actions 中已经有 unit 和 libvirt integration jobs。发布前需要确保这些 job 在干净环境中稳定绿。

特别注意：

- libvirt 集成测试必须覆盖 Debian 13 VM 的真实 `validate`、`apply`、`check`。
- CI failure 不能被文档变更、缓存问题或 flaky 网络掩盖。
- release tag 对应的 commit 必须来自全绿 CI。

### 3. 补齐发布与安装体系

当前已有 `make build` 和 `make install`，但公开发布还需要：

- `LICENSE`。
- `CHANGELOG.md`。
- 版本号策略，例如 semver。
- GitHub Release。
- Linux/macOS 的 amd64 和 arm64 release tarball。
- SHA256 checksums。
- `curl` 安装脚本。
- Homebrew tap。
- 基础安装和升级说明。

建议后续补充：

- `.deb` 包。
- apt repository。
- release artifact 签名。
- 自动化 release workflow。

最低可接受上线标准：

- 用户可以从 GitHub Release 下载 `dbf`。
- 用户可以校验 checksum。
- 用户可以用 Homebrew 或 `curl` 安装、升级、运行 `dbf version` 和 `dbf plan`。
- 具体发布流程见 [release process](release-process.zh.md)。
- 自动化落地计划见 [release automation plan](release-automation-plan.zh.md)。

### 4. 补齐安全与信任材料

DebianForm 会通过 SSH 修改目标主机上的系统文件、服务、apt、kernel、nftables 等配置，因此安全说明必须清楚。

上线前至少需要：

- `SECURITY.md`，说明漏洞报告方式。
- secret redaction 规则说明。
- state 文件中保存哪些数据的说明。
- plan/log/state 不应泄露 `secrets.file` 和 sensitive input 明文的测试说明。
- 远程 URL artifact 必须校验 sha256 的说明。

建议增加：

- 依赖漏洞扫描。
- release checksum/signature。
- 对 SSH 执行模型和远端权限要求的文档。

### 5. 补齐用户运维文档

当前 README 已经说明很多能力，但公开给用户使用还需要更偏操作手册的文档。

建议新增 quickstart：

- 如何安装。
- 如何准备 SSH 用户和权限。
- 如何写第一份 `.dbf.hcl`。
- 如何运行 `validate`。
- 如何运行在线 `plan`。
- 如何确认并运行 `apply`。
- 如何运行 `check` 检测 drift。

建议新增 operations 文档：

- state 文件默认位置。
- lock 文件默认位置。
- lock 超时和 stale lock 处理。
- apply 中途失败后如何判断哪些资源已成功。
- 如何从 state 和远端状态不一致中恢复。
- 如何限制 `--host` 和 `--parallel`。
- 如何安全地移除一个资源。
- `prevent_destroy` 的使用建议。

### 6. 做小范围 beta 验证

公开前建议先在真实但低风险的 Debian 13 主机上做小范围 beta。

建议覆盖：

- BBR/kernel/sysctl。
- files/secrets/directories。
- users/groups/authorized keys。
- systemd unit/service。
- apt repository/package。
- nftables。
- component binary/archive/file/ca_certificate。
- source build component。
- 多 host profile 复用。

验证目标：

- 初次 apply 成功。
- apply 后再次 plan 为 no-op。
- 人工 drift 后 check 能发现。
- 配置删除后 destroy/delete 行为符合预期。
- 失败后 state 不记录未成功资源。
- plan、log、state 不泄露 secret 明文。

## 建议优先级

### P0：没有这些不要公开

- CI 全绿并作为 release gate。
- `LICENSE`。
- `SECURITY.md`。
- `CHANGELOG.md`。
- README 顶部明确 beta 状态和支持范围。
- GitHub Release 四平台二进制和 checksum。
- `curl` 安装脚本。
- Homebrew tap binary formula。
- 至少一轮真实 Debian 13 beta 验证。

### P1：公开 beta 后尽快补

- `.deb` 包或 apt repository。
- 自动化 release workflow。
- 自动更新 Homebrew tap。
- operations/runbook 文档。
- 更完整的 quickstart。
- release artifact 签名。
- 依赖漏洞扫描。

### P2：stable 前补

- 更广泛的 Debian 版本/架构支持策略。
- backward compatibility policy。
- state schema migration 策略。
- 更完整的错误恢复手册。
- 用户案例和真实部署模板。
- 更细的权限模型和最小 sudo 权限建议。

## Stable/GA 判断标准

当满足以下条件后，可以考虑从 beta 推进到 stable：

- 至少连续多个 release 没有破坏性 state/schema 变更。
- 多个真实用户或多组真实主机稳定使用。
- CI 和 libvirt 集成测试长期稳定。
- release、安装、升级、回滚路径清楚。
- 安全文档和漏洞响应流程存在。
- 常见失败场景都有可操作恢复步骤。
- README 中承诺的能力都能被测试或示例覆盖。

## 当前结论

DebianForm 当前已经具备完整核心功能链路，适合进入公开 beta 或小范围 public preview。

上线策略应保守：先发布 beta，明确支持边界，收集真实使用反馈，再逐步补齐发布、安全、运维和兼容性体系。等这些工程化能力稳定后，再考虑 stable/GA。
