# DebianForm v2 实施计划

本文档把 v2 设计落成可执行的开发计划。每个 loop 都必须形成一个可合并闭环：

- [ ] 代码路径可运行
- [ ] 单元测试和 golden 测试覆盖本轮语义
- [ ] 至少一个示例进入验收输入
- [ ] 文档同步更新
- [ ] `make test` 通过

状态约定：

- [x] 已完成
- [ ] 未完成

## 当前基线

- [x] v2 用户需求文档已存在：`docs/v2-requirements.md`
- [x] v2 中间表达文档已存在：`docs/v2-ir-requirements.zh.md`
- [x] v2 plan JSON 格式文档已存在：`docs/v2-plan-format.md`
- [x] v2 设计示例已存在：`examples/v2-*.dbf.hcl`
- [ ] v2 编译器尚未完整实现；已支持 HostSpec、ResourceGraph、state、plan、apply 和 check
- [x] v2 CLI 已接入 `validate`、`plan`、`apply`、`check` 和 `fmt`
- [x] v2 plan/apply 已接入 observed 检测、远端 state 和 SSH 执行路径

## Loop 1a: 解析、合并与 HostSpec

目标：让 `examples/v2-bbr.dbf.hcl` 可以通过 v2 路径完成 `validate`，打通 parser、merge 和
HostSpec 这条前半截编译链，但还不生成 ResourceGraph 和 plan。

范围：

- `locals`
- `profile`
- `host`
- `imports`
- `ssh`
- `state`
- `system`
- `kernel`
- `packages`

暂不做：

- `apply`
- `component`
- `files`
- `secrets`
- `directories`
- `users`
- `groups`
- `systemd`
- `services`
- `apt`
- `nftables`
- ResourceGraph 和 plan（放到 Loop 1b）
- HTML preview

说明：

- Loop 1a 允许 `assert` block 出现在 host/profile 中以兼容现有 BBR 示例，但不会求值；
  assert 校验在 Loop 2 实现。

代码：

- [x] 新增 `internal/v2/parser`，解析 v2 顶层 block 和本轮领域 block
- [x] 保留 `SourceRef`，至少记录 file、line、path
- [x] 新增 `internal/v2/merge`，实现 profile imports 顺序合并
- [x] 实现 list append 去重
- [x] 实现 map 深度合并
- [x] 实现 scalar 后者覆盖
- [x] 实现 `force(value)`
- [x] 实现 `before(value)` 和 `after(value)`
- [x] 实现 `unset()` 删除 map key
- [x] 新增 `internal/v2/ir`，定义 `Program`、`HostSpec` 和本轮领域 spec
- [x] 为 `ssh`、`state`、`system.hostname` 填充默认值
- [x] 归一化 `kernel.modules`
- [x] 归一化 `kernel.sysctl`
- [x] 归一化 `packages.install`
- [x] 选定 snapshot/golden 测试框架，并提供 `make update-golden` 统一刷新机制
- [x] 支持 `dbf validate -f examples/v2-bbr.dbf.hcl` 走 v2 路径

测试：

- [x] parser 单测覆盖 `host`、`profile`、nested domain block、source line
- [x] parser 负例覆盖未知顶层 block、错误 label 数量、重复 host
- [x] merge 单测覆盖 imports 顺序、list 去重、map 覆盖、scalar 覆盖
- [x] merge modifier 单测覆盖 `force`、`before`、`after`、`unset`
- [x] merge 负例覆盖 profile import cycle
- [x] HostSpec snapshot 覆盖 `examples/v2-bbr.dbf.hcl`

示例：

- [x] `examples/v2-bbr.dbf.hcl` 作为本轮主 golden fixture
- [x] 增加一个 profile 合并小示例，覆盖 base packages + bbr profile + host override

文档：

- [x] 在 v2 docs 中注明本轮支持范围和暂不支持范围

验收：

- [x] `dbf validate -f examples/v2-bbr.dbf.hcl` 成功
- [x] profile 合并 fixture 的 HostSpec snapshot 稳定
- [x] `make test` 成功

