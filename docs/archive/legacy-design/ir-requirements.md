<p align="right"><strong>English</strong> | <a href="ir-requirements.zh.md">简体中文</a></p>

# DebianForm Intermediate Representation Requirements

## Goals

DebianForm needs an explicit intermediate representation between the user DSL and the final low-level resources.

The intermediate representation supports the following workflow:

```text
用户 DSL
  -> 解析 HCL
  -> 合并 profile 和 host override
  -> 归一化 component 并挂载到 host
  -> 生成中间表达
  -> 编译成低阶资源图
  -> plan/apply
```

The intermediate representation should not be identical to the final provider resources. It should retain the domain semantics of a host's desired state while being more regular, easier to validate, and easier to compile than the user DSL.

## Layers

The current design should have four layers:

```text
DSL 层
  用户写的 HCL：host、profile、component、kernel、packages、services 等。

合并层
  解析 imports、执行 merge modifier、得到每台 host 的最终领域配置。

中间表达层
  归一化后的 HostSpec 和 ComponentInstanceSpec，仍然表达主机目标状态。

资源图层
  展开成 ResourceGraph，也就是 执行层能处理的资源 DAG。
```

Core principles:

```text
DSL 负责好写
中间表达负责好检查
资源图负责好执行
```

## Why an Intermediate Representation Is Needed

Translating the user DSL directly into provider resources would create several problems:

- Profile merge logic would be scattered across providers.
- Plan output would struggle to explain which host, profile, or component produced a low-level resource.
- Adding new domain blocks could easily affect the execution layer.
- User-facing semantics would be coupled too tightly to provider implementation details.
- Cross-domain validation before compilation, such as a service referencing a package or a user referencing a group, would be difficult.

An intermediate representation isolates these concerns:

- The user layer can continue to evolve.
- The provider layer can remain stable.
- The compiler expands the domain model into a resource graph.
- The scheduler only needs to understand the resource graph and dependency edges.

## Role of the Intermediate Representation

The intermediate representation should be the normalized desired state of each host.

It is not:

```text
不是 HCL AST
不是 provider resource
不是 SSH 命令列表
不是 state 文件格式
```

It is:

```text
合并完成后的 host 配置
字段类型已经规整
默认值已经填充
引用已经解析
unset/force/before/after 已经生效
仍保留 kernel/packages/files/services 等领域结构
```

## Proposed Data Model

The top-level type can be named `Program`:

```go
type Program struct {
    Hosts      []HostSpec
    Components map[string]ComponentTemplateSpec
}
```

Each host corresponds to one `HostSpec`:

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
    Secrets     SecretSpec
    Directories DirectorySpec
    Users       UserSpec
    Groups      GroupSpec
    Services    ServiceSpec
    Systemd     SystemdSpec
    Nftables    NftablesSpec

    Components []ComponentInstanceSpec
}
```

`SourceRef` retains source information for useful error messages and plan explanations:

```go
type SourceRef struct {
    File    string
    Line    int
    Path    string // 例如 host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
    Imports []string
}
```

`LifecycleSpec` contains the optional protection rules available to every domain object that produces a long-lived resource:

```go
type LifecycleSpec struct {
    PreventDestroy bool
    Source         SourceRef
}
```

The first version supports only `PreventDestroy`. It is compiled into ResourceGraph nodes to block delete, destroy, and replace actions without affecting ordinary updates.

## HostSpec

`HostSpec.Name` is the host name.

By default:

```text
host 名 = SSH host 名
```

For example:

```hcl
host "ksvm213" {}
```

Intermediate representation:

```yaml
name: ksvm213
ssh:
  host: ksvm213
state:
  path: /var/lib/debianform/state/ksvm213.json
  lock_path: /var/lock/debianform/state/ksvm213.lock
```

If the user explicitly specifies `ssh`, it overrides the default connection information:

```hcl
host "edge-1" {
  ssh {
    host = "10.0.0.11"
    port = 22
  }
}
```

Intermediate representation:

```yaml
name: edge-1
ssh:
  host: 10.0.0.11
  port: 22
