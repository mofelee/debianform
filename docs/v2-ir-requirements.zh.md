# DebianForm v2 中间表达需求文档

## 目标

DebianForm v2 需要在用户 DSL 和最终低阶资源之间增加一个明确的中间表达。

中间表达用于承接这些工作：

```text
用户 DSL
  -> 解析 HCL
  -> 合并 profile 和 host override
  -> 归一化 component 并挂载到 host
  -> 生成中间表达
  -> 编译成低阶资源图
  -> plan/apply
```

中间表达不应该直接等于最终 provider 资源。它应该保留“主机目标状态”的领域语义，
同时比用户 DSL 更规整、更容易验证、更容易编译。

## 分层

v2 建议分成四层：

```text
DSL 层
  用户写的 HCL：host、profile、component、kernel、packages、services 等。

合并层
  解析 imports、执行 merge modifier、得到每台 host 的最终领域配置。

中间表达层
  归一化后的 HostSpec 和 ComponentInstanceSpec，仍然表达主机目标状态。

资源图层
  展开成 ResourceGraph，也就是 v2 执行层能处理的资源 DAG。
```

核心原则：

```text
DSL 负责好写
中间表达负责好检查
资源图负责好执行
```

## 为什么需要中间表达

如果直接把用户 DSL 翻译成 provider resource，会有几个问题：

- profile 合并逻辑会散落在各个 provider 里。
- plan 输出很难解释“这个低阶资源来自哪个 host/profile/component”。
- 未来加新领域块时容易影响执行层。
- 用户层语义和 provider 实现细节耦合过紧。
- 很难在编译前做跨领域校验，例如 service 引用 package、user 引用 group。

中间表达可以把这些问题隔离开：

- 用户层可以继续演进。
- provider 层可以保持稳定。
- 编译器负责把领域模型展开成资源图。
- 调度器只关心资源图和依赖边。

## 中间表达的定位

中间表达应该是“每台主机的归一化目标状态”。

它不是：

```text
不是 HCL AST
不是 provider resource
不是 SSH 命令列表
不是 state 文件格式
```

它是：

```text
合并完成后的 host 配置
字段类型已经规整
默认值已经填充
引用已经解析
unset/force/before/after 已经生效
仍保留 kernel/packages/files/services 等领域结构
```

## 建议数据模型

顶层可以叫 `Program`：

```go
type Program struct {
    Hosts      []HostSpec
    Components map[string]ComponentTemplateSpec
}
```

每台主机对应一个 `HostSpec`：

```go
type HostSpec struct {
    Name   string
    Source SourceRef

    SSH    SSHSpec
    State  StateSpec
    System SystemSpec

    Kernel      KernelSpec
    Packages    PackageSpec
    APT         APTSpec
    Files       FileSpec
    Directories DirectorySpec
    Users       UserSpec
    Groups      GroupSpec
    Services    ServiceSpec
    Systemd     SystemdSpec

    Components []ComponentInstanceSpec
}
```

`SourceRef` 用来保留来源信息，方便错误提示和 plan 解释：

```go
type SourceRef struct {
    File    string
    Line    int
    Path    string // 例如 host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
    Imports []string
}
```

## HostSpec

`HostSpec.Name` 是主机名。

默认情况下：

```text
host 名 = SSH host 名
```

例如：

```hcl
host "ksvm213" {}
```

中间表达：

```yaml
name: ksvm213
ssh:
  host: ksvm213
state:
  path: /var/lib/debianform/state/ksvm213.yaml
  lock_path: /var/lock/debianform/state/ksvm213.lock
```

如果用户显式写 `ssh`，则覆盖默认连接信息：

```hcl
host "edge-1" {
  ssh {
    host = "10.0.0.11"
    port = 22
  }
}
```

中间表达：

```yaml
name: edge-1
ssh:
  host: 10.0.0.11
  port: 22
```

## SystemSpec

`SystemSpec` 保存主机基础属性和规范架构名：

```go
type SystemSpec struct {
    Hostname     string
    Architecture string
    Codename     string
    Timezone     string
    Locale       string
    Source       SourceRef
}
```

`Hostname` 默认等于 host label。`Architecture` 统一使用 `amd64`、`arm64` 等
DebianForm 名称，不能让 component compiler 直接处理 `x86_64`、`aarch64`、
Debian architecture 和 Go architecture 的多套别名。`Codename` 使用 Debian
release codename，例如 `bookworm` 或 `trixie`。

