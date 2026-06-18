# DebianForm 模块设计准则

本文档定义新增资源、provider、模块示例时必须遵循的设计标准。

核心目标是让用户声明系统事实，而不是编排执行步骤。DebianForm 运行在可变的
Debian 系统上，不可能真的没有执行顺序；但顺序应由资源语义和 provider 自动
推导，而不是由用户在 HCL 中手写一串脚本步骤。

## 1. 总原则

- 用户配置描述目标状态，不描述操作过程。
- 常规场景不要求用户写 `depends_on` 才能正确工作。
- provider 负责把高层目标状态展开为内部依赖图。
- `depends_on` 是逃生口，只用于引擎无法推断的隐藏依赖。
- 资源必须幂等、可预览、可检查漂移。
- 示例必须展示推荐抽象，不能把命令式脚本逐行翻译成资源链。

一句话标准：

```text
HCL 里应该看到“系统应该是什么样”，引擎内部才处理“先做什么再做什么”。
```

## 2. 模块是什么

本文中的“模块”不是单独的 HCL module 机制，而是一个面向用户的运维能力单元，
通常由以下部分组成：

- 一个或多个用户可见资源类型。
- 对应的 provider 读、比对、应用和删除逻辑。
- provider 生成的内部动作节点，例如 `apt-get update` 或 `systemctl daemon-reload`。
- provider 声明的依赖能力、互斥锁和触发关系。
- 文档、示例和测试。

新增模块时，不应只问“需要执行哪些命令”，而应先问：

- 用户真正想声明的长期事实是什么。
- 哪些底层步骤只是实现细节。
- 引擎能否从资源字段自动知道依赖关系。
- 哪些动作只应在相关资源实际变化后触发。

## 3. 不接受的设计

以下设计不应作为常规模块 API：

### 3.1 把脚本步骤暴露给用户

不推荐：

```hcl
debian_directory "keyrings" {
  path = "/etc/apt/keyrings"
}

debian_file "repo_key" {
  path = "/etc/apt/keyrings/vendor.asc"

  depends_on = [
    debian_directory.keyrings,
  ]
}

debian_apt_source "vendor" {
  signed_by = "/etc/apt/keyrings/vendor.asc"

  depends_on = [
    debian_file.repo_key,
  ]
}

debian_package "app" {
  update_cache = true

  depends_on = [
    debian_apt_source.vendor,
  ]
}
```

这个形态虽然可以幂等执行，但本质仍是命令式脚本的资源化版本。用户必须理解
APT keyring、source 文件、cache update 和包安装的执行顺序。

### 3.2 用字段让用户手动触发内部动作

不推荐：

```hcl
debian_package "bird2" {
  name         = "bird2"
  update_cache = true
}
```

`apt-get update` 是 APT 图的内部节点，不应该由每个包手动打开。provider 应从
同一主机上的 APT repository 变化推导出是否需要刷新 cache。

### 3.3 依赖文件声明顺序

配置文件顺序只能影响稳定输出和默认展示顺序，不能成为正确性的来源。

## 4. 推荐设计

推荐把常见流程提升成领域资源，让 provider 生成内部图。

例如安装来自第三方 APT 仓库的 BIRD2，应朝这个形态演进：

```hcl
debian_apt_repository "cznic_bird2" {
  host       = "bird_host"
  uris       = "https://pkg.labs.nic.cz/bird2"
  suites     = local.suite
  components = "main"

  key = {
    path    = "/etc/apt/keyrings/cznic.asc"
    content = file("keys/cznic.asc")
  }
}

debian_package "bird2" {
  host = "bird_host"
  name = "bird2"
}

debian_service "bird" {
  host    = "bird_host"
  name    = "bird"
  enabled = true
  state   = "running"
  package = "bird2"
}
```

用户声明的是：

- 有一个 APT 仓库。
- 仓库有签名 key。
- 主机需要安装 `bird2` 包。
- `bird` 服务应启用并运行。

provider 和引擎内部可以展开为：

```text
write /etc/apt/keyrings/cznic.asc
  -> write /etc/apt/sources.list.d/cznic_bird2.sources
  -> apt_update[bird_host]
  -> install package bird2
  -> enable/start service bird
```

这条顺序仍然存在，但它不出现在用户配置里。

## 5. 资源 API 标准

每个用户可见资源必须满足以下标准。

### 5.1 表达长期状态

资源应描述可重复读取的目标状态，例如：

- 包是否安装。
- 文件内容、owner、group、mode。
- 用户是否存在。
- systemd unit 是否 enabled 或 running。
- APT repository 是否存在。

不应把“一次执行成功”伪装成资源状态。

### 5.2 暴露领域字段，不暴露过程字段

字段应贴近用户要管理的对象，而不是底层命令参数。

推荐：

```hcl
debian_apt_repository "vendor" {
  uris       = "https://example.invalid/debian"
  suites     = "trixie"
  components = "main"
  key = {
    content = file("vendor.asc")
  }
}
```

不推荐：

```hcl
debian_file "vendor_key" {}
debian_apt_source "vendor" {}
debian_package "app" {
  update_cache = true
}
```

底层仍可复用文件写入、校验和命令执行逻辑，但不应要求用户把这些实现细节拼出来。

### 5.3 默认行为应符合 Debian 惯例

资源默认值应让常见 Debian 用法自然工作：

- APT key 默认放在 `/etc/apt/keyrings`。
- deb822 source 默认放在 `/etc/apt/sources.list.d`。
- systemd drop-in 变化后需要 `daemon-reload`。
- 服务是否 reload 或 restart 必须由资源语义、字段或 handler 明确表达。

默认值不能隐藏危险动作，例如整机升级、重启或无条件 restart。

