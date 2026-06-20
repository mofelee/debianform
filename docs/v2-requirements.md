# DebianForm v2 需求文档

## 背景

DebianForm 当前还没有生产环境兼容负担，v2 可以采用破坏式重设计。

v2 的目标不是在 v1 语法上继续堆功能，而是重新设计一层面向用户的系统配置 DSL，
让用户像写 NixOS `configuration.nix` 一样描述“系统应该是什么样”，再由编译器把它
翻译成可执行的低阶资源图。

核心方向：

```text
用户写 host/profile
编译器合并 profile 和 host override
生成中间表达 HostSpec
编译成低阶 ResourceGraph
调度器根据 DAG 自动决定执行顺序和并行层
```

## 设计原则

v2 应遵循这些原则：

- 用户层不暴露 `debian_*` provider 名称。
- `host` 是核心对象，表示一台 Debian 主机的目标状态。
- `profile` 是可复用配置片段，类似简化版 NixOS module。
- 默认合并行为应该适合“追加配置”，只有显式 `force()` 才清空并覆盖。
- 用户表达目标状态和必要依赖，调度器决定可并行的执行层。
- 中间表达和资源图是内部编译边界，方便校验、解释 plan 和扩展功能。
- v2 不要求兼容 v1 配置语法，允许重新设计 state 地址和资源地址。

## 非目标

v2 不做：

- 完整 NixOS module system。
- Nix store、generation、atomic rollback。
- v1 配置的渐进式迁移兼容。
- 公开低阶 `debian_*` 资源作为主要语法。
- 任意 option priority 系统。
- 让用户通过数字 priority 控制执行顺序。
- 把 SSH 命令作为用户层抽象。

## 顶层语法

v2 顶层保留四个主要对象：

```hcl
locals {}

profile "name" {}

resource "name" {}

host "name" {}
```

`host` 是唯一真正会被 apply 的对象。`profile` 和 `resource` 都不独立执行：

- `profile` 贡献可合并的主机基线配置。
- `resource` 描述一个可复用的部署组件，由 host 显式挂载后按目标架构展开。

示例：

```hcl
profile "base" {
  packages {
    install = ["curl", "vim", "ca-certificates"]
  }
}

profile "bbr" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}

host "ksvm213" {
  imports = [
    profile.base,
    profile.bbr,
  ]
}
```

## Host

`host "name"` 表示一台目标主机。

默认规则：

```text
host label = 主机名
host label = 默认 SSH host
state 使用默认路径
```

```hcl
host "ksvm213" {}
```

等价含义：

```text
host.name       = ksvm213
ssh.host        = ksvm213
state.path      = /var/lib/debianform/state/ksvm213.yaml
state.lock_path = /var/lock/debianform/state/ksvm213.lock
```

如果 SSH 目标不同，可以显式写：

```hcl
host "edge-1" {
  ssh {
    host          = "10.0.0.11"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }
}
```

## Host 第一层结构

`host` 内部参考 NixOS 的领域组织方式，但保持 DebianForm 自己的语义：

```hcl
host "ksvm213" {
  imports = []
  resources = []

  state {}
  ssh {}
  system {}

  kernel {}
  packages {}
  apt {}
  files {}
  directories {}
  users {}
  groups {}
  services {}
  systemd {}
  networking {}
  security {}
}
```

v2 第一阶段需要重点实现：

- `imports`
- `resources`
- `state`
- `ssh`
- `system`
- `kernel`
- `packages`
- `files`
- `directories`
- `users`
- `groups`
- `services`
- `systemd`

`apt`、`networking`、`security` 可以放到后续阶段。

完整目标语法还可以包含 `environment`、`sudo`、`sshd`、`firewall`、`docker`
以及重复的 `assert` 块。它们在
[examples/v2-fleet.dbf.hcl](../examples/v2-fleet.dbf.hcl)
中用于检查组合后的 DSL 是否协调，不代表都属于第一阶段实现范围。

