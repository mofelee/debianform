# 项目成熟度与上线清单

本文档用于定期评估 DebianForm 的项目成熟度，并把上线前、公开 beta 后、stable 前需要完成的事项整理成可维护的 checklist。

维护方式：每次检查时直接把 `- [ ]` 改成 `- [x]`，并在「检查记录」中追加本次验证命令和结论。

## 本次检查摘要

- 检查日期：2026-06-23。
- 当前建议成熟度：**4/5：public beta 可上线候选**。
- 推荐发布定位：**`v0.1.0-beta` 或 public preview**。
- 不建议发布定位：stable、GA、production-ready。

本次本地验收：

- [x] `go vet ./...`
- [x] `go test ./...`
- [x] `go test -race -count=1 ./...`
- [x] `make vulncheck`
- [x] `make build`
- [x] `make test-integration-layout`
- [x] GitHub Actions CI 等价完整 Debian 13 libvirt VM 矩阵：
  `28015644419`，覆盖 9 个 case。
- [x] 正式发布 tag `v0.1.0-beta.2` 对应 commit 的 GitHub Actions 全绿。

当前判断：

- [x] 核心 v2 闭环已经完整：validate、plan、apply、check、state、lock、observed drift 和 SSH 执行路径都存在。
- [x] 发布工程化已经基本完成：GoReleaser、GitHub Release、checksum、curl installer、Homebrew tap 自动更新、post-release verification、cosign keyless、SBOM 和 provenance 都已经有仓库配置或文档记录。
- [x] README 已明确 beta/public preview、支持范围、安装、升级、校验和示例边界。
- [x] 正式 public beta tag `v0.1.0-beta.2` 已完成端到端发布确认。
- [ ] 还缺真实低风险 Debian 13 主机上的小范围 beta 使用反馈。
- [ ] 还缺 stable 所需的兼容性、迁移和长期支持承诺。

## 发布定位检查

- [x] README 顶部明确项目处于 public preview / beta 阶段。
- [x] README 明确 v2 是当前唯一主线。
- [x] README 明确旧实验格式已经废弃。
- [x] README 明确目标系统最高优先级是 Debian 13。
- [x] README 明确被管理目标主机优先支持架构是 amd64。
- [x] README 明确 `dbf` CLI 发布产物覆盖 Linux/macOS 的 amd64 和 arm64。
- [x] README 区分可运行示例和 design-only fixture。
- [x] README 提醒真实 apply 前需要先运行 plan，并需要 SSH 可达的受支持 Debian 主机。
- [x] release notes 为正式 public beta tag 明确写出 beta 风险、兼容性限制和迁移影响。
- [ ] stable/GA 文案在 README、release notes、安装文档中均未被误用。

## P0：公开 beta 上线前必须完成

### 支持范围

- [x] 支持的 Debian 版本已在 README 中说明。
- [x] 目标主机架构优先级已在 README 中说明。
- [x] CLI 运行平台和目标主机平台已在 README 与 [release process](release-process.zh.md) 中区分。
- [x] v2 可运行示例已在 README 中列出。
- [x] `examples/v2-fleet.dbf.hcl` 已标记为 design-only fixture。
- [x] 可运行示例有 validate 测试覆盖。
- [x] 为正式 public beta release notes 增加一份简短的「已支持 / 暂不支持」矩阵。

### CI 和测试闸门

- [x] CI 包含 gofmt 检查。
- [x] CI 包含 `go vet ./...`。
- [x] CI 包含 `go test -race -count=1 ./...`。
- [x] CI 包含 `make build`。
- [x] CI 包含 `make test-integration-layout`。
- [x] CI 动态发现 libvirt integration cases。
- [x] CI 包含 Debian 13 libvirt integration job。
- [x] libvirt cases 覆盖 `apt-source`、`bbr`、`component-inputs`、`files`、`nftables`、`shadowsocks-rust`、`source-build`、`systemd-service-unit` 和 `wireguard`。
- [x] `wireguard` case 覆盖双 host runner。
- [x] 正式发布前确认 release tag 对应 commit 的 CI 全绿。
- [x] 正式发布前至少完成一次完整 `make test-integration` 或等价 CI libvirt 矩阵。