### 5.4 显式字段表达隐藏关系

如果关系无法从名称可靠推断，应提供领域字段，而不是让用户写顺序。

推荐：

```hcl
debian_service "bird" {
  name    = "bird"
  package = "bird2"
}
```

不推荐：

```hcl
debian_service "bird" {
  depends_on = [
    debian_package.bird2,
  ]
}
```

`depends_on` 仍然保留，但不应成为文档示例里的常规路径。

## 6. Provider 图标准

provider 不只负责 apply 一个资源，还必须描述这个资源和其他资源的关系。

长期目标中，每个 provider 应能提供这些信息：

```go
Provides(res) []Capability
Requires(res) []Capability
Triggers(change) []SyntheticNode
Locks(res) []Lock
```

当前代码可以逐步演进到这个接口；新增设计文档和资源验收时，应按这个模型思考。

### 6.1 Capability

Capability 表示某个资源提供或需要的一种语义能力。

示例：

```text
path:server1:/etc/apt/keyrings/cznic.asc
apt-repository:server1:cznic_bird2
apt-cache:server1
package:server1:bird2
systemd-unit:server1:bird.service
user:server1:deploy
group:server1:deploy
```

资源通过 `Provides` 声明自己成功后会提供什么，通过 `Requires` 声明执行前需要
什么。引擎根据这些能力自动建图。

### 6.2 SyntheticNode

SyntheticNode 是用户不直接声明、但 apply 过程中需要的内部动作。

常见例子：

- `apt_update[host]`
- `systemctl_daemon_reload[host]`
- `systemd_reload_or_restart[host, unit]`
- `nft_validate[path]`
- `networkd_reload[host]`

SyntheticNode 必须满足：

- 只在需要时进入 plan。
- 同一作用域内可去重。
- 在 plan 输出中可解释。
- 失败时能指出触发它的资源。

### 6.3 Lock

并发执行不能破坏 Debian 本机约束。provider 必须声明互斥域。

常见锁：

```text
apt:server1
dpkg:server1
systemd-daemon-reload:server1
path:server1:/etc/apt/sources.list.d/vendor.sources
userdb:server1
```

引擎可以并发执行没有依赖、没有锁冲突的节点。涉及 `apt` 和 `dpkg` 的节点必须在
同一主机上串行。

## 7. `depends_on` 使用边界

`depends_on` 只用于以下场景：

- 资源之间存在真实依赖，但 DebianForm 暂时没有领域字段可表达。
- 自定义命令或 handler 依赖某个资源完成。
- 迁移旧配置时临时保守排序。
- 用户明确要覆盖 provider 的默认推断。

文档示例不应把 `depends_on` 作为常规资源设计的一部分。若一个常见场景必须写
`depends_on` 才正确，说明资源 API 或 provider 图设计还不够高层。

## 8. Handler 与触发动作

handler 是延迟动作，不是普通资源。设计模块时应区分三类情况：

- 资源自身 apply 后必须立即做的校验或激活，由 provider 内部处理。
- 多个资源变化后需要合并执行一次的动作，建模为 SyntheticNode 或 handler。
- 用户自定义命令，使用 handler。

例如：

- 写 systemd unit 文件后，`daemon-reload` 应由 provider 自动触发并去重。
- 配置文件变化是否 restart 服务，应由明确字段或 handler 表达。
- 任意 shell 不应变成一个长期资源。

## 9. Plan、Apply、Check 标准

模块设计必须同时考虑三个命令。

### 9.1 Plan

Plan 必须能解释：

- 哪些用户资源会变化。
- 哪些内部节点会被触发。
- 内部节点由哪些资源变化触发。
- 为什么某个资源需要等待另一个资源。

### 9.2 Apply

Apply 必须：

- 按依赖图执行。
- 在无依赖、无锁冲突时允许并发。
- 保持输出稳定可读。
- 在失败时保留足够上下文，指出 host、资源、内部节点和 stderr。

### 9.3 Check

Check 不应因为一次性动作没有重跑而报告 drift。

例如 `systemctl reload`、`apt-get update` 和 `daemon-reload` 是动作，不是长期状态。
它们只应在相关持久资源变化时触发。

## 10. State 标准

state 不能只记录线性 `order`。为了支持自动依赖和安全 destroy，长期应记录：

- 用户资源地址。
- 资源类型、host 和远端对象身份。
- provider 版本或 schema 版本。
- 关键 desired hash。
- 资源提供过的 capability。
- 删除时需要的最小信息。

destroy 应优先依据保存的依赖图或 capability 反向处理，而不是依赖配置文件顺序。

## 11. 示例标准

每个新模块示例必须通过以下检查：

- 示例里没有把命令式脚本逐步翻译成一串资源。
- 常见路径不需要 `depends_on`。
- 用户可读出最终系统事实。
- 需要的内部动作在文档中解释，但不要求用户手写。
- 示例可以重复 apply 并收敛。
- 删除或停用行为有明确语义。

如果示例中出现很多 `depends_on`，优先重新设计资源，而不是继续补文档解释顺序。

## 12. 新模块设计流程

新增模块前，按这个顺序写设计：

1. 写出用户真正想声明的目标状态。
2. 列出哪些底层命令只是实现细节。
3. 定义用户可见资源字段。
4. 定义资源身份和 state 内容。
5. 定义 `Provides`、`Requires`、`Triggers` 和 `Locks`。
6. 说明 plan、apply、check、destroy 的行为。
7. 写一个没有常规 `depends_on` 的推荐示例。
8. 再决定是否需要保留低层资源作为 escape hatch。

模块评审时，优先问：

```text
这个设计是在声明系统，还是在声明步骤？
```

如果答案是“声明步骤”，不要进入实现。
