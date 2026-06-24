# DebianForm v2 Runtime Facts Plan

本文档记录 `system.architecture`、`system.codename` 这类 runtime facts 的实现计划，
并明确区分 desired hostname 和 observed hostname。

## 目标

- 用户在真实部署配置中通常不需要声明 `system.architecture` 或 `system.codename`。
- `system.hostname` 是期望状态；用户在配置中声明它表示要设置远端 hostname。
- 在线 `plan`、`apply` 和 `check` 从目标主机探测 `architecture` 和 `codename`，并在
  component 实例化前注入 `target.system`。
- 在线 facts discovery 可以记录当前远端 hostname，但它属于 `facts.system.hostname` 观测值，
  不能覆盖配置中的 `system.hostname`。
- 用户显式声明 `architecture` 或 `codename` 时，它们只作为断言使用：探测值不一致必须报错。
- `dbf validate` 可以在不连接目标主机的情况下检查配置和 component 模板结构。
- 需要 runtime facts 的 `plan --offline` 继续保守失败，直到引入明确的 facts cache 机制。

## 非目标

- 不在编译器中填入默认架构或默认 codename。
- 不用运行 DebianForm 的本机 facts 代替目标主机 facts。
- 不在 validate 阶段伪造 `target.system.architecture` 或 `target.system.codename` 来完成真实
  source 选择。
- 不把 observed hostname 当成 desired hostname，也不把 desired hostname 当成 runtime fact。
- 第一阶段不实现本地 facts cache；cache 需要独立设计刷新、过期和一致性校验。

## 设计原则

- Runtime facts 的可信来源只能是目标主机，或者用户显式声明的 `architecture`/`codename`
  断言。
- `system.hostname` 属于 desired system 配置；`facts.system.hostname` 属于探测到的当前状态。
  两者必须在文档、IR、state 和 plan 展示中保持命名空间区分。
- 未知 facts 可以让 validate 通过结构检查，但不能驱动会改变语义的选择。
- 多架构 component 在 validate 阶段应检查所有 `source` 分支，而不是挑一个假定架构。
- 在线命令必须在探测 facts 后重新完整编译配置，确保 `target.system.architecture`、
  `target.system.codename`、artifact source、APT suite 等字段都来自真实目标。
- state 中保存的 facts 是上次成功在线运行时的观测结果，不应被当作新的默认事实悄悄使用。

## 阶段 1: 明确当前语义

- 更新用户文档，说明 `architecture` 和 `codename` 是 runtime facts。
- 更新用户文档，说明 `system.hostname` 是 desired hostname，默认等于 host label，声明后表示
  要设置远端 hostname。
- 说明显式声明 `architecture` 和 `codename` 的含义是断言，而不是必填项。
- 保留在线 facts discovery 的现有流程：先编译基础 host 信息，再探测 facts，最后完整编译。
- 增加测试覆盖显式声明与探测值不一致时的报错。

验收标准：

- 不声明 `system.architecture` 和 `system.codename` 的真实 host 可以通过在线 `plan`、`apply` 和
  `check` 完成 component source 选择。
- 显式声明 `architecture` 或 `codename` 与远端探测值不一致时，错误信息指出声明值和探测值。
- `system.hostname` 不参与 facts mismatch 校验；设置 hostname 的 converge 和 drift 检查必须由
  独立的系统配置资源实现，不能混入 runtime facts 校验。

## 阶段 2: 改善 validate

- 为 validate 增加 runtime-aware 校验路径。
- validate 不连接 SSH，也不要求 runtime facts。
- validate 检查 host、profile、component input、component body、artifact、service、systemd、APT
  等结构约束。
- 对 component artifact：
  - 无 label 的 `source` 仍按架构无关处理。
  - 多个架构 label 的 `source` 全部检查 URL、sha256、extract 和 install 规则。
  - 混用无 label source 和架构 label source 仍然报错。
- 对引用 `target.system.architecture` 或 `target.system.codename` 的表达式，validate 只验证表达式
  可被识别为 runtime-dependent，不把未知值替换成固定字符串。

验收标准：

- 不声明 runtime facts 的多架构 binary component 可以通过 `dbf validate`。
- 任意架构分支里的非法 sha256、非法 extract format 或缺失 install 仍能被 validate 发现。
- 使用 `target.system.codename` 的 APT suite 在 validate 阶段不会要求用户手写 codename。
- `plan --offline` 遇到需要 runtime facts 的配置仍给出清晰错误。

## 阶段 3: 清理示例和集成测试

- 真实部署示例移除不必要的 `system.architecture` 和 `system.codename`。
- 需要设置远端 hostname 的示例保留 `system.hostname`，并说明它是 desired state。
- 只在纯离线 golden fixture 或特意测试断言语义的 fixture 中保留显式 runtime facts。
- 更新 libvirt 集成案例，让在线 apply/check 通过 VM 探测 facts 来选择架构相关 source。
- 保留至少一个 mismatch 测试，证明显式 facts 仍然是断言。

验收标准：

- shadowsocks-rust 这类多架构二进制示例不需要手写 `system.architecture`。
- libvirt 集成测试在 Debian 13 VM 上自动探测 `amd64` 和 `trixie`，并选择对应 release asset。
- layout-only 校验不再依赖为每个案例手写假 facts。

## 阶段 4: 评估 facts cache

只有在完整离线 plan 成为明确需求后，再设计 facts cache。cache 设计必须至少回答：

- cache 文件存放位置。
- 如何按 host 区分和刷新 facts。
- `detected_at` 和过期策略。
- 在线运行时 cache 与远端探测不一致如何处理。
- 用户如何查看、刷新和删除 cache。

在 cache 设计完成前，`plan --offline` 不应从 state 或本地环境中隐式借用 facts。

## 风险

- 如果 validate 用假 facts 完成 source 选择，可能掩盖其他架构分支的错误。
- 如果离线命令使用本机 facts，交叉部署时会选择错误 artifact 或 APT suite。
- 如果 state facts 被隐式当作当前事实，系统升级后可能继续使用旧 codename。
- 如果 `system.hostname` 和 `facts.system.hostname` 没有区分清楚，用户会误以为配置中的 hostname
  只是断言，而不是会修改远端 hostname 的 desired state。
- 如果显式声明 `architecture` 或 `codename` 既像配置又像 facts，用户会误以为 DebianForm 能
  修改架构或 release codename。

这些风险的共同处理原则是：未知就是未知；validate 可以接受未知，但不能把未知当成事实。
