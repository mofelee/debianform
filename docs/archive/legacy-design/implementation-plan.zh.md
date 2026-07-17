# DebianForm 实施计划

<p align="right"><a href="implementation-plan.md">English</a> | <strong>简体中文</strong></p>

本文档把 设计落成可执行的开发计划。每个 loop 都必须形成一个可合并闭环：

- 代码路径可运行
- 单元测试和 golden 测试覆盖本轮语义
- 至少一个示例进入验收输入
- 文档同步更新
- `make test` 通过

状态约定：

- `[x]` 已完成
- `[ ]` 未完成

## 当前基线

- [x] 用户需求文档已存在：`docs/requirements.md`
- [x] 中间表达文档已存在：`docs/ir-requirements.zh.md`
- [x] plan JSON 格式文档已存在：`docs/plan-format.md`
- [x] 设计示例已存在：`examples/*.dbf.hcl`
- [x] 编译器主流程已支持 HostSpec、ResourceGraph、state、plan、apply 和 check
- [x] CLI 已接入 `validate`、`plan`、`apply`、`check` 和 `fmt`
- [x] plan/apply 已接入 observed 检测、远端 state 和 SSH 执行路径

## Loop 1a: 解析、合并与 HostSpec

目标：让 `examples/bbr.dbf.hcl` 可以通过 路径完成 `validate`，打通 parser、merge 和
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

- [x] 新增 `internal/core/parser`，解析 顶层 block 和本轮领域 block
- [x] 保留 `SourceRef`，至少记录 file、line、path
- [x] 新增 `internal/core/merge`，实现 profile imports 顺序合并
- [x] 实现 list append 去重
- [x] 实现 map 深度合并
- [x] 实现 scalar 后者覆盖
- [x] 实现 `force(value)`
- [x] 实现 `before(value)` 和 `after(value)`
- [x] 实现 `unset()` 删除 map key
- [x] 新增 `internal/core/ir`，定义 `Program`、`HostSpec` 和本轮领域 spec
- [x] 为 `ssh`、`state`、`system.hostname` 填充默认值
- [x] 归一化 `kernel.modules`
- [x] 归一化 `kernel.sysctl`
- [x] 归一化 `packages.install`
- [x] 选定 snapshot/golden 测试框架，并提供 `make update-golden` 统一刷新机制
- [x] 支持 `dbf validate -f examples/bbr.dbf.hcl` 走 路径

测试：

- [x] parser 单测覆盖 `host`、`profile`、nested domain block、source line
- [x] parser 负例覆盖未知顶层 block、错误 label 数量、重复 host
- [x] merge 单测覆盖 imports 顺序、list 去重、map 覆盖、scalar 覆盖
- [x] merge modifier 单测覆盖 `force`、`before`、`after`、`unset`
- [x] merge 负例覆盖 profile import cycle
- [x] HostSpec snapshot 覆盖 `examples/bbr.dbf.hcl`

示例：

- [x] `examples/bbr.dbf.hcl` 作为本轮主 golden fixture
- [x] 增加一个 profile 合并小示例，覆盖 base packages + bbr profile + host override

文档：

- [x] 在 docs 中注明本轮支持范围和暂不支持范围

验收：

- [x] `dbf validate -f examples/bbr.dbf.hcl` 成功
- [x] profile 合并 fixture 的 HostSpec snapshot 稳定
- [x] `make test` 成功

## Loop 1b: ResourceGraph 与 BBR plan

目标：在 Loop 1a 的 HostSpec 基础上编译 ResourceGraph 并生成结构化 plan，让
`examples/bbr.dbf.hcl` 可以走完 `plan`，不需要写任何 `低阶 provider 资源` 用户资源。

说明：本轮 plan 是 create-only 预览，所有资源都按“远端为空”假设输出 create，尚未读取
observed。真正的 desired/state/observed 三元比对在 Loop 4 接入，届时本轮的 plan golden 会
按新模型重写。

代码：

- [x] 新增 `internal/core/graph`，从 HostSpec 编译 ResourceGraph
- [x] 生成稳定 地址，例如 `host.bbr1.kernel.module["tcp_bbr"]`
- [x] 生成低阶 provider payload，但不暴露给普通用户 plan
- [x] 推导 BBR 依赖：`tcp_congestion_control = "bbr"` 依赖 `tcp_bbr` module
- [x] 新增 `internal/core/plan`，生成结构化 plan 模型
- [x] 支持 `dbf plan -f examples/bbr.dbf.hcl`
- [x] 支持 `dbf plan -f examples/bbr.dbf.hcl --format json`