### 发布产物和安装

- [x] `LICENSE` 存在。
- [x] `CHANGELOG.md` 存在。
- [x] `SECURITY.md` 存在。
- [x] [release process](release-process.zh.md) 存在。
- [x] [release quick runbook](release-quick-runbook.zh.md) 存在。
- [x] `.goreleaser.yaml` 覆盖 Linux/macOS amd64/arm64。
- [x] release tarball 包含 `dbf`、README、docs、examples、LICENSE 和 CHANGELOG。
- [x] release workflow 会生成 `checksums.txt`。
- [x] release workflow 会创建 GitHub Release。
- [x] release workflow 会生成 `checksums.txt.sigstore.json`。
- [x] release workflow 会生成 SBOM。
- [x] release workflow 会生成 GitHub provenance attestation。
- [x] `scripts/install.sh` 支持 latest 和指定版本安装。
- [x] `scripts/install.sh` 校验 tarball SHA256。
- [x] `scripts/install.sh` 支持 Linux/macOS amd64/arm64 检测或覆盖。
- [x] `scripts/install.sh` 支持 `--prefix`、`--bin-dir`、`--dry-run` 和 `--force`。
- [x] Homebrew tap 自动更新流程已在 release workflow 中接入。
- [x] release dry-run workflow 可验证 GoReleaser snapshot artifact。
- [x] post-release verification 覆盖 Linux amd64 curl installer。
- [x] post-release verification 覆盖 macOS amd64 和 macOS arm64 curl installer。
- [x] post-release verification 覆盖 macOS Homebrew install/test/upgrade。
- [x] Linux arm64 artifact build 已覆盖。
- [ ] Linux arm64 curl installer 在真实 arm64 runner 或机器上验证。
- [ ] Linux Homebrew install/test/upgrade 在有 Homebrew 的 Linux runner 或机器上验证。
- [x] 正式 public beta tag `v0.1.0-beta.2` 创建并通过 release workflow。
- [x] 正式 GitHub Release assets 和 verification matrix 人工抽查通过。
- [x] 正式 Homebrew formula 指向 public beta tag，而不是测试 tag。
- [x] `CHANGELOG.md` 为正式 public beta tag 写入真实变更，而不是占位说明。

### 安全和信任

- [x] `SECURITY.md` 指向 GitHub Security Advisories。
- [x] README 说明 checksum 校验。
- [x] README 说明 cosign keyless bundle 校验。
- [x] README 说明 GitHub provenance attestation 校验。
- [x] [v2 state](v2-state.md) 说明 state 保存哪些字段。
- [x] [v2 state](v2-state.md) 说明 secret content、sensitive input、SSH 私钥、命令日志和 lock lease 不写入 state。
- [x] [v2 plan format](v2-plan-format.md) 说明 sensitive diff 不输出明文。
- [x] README 说明远程 URL artifact 必须声明 64 位 sha256。
- [x] WireGuard integration checks 覆盖 private key 不写入 state 明文。
- [x] 增加 `govulncheck` 或等价依赖漏洞扫描。
- [x] 增加 Dependabot/Renovate 或等价依赖更新策略。
- [x] README 说明 root-only SSH 执行模型，不支持 sudo/become/非 root 管理连接。
- [x] [CLI 文档](cli.zh.md) 说明 root-only SSH 执行模型。
- [x] [v2 requirements](v2-requirements.md) 说明 root-only 权限边界。
- [x] 增加覆盖 text/json/html plan、stdout/stderr、state 的集中式 secret redaction 回归矩阵。

### 用户文档