对于 file path、用户名、unit 名等带稳定 identity 的集合，用户语法统一使用
“领域容器 + labeled object block”：

```hcl
files {
  file "/etc/motd" {}
}

users {
  user "deploy" {}
}
```

编译器按 block label 将它们归一化为 map。不能使用
`files { "/etc/motd" = {} }`，因为 quoted path 不是合法的 HCL attribute 名。

## System

`system` 描述主机自身的基础属性：

```hcl
system {
  hostname     = "server1"
  architecture = "amd64"
  timezone     = "Asia/Tokyo"
  locale       = "en_US.UTF-8"
}
```

要求：

- `hostname` 默认等于 host label。
- `architecture` 使用 DebianForm 规范架构名，例如 `amd64`、`arm64`。
- `architecture` 可以省略并在连接目标后探测；显式声明时必须与探测结果一致。
- 离线 `validate` 不依赖 SSH，但带多架构 source 的 resource 只有在 architecture
  已知时才能完成 source 选择检查。
- `timezone` 和 `locale` 可由 profile 提供。
- `hostname` 和 `architecture` 只能在 host 中声明，profile 中声明应报错。

## State

DebianForm 管理的是普通 Debian 主机，需要自己的远端 state 文件记录已管理资源、
orphan cleanup 和 destroy 顺序。

`state` 位于 `host` 下：

```hcl
host "ksvm213" {
  state {
    path      = "/var/lib/debianform/state/ksvm213.yaml"
    lock_path = "/var/lock/debianform/state/ksvm213.lock"
  }
}
```

默认值：

```text
state.path      = /var/lib/debianform/state/<host>.yaml
state.lock_path = /var/lock/debianform/state/<host>.lock
```

要求：

- 用户不配置 `state` 时必须自动填充默认值。
- `state.path` 和 `state.lock_path` 必须是绝对路径。
- 每个 host 独立 state，避免多主机共享 state 带来的锁和销毁边界问题。
- v2 可以重新设计 state address，不需要兼容 v1 state address。

### State 文件内容

state 记录 DebianForm 对远端主机的管辖事实，不是用户配置的副本。

建议 state 使用机器写入的规范 YAML：

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
    last_applied_at: "2026-06-19T12:00:00Z"