测试：

- [x] ResourceGraph snapshot 覆盖 BBR module/sysctl 地址和依赖边
- [x] plan JSON golden 覆盖 BBR create plan
- [x] CLI smoke 覆盖 validate、plan text、plan JSON

文档：

- [x] README 标记 BBR 路径已可 validate/plan
- [x] 增加 BBR plan 输出样例

验收：

- [x] `dbf plan -f examples/bbr.dbf.hcl` 输出 用户地址
- [x] `dbf plan -f examples/bbr.dbf.hcl --format json` 输出 `debianform.plan.alpha1`
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

- [x] 增加 `examples/profile-merge.dbf.hcl`
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

目标：补齐基础系统配置所需的本地领域块，并保持 plan 地址仍为 用户地址。

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
- WireGuard/networkd 用例使用 `systemd.networkd` 结构化语法生成原生 `.netdev` 和
  `.network` 文件，不依赖 `wg-quick`。

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
- [x] 从 `examples/fleet.dbf.hcl` 抽出小型可测 fixture

文档：

- [x] 更新 README 的基础系统配置示例
- [x] 补充 files sensitive 限制
- [x] 补充 secrets 不落明文限制
- [x] 补充 services state 语义

验收：

- [x] 新增示例均可 validate
- [x] plan 显示 files、secrets、users、services 的 地址
- [x] plan/state/日志 不包含 secret 明文（本轮即建立，不等到 Loop 5）
- [x] `make test` 成功

## Loop 4: state 和 apply

目标：让 可以对 kernel、packages 和第一批日常领域块执行 apply，并写入 state。

代码：

- [x] 定义 state 文件 schema
- [x] 定义 desired/state/observed 状态对比模型
- [x] 每个 host 使用独立 state path 和 lock path
- [x] 实现远端原子获取 lock，写入 owner/pid/token/expires_at
- [x] 释放 lock 时校验 token，避免误删其他进程的锁
- [x] stale lock 过期后可被新操作接管，并在输出中提示
- [x] lock 文件不进入 plan diff，也不进入 state
- [x] state resource key 优先使用 地址
- [x] state 保留低阶 provider address 作为 debug 信息
- [x] state 保存上次 desired 摘要和必要 observed 摘要
- [x] provider 支持按 ResourceGraph 读取 observed 状态
- [x] plan 基于 desired、state、observed 三者生成 create/update/delete/destroy/no-op
- [x] plan 能区分远端不存在、远端已存在可接管、远端 drift 三种情况
- [x] desired 缺失但 state 存在时，根据 ownership 计划 destroy 或解除管辖
- [x] observed 读取失败时返回明确诊断，不假设远端状态等于 state
- [x] 实现 原生 SSH runner
- [x] 实现 原生 provider 执行路径
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

- [x] 新增 state 文件说明
- [x] README 增加 apply/check 使用说明
- [x] 明确旧 state address 不兼容 current

验收：

- [x] fake runner apply 能写入 state
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

- [x] plan 顶层输出 `format_version = "debianform.plan.alpha1"`
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

- [x] 更新 `docs/plan-format.md` 中的实际字段
- [x] README 增加 `plan --format json`
- [x] README 增加 `plan --html plan.html`

验收：

- [x] `dbf plan -f examples/files-plan-preview.dbf.hcl --format json` 不泄露 secret 明文
- [x] `dbf plan -f examples/files-plan-preview.dbf.hcl --html plan.html` 生成静态文件
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
- [x] IR 增加 ComponentTemplateSpec
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
- [x] archive/file artifact 编译为下载、校验、安装节点
- [x] ca_certificate 生成 `update-ca-certificates` operation node
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

- [x] `examples/apt-repository.dbf.hcl` 进入 golden 测试
- [x] `examples/bird2.dbf.hcl` 进入 golden 测试
- [x] `examples/component-binary.dbf.hcl` 进入 golden 测试

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

- [x] parser 支持 `nftables.enable`
- [x] parser 支持 `nftables.main`
- [x] parser 支持 `nftables.file "<label>"`
- [x] IR 增加 NftablesSpec
- [x] main 默认 path 为 `/etc/nftables.conf`
- [x] file 默认 path 为 `/etc/nftables.d/<label>.nft`
- [x] content/source 二选一
- [x] 检测最终 path 冲突
- [x] 编译 nftables file resource
- [x] 编译 `nftables.validate` operation node
- [x] 编译 `nftables.activate` operation node
- [x] 多个 nftables 文件变化时每 host 只 validate/activate 一次
- [x] plan 显示 nftables content 行级 diff