```

## SystemSpec

`SystemSpec` stores fundamental host properties and canonical architecture names:

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

`Hostname` defaults to the host label. `Architecture` and `Codename` are runtime facts: online `plan`, `check`, and `apply` operations discover them after connecting to the target, then inject them into HostSpec before evaluating component expressions, selecting sources, and compiling the ResourceGraph. `Architecture` consistently uses DebianForm names such as `amd64` and `arm64`; the component compiler must not handle separate alias sets for `x86_64`, `aarch64`, Debian architectures, and Go architectures directly. `Codename` uses the Debian release codename, such as `bookworm` or `trixie`. If the DSL explicitly declares an architecture or codename, the discovered value must match the declared value.

## StateSpec

StateSpec configures DebianForm's own remote state. It is not equivalent to NixOS `system.stateVersion`.

```go
type StateSpec struct {
    Path     string
    LockPath string
}
```

Defaults:

```text
path      = /var/lib/debianform/state/<host>.json
lock_path = /var/lock/debianform/state/<host>.lock
```

The intermediate representation must always contain a complete `StateSpec`, so the execution layer does not need to apply defaults again.

StateSpec describes only the location of the state file, not its contents.

Runtime state uses canonical JSON written by DebianForm:

```json
{
  "version": 2,
  "host": "ksvm213",
  "serial": 17,
  "updated_at": "2026-06-19T12:00:00Z",
  "resources": {
    "host.ksvm213.packages.install[\"curl\"]": {
      "kind": "package",
      "provider_type": "package",
      "ownership": "managed",
      "desired_digest": "sha256-summary",
      "observed": {
        "installed": true
      },
      "order": 0
    }
  }
}
```

State records the facts under DebianForm's management, not a complete copy of HostSpec.

`ownership` determines what happens after a resource disappears from the configuration:

```text
created   DebianForm 创建或安装；从配置删除后销毁。
adopted   接管前已经存在；从配置删除后解除管辖。
external  只用于依赖或观测；从配置删除后只丢弃记录。
```

The lock file is also JSON, but it is not part of state and stores only the lease:

```json
{
  "owner": "dbf",
  "pid": "12345",
  "token": "random-128-bit-token",
  "expires_at": "2026-06-19T12:05:00Z",
  "expires_at_unix": 1781870700
}
```

Releasing a lock must validate the token to avoid deleting a lock held by another process.

## KernelSpec

KernelSpec expresses the desired state of kernel modules and sysctls.

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

User DSL:

```hcl
kernel {
  modules = ["tcp_bbr"]

  sysctl = {
    "net.core.default_qdisc" = "fq"
    "net.ipv4.tcp_congestion_control" = "bbr"
  }
}
```

Intermediate representation:

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

It is expanded into the following resources only when compiled into the resource graph:

```text
kernel_module.tcp_bbr
sysctl.bbr_qdisc
sysctl.bbr_congestion_control
```

## PackageSpec

PackageSpec expresses the set of installed packages managed by DebianForm without directly exposing apt commands.

```go
type PackageSpec struct {
    Install []PackageItem
}

type PackageItem struct {
    Name         string
    Repositories []string
    Lifecycle    LifecycleSpec
    Source       SourceRef
}
```

User DSL:

```hcl
packages {
  install = ["curl", "vim"]
}
```

Intermediate representation:

```yaml
packages:
  install:
    - name: curl
    - name: vim
```

Compiled resource graph:

```text
package.curl
package.vim
```

When a package is removed from the configuration, state ownership determines whether it is uninstalled:

```text
created   由 DebianForm 安装；删除配置后卸载。
adopted   接管前已经安装；删除配置后解除管辖，默认不卸载。
external  只作为依赖或观测对象；不卸载。
```

When a software source must be specified, the user DSL uses a package object:

```hcl
packages {
  package "bird2" {
    repositories = ["cznic_bird2"]
  }
}
```

After normalization:

```yaml
packages:
  install:
    - name: bird2
      repositories:
        - cznic_bird2
```

`repositories` stores logical repository labels rather than low-level source-file paths. After merging the host, profiles, and components, the compiler resolves references and generates dependency edges.

## APTSpec

APT repositories remain structured in the intermediate representation:

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
    Lifecycle     *LifecycleSpec
    Source        SourceRef
}

type APTSigningKeySpec struct {
    URL     string
    Content string
    SHA256  string
    Path    string
    Source  SourceRef
}
```

During compilation, each repository expands into at least:

```text
optional signing key
  -> deb822 source file
  -> host-scoped APT cache refresh
```

Multiple repositories on the same host must not generate separate `apt-get update` nodes. The compiler should converge them into one stable address:

```text
host.server1.apt.cache_refresh
```

A package's `Repositories` establishes dependencies only on the referenced repositories. If any of those repositories change, the package must also depend on the converged cache refresh. Packages that do not reference the repository should not gain this edge.

## NftablesSpec