```

`ownership` 用于决定从配置中删除后是否销毁：

```text
created   DebianForm 创建或安装的资源；从配置删除后应销毁。
adopted   DebianForm 接管时已经存在的资源；从配置删除后默认只解除管辖。
external  只作为依赖或观测对象记录；不销毁。
```

state 至少应保存：

- state 格式版本。
- host 名。
- 单调递增 serial。
- 每个受管辖资源的 v2 地址。
- provider kind 和远端 identity。
- ownership。
- 最后一次应用的 desired 摘要。
- 必要的 observed 信息，例如包版本、文件 hash、路径、服务名。
- 资源创建/接管/最后应用时间。

state 不应保存：

- SSH 私钥内容。
- 明文 secret。
- lock 租约。
- 任意命令输出日志。

### Lock 文件内容

lock 文件只表示“当前有 apply/plan 正在持有 state 写锁”，不保存目标状态。

建议 lock 也使用规范 YAML：

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

要求：

- 获取 lock 必须是远端原子操作。
- 释放 lock 时必须校验 token，避免误删别的进程的锁。
- lock 过期后可以由新的操作接管，但必须在输出中提示 stale lock。
- lock 文件不参与 plan diff，不进入 state。

## SSH

`ssh` 描述如何连接目标主机。

```hcl
host "edge-1" {
  ssh {
    host          = "10.0.0.11"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }
}
```

要求：

- `ssh.host` 默认等于 host label。
- `ssh.port` 可选。
- `ssh.user` 可选，未配置时交给本地 SSH config 或 ssh 默认值。
- `ssh.identity_file` 可选。
- 后续可以支持 `ssh.config_host`，用于直接引用本地 SSH config host。

## Profile

`profile` 是可复用配置片段：

```hcl
profile "base" {
  packages {
    install = ["curl", "vim", "ca-certificates"]
  }
}
```

`host` 通过 `imports` 引入：

```hcl
host "ksvm213" {
  imports = [
    profile.base,
    profile.bbr,
  ]
}
```

要求：

- `profile` 可以包含与 `host` 相同的可合并领域块，但不能包含 `ssh`、`state`；
  `system` 中只能提供 `timezone` 和 `locale`，不能提供 `hostname` 或
  `architecture`。
- `profile` 不会独立 apply。
- `imports` 必须是静态 profile 引用。
- `imports` 按声明顺序合并。
- `host` 自己的配置最后合并，优先级高于所有 profile。
- profile 可以 import profile，但必须检测循环引用。

## Resource

`resource` 表示一个可以挂载到多台 host 的部署组件。它适合描述第三方二进制、
应用归档、CA 证书，以及组件附带的用户、目录、配置文件和 systemd unit。

它和 `profile` 的边界不同：

```text
profile   合并主机领域配置，例如基础包、BBR、SSH 策略。
resource  封装一个有稳定身份和版本的部署组件，例如 rclone 或 myapp。
host      组合 profile/resource，并声明主机特有配置。
```

最小示例：

```hcl
resource "rclone" {
  type    = "binary"
  version = "1.66.0"

  source "amd64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-amd64.zip"
    sha256 = "REPLACE_WITH_RCLONE_AMD64_SHA256"
  }

  source "arm64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-arm64.zip"
    sha256 = "REPLACE_WITH_RCLONE_ARM64_SHA256"
  }

  extract {
    format           = "zip"
    strip_components = 1
    include          = "rclone"
  }

  install {
    path = "/usr/local/bin/rclone"
    mode = "0755"
  }
}

host "server1" {
  resources = [
    resource.rclone,
  ]
}
```

第一批 resource 类型建议为：

```text
binary          下载并安装单个可执行文件；source 可以是直接文件或压缩文件。
archive         下载并展开完整目录树。
file            下载并安装普通文件。
ca_certificate  安装 CA 证书，并在内容变化后生成 update-ca-certificates 激活动作。
```

要求：

- resource label 在程序内唯一。
- resource 不能包含 `ssh`、`state`、`imports` 或 host 选择逻辑。
- resource 只有被 host 的 `resources` 引用时才实例化。
- `source "<arch>"` 的 label 使用 DebianForm 规范架构名，第一版至少支持
  `amd64` 和 `arm64`。
- 无 label 的 `source` 表示架构无关来源。
- 同一 resource 不能同时声明无 label source 和带架构 label source。
- host 架构有匹配 source 时必须精确选择；没有匹配项时 validate 报错。
- 所有远程下载必须有内容校验和。第一版 `sha256` 使用 64 位小写十六进制；
  后续如果支持 SRI，应使用独立 `checksum` 字段，不能让同一字段接受多种格式。
- `extract.format` 可以显式声明；省略时只允许根据 URL 后缀无歧义推导。
- `strip_components` 必须大于等于 0。
- `include` 对 binary resource 必须最终只匹配一个普通文件。
- `install.path` 必须是绝对路径。
- 校验和必须在解压前验证；校验失败不能触碰目标路径。
- 解压必须拒绝绝对路径、`..` 路径穿越，以及逃出 staging directory 的 symlink。
- binary/file 使用同目录临时文件、设置 owner/group/mode 后再原子 rename。
- archive 先完整展开到 staging directory，再替换目标目录；失败时保留原版本。
- archive 的 owner/group 默认递归应用于本次展开内容，不递归修改目标目录之外的文件。
- 用户不需要为 artifact pipeline 手工声明 `curl`、`tar`、`unzip`、`bzip2` 等实现
  工具；provider 应使用内建实现，或把缺失工具作为内部依赖明确展示在 plan 中。
- resource 内可以包含 `groups`、`users`、`directories`、`files`、`systemd`
  等领域块；它们使用与 host 相同的字段语义。
- 编译器自动推导 resource 内部依赖，例如 group -> user -> directory/file ->
  systemd unit -> service。
- resource 与 host/profile 最终产生相同远端 identity 时必须报冲突，不能按声明顺序
  静默覆盖。
- resource 不允许通用 `before_install`、`after_install` shell hook。需要激活动作时，
  应由有语义的资源类型或显式 systemd service/timer 表达。

host 中 `resources` 按声明顺序保留，用于稳定 plan 展示；正确性不能依赖该顺序，
执行顺序仍由编译后的资源图决定。

完整组合示例见
[examples/v2-fleet.dbf.hcl](../examples/v2-fleet.dbf.hcl)。

## Assertions

host 可以声明重复的 `assert` block，校验 profile 合并后的最终主机配置：

```hcl
assert {
  condition = contains(self.kernel.modules, "tcp_bbr")
  message   = "BBR requires the tcp_bbr kernel module."
}
```

要求：

- `condition` 是 HCL boolean expression，不能是等待二次解析的字符串。
- `self` 指向完成 profile merge 和默认值填充后的 host 视图。
- assertion 在 ResourceGraph 编译前求值；失败时 `validate`、`plan`、`apply` 都停止。
- assertion 不能读取远端运行时状态；运行时前置条件属于 provider check。
- `message` 必须是非空字符串。

## 合并规则

默认合并规则：

```text
imports 按顺序合并
profile 先合并
host 自身配置最后合并