## Loop 1b: ResourceGraph 与 BBR plan

目标：在 Loop 1a 的 HostSpec 基础上编译 ResourceGraph 并生成结构化 plan，让
`examples/v2-bbr.dbf.hcl` 可以走完 `plan`，不需要写任何 `低阶 provider 资源` 用户资源。

说明：本轮 plan 是 create-only 预览，所有资源都按“远端为空”假设输出 create，尚未读取
observed。真正的 desired/state/observed 三元比对在 Loop 4 接入，届时本轮的 plan golden 会
按新模型重写。

代码：

- [x] 新增 `internal/v2/graph`，从 HostSpec 编译 ResourceGraph
- [x] 生成稳定 v2 地址，例如 `host.bbr1.kernel.module["tcp_bbr"]`
- [x] 生成低阶 provider payload，但不暴露给普通用户 plan
- [x] 推导 BBR 依赖：`tcp_congestion_control = "bbr"` 依赖 `tcp_bbr` module
- [x] 新增 `internal/v2/plan`，生成结构化 plan 模型
- [x] 支持 `dbf plan -f examples/v2-bbr.dbf.hcl`
- [x] 支持 `dbf plan -f examples/v2-bbr.dbf.hcl --format json`

测试：

- [x] ResourceGraph snapshot 覆盖 BBR module/sysctl 地址和依赖边
- [x] plan JSON golden 覆盖 BBR create plan
- [x] CLI smoke 覆盖 validate、plan text、plan JSON

文档：

- [x] README 标记 BBR v2 路径已可 validate/plan
- [x] 增加 BBR plan 输出样例

验收：

- [x] `dbf plan -f examples/v2-bbr.dbf.hcl` 输出 v2 用户地址
- [x] `dbf plan -f examples/v2-bbr.dbf.hcl --format json` 输出 `debianform.plan.v2alpha1`
- [x] `make test` 成功

## Loop 2: 合并语义和错误定位收口

目标：让 profile/host 组合语义稳定，错误能指向用户 DSL，而不是内部 provider。

代码：

- [x] 完善 `SourceRef.Path`，覆盖 list、map、labeled object block
- [x] 让 merge 错误携带最终用户来源
- [x] 支持 host 覆盖 profile 中的 list、map、scalar
- [x] 支持 labeled object 归一化为 map 后字段级合并
- [x] 检测 host/profile 中禁止声明的字段，例如 profile 中的 `system.hostname`
- [x] 检测同一 host 内重复 resource identity
- [x] 检测同一 package 重复声明
- [x] 检测空 sysctl key 和空 kernel module
- [x] 检测 `unset()` 用在 list 上并报错
- [x] 解析 host 内重复 `assert` block，`condition` 为 HCL boolean 表达式
- [x] 在 profile merge 和默认值填充后、ResourceGraph 编译前求值 assert
- [x] `self` 指向合并后的 host 视图；assert 不能读取远端运行时状态
- [x] assert 失败时 validate/plan/apply 停止并指向 source location
- [x] 校验 `message` 为非空字符串

测试：

- [x] merge golden 覆盖多 profile imports
- [x] merge golden 覆盖 host override
- [x] merge golden 覆盖 labeled block 合并
- [x] 负例覆盖 import cycle 的完整路径
- [x] 负例覆盖 profile 中非法 host-only 字段
- [x] 负例覆盖重复 package、空 key、非法 modifier
- [x] assert 单测覆盖 `self` 视图求值通过
- [x] assert 负例覆盖条件为 false、空 message、读取非法字段
- [x] 错误消息测试覆盖 file:line:path

示例：

- [x] 增加 `examples/v2-profile-merge.dbf.hcl`
- [x] 增加至少一个 intentional invalid fixture 到测试目录，而不是 examples 目录

文档：

- [x] 合并规则文档补充 labeled block 合并示例
- [x] merge modifier 文档补充错误用法

验收：

- [x] profile 合并 fixture 的 HostSpec snapshot 稳定
- [x] 所有负例返回可读 source location
- [x] assert 失败能指向用户 DSL 行号
- [x] `make test` 成功