NftablesSpec expresses the main nftables ruleset and snippet files managed by DebianForm. The intermediate representation does not attempt to parse the nft language itself; nft syntax is validated with `nft -c -f`, while DebianForm manages files, activation, and structured plan diffs.

```go
type NftablesSpec struct {
    Enable bool
    Main   *NftablesFileSpec
    Files  map[string]NftablesFileSpec
    Source SourceRef
}

type NftablesFileSpec struct {
    Label     string
    Path      string
    Content   string
    SourcePath string
    Owner     string
    Group     string
    Mode      string
    Sensitive bool
    Validate  bool
    Activate  bool
    Ensure    Ensure
    Lifecycle LifecycleSpec
    Source    SourceRef
}
```

User DSL:

```hcl
nftables {
  enable = true

  main {
    content = <<-EOF
      flush ruleset
      include "/etc/nftables.d/*.nft"
    EOF
  }

  file "20-services" {
    content = <<-EOF
      table inet filter {
        chain input {
          type filter hook input priority 0; policy drop;
          tcp dport { 22, 80, 443 } accept
        }
      }
    EOF
  }
}
```

Intermediate representation:

```yaml
nftables:
  enable: true
  main:
    label: main
    path: /etc/nftables.conf
    validate: true
    activate: true
  files:
    20-services:
      label: 20-services
      path: /etc/nftables.d/20-services.nft
      owner: root
      group: root
      mode: "0644"
      validate: true
      activate: true
```

Compilation should generate:

```text
host.server1.nftables.file["main"]
host.server1.nftables.file["20-services"]
host.server1.nftables.validate
host.server1.nftables.activate
```

When several nftables files change, `validate` and `activate` should each converge into one operation on the same host so that snippets do not reload the rules independently. `main` defaults to `/etc/nftables.conf`; `file "<label>"` defaults to `/etc/nftables.d/<label>.nft`. The intermediate representation stores only the final content digest or source path; when `Sensitive` is true, plaintext must not be written to plan or state.

## FileSpec

Files should remain structured in the intermediate representation rather than being converted to shell commands early.

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
    Lifecycle  LifecycleSpec
    Source     SourceRef
}
```

The user DSL uses an object block labeled with a path:

```hcl
files {
  file "/etc/motd" {
    content = "hello\n"
    mode    = "0644"
  }
}
```

The `file` block label is its stable identity. The merge layer normalizes blocks into a map by label, and the intermediate representation uniformly uses `ManagedFile`. The same rule applies to `directory`, `user`, `group`, systemd units, and networkd objects.

When `Sensitive` is true, the intermediate representation may hold content needed for rendering in the current process, but plans, logs, and persistent state must not emit plaintext. State may store only the content hash, length, and remote identity.

## SecretSpec

SecretSpec is the semantic entry point for sensitive files. Both it and FileSpec can ultimately compile into low-level file resources, but SecretSpec prohibits plaintext in plans, state, and logs by default.

```go
type SecretSpec struct {
    Files map[string]SecretFileSpec
}

type SecretFileSpec struct {
    Path       string
    SourcePath string
    Owner      string
    Group      string
    Mode       string
    Ensure     Ensure
    Lifecycle  LifecycleSpec
    Source     SourceRef
}
```

User DSL:

```hcl
secrets {
  file "/etc/wireguard/private.key" {
    source = "secrets/server1-wireguard.key"
    owner  = "root"
    group  = "systemd-network"
    mode   = "0640"
  }
}
```

Intermediate representation:

```yaml
secrets:
  files:
    /etc/wireguard/private.key:
      path: /etc/wireguard/private.key
      source_path: secrets/server1-wireguard.key
      owner: root
      group: systemd-network
      mode: "0640"
      ensure: present
```

SecretSpec stores only the local source path and target metadata. At execution time, DebianForm can read the source content and calculate a hash; plans, state, and logs may store only summaries such as the hash, length, and whether it changed.

## ComponentSpec

A top-level `component` is a deployment-unit template, not a provider resource or an independently applicable object. It must first be normalized and then select an architecture-specific source for each host that references it, producing a `ComponentInstanceSpec`.

Proposed model:

```go
type ComponentTemplateSpec struct {
    Name    string
    Type    ArtifactType
    Version string
    Inputs  map[string]ComponentInputSpec
    Sources map[string]DownloadSourceSpec // key 为空表示架构无关
    Extract *ExtractSpec
    Install *InstallSpec

    APT         APTSpec
    Packages    PackageSpec
    Services    ServiceSpec
    Groups      GroupSpec
    Users       UserSpec
    Files       FileSpec
    Secrets     SecretSpec
    Directories DirectorySpec
    Systemd     SystemdSpec
    Nftables    NftablesSpec

    Source SourceRef
}