- [x] README 包含 Homebrew 安装方式。
- [x] README 包含 curl 安装方式。
- [x] README 包含升级和回滚说明。
- [x] README 包含 `dbf version` 安装验证。
- [x] README 包含 validate、offline plan、json plan、html plan、apply、check 的基础示例。
- [x] [CLI 文档](cli.zh.md) 说明 `validate`、`plan`、`apply`、`check`、`fmt`、`variable inspect`、`component inspect` 和 version。
- [x] [CLI 文档](cli.zh.md) 说明 `--host`、`--parallel` 和 `--lock-timeout`。
- [x] [v2 state](v2-state.md) 说明 state path、lock path、ownership、lock 和 atomic write。
- [x] [release quick runbook](release-quick-runbook.zh.md) 说明发布前、发布中、发布后和回滚流程。
- [x] 新增独立 quickstart，覆盖准备 SSH 用户、写第一份配置、validate、在线 plan、apply、check。
- [x] 新增 operations/runbook，覆盖 stale lock、apply 中途失败、state 与远端不一致、资源移除和恢复步骤。
- [x] 新增常见故障排查文档，使用真实错误信息和修复步骤。
- [x] 新增简明支持矩阵，把 DSL block、resource/domain 类型和当前稳定性放在一处。

### beta 验证

- [x] libvirt integration tests 使用 Debian 13 cloud VM。
- [x] libvirt integration tests 执行真实 validate、apply 和 check。
- [x] integration cases 包含 drift 检查脚本。
- [x] integration cases 包含删除、forget 或 restore 行为验证。
- [x] release automation plan 记录过 test tag 的端到端 release workflow 验证。
- [x] 正式 beta tag 发布后，在干净环境验证 `dbf version`、`dbf validate`、`dbf plan --offline`。
- [ ] 正式 beta tag 发布后，在至少一台低风险 Debian 13 主机上验证在线 `plan`、`apply`、再次 `plan` no-op 和 `check`。
- [ ] 正式 beta tag 发布后，人工制造 drift 并确认 `check` 非零退出且输出可理解。
- [ ] 正式 beta tag 发布后，验证失败 apply 不记录未成功资源。
- [ ] 正式 beta tag 发布后，验证 plan、log、state 不泄露 secret 明文。
- [ ] 收集至少一轮真实使用反馈，并把阻塞问题修复或记录到 known issues。

## P1：公开 beta 后尽快补齐

- [ ] `.deb` 包。
- [ ] apt repository 可行性评估或计划。
- [ ] Linux arm64 安装路径自动验证。
- [ ] Linux Homebrew 路径自动验证或明确 best-effort 策略。
- [x] 更完整的 operations/runbook 文档。
- [x] 更完整的 quickstart 文档。
- [x] 依赖漏洞扫描进入 CI。
- [x] release notes 模板，固定包含 breaking changes、known issues、verification matrix 和 migration notes。
- [x] beta 用户反馈入口和 triage 流程。
- [ ] 真实部署模板或小型案例。
- [x] 对高风险资源的 `prevent_destroy` 使用建议。

## P2：stable/GA 前必须完成

- [ ] 连续多个 release 没有破坏性 DSL/state/plan JSON 变更。
- [ ] 多个真实用户或多组真实主机稳定使用。
- [ ] CI 和 libvirt integration tests 长期稳定，flaky 情况有记录和处理。
- [ ] 明确 backward compatibility policy。
- [ ] 明确 state schema migration policy。
- [ ] 明确 plan JSON format compatibility policy。
- [ ] release、安装、升级、回滚路径经过多个正式 release 验证。
- [ ] 安全文档包含 root-only SSH 执行模型、权限边界、secret 处理和漏洞响应流程。
- [x] 常见失败场景都有可执行恢复步骤。
- [ ] 更广泛的 Debian 版本或架构支持策略。
- [x] 常见故障排查覆盖 root SSH 不可用、权限不足和目标系统不受支持。
- [ ] README 中承诺的能力都能被测试、示例或文档覆盖。

## 详细成熟度检查

### 核心产品