## Loop 3: 第一批日常领域块

目标：补齐基础系统配置所需的本地领域块，并保持 plan 地址仍为 v2 用户地址。

范围：

- `files`
- `secrets`
- `directories`
- `groups`
- `users`
- `systemd`（仅 raw unit 文件）
- `services`

范围说明：

- 本轮 `systemd` 只做 raw unit 文件管理。结构化 `service "app"` / `timer "app"` 语法，
  以及 `systemd.networkd`/`resolved`/`journald` 的一对一序列化为后续阶段。
- 第一版用 raw `systemd` unit + `files` 覆盖 networkd 用例；
  `examples/v2-systemd-networkd-wireguard.dbf.hcl` 暂作 design-only fixture，不进入第一版
  golden，直到 networkd 结构化语法单独排期。

代码：

- [x] parser 支持本轮领域 block 和 labeled object block
- [x] IR 增加 FileSpec、SecretSpec、DirectorySpec、GroupSpec、UserSpec、SystemdSpec、ServiceSpec
- [x] files 支持 content/source 二选一
- [x] files 支持 owner、group、mode、ensure、sensitive
- [x] secrets 支持本地 source 文件
- [x] secrets 默认 owner/group/mode 为 root/root/0600
- [x] secrets 在 plan、日志、state 中只输出摘要
- [x] directories 支持 owner、group、mode、ensure
- [x] groups 支持 system、gid、ensure
- [x] users 支持 home、shell、group、groups、system、ssh_authorized_keys、ensure
- [x] systemd 支持 unit file 管理
- [x] services 支持 enabled 和 state
- [x] 编译 systemd daemon_reload operation node
- [x] 编译 service restart/reload operation node
- [x] 推导 user -> group 依赖
- [x] 推导 service -> package 依赖
- [x] 推导 service -> systemd unit 依赖
- [x] 检测 files 和 secrets 远端 path 冲突

测试：

- [x] parser 单测覆盖 labeled object block
- [x] HostSpec snapshot 覆盖 files/secrets/directories/users/groups/systemd/services
- [x] ResourceGraph snapshot 覆盖语义依赖边
- [x] plan golden 覆盖 file text diff
- [x] 负例覆盖重复 path、非法 mode、缺失 group 引用、secret source 不存在
- [x] 引入 secrets 的同时建立断言：plan、日志、state 不出现 secret 明文，只输出摘要

示例：

- [x] 增加 user/group/authorized_key 示例
- [x] 增加 systemd service 示例
- [x] 从 `examples/v2-fleet.dbf.hcl` 抽出小型可测 fixture

文档：

- [x] 更新 README 的基础系统配置示例
- [x] 补充 files sensitive 限制
- [x] 补充 secrets 不落明文限制
- [x] 补充 services state 语义

验收：

- [x] 新增示例均可 validate
- [x] plan 显示 files、secrets、users、services 的 v2 地址
- [x] plan/state/日志 不包含 secret 明文（本轮即建立，不等到 Loop 5）
- [x] `make test` 成功

## Loop 4: v2 state 和 apply

目标：让 v2 可以对 kernel、packages 和第一批日常领域块执行 apply，并写入 v2 state。

代码：

- [x] 定义 v2 state 文件 schema
- [x] 定义 desired/state/observed 状态对比模型
- [x] 每个 host 使用独立 state path 和 lock path
- [x] 实现远端原子获取 lock，写入 owner/pid/token/expires_at
- [x] 释放 lock 时校验 token，避免误删其他进程的锁
- [x] stale lock 过期后可被新操作接管，并在输出中提示
- [x] lock 文件不进入 plan diff，也不进入 state
- [x] state resource key 优先使用 v2 地址
- [x] state 保留低阶 provider address 作为 debug 信息
- [x] state 保存上次 desired 摘要和必要 observed 摘要
- [x] provider 支持按 ResourceGraph 读取 observed 状态
- [x] plan 基于 desired、state、observed 三者生成 create/update/delete/destroy/no-op
- [x] plan 能区分远端不存在、远端已存在可接管、远端 drift 三种情况
- [x] desired 缺失但 state 存在时，根据 ownership 计划 destroy 或解除管辖
- [x] observed 读取失败时返回明确诊断，不假设远端状态等于 state
- [x] 实现 v2 原生 SSH runner
- [x] 实现 v2 原生 provider 执行路径
- [x] 实现单 host 串行 apply
- [x] 实现多 host 并行 apply 的 CLI 参数接口
- [x] apply 失败后停止依赖它的后续节点
- [x] apply 部分失败时持久化已成功节点到 state，保证 state 与远端一致
- [x] `check` 在 plan 有变更时返回非零
- [x] 防止 `prevent_destroy` 资源被 delete/destroy/replace