scalar: 后者覆盖前者
map:    深度合并，同 key 后者覆盖前者
list:   默认 append，保持顺序并去重
```

带 label 的领域对象在合并前按 `(block type, label)` 归一化成 map。同 type、同 label
的对象执行字段级深度合并，因此 profile 中的 `user "deploy"` 可以由 host 的同名
user 追加 supplementary groups；不同 label 保留为不同对象。

`resources` 不是普通领域 list。重复引用同一个 resource 时去重；不同 resource
之间不做字段合并，而是在实例化后执行远端 identity 冲突检查。

示例：

```hcl
profile "base" {
  packages {
    install = ["curl", "vim"]
  }
}

host "ksvm213" {
  imports = [profile.base]

  packages {
    install = ["htop"]
  }
}
```

有效结果：

```text
packages.install = ["curl", "vim", "htop"]
```

Map 覆盖：

```hcl
profile "bbr" {
  kernel {
    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}

host "ksvm214" {
  imports = [profile.bbr]

  kernel {
    sysctl = {
      "net.core.default_qdisc" = "fq_codel"
    }
  }
}
```

有效结果：

```text
kernel.sysctl = {
  "net.core.default_qdisc" = "fq_codel"
  "net.ipv4.tcp_congestion_control" = "bbr"
}
```

## Merge Modifier

v2 需要支持少量明确的合并修饰符：

```hcl
force(value)   # 丢弃之前所有定义，只使用当前值
before(value)  # list 值插到已有值前面
after(value)   # list 值追加到已有值后面，默认行为
unset()        # 删除 map key 或禁用继承来的 option
```

示例：

```hcl
host "ksvm213" {
  imports = [profile.base]

  packages {
    install = force(["curl"])
  }

  kernel {
    modules = force([])
  }
}
```

有效结果：

```text
packages.install = ["curl"]
kernel.modules   = []
```

删除继承来的 map key：

```hcl
host "ksvm214" {
  imports = [profile.bbr]

  kernel {
    sysctl = {
      "net.ipv4.tcp_congestion_control" = unset()
    }
  }
}
```

要求：

- merge modifier 只存在于合并层。
- 生成中间表达后，不再保留 `force/before/after/unset`。
- `unset()` 用在 list 上应报错，清空 list 应使用 `force([])`。
- `force()` 可以用于 scalar、map、list。

## Kernel 功能

`kernel` 管理内核模块和 sysctl。

```hcl
host "ksvm213" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}
```

要求：

- `modules` 默认持久化到 modules-load 配置。
- `sysctl` 默认持久化到 sysctl.d，并立即应用运行时值。
- `modules` 支持 list 简写。
- 后续可支持对象形式：

```hcl
kernel {
  module "br_netfilter" {
    persist = true
    ensure  = "present"
  }
}
```

BBR 语义依赖：

- 如果 `kernel.sysctl["net.ipv4.tcp_congestion_control"] = "bbr"`；
- 且同一 host 配置了 `kernel.modules` 包含 `tcp_bbr`；
- 编译器必须自动生成 sysctl 依赖 module 的边。

## Packages 功能

`packages` 管理系统包。

```hcl
packages {
  install = ["curl", "vim", "ca-certificates"]
}
```

要求：

- `install` 表示 DebianForm 负责管辖的已安装包集合。
- 如果某个包由 DebianForm 安装，之后从 `install` 中删除，plan 应生成卸载动作。
- 如果某个包在 DebianForm 接管前已经安装，DebianForm 可以记录为 adopted；之后从
  `install` 中删除时默认只解除管辖，不卸载该包。
- v2 第一阶段不提供 `remove` 字段，避免把“期望缺失”和“从管辖集合移除”混在一起。
- list 合并默认 append 去重。
- 包名必须非空。
- 后续可支持对象形式，例如 pin version，但第一阶段不做版本锁定。

## Files 功能

`files` 管理普通文件。

建议语法：

```hcl
files {
  file "/etc/motd" {
    content = "hello\n"
    mode    = "0644"
    owner   = "root"
    group   = "root"
  }
}
```

要求：

- file label 是远端绝对路径，也是该对象在 merge 和资源图中的稳定 identity。
- `content` 和 `source` 二选一。
- `owner` 默认 `root`。
- `group` 默认 `root`。
- `mode` 默认 `0644`。
- `ensure` 默认 `present`，支持 `absent`。
- `sensitive` 默认 false。为 true 时，plan、日志和 state 不能记录明文内容，只能
  记录 hash、长度等非敏感摘要。
- 同一路径合并后只能有一个最终定义。

## Directories 功能

`directories` 管理目录。

```hcl
directories {
  directory "/opt/app" {
    owner = "app"
    group = "app"
    mode  = "0755"
  }
}
```

要求：

- directory label 是远端绝对路径，也是稳定 identity。
- `owner` 默认 `root`。
- `group` 默认 `root`。
- `mode` 默认 `0755`。
- `ensure` 默认 `present`，支持 `absent`。
- 后续再设计 recursive 行为，第一阶段不递归修改已有内容。

## Groups 功能

`groups` 管理 Unix group。

```hcl
groups {
  group "deploy" {
    gid    = 1500
    system = false
  }
}
```

要求：

- group label 是 group 名。
- `gid` 可选。
- `system` 默认 false。
- `ensure` 默认 `present`，支持 `absent`。
- group 名不能为空。

## Users 功能

`users` 管理 Unix user。

```hcl
users {
  user "deployer" {
    uid   = 1500
    group = "deploy"
    groups = [
      "sudo",
    ]
    home  = "/home/deployer"
    shell = "/bin/bash"
  }
}
```

要求：

- user label 是 user 名。
- `uid` 可选。
- `group` 可选，表示 primary group，可以引用同一 host 中声明的 group。
- `groups` 默认 append 去重。
- `system` 默认 false。
- `home` 可选。
- `shell` 可选。
- `ssh_authorized_keys` 默认 append 去重，并编译成独立 authorized-key 资源。
- `ensure` 默认 `present`，支持 `absent`。
- 如果 `group` 引用同一配置内的 group，编译器自动生成 user 依赖 group。

## Systemd 功能

`systemd` 管理 systemd unit 文件。

```hcl
systemd {
  units = {
    "app.service" = {
      content = <<-EOF
        [Service]
        ExecStart=/usr/local/bin/app
      EOF
    }
  }
}
```

要求：

- unit 名必须非空。
- `content` 和 `source` 二选一。
- 默认写入 `/etc/systemd/system/<unit-name>`。
- unit 内容变化或删除后需要 daemon-reload。
- 与 `services` 同名服务自动建立依赖。

在 raw unit 能力稳定后，可以增加接近 systemd 原生字段的结构化语法：

```hcl
systemd {
  service "app" {
    enable = true
    state  = "running"

    wanted_by = ["multi-user.target"]
    after     = ["network-online.target"]

    service_config = {
      ExecStart = "/usr/local/bin/app"
      Restart   = "on-failure"
    }
  }

  timer "app" {
    enable = true
    state  = "running"

    timer_config = {
      OnCalendar = "daily"
      Persistent = true
    }
  }
}
```

结构化语法要求：

- `service "app"` 生成 `app.service`，`timer "app"` 生成 `app.timer`。
- 字段名尽量保持 systemd 原生 option 名，不另造一套含义相近的命名。
- `enable` 和 `state` 是 DebianForm 管理状态，不直接写入 unit section。
- `service_config`、`timer_config` 等 map 分别生成对应 section。
- 同名 timer 自动关联同名 service，但执行依赖仍由资源图表达。
- 结构化语法必须能输出规范文本并支持预览；不能退化成运行
  `systemctl edit` 或任意 shell。

对于 systemd-networkd、resolved 和 journald，目标语法也采用“结构化序列化原生
配置”，而不是发明高级网络模型。例如：

```hcl
systemd {
  networkd {
    netdev "20-wg0" {
      netdev = {
        Name = "wg0"
        Kind = "wireguard"
      }

      wireguard = {
        PrivateKeyFile = "/etc/wireguard/private.key"
        RouteTable     = "off"
      }

      wireguard_peer "server2" {
        PublicKey = "..."
        AllowedIPs = [
          "10.100.0.2/32",
        ]
      }
    }
  }
}
```

这里分别生成 `[NetDev]`、`[WireGuard]` 和重复的 `[WireGuardPeer]` section。
`RouteTable = "off"` 保留 systemd-networkd 原生语义：不根据 `AllowedIPs` 自动
创建路由。用户如果需要路由，必须在 `.network` 中显式声明或交给外部路由系统。
编译器应根据目标 systemd 版本校验 option 是否受支持。

## Services 功能

`services` 管理 systemd 服务状态。

```hcl
services {
  nginx = {
    package = "nginx"
    enabled = true
    state   = "running"
  }
}
```

要求：

- key 是服务名。
- `package` 可选；如果同一 host 管理该 package，自动依赖 package。
- `enabled` 可选；未配置表示不管理 enable 状态。
- `state` 可选；支持 `running`、`stopped`、`restarted`、`reloaded`。
- 如果同一 host 中存在同名 systemd unit，自动依赖 unit。

## APT 功能

APT 相关能力可以分阶段实现。

第一阶段可以只支持 packages。

后续 `apt` 设计：

```hcl
apt {
  repositories = {
    cznic_bird2 = {
      uris       = "https://pkg.labs.nic.cz/bird2"
      suites     = "bookworm"
      components = "main"
      key = {
        url = "https://pkg.labs.nic.cz/gpg"
      }
    }
  }
}
```

要求：

- repository 自动成为 package install 的语义依赖。
- key、source 文件、apt update 的关系需要明确建图。
- 第一阶段可以先不实现。

## Networking 和 Security

`networking`、`security` 作为后续扩展域保留。

要求：

- 第一阶段不设计复杂网络抽象。
- 不要过早发明覆盖 nftables、networkd、ssh hardening 的另一套领域模型。
- 对成熟的 systemd 原生格式，可以提供一对一结构化序列化和 raw file 逃生口。
- 可以先通过 `files`、`systemd`、`services` 覆盖相关用例。

## 中间表达

v2 必须定义中间表达 `HostSpec`。

编译链路：

```text
HCL AST
  -> profile/host merge
  -> resource attachment and expansion
  -> HostSpec
  -> ResourceGraph
  -> Plan
  -> Apply