如果 DSL 没有显式 architecture/codename，离线生成的 HostSpec 可以暂时标记为
unknown；连接目标并探测后，必须在 component 表达式求值、source 选择和
ResourceGraph 编译前完成归一化。

## StateSpec

StateSpec 是 DebianForm 自己的远端状态配置，不等同于 NixOS 的
`system.stateVersion`。

```go
type StateSpec struct {
    Path     string
    LockPath string
}
```

默认值：

```text
path      = /var/lib/debianform/state/<host>.yaml
lock_path = /var/lock/debianform/state/<host>.lock
```

中间表达中必须始终有完整的 `StateSpec`，这样后续执行层不需要再推默认值。

StateSpec 只描述 state 文件位置，不描述 state 文件内容。

运行时 state 建议使用规范 YAML，由 DebianForm 机器写入：

```yaml
version: 2
host: ksvm213
serial: 17
updated_at: "2026-06-19T12:00:00Z"
resources:
  host.ksvm213.packages.install["curl"]:
    kind: package
    provider: debian_package
    identity:
      name: curl
    ownership: created
    desired:
      ensure: present
    observed:
      version: "8.14.1-2"
```

state 记录的是 DebianForm 的管辖事实，不是 HostSpec 的完整拷贝。

`ownership` 决定资源从配置中消失后的处理方式：

```text
created   DebianForm 创建或安装；从配置删除后销毁。
adopted   接管前已经存在；从配置删除后解除管辖。
external  只用于依赖或观测；从配置删除后只丢弃记录。
```

lock 文件建议也是 YAML，但它不是 state 的一部分，只保存租约：

```yaml
version: 1
host: ksvm213
operation: apply
owner:
  user: mofe
  hostname: macbook
  pid: 12345
token: "random-128-bit-token"
created_at: "2026-06-19T12:00:00Z"
expires_at: "2026-06-19T12:05:00Z"
state_path: "/var/lib/debianform/state/ksvm213.yaml"
```

释放 lock 必须校验 token，避免误删其他进程持有的锁。

## KernelSpec

KernelSpec 表达内核模块和 sysctl 目标状态。

```go
type KernelSpec struct {
    Modules []KernelModuleSpec
    Sysctl  map[string]SysctlSpec
}

type KernelModuleSpec struct {
    Name    string
    Persist bool
    Ensure  Ensure
    Source  SourceRef
}

type SysctlSpec struct {
    Key          string
    Value        string
    Persist      bool
    ApplyRuntime bool
    Source       SourceRef
}
```

用户 DSL：

```hcl
kernel {
  modules = ["tcp_bbr"]

  sysctl = {
    "net.core.default_qdisc" = "fq"
    "net.ipv4.tcp_congestion_control" = "bbr"
  }
}
```

中间表达：

```yaml
kernel:
  modules:
    - name: tcp_bbr
      persist: true
      ensure: present
  sysctl:
    net.core.default_qdisc:
      key: net.core.default_qdisc
      value: fq
      persist: true
      apply_runtime: true
    net.ipv4.tcp_congestion_control:
      key: net.ipv4.tcp_congestion_control
      value: bbr
      persist: true
      apply_runtime: true
```

编译到资源图时再展开为：

```text
debian_kernel_module.tcp_bbr
debian_sysctl.bbr_qdisc
debian_sysctl.bbr_congestion_control
```

## PackageSpec

PackageSpec 表达 DebianForm 管辖的已安装包集合，不直接暴露 apt 命令。

```go
type PackageSpec struct {
    Install []PackageItem
}

type PackageItem struct {
    Name         string
    Repositories []string
    Source       SourceRef
}
```

用户 DSL：

```hcl
packages {
  install = ["curl", "vim"]
}
```

中间表达：

```yaml
packages:
  install:
    - name: curl
    - name: vim
```

编译到资源图：

```text
debian_package.curl
debian_package.vim
```

包从配置中删除时，是否卸载由 state 中的 ownership 决定：

```text
created   由 DebianForm 安装；删除配置后卸载。
adopted   接管前已经安装；删除配置后解除管辖，默认不卸载。
external  只作为依赖或观测对象；不卸载。
```

需要指定软件来源时，用户 DSL 使用 package object：