测试：

- [x] state read/write 单测
- [x] 状态对比单测覆盖 desired/state/observed 判断矩阵
- [x] 状态对比单测覆盖 create、update、delete、destroy、解除管辖、adopted ownership、no-op
- [x] drift 检测测试覆盖 state 显示上次一致但 observed 已变化
- [x] observed 读取失败测试覆盖明确诊断
- [x] apply dry-run 或 fake runner 测试
- [x] lock 获取/释放/token 校验/stale 接管测试
- [x] 幂等性测试：fake runner apply 后立即 plan 应为 no-op
- [x] 部分失败后 state 只包含已成功节点的测试
- [x] prevent_destroy 负例测试
- [x] check 命令返回码测试

示例：

- [x] BBR 示例进入 fake runner apply 测试
- [x] packages 示例进入 fake runner apply 测试

文档：

- [x] 新增 v2 state 文件说明
- [x] README 增加 apply/check 使用说明
- [x] 明确旧 state address 不兼容 v2

验收：

- [x] fake runner apply 能写入 v2 state
- [x] plan 不只比较 desired 和 state，必须读取 observed
- [x] plan 能正确报告 create、update、delete、destroy、解除管辖、adopted ownership、no-op
- [x] fake runner apply 后立即 plan 输出 no-op
- [x] apply 部分失败后 state 与已执行结果一致
- [x] `dbf check` 能检测 drift 或变更
- [x] `make test` 成功

## Loop 5: 结构化 plan renderer

目标：让 plan 成为稳定机器接口，同时提供可读的终端和 HTML preview。

范围：

- text diff
- sensitive diff
- JSON renderer
- terminal tree renderer
- static HTML renderer

代码：

- [x] plan 顶层输出 `format_version = "debianform.plan.v2alpha1"`
- [x] DiffNode 支持 object、map、set、list、scalar、text、sensitive
- [x] 文本内容支持行级 diff hunks
- [x] sensitive 内容只输出摘要，不输出明文
- [x] JSON renderer 输出稳定字段顺序
- [x] terminal renderer 显示 source location 和字段级 diff
- [x] terminal renderer 在无颜色环境保留 `+`、`-`、`~` 符号
- [x] HTML renderer 生成可独立打开的静态文件
- [x] HTML preview 支持 action 过滤
- [x] HTML preview 支持 host 过滤
- [x] HTML preview 支持搜索字段路径
- [x] debug 模式输出 provider address

测试：

- [x] plan JSON golden 覆盖 create/update/delete/no_op/run
- [x] text diff golden 覆盖普通 file content
- [x] sensitive diff golden 覆盖 secret 文件
- [x] terminal renderer snapshot
- [x] HTML renderer smoke 测试
- [x] 检查 plan/state/log 不包含 secret 明文

示例：

- [x] 增加小型 files/secrets plan preview fixture 作为本轮主 fixture
- [x] 增加示例 secret 占位说明，但不提交真实 secret
- [x] `.gitignore` 忽略示例 secrets 目录

文档：

- [x] 更新 `docs/v2-plan-format.md` 中的实际字段
- [x] README 增加 `plan --format json`
- [x] README 增加 `plan --html plan.html`

验收：

- [x] `dbf plan -f examples/v2-files-plan-preview.dbf.hcl --format json` 不泄露 secret 明文
- [x] `dbf plan -f examples/v2-files-plan-preview.dbf.hcl --html plan.html` 生成静态文件
- [x] `make test` 成功