```

要求：

- `HostSpec` 中所有默认值已填充。
- `HostSpec` 中没有 merge modifier。
- `HostSpec` 中保留 source location，用于错误提示和 plan 输出。
- `HostSpec` 保留领域结构，不直接等于 provider resource。
- `ResourceGraph` 才包含低阶 provider resource。

详细设计见 [v2-ir-requirements.zh.md](v2-ir-requirements.zh.md)。

## 资源地址

v2 可以重新设计资源地址。

用户层地址建议：

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
host.ksvm213.resources.rclone.install["/usr/local/bin/rclone"]
```

要求：

- plan 默认展示用户层地址。
- 低阶资源地址可以作为 debug 信息展示。
- state 应优先记录稳定的 v2 地址。
- v2 不需要兼容 v1 address。

## 调度语义

用户默认不写 stage。

调度规则：

```text
用户声明目标状态
编译器生成显式依赖和语义依赖
资源图必须无环
调度器从 DAG 计算 wave
不同 wave 串行
同一 wave 内按并发策略执行
```

默认并发策略：

```text
多 host 可并行
单 host 默认串行
后续允许部分资源类型声明 safe_parallel
```

要求：

- 不使用数字 priority 表达依赖。
- `depends_on` 只在需要逃出语义推导时使用。
- 显式 stage 如果未来引入，只能作为额外图约束，不能替代依赖。
- apply 中任一资源失败后，应停止依赖它的后续资源。