```hcl
packages {
  package "bird2" {
    repositories = ["cznic_bird2"]
  }
}
```

归一化后：

```yaml
packages:
  install:
    - name: bird2
      repositories:
        - cznic_bird2
```

`repositories` 保存逻辑 repository label，而不是低阶 source 文件路径。编译器在合并
host、profile、component 后解析引用并生成依赖边。

## APTSpec

APT repository 在中间表达中保持结构化：

```go
type APTSpec struct {
    Repositories map[string]APTRepositorySpec
}

type APTRepositorySpec struct {
    Name          string
    URIs          []string
    Suites        []string
    Components    []string
    Architectures []string
    SigningKey    *APTSigningKeySpec
    Ensure        Ensure
    Source        SourceRef
}

type APTSigningKeySpec struct {
    URL       string
    Content   string
    SHA256    string
    Path      string
    Sensitive bool
    Source    SourceRef
}
```

编译时每个 repository 至少展开为：

```text
optional signing key
  -> deb822 source file
  -> host-scoped APT cache refresh
```

同一 host 的多个 repository 不能分别生成独立的 `apt-get update` 节点。编译器应将
它们汇聚到一个稳定地址：

```text
host.server1.apt.cache_refresh
```

package 的 `Repositories` 只建立到所引用 repository 的依赖。若这些 repository
发生变化，package 还必须依赖汇聚后的 cache refresh。未引用该 repository 的 package
不应产生这条边。

## FileSpec

文件建议在中间表达里保持结构化，不要提前变成 shell 命令。

```go
type FileSpec struct {
    Files map[string]ManagedFile
}

type ManagedFile struct {
    Path       string
    Content    string
    SourcePath string
    Owner      string
    Group      string
    Mode       string
    Sensitive  bool
    Ensure     Ensure
    Source     SourceRef
}
```

用户 DSL 使用带路径 label 的对象块：

```hcl
files {
  file "/etc/motd" {
    content = "hello\n"
    mode    = "0644"
  }
}
```

`file` block label 是稳定 identity。合并层按 label 归一化成 map，中间表达统一为
`ManagedFile`。同样的规则用于 `directory`、`user`、`group`、systemd unit 和
networkd object。

`Sensitive` 为 true 时，中间表达可以在当前进程内持有渲染所需内容，但 plan、
日志和持久化 state 不得输出明文。state 只允许保存内容 hash、长度和远端 identity。

## ComponentSpec

顶层 `component` 是部署单元模板，不是 provider resource，也不是可独立 apply 的对象。
它需要先归一化，再针对引用它的 host 选择架构 source，形成
`ComponentInstanceSpec`。

建议模型：

```go
type ComponentTemplateSpec struct {
    Name    string
    Type    ArtifactType
    Version string
    Sources map[string]DownloadSourceSpec // key 为空表示架构无关
    Extract *ExtractSpec
    Install *InstallSpec

    APT         APTSpec
    Packages    PackageSpec
    Services    ServiceSpec
    Groups      GroupSpec
    Users       UserSpec
    Files       FileSpec
    Directories DirectorySpec
    Systemd     SystemdSpec

    Source SourceRef
}

type ComponentInstanceSpec struct {
    Name         string
    Type         ArtifactType
    Version      string
    Host         string
    Architecture string

    SelectedSource *DownloadSourceSpec
    Extract        *ExtractSpec
    Install        *InstallSpec

    APT         APTSpec
    Packages    PackageSpec
    Services    ServiceSpec
    Groups      GroupSpec
    Users       UserSpec
    Files       FileSpec
    Directories DirectorySpec
    Systemd     SystemdSpec

    Source SourceRef
}

type DownloadSourceSpec struct {
    Architecture string
    URL          string
    SHA256       string
    Source       SourceRef
}

type ExtractSpec struct {
    Format          string
    StripComponents int
    Include         string
}

type InstallSpec struct {
    Path  string
    Owner string
    Group string
    Mode  string
}
```

归一化规则：

- `source` 无 label 时，`Architecture` 为空并可用于所有 host。
- 多架构 source 必须按 `HostSpec.System.Architecture` 精确选择。
- host 没有显式 architecture 时，可以在连接后探测，但 plan 前必须得到规范架构名；
  离线 validate 只能完成与架构无关的检查。