## Loop 6: APT 和 component

目标：支持复用部署单元和显式 APT repository 关系。

范围：

- `apt.repository`
- `packages.package`
- component input
- component source architecture selection
- binary artifact
- archive/file/ca_certificate artifact

代码：

- [x] parser 支持 `apt.repository`
- [x] parser 支持 `packages.package`
- [x] parser 支持 `component`
- [x] parser 支持 host `components`
- [x] IR 增加 APTSpec
- [x] IR 增加 ComponentInstanceSpec
- [ ] IR 增加 ComponentTemplateSpec
- [x] component input 支持 string、number、bool、list(string)、map(string)
- [x] component instance 校验缺失 input 和未知 input
- [x] 实现 component 内只读 `input.<name>` 表达式求值与模板渲染
- [x] 实现 component 内只读 `target` 上下文（如 `target.system.codename`），禁止借此修改 host
- [x] 按 host architecture 选择唯一 source
- [x] 远程 URL source 必须声明 sha256
- [x] repository signing_key 支持 url/content 二选一
- [x] repository signing_key URL 模式要求 sha256 并由 provider 下载校验
- [x] repository 输出 deb822 sources payload
- [x] package repositories 引用同 host repository
- [x] 生成 host-scoped `apt.cache_refresh` operation node
- [x] 多 repository 变化时每 host 只刷新一次 APT cache
- [x] binary artifact 编译为下载、校验、安装节点
- [ ] archive/file artifact 编译为下载、校验、安装节点
- [ ] ca_certificate 生成 `update-ca-certificates` operation node
- [x] component 与 host/profile 产生相同远端 identity 时 validate 报错

测试：

- [x] APT repository HostSpec snapshot
- [x] APT ResourceGraph snapshot 覆盖 cache refresh 汇聚
- [x] APT repository plan JSON golden 覆盖 cache refresh operation
- [x] component parser 单测
- [x] component architecture selection 单测
- [x] component `input.<name>` 渲染单测
- [x] component `target` 上下文读取单测（bird2 `suites` 选 `target.system.codename`）
- [x] component input validation 负例
- [x] artifact sha256 负例
- [x] identity 冲突负例

示例：

- [x] `examples/v2-apt-repository.dbf.hcl` 进入 golden 测试
- [x] `examples/v2-bird2.dbf.hcl` 进入 golden 测试
- [x] `examples/v2-component-binary.dbf.hcl` 进入 golden 测试

文档：

- [x] README 增加 component 基本示例
- [x] APT 文档补充 repository/package 依赖规则
- [x] component 文档补充 architecture source 选择规则

验收：

- [x] 同一个 component 可挂载到多台 host
- [x] 不同 architecture 能选择对应 source
- [x] 不能唯一选择 source 时 validate 失败
- [x] `make test` 成功

## Loop 7: nftables

目标：以原生 nftables 文件为主路径，提供校验、激活和文本 diff。

代码：

- [ ] parser 支持 `nftables.enable`
- [ ] parser 支持 `nftables.main`
- [ ] parser 支持 `nftables.file "<label>"`
- [ ] IR 增加 NftablesSpec
- [ ] main 默认 path 为 `/etc/nftables.conf`
- [ ] file 默认 path 为 `/etc/nftables.d/<label>.nft`
- [ ] content/source 二选一
- [ ] 检测最终 path 冲突
- [ ] 编译 nftables file resource
- [ ] 编译 `nftables.validate` operation node
- [ ] 编译 `nftables.activate` operation node
- [ ] 多个 nftables 文件变化时每 host 只 validate/activate 一次
- [ ] plan 显示 nftables content 行级 diff

测试：

- [ ] HostSpec snapshot 覆盖 main 和 snippet
- [ ] ResourceGraph snapshot 覆盖 validate/activate 汇聚
- [ ] text diff golden 覆盖端口变化
- [ ] 负例覆盖重复 path、content/source 同时设置

示例：