测试：

- [x] HostSpec snapshot 覆盖 main 和 snippet
- [x] ResourceGraph snapshot 覆盖 validate/activate 汇聚
- [x] text diff golden 覆盖端口变化
- [x] 负例覆盖重复 path、content/source 同时设置

示例：

- [x] `examples/nftables.dbf.hcl` 进入 golden 测试
- [x] `examples/plan-preview.dbf.hcl` 作为 nftables + secret 完整 plan preview fixture

文档：

- [x] README 增加 nftables 简短示例
- [x] nftables 文档明确不提供通用 firewall 抽象

验收：

- [x] nftables 示例可 validate/plan
- [x] plan 中展示 validate 和 activate operation
- [x] `make test` 成功

## Loop 8: 调度器增强

目标：从保守串行执行升级为 ResourceGraph DAG wave 调度。

代码：

- [x] 实现 ResourceGraph 拓扑排序
- [x] 实现 DAG wave 计算
- [x] 实现资源地址唯一校验
- [x] 实现依赖地址存在校验
- [x] 实现环检测并输出环路径
- [x] 实现全局并发限制
- [x] 实现 per-host 并发限制
- [x] 增加资源类型 safe_parallel 标记
- [x] 失败节点阻止依赖节点执行
- [x] 第一版不实现用户 `depends_on`；依赖由编译器语义推导，`depends_on` 逃生口留待后续单独设计

测试：

- [x] wave 计算单测
- [x] 依赖缺失负例
- [x] 环检测负例
- [x] 并发限制测试
- [x] 失败传播测试

文档：

- [x] 调度语义文档补充 wave 示例
- [x] CLI 文档补充 `--parallel`

验收：

- [x] 多 host plan/apply 保持稳定顺序展示
- [x] 单 host 默认仍可保守串行
- [x] `make test` 成功

## Loop 9: 作为主版本收口

目标：让 可以作为 DebianForm 主版本使用。

代码：

- [x] CLI 默认路径切到 current
- [x] 实现 `dbf fmt`，对 DSL 做规范化格式化
- [x] 移除旧实验入口和代码路径
- [x] 清理 中不再需要的临时 feature gate；仅保留 validate/fmt/runtime facts bootstrap 的内部编译选项
- [x] 错误信息统一使用 用户术语
- [x] release build 包含 文档和示例

测试：

- [x] 所有第一版 examples 进入 validate 测试（design-only fixture 除外）
- [x] 小型 golden examples 覆盖 parser、HostSpec、ResourceGraph、plan
- [x] `dbf fmt` 幂等性测试
- [x] `fleet` 只作为组合压力测试

示例：

- [x] BBR 示例
- [x] nginx 或 systemd service 示例
- [x] user/group/authorized_key 示例
- [x] multi host/profile 示例
- [x] APT repository 示例
- [x] BIRD2 component 示例
- [x] binary component 示例
- [x] nftables 示例
- [x] plan preview 示例

文档：

- [x] README 改为 语法主入口
- [x] 增加旧实验格式已废弃说明
- [x] 增加从设计 fixture 到可运行示例的说明
- [x] 增加常见错误说明

验收：

- [x] 第一版验收标准全部满足
- [x] `make test` 成功
- [x] README 中所有 命令可复制运行

## 第一版总体验收标准

- [x] 可以用 `host` 配置单台主机 BBR
- [x] 可以用 `profile` 复用 BBR 和 base packages
- [x] host 能覆盖 profile 中的 map 和 list
- [x] `force([])` 能清空继承的 list
- [x] 同一个 component 可以挂载到多台 host，并按 architecture 选择唯一 source
- [x] component 与 host/profile 产生相同远端 identity 时 validate 报错
- [x] plan 输出用户层 地址
- [x] apply 能正确执行 kernel module、sysctl、package
- [x] apply 后再次 plan 为 no-op（幂等）
- [x] 每台 host 使用独立 state，并通过 lock 保证并发安全
- [x] validate 能发现 imports 循环、字段冲突、资源图环
- [x] assert 能在 ResourceGraph 编译前拦截非法组合并指向来源
- [x] 不需要写任何 `低阶 provider 资源` 资源即可完成基础系统配置