type ComponentInstanceSpec struct {
    Name         string
    Template     string
    Type         ArtifactType
    Version      string
    Host         string
    Architecture string
    InputValues  map[string]Value

    SelectedSource *DownloadSourceSpec
    Extract        *ExtractSpec
    Install        *InstallSpec

    APT         APTSpec
    Packages    PackageSpec
    Services    ServiceSpec
    Groups      GroupSpec
    Users       UserSpec
    Files       FileSpec
    Secrets     SecretSpec
    Directories DirectorySpec
    Systemd     SystemdSpec
    Nftables    NftablesSpec

    Source SourceRef
}

type ComponentInputSpec struct {
    Name        string
    Type        string
    TypeSpec    ComponentInputTypeSpec
    Description string
    Default     *Value
    Sensitive   bool
    Nullable    bool
    Source      SourceRef
}

type ComponentInputTypeSpec struct {
    Kind       string
    Element    *ComponentInputTypeSpec
    Attributes map[string]ComponentObjectAttributeSpec
    Tuple      []ComponentInputTypeSpec
}

type ComponentObjectAttributeSpec struct {
    Type     ComponentInputTypeSpec
    Optional bool
    Default  *Value
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

A parameterless host reference:

```hcl
components = [
  component.rclone,
]
```

Normalizes to:

```yaml
components:
  - name: rclone
    template: rclone
    input_values: {}
```

Use an in-host component instance block when arguments or multiple instances of the same template are needed:

```hcl
component "api" {
  source = component.myapp

  inputs = {
    listen_addr = "127.0.0.1:8080"
  }
}
```

Normalizes to:

```yaml
components:
  - name: api
    template: myapp
    input_values:
      listen_addr: "127.0.0.1:8080"
```

When instantiating a component, the compiler first validates inputs and then evaluates expressions inside the component with a read-only `input.<name>` context. `target` continues to represent the read-only view of the host after profile merging and default application. Neither `input` nor `target` can modify the host.

Normalization rules:

- When `source` has no label, `Architecture` is empty and the source can be used for every host.
- Multiple architecture sources must be selected by an exact match against `HostSpec.System.Architecture`.
- When a host has no explicit architecture, online facts discovery detects it; `plan --offline` can fully process only components that do not depend on runtime facts.
- A component can omit artifact fields and package only domain objects such as APT repositories, packages, and services.
- Component instantiation expressions can read the read-only `target`, whose value is the HostSpec after profile merging and default application; for example, a repository suite can come from `target.system.codename`.
- The `binary`, `archive`, `file`, and `ca_certificate` artifacts each use an independent schema validation; `type` must not be treated as a completely dynamic, weakly typed map.
- Domain objects inside a component retain the component source address and must not masquerade as declarations originating from the host itself.
- A component instance cannot directly modify the host's `ssh`, `state`, or profile merge result.

Examples of intermediate addresses inside component instances:

```text
host.server1.components.rclone.download
host.server1.components.rclone.install["/usr/local/bin/rclone"]
host.server1.components.myapp.users.user["myapp"]
host.server1.components.myapp.files.file["/etc/myapp/config.yaml"]
host.server1.components.myapp.systemd.service["myapp.service"]
```

Dependencies inside a component are inferred from domain semantics at compile time:

```text
download -> verify checksum -> extract -> install
repository key -> repository source -> host APT cache refresh -> package -> service
group -> user
user/install -> owned directory and file
install/config -> systemd unit
systemd unit -> enabled/running service
```

If a component produces the same remote identity as the host, a profile, or another component, such as the same repository label, file path, user, group, or systemd unit, compilation must report a conflict. Component declaration order must not act as override precedence.

## Users and Groups

Users and Groups should retain name relationships in the intermediate representation so they can be validated before compilation.

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

The compiler can infer:

```text
user.primary_group 引用 group -> user depends_on group
authorized_key.user -> authorized_key depends_on user
```

## ServiceSpec and SystemdSpec

Services and systemd units should remain separate in the intermediate representation.

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

The compiler can infer:

```text
service.package 匹配 package -> service depends_on package
service.name 匹配 systemd unit -> service depends_on systemd unit
```

## Ensure

The intermediate representation should use one enum consistently to express presence:

```go
type Ensure string

const (
    EnsurePresent Ensure = "present"
    EnsureAbsent  Ensure = "absent"
)
```

Different domains can have different user-layer syntax, such as `disable` or `ensure = "absent"`, but they must be normalized into the same enum before entering the intermediate representation. The first package implementation does not provide a `remove` field.

## Normalizing Merge Modifiers

`force`, `before`, `after`, and `unset` must not appear in the final intermediate representation.

They belong only to the merge layer:

```text
DSL value + modifier
  -> merge algorithm
  -> final plain value
  -> HostSpec
```

For example:

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

The intermediate representation should contain only:

```yaml
packages:
  install:
    - name: curl
```

It must not retain:

```text
force(["curl"])
```

## Address Design

The intermediate representation needs stable addresses for:

- Error messages
- Plan output
- State address mapping
- Helping users understand where a low-level resource came from

Proposed intermediate address format:

```text
host.<host>.<domain>.<kind>[<key>]
```

Examples:

```text
host.ksvm213.kernel.module["tcp_bbr"]
host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
host.ksvm213.packages.install["curl"]
host.ksvm213.files.file["/etc/motd"]
host.ksvm213.services.service["nginx"]
host.server1.components.rclone.install["/usr/local/bin/rclone"]
host.router1.components.bird2.apt.repository["cznic_bird2"]
host.router1.apt.cache_refresh
host.server1.nftables.file["20-services"]
host.server1.nftables.activate
```

When compiled into a low-level resource, that resource should record the source address:

```text
compiled resource address: sysctl.ksvm213_bbr_congestion_control
source address:           host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
```

The current design does not need to remain compatible with old state addresses. In the first version, state should primarily record intermediate addresses and retain the low-level resource address as debugging information. This keeps the primary addresses in plans, state, and user configuration aligned.

## Compiling to the Resource Graph

Compilation from the intermediate representation into the low-level resource graph should be one-way and deterministic.

Input:

```text
Program/HostSpec
```

Output:

```text
ResourceGraph
  nodes: provider resources and operation nodes
  edges: dependencies
```

The compiler is responsible for:

- Expanding domain configuration into low-level resources.
- Expanding the `ComponentInstanceSpec` objects attached to each host.
- Generating stable resource addresses.
- Generating explicit dependency edges.
- Generating semantic dependency edges.
- Retaining source addresses.

Proposed structure:

```go
type ResourceGraph struct {
    Nodes []GraphNode
    Edges []GraphEdge
}

type GraphNode struct {
    Address         string
    Kind            GraphNodeKind // resource or operation
    Source          SourceRef
    Lifecycle       LifecycleSpec
    ProviderAddress string
    ProviderPayload  any
    Operation       *OperationSpec
}

type OperationSpec struct {
    OperationKind  string
    CommandPreview string
    TriggeredBy    []string
    Scope          string // host, component, service, file 等
}
```

OperationNode examples:

```text
host.server1.apt.cache_refresh
host.server1.nftables.validate
host.server1.nftables.activate
host.server1.systemd.daemon_reload
host.server1.services.service["myapp"].restart
```

An OperationNode is a semantic DAG node, not an arbitrary shell hook. The compiler should converge duplicate operations within the same host or scope into one stable address and use `TriggeredBy` to record their trigger sources.

Example compilation result:

```text
HostSpec:
  host.ksvm213.kernel.modules = ["tcp_bbr"]
  host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"] = "bbr"

ResourceGraph:
  node kernel_module.ksvm213_tcp_bbr
    source = host.ksvm213.kernel.module["tcp_bbr"]

  node sysctl.ksvm213_net_ipv4_tcp_congestion_control
    source = host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
    depends_on = [kernel_module.ksvm213_tcp_bbr]
```

## Pre-Compilation Validation

After generating the intermediate representation and before compilation, DebianForm should perform domain-level validation:

- Every host must have an SSH host.
- Every host must have a complete state path and lock path.
- Package names in packages.install must not be duplicated.
- A repository referenced by a package must exist in the same host's final APTSpec and have `ensure = present`.
- Repository labels must be unique; a remote signing key must have a 64-character hexadecimal SHA-256.
- Nftables file labels must be unique, and final paths must not conflict.
- An nftables file must specify exactly one of `content` and `source_path`.
- There can be at most one nftables main ruleset; its default path is `/etc/nftables.conf`.
- Files must not have conflicting definitions for the same path.
- secrets.file and files.file must not manage the same remote path.
- A secret source must be readable, and its plaintext must not be written to plans, state, or logs.
- Sensitive files must not write plaintext to plans or state.
- The first lifecycle implementation allows only `prevent_destroy`; unknown lifecycle fields must produce an error.
- A user's primary group must exist if it is declared in the configuration.
- A service's package reference must resolve if the package is declared in the configuration.
- A sysctl key must not be empty.
- A kernel module name must not be empty.
- Components referenced by a host must exist and must not be instantiated more than once.
- Component input names must be unique and their types must be supported.
- A component instance's `source` must reference a declared component.
- A component instance must not provide undeclared inputs or omit inputs that have no default.
- Component instance input values must conform to the declared input types.
- The host architecture must select exactly one component source.
- A component's URL, SHA-256, installation path, and extraction arguments must pass validation for the corresponding type.
- Hosts, profiles, and components must not produce conflicting remote identities.

These errors should point to locations in the user DSL, not to low-level providers.

## Post-Compilation Validation

After generating the resource graph, DebianForm should perform execution-level validation:

- Resource addresses are unique.
- Referenced dependency addresses exist.
- The graph is acyclic.
- Two intermediate addresses cannot generate the same low-level resource.
- Conflicting resources within the same host produce an error.

## Scheduling Relationships

The intermediate representation is not responsible for scheduling.

Scheduling occurs only at the resource-graph layer:

```text
HostSpec -> ResourceGraph -> DAG waves -> apply
```

The intermediate representation must nevertheless provide enough information for the compiler to generate correct dependency edges.

For example, BBR:

```text
kernel.sysctl["net.ipv4.tcp_congestion_control"] = "bbr"
kernel.modules contains "tcp_bbr"
```

The compiler uses this information to generate:

```text
sysctl depends_on module
```

## Plan Model and Presentation

A plan should not show only low-level resource addresses, because that would expose implementation details to users.

A plan should first form a structured model, then render it as terminal text, JSON, or HTML. Proposed model:

```go
type Plan struct {
    Changes  []PlanChange
    Handlers []HandlerRun
}

type PlanChange struct {
    Address         string
    Action          ChangeAction
    Summary         string
    Source          SourceRef
    ProviderAddress string
    Diff            DiffNode
    LowLevelActions []LowLevelAction
}

type DiffNode struct {
    Path      []PathSegment
    Kind      DiffKind // object, map, set, list, scalar, text, sensitive
    Action    ChangeAction
    Before    any
    After     any
    Sensitive bool
    Children  []DiffNode
}
```

By default, terminal output shows the user source and field-level diffs:

```text
Plan:
  ~ host.server1.systemd.networkd.netdev["10-wg0"]
    source: examples/fleet.dbf.hcl:430

    ~ wireguard_peer["server2"]
      ~ PersistentKeepalive: 15 -> 25
      ~ AllowedIPs
        + "10.200.0.0/24"

  ~ host.server1.nftables.file["20-services"]
    source: examples/fleet.dbf.hcl:560

    ~ content
      - tcp dport { 22, 80 } accept
      + tcp dport { 22, 80, 443 } accept
```

Requirements:

- `PlanChange.Address` uses the stable intermediate address.
- `ProviderAddress` appears only in debug output.
- Objects and maps are diffed by key.
- Sets are diffed by element identity.
- Lists are diffed by index only when order is meaningful.
- Labeled block lists must be normalized into maps before entering the diff.
- Text content uses line-level diffs.
- Sensitive diffs may show only summaries such as `<sensitive>`, hashes, and lengths.
- The JSON renderer emits the structured plan directly.
- The HTML renderer reads the same structured plan and provides filtering, search, collapsing, and color markers.

## Relationship to the Old Experimental Format

The current design is a breaking redesign. It does not require gradual migration or continued support for the old `low-level provider resource` configuration as user syntax.

The current design has its own provider execution path. The old implementation is not a runtime dependency and must not determine the current user model.

The current design has one primary path:

```text
host/profile/component DSL -> HostSpec -> ResourceGraph -> plan/apply
```

If a debugging or developer escape hatch is needed later, it should be clearly marked as an internal capability and must not become the primary configuration method in ordinary user documentation.

## Non-Goals

The first intermediate-representation implementation does not:

- Persist to disk as a public format
- Serve as a stable external API
- Represent arbitrary HCL ASTs
- Represent shell commands
- Implement the complete NixOS option-priority model

The intermediate representation is first and foremost an internal compilation boundary that makes semantics clear, validatable, and explainable.