- [ ] `examples/v2-nftables.dbf.hcl` 进入 golden 测试
- [ ] `examples/v2-plan-preview.dbf.hcl` 作为 nftables + secret 完整 plan preview fixture

文档：

- [ ] README 增加 nftables 简短示例
- [ ] nftables 文档明确不提供通用 firewall 抽象

验收：

- [ ] nftables 示例可 validate/plan
- [ ] plan 中展示 validate 和 activate operation
- [ ] `make test` 成功

## Loop 8: 调度器增强

目标：从保守串行执行升级为 ResourceGraph DAG wave 调度。

代码：

- [ ] 实现 ResourceGraph 拓扑排序
- [ ] 实现 DAG wave 计算
- [ ] 实现资源地址唯一校验
- [ ] 实现依赖地址存在校验
- [ ] 实现环检测并输出环路径
- [ ] 实现全局并发限制
- [ ] 实现 per-host 并发限制
- [ ] 增加资源类型 safe_parallel 标记
- [ ] 失败节点阻止依赖节点执行
- [ ] 第一版不实现用户 `depends_on`；依赖由编译器语义推导，`depends_on` 逃生口留待后续单独设计

测试：

- [ ] wave 计算单测
- [ ] 依赖缺失负例
- [ ] 环检测负例
- [ ] 并发限制测试
- [ ] 失败传播测试

文档：

- [ ] 调度语义文档补充 wave 示例
- [x] CLI 文档补充 `--parallel`

验收：

- [x] 多 host plan/apply 保持稳定顺序展示
- [x] 单 host 默认仍可保守串行
- [ ] `make test` 成功

## Loop 9: v2 作为主版本收口

目标：让 v2 可以作为 DebianForm 主版本使用。

代码：

- [x] CLI 默认路径切到 v2
- [x] 实现 `dbf fmt`，对 v2 DSL 做规范化格式化
- [x] 移除旧实验入口和代码路径
- [ ] 清理 v2 中不再需要的临时 feature gate
- [ ] 错误信息统一使用 v2 用户术语
- [ ] release build 包含 v2 文档和示例

测试：

- [ ] 所有第一版 v2 examples 进入 validate 测试（design-only fixture 除外）
- [ ] 小型 golden examples 覆盖 parser、HostSpec、ResourceGraph、plan
- [x] `dbf fmt` 幂等性测试
- [ ] `v2-fleet` 只作为组合压力测试

示例：

- [ ] BBR 示例
- [ ] nginx 或 systemd service 示例
- [ ] user/group/authorized_key 示例
- [ ] multi host/profile 示例
- [ ] APT repository 示例
- [ ] BIRD2 component 示例
- [x] binary component 示例
- [ ] nftables 示例
- [ ] plan preview 示例

文档：

- [x] README 改为 v2 语法主入口
- [ ] 增加旧实验格式已废弃说明
- [ ] 增加从设计 fixture 到可运行示例的说明
- [ ] 增加常见错误说明

验收：

- [ ] v2 第一版验收标准全部满足
- [ ] `make test` 成功
- [ ] README 中所有 v2 命令可复制运行

## 第一版总体验收标准

- [ ] 可以用 `host` 配置单台主机 BBR
- [ ] 可以用 `profile` 复用 BBR 和 base packages
- [ ] host 能覆盖 profile 中的 map 和 list
- [ ] `force([])` 能清空继承的 list
- [ ] 同一个 component 可以挂载到多台 host，并按 architecture 选择唯一 source
- [ ] component 与 host/profile 产生相同远端 identity 时 validate 报错
- [ ] plan 输出用户层 v2 地址
- [ ] apply 能正确执行 kernel module、sysctl、package
- [ ] apply 后再次 plan 为 no-op（幂等）
- [ ] 每台 host 使用独立 state，并通过 lock 保证并发安全
- [ ] validate 能发现 imports 循环、字段冲突、资源图环
- [ ] assert 能在 ResourceGraph 编译前拦截非法组合并指向来源
- [ ] 不需要写任何 `低阶 provider 资源` 资源即可完成基础系统配置