- [x] v2 CLI 主路径：`validate`、`fmt`、`plan`、`apply`、`check`。
- [x] 辅助 CLI：`version`、`component inspect`、`variable inspect`。
- [x] parser 支持 v2 顶层结构和领域 block。
- [x] profile/host merge 已实现。
- [x] component input、validation、deprecated warning 和 sensitive metadata 已实现。
- [x] variable、var-file、auto var file、env var 和 CLI var 已实现。
- [x] HostSpec、ResourceGraph、plan 和 state 路径已实现。
- [x] 在线 plan 支持 SSH、runtime facts、observed state 和 drift 对比。
- [x] 离线 plan 支持本地预览。
- [x] apply 支持远端 state lock 和 state 持久化。
- [x] check 通过在线 plan 检测 drift，并在有变更时返回非零。
- [x] DAG 调度和多 host apply 并发控制已实现。
- [x] plan 支持 text、JSON 和静态 HTML。
- [x] domain 覆盖 kernel/sysctl、files/secrets/directories、users/groups、systemd/services、APT、nftables、component binary/archive/file/ca_certificate/source build。
- [ ] stable 级别的兼容性和迁移策略尚未完成。

### 测试覆盖

- [x] parser 单测。
- [x] merge 单测。
- [x] graph/schedule 单测。
- [x] plan/diff 单测。
- [x] state 单测。
- [x] engine 单测。
- [x] CLI 单测。
- [x] version 单测。
- [x] source build integration Go test。
- [x] golden test 覆盖 parser、HostSpec、graph 和 plan。
- [x] runnable v2 examples validate 测试。
- [x] libvirt case layout validate。
- [x] Debian 13 libvirt VM integration 设计和 CI job 已存在。
- [ ] 本次检查未重新运行完整 Debian 13 libvirt VM integration 矩阵。
- [ ] 还缺长期 flaky 记录和趋势观察。

### 发布成熟度

- [x] 本地 build 注入 version、commit、date。
- [x] GoReleaser 多平台构建配置。
- [x] release dry-run workflow。
- [x] tag-triggered release workflow。
- [x] curl installer。
- [x] Homebrew tap 自动更新脚本和 workflow step。
- [x] post-release verification summary 写入 release notes。
- [x] checksum、cosign keyless、SBOM、provenance。
- [x] 正式 public beta release 已按 runbook 执行并确认。
- [ ] `.deb` 和 apt repository 尚未实现。

### 文档成熟度

- [x] README 覆盖项目定位、安装、升级、校验、示例、基础命令和集成测试入口。
- [x] [CLI 文档](cli.zh.md) 覆盖主要命令和参数。
- [x] [v2 requirements](v2-requirements.md) 和相关设计文档存在。
- [x] [v2 state](v2-state.md) 文档存在。
- [x] [v2 plan format](v2-plan-format.md) 文档存在。
- [x] [support matrix](support-matrix.zh.md) 文档存在。
- [x] [beta feedback and triage](beta-feedback-triage.zh.md) 文档存在。
- [x] 发布流程、自动化计划和快速操作手册存在。
- [x] 面向新用户的一页 quickstart 已独立成文。
- [x] 面向运维恢复的 runbook 已覆盖 stale lock、失败 apply、drift、资源移除和常见错误恢复。
- [ ] 面向 stable 的 compatibility policy 和 migration policy 还未成文。

## 建议发布决策

可以发布 public beta 的条件：

- [x] 核心功能闭环已经具备。
- [x] 本地 Go 检查和构建通过。
- [x] release automation 已经具备端到端能力。
- [x] 正式 tag 前完整 CI 全绿。
- [x] 正式 tag 前完成或接受完整 libvirt 矩阵的验证结果。
- [x] release notes 明确 beta 风险和支持边界。

不应发布 stable/GA 的原因：

- [ ] 真实用户和真实主机验证不足。
- [ ] state/schema migration policy 尚未完成。
- [ ] backward compatibility policy 尚未完成。
- [ ] 还没有连续多个正式 release 的稳定记录。

已缓解的 stable 阻塞项：

- [x] operations recovery 文档已补齐 public beta 阶段需要的本地恢复流程。

## 检查记录

### 2026-06-23

本次检查命令：