- component 可以没有 artifact 字段，只封装 APT、package、service 等领域对象。
- component 实例化表达式可以读取只读 `target`，其值是完成 profile merge 和默认值
  填充后的 HostSpec；例如 repository suite 可以来自 `target.system.codename`。
- `binary`、`archive`、`file`、`ca_certificate` artifact 各自使用独立 schema
  校验，不能把
  `type` 当成完全动态的弱类型 map。
- component 的内部领域对象保留 component 来源地址，不能伪装成 host 自身声明。
- component 实例不能直接修改 host 的 `ssh`、`state` 或 profile merge 结果。

component 实例的中间地址示例：

```text
host.server1.components.rclone.download
host.server1.components.rclone.install["/usr/local/bin/rclone"]
host.server1.components.myapp.users.user["myapp"]
host.server1.components.myapp.files.file["/etc/myapp/config.yaml"]
host.server1.components.myapp.systemd.service["myapp.service"]
```

component 内部依赖在编译时按领域语义推导：

```text
download -> verify checksum -> extract -> install
repository key -> repository source -> host APT cache refresh -> package -> service
group -> user
user/install -> owned directory and file
install/config -> systemd unit
systemd unit -> enabled/running service
```

如果 component 与 host/profile 或另一个 component 生成相同远端 identity，例如同一
repository label、文件路径、用户、group 或 systemd unit，编译前必须报冲突。
component 声明顺序不能
作为覆盖优先级。

## Users 和 Groups

Users 和 Groups 应该在中间表达中保留名称关系，方便编译前验证。

```go
type UserSpec struct {
    Users map[string]ManagedUser
}

type GroupSpec struct {
    Groups map[string]ManagedGroup
}

type ManagedUser struct {
    Name              string
    UID               string
    PrimaryGroup      string
    Groups            []string
    System            bool
    Home              string
    Shell             string
    SSHAuthorizedKeys []string
    Ensure            Ensure
    Source            SourceRef
}

type ManagedGroup struct {
    Name   string
    GID    string
    System bool
    Ensure Ensure
    Source SourceRef
}
```

编译器可以推导：

```text
user.primary_group 引用 group -> user depends_on group
authorized_key.user -> authorized_key depends_on user
```

## ServiceSpec 和 SystemdSpec

服务和 systemd unit 应该在中间表达中分开。

```go
type SystemdSpec struct {
    Units map[string]SystemdUnit
}

type ServiceSpec struct {
    Services map[string]ManagedService
}

type SystemdUnit struct {
    Name    string
    Content string
    SourcePath string
    Owner   string
    Group   string
    Mode    string
    Source  SourceRef
}

type ManagedService struct {
    Name    string
    Package string
    Enabled *bool
    State   ServiceState
    Source  SourceRef
}
```

编译器可以推导：

```text
service.package 匹配 package -> service depends_on package
service.name 匹配 systemd unit -> service depends_on systemd unit
```

## Ensure

中间表达应使用统一枚举表达存在性：

```go
type Ensure string

const (
    EnsurePresent Ensure = "present"
    EnsureAbsent  Ensure = "absent"
)
```

用户层不同领域可以有不同语法，例如 `disable`、`ensure = "absent"` 等，
但进入中间表达后必须归一到统一枚举。packages 第一版不提供 `remove` 字段。

## Merge Modifier 的归一化

`force`、`before`、`after`、`unset` 不应该出现在最终中间表达中。

它们只属于合并层：

```text
DSL value + modifier
  -> merge algorithm
  -> final plain value
  -> HostSpec
```

例如：

```hcl
profile "base" {
  packages {
    install = ["curl", "vim"]
  }
}

host "ksvm213" {
  imports = [profile.base]

  packages {
    install = force(["curl"])
  }
}
```

中间表达只应该看到：

```yaml
packages:
  install:
    - name: curl
```

不应该保留：

```text
force(["curl"])
```

## 地址设计

中间表达需要稳定地址，用于：

- 错误提示
- plan 输出
- state 地址映射
- 用户理解低阶资源来自哪里

建议中间地址格式：

```text
host.<host>.<domain>.<kind>[<key>]
```

示例：

```text
host.ksvm213.kernel.module["tcp_bbr"]
host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
host.ksvm213.packages.install["curl"]
host.ksvm213.files.file["/etc/motd"]
host.ksvm213.services.service["nginx"]
host.server1.components.rclone.install["/usr/local/bin/rclone"]
host.router1.components.bird2.apt.repository["cznic_bird2"]
host.router1.apt.cache_refresh
```