## 后续增强：component input 2.0

以下 loop 把 `docs/component-input-requirements.zh.md` 拆成可合并闭环。它们属于 current
第一版之后的增强，不改变上面的第一版验收结论。

总体原则：

- [x] 每个 loop 都必须保持现有 `type/default/sensitive` input 配置兼容
- [x] 每个 loop 都必须有 parser 单测、merge/compiler 单测和至少一个 golden 覆盖
- [x] 新增复杂 input 能力必须先通过 `dbf validate`，再进入 plan/apply 路径
- [x] 敏感数据不能因为新类型系统进入 HostSpec JSON、plan JSON 或 state JSON 明文
- [x] `make test` 必须通过

## Loop 10: component input 类型系统和归一化

目标：把 component input 从字符串类型名升级为结构化 type schema，并支持
`list(object({...}))`、`map(object({...}))`、`optional(...)`、`nullable` 和
`description`。本轮不做 validation block，也不改变 sensitive 的传播语义。

范围：

- `description`
- `nullable`
- `any`
- `list(T)`
- `set(T)`
- `map(T)`
- `object({ ... })`
- `tuple([ ... ])`
- `optional(T)`
- `optional(T, default)`
- strict object schema
- component instance input 归一化

暂不做：

- input `validation` block
- sensitive mark 传播
- `deprecated`
- `ephemeral`
- Terraform root module variable 赋值来源
- `array(T)` alias
- 宽松 object conversion 和静默丢弃多余字段

代码：

- [x] 新增 component input type parser，基于 HCL AST 解析类型表达式
- [x] 新增机器可读 `ComponentInputTypeSpec`，不要只保存 canonical string
- [x] `parser.ComponentInput` 增加 `TypeSpec`、`TypeExpr`、`Description`、`Nullable`
- [x] `ir.ComponentInputSpec` 增加 `TypeSpec`、`TypeExpr`、`Description`、`Nullable`
- [x] `parseComponentInputBlock` 支持 `description` 和 `nullable`
- [x] 保持旧 `Type string` JSON/调试输出兼容，值为 canonical type expression
- [x] 实现 default 值按 type conversion 和 optional default 归一化
- [x] 实现 instance input 值按 type conversion 和 optional default 归一化
- [x] `nullable = false` 拒绝顶层 null
- [x] `object` 缺失必填字段时报错
- [x] `object` 多余字段默认报错
- [x] `tuple` 长度不一致时报错
- [x] `set(T)` 输出使用确定性顺序，保证 HostSpec/golden 稳定
- [x] `array(T)` 明确报错，并提示使用 `list(T)`
- [x] component body 展开时注入归一化后的结构化 `input`
- [x] 表达式 evaluator 增加 `jsonencode`，用于把结构化 input 写入文件内容
- [x] 错误 path 能定位到嵌套字段和 list 下标

测试：

- [x] parser 单测覆盖 primitive、`any`、`list(T)`、`set(T)`、`map(T)`、`tuple([...])`
- [x] parser 单测覆盖 `object({ name = string, enabled = optional(bool, true) })`
- [x] parser 单测覆盖 `list(object({ ... }))`
- [x] parser 负例覆盖 `array(string)`、裸 `list`、裸 `map`、object 外的 `optional(...)`
- [x] merge 单测覆盖 default 归一化
- [x] merge 单测覆盖 instance input 归一化
- [x] merge 负例覆盖缺失 object 必填字段
- [x] merge 负例覆盖 object 多余字段
- [x] merge 负例覆盖嵌套字段类型错误，并断言 source path 含下标
- [x] HostSpec golden 覆盖 `list(object(...))` 归一化结果
- [x] ResourceGraph/plan golden 覆盖结构化 input 生成文件内容

示例：

- [x] 新增 `examples/component-inputs.dbf.hcl`
- [x] 示例 component 使用 `list(object(...))` 生成 JSON 配置文件
- [x] 示例覆盖 optional 字段默认值补齐
- [x] 示例进入 runnable examples validate 测试

文档：

- [x] README 增加 component input 2.0 简短示例
- [x] `docs/component-input-requirements.zh.md` 标记本轮已实现范围
- [x] `docs/requirements.md` 更新 component input 支持类型列表
- [x] `docs/ir-requirements.zh.md` 更新 ComponentInputSpec

验收：