## Plan 输出

Plan 应以用户能理解的 v2 地址为主。

示例：

```text
Plan:
  ~ host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
    sysctl -w net.ipv4.tcp_congestion_control=bbr
    writes /etc/sysctl.d/99-dbf-bbr_congestion_control.conf

  + host.ksvm213.packages.install["curl"]
    install package curl
```

要求：

- plan 展示 create/update/delete/no-op 语义。
- plan 展示用户层地址。
- debug 模式可展示低阶 provider address。
- 错误信息必须指向用户配置文件和行号。

## CLI 需求

v2 CLI 第一阶段：

```text
dbf validate
dbf plan
dbf apply
dbf check
dbf fmt
```

要求：

- `validate` 执行 HCL 解析、profile 合并、HostSpec 校验、ResourceGraph 校验。
- `plan` 生成并展示按 v2 地址组织的计划。
- `apply` 先 plan，再按 ResourceGraph 执行。
- `check` 如果存在 drift 或 plan 有变更，返回非零。
- `--host <name>` 过滤单个 host。
- `--parallel <n>` 控制跨 host 并发。
- `--dry-run` 可以作为 `plan` 的别名或后续补充。

## Roadmap

### Milestone 0: 设计冻结

目标：冻结 v2 的用户语法、合并规则和中间表达边界。