编译成低阶资源时，低阶资源应记录来源地址：

```text
compiled resource address: debian_sysctl.ksvm213_bbr_congestion_control
source address:           host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
```

v2 不需要兼容 v1 state address。第一版建议 state 优先记录中间地址，并保留低阶
resource address 作为调试信息。这样 plan、state 和用户配置的主地址保持一致。

## 编译到资源图

中间表达到低阶资源图的编译应是单向、确定性的。

输入：

```text
Program/HostSpec
```

输出：

```text
ResourceGraph
  nodes: provider resources
  edges: dependencies
```

编译器负责：

- 展开领域配置到低阶资源。
- 展开每个 host 挂载的 `ComponentInstanceSpec`。
- 生成稳定资源地址。
- 生成显式依赖边。
- 生成语义依赖边。
- 保留 source address。

编译结果示例：

```text
HostSpec:
  host.ksvm213.kernel.modules = ["tcp_bbr"]
  host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"] = "bbr"

ResourceGraph:
  node debian_kernel_module.ksvm213_tcp_bbr
    source = host.ksvm213.kernel.module["tcp_bbr"]

  node debian_sysctl.ksvm213_net_ipv4_tcp_congestion_control
    source = host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
    depends_on = [debian_kernel_module.ksvm213_tcp_bbr]
```

## 编译前验证

中间表达生成后，编译前应做领域级验证：

- 每个 host 必须有 SSH host。
- 每个 host 必须有完整 state path 和 lock path。
- packages.install 中包名不能重复。
- package 引用的 repository 必须在同一 host 的最终 APTSpec 中存在。
- repository label 必须唯一；远程 signing key 必须有 SHA-256。
- file 同一路径不能出现冲突定义。
- sensitive file 不能把明文写入 plan 或 state。
- user 引用的 primary group 如果在配置内声明，应存在。
- service 引用的 package 如果在配置内声明，应能解析。
- sysctl key 不能为空。
- kernel module 名不能为空。
- host 引用的 component 必须存在且不能重复实例化。
- host 架构必须能选择唯一 component source。
- component 的 URL、SHA-256、安装路径和解压参数必须通过对应类型校验。
- host/profile/component 之间不能产生远端 identity 冲突。

这些错误应该指向用户 DSL 的来源位置，而不是指向低阶 provider。

## 编译后验证

资源图生成后，应做执行级验证：

- 资源地址唯一。
- 依赖地址存在。
- 图无环。
- 同一低阶资源不能被两个中间地址重复生成。
- 同一 host 内会产生冲突的资源应报错。

## 调度关系

中间表达不负责调度。

调度只发生在资源图层：

```text
HostSpec -> ResourceGraph -> DAG waves -> apply
```

但中间表达要提供足够信息，让编译器能生成正确依赖边。

例如 BBR：

```text
kernel.sysctl["net.ipv4.tcp_congestion_control"] = "bbr"
kernel.modules contains "tcp_bbr"
```

编译器据此生成：

```text
sysctl depends_on module
```

## Plan 展示

Plan 不应该只展示低阶资源地址，否则用户会看到实现细节。

建议 plan 同时展示用户来源和低阶动作：

```text
Plan:
  ~ host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
    update debian_sysctl.ksvm213_net_ipv4_tcp_congestion_control
    sysctl -w net.ipv4.tcp_congestion_control=bbr and write /etc/sysctl.d/...
```

## 与 v1 的关系

v2 是破坏式重设计，不要求渐进式迁移，也不要求 v1 `debian_*` 配置继续作为用户语法
存在。

v1 的 provider 实现可以作为内部执行代码复用，但不能决定 v2 的用户模型。

v2 的唯一主路径是：

```text
host/profile/component DSL -> HostSpec -> ResourceGraph -> plan/apply
```

如果后续需要调试或开发者逃生口，也应明确标记为内部能力，不能成为普通用户文档中的
主要配置方式。

## 非目标

中间表达第一版不做：

- 保存到磁盘作为公开格式
- 作为稳定外部 API
- 表达任意 HCL AST
- 表达 shell 命令
- 实现 NixOS 完整 option priority

中间表达首先是内部编译边界，用于让 v2 语义清晰、可验证、可解释。