- [x] `dbf validate -f examples/component-inputs.dbf.hcl` 成功
- [x] `dbf plan -f examples/component-inputs.dbf.hcl --offline --format json` 成功
- [x] HostSpec 中 `list(object(...))` 已归一化并稳定排序
- [x] `make test` 成功

## Loop 11: component input validation block

目标：支持 Terraform-like `validation` block，让 component 在展开前校验 input 自身契约。

范围：

- input 内重复 `validation` block
- `condition`
- `error_message`
- 只读 validation 上下文 `input.<current_input_name>`
- 第一批纯函数
- validation source location

暂不做：

- 跨 input validation
- 访问 `target`
- 访问 `local`
- `file`、`templatefile`、网络、外部命令
- provider runtime check

代码：

- [x] `parser.ComponentInput` 增加 `Validations []ComponentInputValidation`
- [x] `ir.ComponentInputSpec` 增加 validation metadata
- [x] `parseComponentInputBlock` 允许 nested `validation` block
- [x] validation block 必须无 label
- [x] validation block 必须包含 `condition` 和 `error_message`
- [x] `error_message` 必须是非空字符串
- [x] validation 在 default、type conversion、optional default、nullable 检查之后执行
- [x] validation condition 必须求值为 bool
- [x] validation 只能读取当前 input：`input.<name>`
- [x] 禁止 validation 读取其他 input、`target`、`local`、`path`
- [x] 增加 validation 专用纯函数集合：`length`、`contains`、`startswith`、`endswith`
- [x] 增加集合函数：`alltrue`、`anytrue`、`distinct`、`sort`、`keys`、`values`
- [x] 增加转换函数：`tonumber`、`tostring`、`tobool`
- [x] 增加 `regex` 和 `can`
- [x] validation 失败时阻止 validate/plan/apply，并指向 validation source

测试：

- [x] parser 单测覆盖重复 validation block
- [x] parser 负例覆盖 validation label、缺少 condition、缺少 error_message、空 message
- [x] merge 单测覆盖 validation 成功
- [x] merge 单测覆盖 validation 失败
- [x] merge 单测覆盖 condition 非 bool
- [x] merge 负例覆盖读取其他 input
- [x] merge 负例覆盖读取 `target`、`local`、`path`
- [x] 函数单测覆盖 `alltrue`、`anytrue`、`regex`、`can`
- [x] CLI smoke 覆盖 `dbf validate` 输出 validation failure source path

示例：

- [x] 更新 `examples/component-inputs.dbf.hcl`，增加端口范围 validation
- [x] 增加 invalid fixture 覆盖 validation failure

文档：

- [x] README 示例展示一个短 validation
- [x] `docs/component-input-requirements.zh.md` 标记 validation 已实现函数集合
- [x] 常见错误增加 validation failure 示例

验收：

- [x] 合法 input validation 通过
- [x] 非法 port validation 报错并指向 `component.<name>.input["..."].validation[0]`
- [x] validation 不能访问 `target.system.codename`
- [x] `make test` 成功

## Loop 12: component input sensitive 传播

目标：把 `sensitive = true` 从“input_values 输出打码”升级为表达式级敏感性传播，确保复杂
input 和派生内容不会泄漏到 HostSpec、plan、state 或日志。

范围：

- sensitive cty mark
- `parser.Value` sensitive metadata
- 由 sensitive input 派生的表达式结果继续 sensitive
- HostSpec JSON redaction
- plan JSON redaction 或摘要
- state JSON 不保存敏感明文
- file/unit/secret 等 payload 的敏感摘要

暂不做：

- `ephemeral`
- provider write-only 参数
- 对所有第三方格式做语义级 secret 检测

代码：

- [x] 在 input 注入 cty value 时为 sensitive input 加 mark
- [x] `ctyToValue` 保留 sensitive mark
- [x] `parser.Value` 或等价 wrapper 增加 sensitive metadata
- [x] 表达式求值后聚合 sensitive mark
- [x] `parserValueToAny` 对 sensitive value 输出 `"<sensitive>"`
- [x] HostSpec JSON 中 component `input_values` 对 sensitive 值 redacted
- [x] 由 sensitive value 生成的 `files.file.content` 自动视为 sensitive
- [x] 由 sensitive value 生成的 `systemd.unit.content` 自动视为 sensitive
- [x] 由 sensitive value 生成的 service environment 自动视为 sensitive
- [x] plan text/JSON 对 sensitive content 只输出 hash、长度等摘要
- [x] state JSON 不保存 sensitive content 明文
- [x] 如果某类资源暂不能安全承载 sensitive 派生值，编译时必须报错（当前实现只把
  sensitive 派生值接入 file 和 systemd unit 等可摘要承载字段，暂无暴露的非敏感承载字段）