- [x] `go vet ./...`
- [x] `go test ./...`
- [x] `go test -race -count=1 ./...`
- [x] `make vulncheck`
- [x] `go test ./cmd/dbf -run TestSecretRedactionRegressionMatrix`
- [x] `make build`
- [x] `make test-integration-layout`
- [x] GitHub Actions CI 等价完整 libvirt 矩阵：`28015644419`
- [x] GitHub Actions release dry-run：`28015644510`
- [x] GitHub Actions release workflow：`28015905534`
- [x] `cosign verify-blob ... checksums.txt`
- [x] `gh attestation verify dbf_v0.1.0-beta.2_linux_amd64.tar.gz --repo mofelee/debianform`
- [x] `scripts/install.sh --version v0.1.0-beta.2 --prefix /tmp/dbf-install-check-v0.1.0-beta.2 --os linux --arch amd64 --force`

结论：

- [x] 项目成熟度从「beta 初期」提升到「public beta 可上线候选」。
- [x] 本地代码质量、race 单测、build 和 integration layout 检查通过。
- [x] 已新增集中式 secret redaction 回归矩阵，覆盖 text/json/html plan、CLI
      stdout/stderr、HostSpec、ResourceGraph desired、state 和 native provider preview/error。
- [x] 已新增独立 quickstart，覆盖 root SSH 准备、第一份配置、validate、offline/online
      plan、apply、no-op plan 和 check。
- [x] 发布、安装和供应链自动化已从计划状态推进为仓库内可执行配置。
- [x] 正式 public beta `v0.1.0-beta.2` 已按 release runbook 创建 tag，并确认 CI、GitHub Release、Homebrew tap 和 clean install 验证全部通过。
- [ ] stable/GA 仍需要真实使用反馈、兼容性政策和 state migration 策略。

### 2026-06-23 operations runbook 补充

本次检查命令：

- [x] `dbf` CLI、v2 state 和 SSH lock 实现口径审阅。
- [x] `docs/operations-runbook.zh.md` 新增 stale lock、apply 中途失败、drift、资源移除、
      `prevent_destroy` 和常见故障恢复步骤。
- [x] `docs/operations-runbook.zh.md` 覆盖 root SSH 不可用、权限不足和目标 facts 探测失败。

结论：

- [x] 运维恢复文档现在覆盖 public beta 阶段常见失败场景，并从 README、CLI 文档和
      quickstart 提供入口。

### 2026-06-23 支持矩阵补充

本次检查命令：

- [x] 核对 README、release process、parser allowed attrs、IR types 和 Docker graph 实现。
- [x] 新增 `docs/support-matrix.zh.md`，覆盖 CLI 平台、目标主机、CLI 命令、v2 顶层
      DSL、host domain、resource/provider 类型、Docker DSL、component/variable 和示例验证。
- [x] 同步补齐 CLI 文档中的 `variable inspect` 命令入口。

结论：

- [x] 支持矩阵现在把 DSL block、resource/domain 类型和当前 beta/preview/compat/design-only
      状态集中到一处，并从 README 与 release process 提供入口。

### 2026-06-23 release notes 模板补充

本次检查命令：

- [x] 核对 `CHANGELOG.md`、release process、release quick runbook 和 release workflow 的
      Verification Matrix 追加逻辑。
- [x] 新增 `docs/release-notes-template.md`，固定包含 Summary、Compatibility、
      Breaking Changes、Migration Notes、Known Issues、Support Matrix、Verification 和
      Verification Matrix。
- [x] release process、release quick runbook 和 README 均提供 release notes template 入口。

结论：

- [x] 发布前现在有固定 release notes 模板，避免遗漏 breaking changes、known issues、
      verification matrix 和 migration notes。

### 2026-06-23 beta feedback triage 补充

本次检查命令：

- [x] 核对 `.github`、README、SECURITY.md、support matrix 和现有反馈/issue 入口。
- [x] 新增 GitHub Issue Forms：`Beta feedback` 和 `Bug report`，默认添加
      `needs-triage`。
- [x] 新增 `.github/ISSUE_TEMPLATE/config.yml`，把安全漏洞引导到 GitHub Security
      Advisories。
- [x] 新增 `docs/beta-feedback-triage.zh.md`，定义反馈入口、标签、优先级、triage
      步骤、关闭条件和 known issues 同步规则。

结论：

- [x] public beta 现在有明确反馈入口和 triage 流程；真实反馈收集本身仍需要后续外部使用。