交付：

- 完成 v2 需求文档。
- 完成 v2 中间表达需求文档。
- 明确不兼容 v1 的范围。
- 确认第一阶段领域块范围。

### Milestone 1: Parser 和 AST

目标：解析 v2 顶层语法。

交付：

- 支持 `host` block。
- 支持 `profile` block。
- 支持 `resource` block 和 host `resources` 静态引用。
- 支持 `imports` 静态引用。
- 支持 `ssh`、`state`、`system`、`kernel`、`packages` 的最小字段。
- 支持 `force/before/after/unset` 的 AST 表达。
- 增加 parser 测试。

### Milestone 2: Merge Engine

目标：实现 profile/host 合并。

交付：

- imports 顺序合并。
- list append 去重。
- map 深度合并。
- scalar 后者覆盖。
- `force()` 覆盖。
- `before()` / `after()` list 顺序。
- `unset()` 删除 map key。
- profile import cycle 检测。
- 合并错误带 source location。

### Milestone 3: HostSpec

目标：生成并验证中间表达。

交付：

- 定义 `Program`、`HostSpec`、各领域 spec。
- 填充 ssh/state/system 默认值并归一化 architecture。
- 归一化 kernel modules/sysctl。
- 归一化 packages install 管辖集合。
- 校验必填字段和冲突。
- 增加中间表达 snapshot 测试。