测试：

- [x] unit test 覆盖 sensitive input value redaction
- [x] unit test 覆盖 sensitive object 内部字段 redaction
- [x] unit test 覆盖 sensitive input 派生 file content
- [x] unit test 覆盖 sensitive input 派生 systemd unit content
- [x] plan golden 确认 sensitive 明文不出现
- [x] state 单测确认 sensitive 明文不出现
- [x] 负例覆盖 sensitive 派生值进入暂不支持的非敏感字段时失败（当前无暴露的非敏感
  承载字段；通过 file/unit 正向覆盖和 JSON 泄漏断言约束）

示例：

- [x] 更新 `examples/component-inputs.dbf.hcl`，增加 sensitive `environment` input
- [x] 示例 plan golden 确认 secret value 不泄漏

文档：

- [x] README 补充 sensitive input 行为说明
- [x] `docs/state.md` 补充 sensitive input 派生内容不落盘规则
- [x] `docs/plan-format.md` 补充 sensitive summary/redaction 字段
- [x] `docs/component-input-requirements.zh.md` 标记 sensitive propagation 已实现范围

验收：

- [x] `dbf plan -f examples/component-inputs.dbf.hcl --offline --format json` 不包含 sensitive 明文
- [x] state 序列化测试不包含 sensitive 明文
- [x] `make test` 成功

## Loop 13: component input API 演进和用户收口

目标：完成 component input 2.0 的用户侧收口，包括 deprecated warning、组件接口查看、文档、
示例和集成测试。

范围：

- `deprecated`
- warning 聚合和输出
- component input inspect
- README/docs 收口
- libvirt 集成测试

暂不做：

- `ephemeral`
- Terraform `.tfvars`/环境变量/CLI var
- `const`
- component registry/package manager

代码：

- [x] `parseComponentInputBlock` 支持 `deprecated`
- [x] `parser.ComponentInput` 和 `ir.ComponentInputSpec` 增加 `Deprecated`
- [x] 调用方显式传入 deprecated input 时产生 warning
- [x] 仅使用 deprecated input 的 default 时不产生 warning
- [x] CLI warning 统一聚合，`validate`、`plan`、`apply` 输出格式一致
- [x] warning 不改变退出码
- [x] 新增 `dbf component inspect -f <file> <component>`，输出 input 名称、类型、默认值、nullable、sensitive、deprecated 和 description
- [x] inspect 输出 redacts sensitive default
- [x] inspect 对 unknown component 返回明确错误
- [x] release build 包含新增文档和示例

测试：

- [x] parser 单测覆盖 `deprecated`
- [x] merge 单测覆盖显式传 deprecated input 产生 warning
- [x] merge 单测覆盖 default 不产生 deprecated warning
- [x] CLI smoke 覆盖 validate warning 输出
- [x] CLI smoke 覆盖 plan warning 输出
- [x] CLI smoke 覆盖 `dbf component inspect`
- [x] inspect golden 覆盖复杂 `list(object(...))` 类型显示
- [x] libvirt 集成测试覆盖 component input 2.0 生成实际文件

示例：

- [x] `examples/component-inputs.dbf.hcl` 进入 README 示例列表
- [x] 新增 `test/integration/libvirt/cases/component-inputs`
- [x] 集成测试验证 optional default 后的文件内容
- [x] 集成测试验证 sensitive value 未进入远端 state 文件

文档：

- [x] README 增加 component input 2.0 完整入口
- [x] README 常见错误补充 object 字段类型错误和 validation 失败
- [x] `docs/component-input-requirements.zh.md` 更新实现状态
- [x] `docs/implementation-plan.zh.md` 勾选完成项

验收：

- [x] `dbf component inspect -f examples/component-inputs.dbf.hcl reverse_proxy` 输出完整 input API
- [x] `dbf validate -f examples/component-inputs.dbf.hcl` 成功且 warning 行为符合预期
- [x] `make test` 成功
- [x] `component-inputs` Debian 13 amd64 VM flow 成功
  - 2026-06-23：在 disposable VM `dbf-test-component-inputs` 上执行 `validate`、
    `apply --auto-approve`、`check` 和 `1.check.sh`/`2.check.sh`；CI 失败点为 state
    address 匹配未使用 JSON escaped quote，已修正。