### Milestone 4: ResourceGraph Compiler

目标：从 HostSpec 编译低阶资源图。

交付：

- kernel 编译到 kernel module/sysctl 资源。
- packages 编译到 package 资源。
- 生成稳定 v2 地址。
- 生成低阶 provider payload。
- 生成 source address 映射。
- 推导 BBR module/sysctl 依赖。
- 检测资源地址冲突和依赖环。

### Milestone 5: Plan v2

目标：用 ResourceGraph 生成用户可读 plan。

交付：

- plan 输出 v2 地址。
- debug 模式输出低阶 provider 地址。
- 保留现有 drift 检测能力。
- 错误指向用户配置来源。
- 增加 BBR plan 测试。

### Milestone 6: Apply v2

目标：执行 ResourceGraph。

交付：

- 每个 host 独立 state。
- 单 host 串行 apply。
- 多 host 可并行 apply。
- apply 失败后停止依赖资源。
- handler 或 systemd reload 语义重新纳入资源图。
- 更新 state 写入 v2 地址。

### Milestone 7: 第一批领域块

目标：补齐日常系统配置能力。

交付：

- `files`
- `directories`
- `groups`
- `users`
- `systemd`
- `services`
- 对应语义依赖推导。
- 对应 plan/apply 测试。

### Milestone 8: APT 和发布资源

目标：支持更完整的软件来源和二进制发布。

交付：

- `apt.repositories`
- repository key 管理。
- package 自动依赖 repository。
- 可选 apt cache update 资源。
- `binary`、`archive`、`file`、`ca_certificate` resource。
- 按 host 架构选择 source。
- 下载校验、解压和原子安装。
- resource 内部领域配置展开与冲突检测。

### Milestone 9: 调度器增强

目标：从“多 host 并行、单 host 串行”升级为 DAG wave 调度。

交付：

- ResourceGraph wave 计算。
- 全局并发限制。
- per-host 并发限制。
- 资源类型 safe_parallel 标记。
- 失败传播规则。
- 调度测试。

### Milestone 10: 文档和示例

目标：让 v2 可以作为主版本使用。

交付：

- README 改为 v2 语法。
- BBR 示例。
- nginx 示例。
- user/group/authorized_key 示例。
- systemd service 示例。
- 多 host/profile 示例。
- 迁移说明只解释 v1 已废弃，不提供兼容承诺。

## 验收标准

v2 第一版至少应满足：

- 可以用 `host` 配置单台主机 BBR。
- 可以用 `profile` 复用 BBR 和 base packages。
- host 能覆盖 profile 中的 map 和 list。
- `force([])` 能清空继承的 list。
- 同一个 resource 可以挂载到多台 host，并按 architecture 选择唯一 source。
- resource 与 host/profile 产生相同远端 identity 时 validate 报错。
- plan 输出用户层 v2 地址。
- apply 能正确执行 kernel module、sysctl、package。
- 每台 host 使用独立 state。
- validate 能发现 imports 循环、字段冲突、资源图环。
- 不需要写任何 `debian_*` 资源即可完成基础系统配置。
