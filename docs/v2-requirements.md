# DebianForm v2 需求文档

## 背景

DebianForm v2 是当前主线，可以采用破坏式重设计。

v2 的目标不是在旧实验语法上继续堆功能，而是重新设计一层面向用户的系统配置 DSL，
让用户像写 NixOS `configuration.nix` 一样描述“系统应该是什么样”，再由编译器把它
翻译成可执行的低阶资源图。

核心方向：

```text
用户写 host/profile/component
编译器合并 profile 和 host override
编译器在最终 host 上实例化 component
生成中间表达 HostSpec
编译成低阶 ResourceGraph
调度器根据 DAG 自动决定执行顺序和并行层
```

## 设计原则

v2 应遵循这些原则：

- 用户层不暴露 `低阶 provider 资源` provider 名称。
- `host` 是核心对象，表示一台 Debian 主机的目标状态。
- `profile` 是可复用配置片段，类似简化版 NixOS module。
- 默认合并行为应该适合“追加配置”，只有显式 `force()` 才清空并覆盖。
- 用户表达目标状态和必要依赖，调度器决定可并行的执行层。
- 中间表达和资源图是内部编译边界，方便校验、解释 plan 和扩展功能。
- 对已经有稳定原生配置格式的系统组件，DebianForm 不重新发明高阶抽象；
  主路径应是薄封装：管理文件、校验、激活、依赖、diff 和组合能力。例如
  `nftables`、`systemd.networkd`、systemd unit、sysctl 和 modules-load。
- v2 不要求兼容旧实验配置语法，允许重新设计 state 地址和资源地址。

## 非目标

v2 不做：

- 完整 NixOS module system。
- Nix store、generation、atomic rollback。
- 旧实验配置的渐进式迁移兼容。
- 公开低阶 `低阶 provider 资源` 资源作为主要语法。
- 任意 option priority 系统。
- 让用户通过数字 priority 控制执行顺序。
- 把 SSH 命令作为用户层抽象。

## 顶层语法

v2 顶层保留四个主要对象：

```hcl
locals {}

profile "name" {}

component "name" {}

host "name" {}
```

`host` 是唯一真正会被 apply 的对象。`profile` 和 `component` 都不独立执行：

- `profile` 贡献可合并的主机基线配置。
- `component` 描述一个可复用的部署单元，由 host 显式挂载后按目标架构展开。

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
state.path      = /var/lib/debianform/state/ksvm213.json
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
  components = []

  state {}
  ssh {}
  system {}

  kernel {}
  packages {}
  apt {}
  files {}
  secrets {}
  directories {}
  users {}
  groups {}
  services {}
  systemd {}
  nftables {}
  networking {}
  security {}
}
```

v2 主线领域范围：

- `imports`
- `components`
- `state`
- `ssh`
- `system`
- `kernel`
- `packages`
- `apt`
- `files`
- `secrets`
- `directories`
- `users`
- `groups`
- `services`
- `systemd`
- `nftables`

`networking`、`security` 可以放到后续阶段。`nftables` 是目标 DSL 的一等领域；
当前第一版采用原生 nftables 文件管理，不发明通用 firewall 抽象。

完整目标语法还可以包含 `environment`、`sudo`、`sshd`、`nftables`、`docker`
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

`system` 描述用户希望管理的基础系统配置；主机架构和 Debian codename 属于运行时
facts，通常不需要在 DSL 中手写：

```hcl
system {
  timezone = "Asia/Tokyo"
  locale   = "en_US.UTF-8"
}
```

要求：

- `hostname` 默认等于 host label。
- `architecture` 使用 DebianForm 规范架构名，例如 `amd64`、`arm64`，由在线
  `plan`/`check`/`apply` 在连接目标后探测；显式声明时必须与探测结果一致。
- `codename` 是 Debian release codename，例如 `bookworm`、`trixie`，由在线
  `plan`/`check`/`apply` 从 `/etc/os-release` 或 `lsb_release` 探测。
- 探测到的 `architecture`、`codename` 和远端 `hostname` 写入 state 顶层
  `facts.system`，并在 component 实例化前注入 `target.system` 只读视图。
- 离线 `validate` 不依赖 SSH；`plan --offline` 只生成纯本地预览。
- `timezone` 和 `locale` 可由 profile 提供。
- `hostname`、`architecture` 和 `codename` 只能在 host 中声明，profile 中声明应报错。

## State

DebianForm 管理的是普通 Debian 主机，需要自己的远端 state 文件记录已管理资源、
orphan cleanup 和 destroy 顺序。

`state` 位于 `host` 下：

```hcl
host "ksvm213" {
  state {
    path      = "/var/lib/debianform/state/ksvm213.json"
    lock_path = "/var/lock/debianform/state/ksvm213.lock"
  }
}
```

默认值：

```text
state.path      = /var/lib/debianform/state/<host>.json
state.lock_path = /var/lock/debianform/state/<host>.lock
```

要求：

- 用户不配置 `state` 时必须自动填充默认值。
- `state.path` 和 `state.lock_path` 必须是绝对路径。
- 每个 host 独立 state，避免多主机共享 state 带来的锁和销毁边界问题。
- v2 可以重新设计 state address，不需要兼容旧 state address。

### State 文件内容

state 记录 DebianForm 对远端主机的管辖事实，不是用户配置的副本。

state 使用机器写入的规范 JSON：

```json
{
  "version": 2,
  "host": "ksvm213",
  "serial": 17,
  "updated_at": "2026-06-19T12:00:00Z",
  "resources": {
    "host.ksvm213.packages.install[\"curl\"]": {
      "host": "ksvm213",
      "kind": "package",
      "provider_type": "package",
      "provider_address": "package.ksvm213_curl",
      "ownership": "managed",
      "desired": {
        "ensure": "present",
        "name": "curl"
      },
      "desired_digest": "sha256-summary",
      "observed": {
        "installed": true
      },
      "updated_at": "2026-06-19T12:00:00Z",
      "order": 0
    }
  }
}
```

`ownership` 用于决定从配置中删除后是否销毁：

```text
created   DebianForm 创建或安装的资源；从配置删除后应销毁。
adopted   DebianForm 接管时已经存在的资源；从配置删除后默认只解除管辖。
external  只作为依赖或观测对象记录；不销毁。
```

“从配置删除”和“配置中显式声明 `ensure = "absent"`”语义不同：

- 从配置删除：表示 DebianForm 不再管理该对象；是否销毁由 state ownership 和
  lifecycle 决定。
- `ensure = "absent"`：表示用户明确要求远端对象不存在；只要没有
  `prevent_destroy` 阻止，plan 应生成删除动作。

默认删除策略：

| kind | ownership=created 时从配置删除 | ownership=adopted/external 时从配置删除 |
| --- | --- | --- |
| package | 卸载 | 解除管辖 |
| file/secret/nftables file/systemd unit | 删除远端文件 | 解除管辖 |
| service state | 停止管理 enable/running 状态 | 解除管辖 |
| user/group/directory | 可销毁，但必须在 plan 中高亮；建议用户对关键对象设置 `prevent_destroy` | 解除管辖 |
| operation node | 不作为长期资源销毁 | 不作为长期资源销毁 |

DebianForm 提供最小 lifecycle 保护，不引入 Terraform 完整 lifecycle 语义：

```hcl
files {
  file "/etc/nftables.conf" {
    content = file("nftables.conf")

    lifecycle {
      prevent_destroy = true
    }
  }
}
```

要求：

- `lifecycle.prevent_destroy` 默认 false。
- 当资源从配置删除、`ensure = "absent"` 或 replace 需要销毁旧对象时，如果
  `prevent_destroy = true`，plan 必须失败并指向来源位置。
- `prevent_destroy` 只阻止 destroy/delete/replace，不阻止普通 update。
- high-risk domain 可以在文档中建议用户开启 `prevent_destroy`，但默认值必须明确。
- 后续如需要 `ignore_changes`、`replace_triggered_by` 等能力，必须单独设计；第一版不做。

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

### 状态对比模型

v2 plan、check 和 apply 必须基于三类状态输入，而不能只比较当前配置和上次写入的
state：

```text
desired   当前 HCL 经过解析、profile 合并、component 展开后生成的 HostSpec 和 ResourceGraph。
state     远端 state 文件中记录的 DebianForm 管辖事实。
observed  provider 在计划或执行前从目标主机实时读取的实际状态。
```

三者职责不同：

- `desired` 是本次用户声明的目标状态，来自当前配置文件；它不是持久化 state 的完整副本。
- `state` 是 DebianForm 上次 apply 后保存的资源管辖事实，用于判断 ownership、orphan
  cleanup、destroy 顺序和上次 desired/observed 摘要。
- `observed` 是目标主机当前真实状态，由 provider 按需读取；它用于发现 drift，决定
  create、update、delete、destroy 或 no-op 等计划动作，以及 adopt 或解除管辖等
  state 处理语义。

plan/check 的比较规则：

- desired 有、state 没有：读取 observed；远端不存在时计划 create，远端已存在时根据
  provider 策略计划 update 或记录为 adopted ownership。
- desired 有、observed 与 desired 不同：计划 update；如果 state 显示上次已一致，则该
  update 同时表示 drift 修复。
- state 有、desired 没有：根据 ownership 和 lifecycle 计划 destroy；对
  adopted/external 资源默认只解除管辖并清理 state 记录，不改变远端对象。
- desired、state 和 observed 都一致：计划 no-op。
- observed 读取失败时，provider 必须返回明确诊断；不能静默假设远端状态等于 state。

`check` 必须复用同一套状态对比逻辑。如果存在 drift 或 plan 有任何 create、update、
delete、destroy、replace、run 动作，`check` 返回非零。

### Lock 文件内容

lock 文件只表示“当前有 apply/plan 正在持有 state 写锁”，不保存目标状态。

lock 也使用机器写入的 JSON：

```json
{
  "owner": "dbf",
  "pid": "12345",
  "token": "random-128-bit-token",
  "expires_at": "2026-06-19T12:05:00Z",
  "expires_at_unix": 1781870700
}
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
  `architecture`、`codename`。
- `profile` 不会独立 apply。
- `imports` 必须是静态 profile 引用。
- `imports` 按声明顺序合并。
- `host` 自己的配置最后合并，优先级高于所有 profile。
- profile 可以 import profile，但必须检测循环引用。

## Component

`component` 表示一个可以挂载到多台 host 的部署单元。它适合描述第三方二进制、
应用归档、CA 证书，以及组件附带的用户、目录、配置文件和 systemd unit。

它和 `profile` 的边界不同：

```text
profile   合并主机领域配置，例如基础包、BBR、SSH 策略。
component 封装一个有稳定身份和版本的部署单元，例如 rclone、BIRD2 或 myapp。
host      组合 profile/component，并声明主机特有配置。
```

最小示例：

```hcl
component "rclone" {
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
  components = [
    component.rclone,
  ]
}
```

参数化示例：

```hcl
component "myapp" {
  input "listen_addr" {
    type    = string
    default = "127.0.0.1:8080"
  }

  input "data_dir" {
    type    = string
    default = "/var/lib/myapp"
  }

  files {
    file "/etc/myapp/config.yaml" {
      content = <<-EOF
        listen: ${input.listen_addr}
        data_dir: ${input.data_dir}
      EOF
    }
  }
}

host "server1" {
  component "myapp" {
    source = component.myapp

    inputs = {
      listen_addr = "127.0.0.1:9090"
      data_dir    = "/srv/myapp"
    }
  }
}
```

第一批 component artifact 类型建议为：

```text
binary          下载并安装单个可执行文件；source 可以是直接文件或压缩文件。
archive         下载并展开完整目录树。
file            下载并安装普通文件。
ca_certificate  安装 CA 证书，并在内容变化后生成 update-ca-certificates 激活动作。
```

要求：

- component label 在程序内唯一。
- component 不能包含 `ssh`、`state`、`imports` 或 host 选择逻辑。
- component 只有被 host 的 `components` 引用，或被 host 内的 `component "<instance>"`
  block 引用时才实例化。
- `components = [component.rclone]` 是无参数单实例 shorthand；实例名默认等于模板名。
- 需要传参或同一模板多实例时，必须使用 host 内的 `component "<instance>"` block。
- host 内的 component instance label 在同一 host 内唯一，并进入 v2 地址。
- component input 必须显式声明类型；第一版支持 `string`、`number`、`bool`、
  `list(string)` 和 `map(string)`。
- input 没有 default 时，所有实例必须传值。
- 实例传入未知 input 或遗漏必填 input 时 validate 报错。
- component 内部通过只读 `input.<name>` 访问参数。
- component 表达式可以通过只读的 `target` 访问完成 profile merge 和默认值填充后的
  host，例如 `target.system.codename`；不能通过该上下文修改 host。
- component 可以只包含领域对象，不要求必须声明下载 artifact。
- component 声明 artifact 时，`source "<arch>"` 的 label 使用 DebianForm 规范架构名，
  第一版至少支持
  `amd64` 和 `arm64`。
- 无 label 的 `source` 表示架构无关来源。
- 同一 component 不能同时声明无 label source 和带架构 label source。
- host 架构有匹配 source 时必须精确选择；没有匹配项时 validate 报错。
- 所有远程下载必须有内容校验和。第一版 `sha256` 使用 64 位小写十六进制；
  后续如果支持 SRI，应使用独立 `checksum` 字段，不能让同一字段接受多种格式。
- `extract.format` 可以显式声明；省略时只允许根据 URL 后缀无歧义推导。
- `strip_components` 必须大于等于 0。
- `include` 对 binary component 必须最终只匹配一个普通文件。
- `install.path` 必须是绝对路径。
- 校验和必须在解压前验证；校验失败不能触碰目标路径。
- 解压必须拒绝绝对路径、`..` 路径穿越，以及逃出 staging directory 的 symlink。
- binary/file 使用同目录临时文件、设置 owner/group/mode 后再原子 rename。
- archive 先完整展开到 staging directory，再替换目标目录；失败时保留原版本。
- archive 的 owner/group 默认递归应用于本次展开内容，不递归修改目标目录之外的文件。
- 用户不需要为 artifact pipeline 手工声明 `curl`、`tar`、`unzip`、`bzip2` 等实现
  工具；provider 应使用内建实现，或把缺失工具作为内部依赖明确展示在 plan 中。
- component 内可以包含 `apt`、`packages`、`services`、`groups`、`users`、
  `directories`、`files`、`systemd`
  等领域块；它们使用与 host 相同的字段语义。
- 编译器自动推导 component 内部依赖，例如 repository -> package -> service，
  以及 group -> user -> directory/file ->
  systemd unit -> service。
- component 与 host/profile 最终产生相同远端 identity 时必须报冲突，不能按声明顺序
  静默覆盖。
- component 不允许通用 `before_install`、`after_install` shell hook。需要激活动作时，
  应由有语义的资源类型或显式 systemd service/timer 表达。

host 中 `components` 按声明顺序保留，用于稳定 plan 展示；正确性不能依赖该顺序，
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

例如 labeled package block 会先归一化为 `packages.package["bird2"]` 再合并：

```hcl
profile "bird" {
  packages {
    package "bird2" {
      repositories = ["base_repo"]
    }
  }
}

host "router1" {
  imports = [profile.bird]

  packages {
    package "bird2" {
      repositories = ["host_repo"]
    }
  }
}
```

有效结果：

```text
packages.package["bird2"].repositories = ["base_repo", "host_repo"]
```

`components` 不是普通领域 list。重复引用同一个 component 时去重；不同 component
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

错误用法：

```hcl
host "ksvm215" {
  packages {
    install = unset()
  }
}
```

应改为：

```hcl
host "ksvm215" {
  packages {
    install = force([])
  }
}
```

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

## Secrets 功能

`secrets` 管理本地 secret 输入到远端敏感文件。它是 `files.file sensitive = true`
的清晰语义化入口，适合 WireGuard private key、restic environment、应用密钥等。

建议语法：

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

要求：

- secret file label 是远端绝对路径，也是稳定 identity。
- `source` 是本地文件路径，相对当前配置文件所在目录解析。
- 第一版只支持本地文件 source；后续可增加外部 secret provider。
- `owner` 默认 `root`，`group` 默认 `root`，`mode` 默认 `0600`。
- secret 文件内容可以在当前进程内用于渲染和上传，但 plan、日志和 state 不能记录明文。
- plan 只能显示 secret 是否变化、hash、长度等非敏感摘要。
- state 只能记录远端 path、content hash、mode、owner/group 和 ownership。
- `secrets.file` 与 `files.file` 不能管理同一个远端 path。
- 示例中的 `examples/secrets/` 必须被 `.gitignore` 忽略；文档和示例不能提交真实 secret。
- external secret provider 后续必须遵循同样的 plan/state 不落明文规则。

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
  service "nginx" {
    package = "nginx"
    enabled = true
    state   = "running"
  }
}
```

要求：

- service label 是服务名；省略 suffix 时默认使用 `.service` unit。
- `package` 可选；如果同一 host 管理该 package，自动依赖 package。
- `enabled` 可选；未配置表示不管理 enable 状态。
- `state` 可选；支持 `running`、`stopped`、`restarted`、`reloaded`。
- 如果同一 host 中存在同名 systemd unit，自动依赖 unit。

## APT 功能

APT repository 是主机的领域对象，不是顶层 component。直接配置单台主机时放在
host，复用仓库策略时可以放在 profile，需要连同软件和服务整体复用时则封装在
component 内。

```hcl
apt {
  repository "cznic_bird2" {
    uris       = ["https://pkg.labs.nic.cz/bird2"]
    suites     = ["trixie"]
    components = ["main"]

    signing_key {
      url    = "https://pkg.labs.nic.cz/gpg"
      sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      path   = "/etc/apt/keyrings/cznic-bird2.asc"
    }
  }
}
```

repository 要求：

- repository label 是同一 host 内的稳定逻辑名。
- 使用 deb822 `.sources` 作为默认输出格式。
- `uris`、`suites`、`components` 使用 list，避免字段从单值升级到多值时破坏语法。
- `signing_key` 可选；声明时 `url` 和 `content` 二选一。
- 远程 signing key 必须声明 `sha256`；后续可以增加 fingerprint 验证。
- `sha256` 必须是 64 位 hex；对内联 `content` 声明 `sha256` 时必须与内容匹配。
- `path` 默认 `/etc/apt/keyrings/<repository>.asc`。
- source 自动引用自己的 signing key path。
- signing key 或 source 变化后，编译器生成 host-scoped APT cache refresh 节点。
- 多个 repository 同时变化时，每个 host 最多执行一次 `apt-get update`。
- `ensure = "absent"` 的 repository 会删除 source/key，并触发 APT cache refresh。

package list 简写继续适合 Debian 官方仓库中的普通包：

```hcl
packages {
  install = ["curl", "vim"]
}
```

需要声明来源关系时使用 package object：

```hcl
packages {
  package "bird2" {
    repositories = ["cznic_bird2"]
  }
}
```

要求：

- `package "name"` 与 `install = ["name"]` 归一化成同一种 PackageItem。
- 同一个包不能同时用 list 和 object 重复声明。
- `repositories` 引用同一 host 最终配置中的 repository label。
- `repositories` 只能引用存在且 `ensure = "present"` 的 repository。
- package 只依赖自己显式引用的 repository，以及这些 repository 汇聚出的
  host-scoped cache refresh。
- 不再沿用旧实验版本中“所有 package 自动依赖同一 host 的所有 repository”的宽泛规则。
- 未声明 `repositories` 的 package 使用当前已配置的 APT source，但不会因为无关
  repository 的变化而增加依赖边。

BIRD2 适合封装为完整 component，而不是把 repository 本身提升为 component：

```hcl
component "bird2" {
  apt {
    repository "cznic_bird2" {
      uris       = ["https://pkg.labs.nic.cz/bird2"]
      suites     = [target.system.codename]
      components = ["main"]

      signing_key {
        url    = "https://pkg.labs.nic.cz/gpg"
        sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      }
    }
  }

  packages {
    package "bird2" {
      repositories = ["cznic_bird2"]
    }
  }

  services {
    service "bird" {
      package = "bird2"
      enabled = true
      state   = "running"
    }
  }
}
```

完整示例见
[examples/v2-bird2.dbf.hcl](../examples/v2-bird2.dbf.hcl)。

## Nftables 功能

v2 的主模型应直接暴露 nftables，而不是把防火墙抽象成
`allowed_tcp_ports` / `allowed_udp_ports` 这类低能力字段。nftables 已经是 Debian
上的稳定原生配置语言，DebianForm 只负责文件管理、校验、激活、依赖和 plan diff。

建议语法：

```hcl
nftables {
  enable = true

  main {
    path     = "/etc/nftables.conf"
    validate = true
    activate = true

    content = <<-EOF
      flush ruleset

      include "/etc/nftables.d/*.nft"
    EOF
  }

  file "20-services" {
    path = "/etc/nftables.d/20-services.nft"

    content = <<-EOF
      table inet filter {
        chain input {
          type filter hook input priority 0; policy drop;

          ct state established,related accept
          iifname "lo" accept
          tcp dport { 22, 80, 443 } accept
          udp dport 51820 accept
          counter drop
        }
      }
    EOF
  }
}
```

要求：

- `nftables.enable = true` 表示安装/启用 nftables 运行时；是否安装包可以由
  `packages` 或编译器生成的内部依赖负责。
- `main` 表示主 ruleset，默认 path 为 `/etc/nftables.conf`。
- `file "<label>"` 表示 DebianForm 管理的 snippet，默认 path 为
  `/etc/nftables.d/<label>.nft`。
- `content` 和 `source` 二选一。
- `validate` 默认 true；激活前必须执行 `nft -c -f /etc/nftables.conf` 或等价校验。
- `activate` 默认 true；通过校验后执行 `nft -f /etc/nftables.conf`。
- 多个 nftables 文件变化时，同一 host 只校验和激活一次主 ruleset。
- plan 必须展示 nft 文件的文本 diff；sensitive snippet 不显示明文。
- profile 可以贡献 snippet，但同一 host 最终 path 不能冲突；component 贡献
  nftables snippet 留给后续 component 领域扩展。
- 第一版不提供通用 `firewall` 主块。后续如增加 helper，也只能编译成明确的
  nftables snippet，不能成为替代 nftables 的第二套语义。

推荐组合约束：

- `main` 只负责 `flush ruleset` 和 `include "/etc/nftables.d/*.nft"`。
- 一个基础 snippet（例如 `10-base`）负责定义 table、chain、hook 和默认 policy。
- 其他 snippet 优先使用 `add rule inet filter input ...` 追加规则，避免重复定义同一
  table/chain。
- snippet label 建议使用数字前缀表达加载顺序，例如 `10-base`、`20-wireguard`、
  `30-services`。
- DebianForm 不深度解析 nft 语言，不尝试提前判断所有语义冲突；最终以
  `nft -c -f /etc/nftables.conf` 作为权威校验。
- 如果多个 component 都需要开放端口，优先让它们贡献各自的 snippet，而不是修改
  同一个共享文件。

完整示例见
[examples/v2-nftables.dbf.hcl](../examples/v2-nftables.dbf.hcl)。

## Networking 和 Security

`networking`、`security` 作为后续扩展域保留。

要求：

- 第一阶段不设计复杂网络抽象。
- 不要过早发明覆盖 networkd、ssh hardening 的另一套领域模型。
- 防火墙能力以原生 `nftables` domain 为主路径，不以通用 `firewall` 抽象为主路径。
- 对成熟的 systemd 原生格式，可以提供一对一结构化序列化和 raw file 逃生口。
- 可以先通过 `files`、`systemd`、`services` 覆盖相关用例。

## 中间表达

v2 必须定义中间表达 `HostSpec`。

编译链路：

```text
HCL AST
  -> profile/host merge
  -> component attachment and expansion
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
- `ResourceGraph` 才包含低阶 provider resource 和有语义的 operation node。

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
host.ksvm213.components.rclone.install["/usr/local/bin/rclone"]
```

要求：

- plan 默认展示用户层地址。
- 低阶资源地址可以作为 debug 信息展示。
- state 应优先记录稳定的 v2 地址。
- v2 不需要兼容旧 address。

## Operation 节点

有些动作不是长期存在的远端对象，但必须进入依赖图，否则会退化成 provider side
effect。例如：

```text
host.server1.apt.cache_refresh
host.server1.nftables.validate
host.server1.nftables.activate
host.server1.systemd.daemon_reload
host.server1.services.service["myapp"].restart
host.server1.ca_certificates.update
```

要求：

- 这类动作统一建模为 `OperationNode`。
- `OperationNode` 是 ResourceGraph 中的一等 DAG 节点，有稳定 v2 地址。
- `OperationNode` 只能由领域语义生成，不能作为用户任意 shell hook。
- 多个上游变化需要同一个动作时，编译器必须按 host 或作用域汇聚成一个节点。
- plan 必须展示 operation 的触发来源和执行预览。
- apply 必须按 DAG 执行 operation，失败后停止依赖它的后续节点。
- state 可以记录 operation 的最近执行摘要，但不能把 operation 当成长期资源所有权。

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

Plan 应以用户能理解的 v2 地址为主，但不能只输出扁平资源列表。v2 的变更经常发生在
很深的领域结构内，plan 内部必须保留结构化字段级 diff，再由不同 renderer 输出为
终端文本、JSON 或 HTML。

示例：

```text
Plan:
  ~ host.server1.systemd.networkd.netdev["10-wg0"]
    source: examples/v2-fleet.dbf.hcl:430

    ~ wireguard_peer["server2"]
      ~ PersistentKeepalive: 15 -> 25
      ~ AllowedIPs
        + "10.200.0.0/24"

  ~ host.server1.nftables.file["20-services"]
    source: examples/v2-fleet.dbf.hcl:560

    ~ content
      - tcp dport { 22, 80 } accept
      + tcp dport { 22, 80, 443 } accept

    validates: nft -c -f /etc/nftables.conf
    activates: nft -f /etc/nftables.conf
```

要求：

- plan 展示 create/update/delete/no-op 语义。
- plan 展示用户层地址。
- debug 模式可展示低阶 provider address。
- 错误信息必须指向用户配置文件和行号。
- plan 内部应使用结构化 `DiffNode`，而不是只有 summary 字符串。
- scalar diff 显示 before/after。
- object/map diff 按 key 递归显示。
- set diff 按元素 identity 显示新增/删除，不能把无序集合误报成 index 变化。
- 只有顺序有语义的 list 才按 index diff。
- labeled block list 必须先归一化成 map，例如 `wireguard_peer["server2"]`。
- text content 使用行级 diff；大文件默认折叠上下文。
- sensitive 字段只显示 `<sensitive>`、hash、长度等摘要，不能显示明文。
- 终端输出使用颜色时，新增为绿色，删除为红色，更新为黄色；无颜色环境必须保留
  `+`、`-`、`~` 符号。
- JSON 输出是稳定机器接口；HTML preview 只是这个结构化 plan 的一个 renderer。
- HTML preview 必须是可独立打开的静态文件，支持按 host、component、action 过滤，
  支持折叠/展开、搜索字段路径，并展示 source location。
- 交互式 TUI 可以后续增加，不作为第一版要求。

JSON 格式详见 [v2-plan-format.md](v2-plan-format.md)。

## CLI 需求

v2 CLI 第一阶段：

```text
dbf validate
dbf plan
dbf plan --offline
dbf plan --format json
dbf plan --html plan.html
dbf apply
dbf check
dbf fmt
```

要求：

- `validate` 执行 HCL 解析、profile 合并、HostSpec 校验、ResourceGraph 校验。
- `plan` 先探测 runtime facts、读取 state 和 observed，再展示按 v2 地址组织的计划；
  不写 state，不执行变更。
- `plan --offline` 不连接 SSH，只生成静态本地预览。
- `plan --format json` 输出结构化 plan，供 CI、审计和外部 viewer 使用。
- `plan --html <file>` 生成静态 HTML preview，不改变远端状态。
- `apply` 先 plan，再按 ResourceGraph 执行。
- `check` 如果存在 drift 或 plan 有变更，返回非零。
- `--host <name>` 过滤单个 host。
- `--parallel <n>` 控制跨 host 并发。
- `--dry-run` 可以作为 `plan` 的别名或后续补充。
- `plan --interactive` 可以作为后续 TUI viewer 扩展，不阻塞第一版。

## Roadmap

### Milestone 0: 设计冻结

目标：冻结 v2 的用户语法、合并规则和中间表达边界。

交付：

- 完成 v2 需求文档。
- 完成 v2 中间表达需求文档。
- 明确不兼容旧实验格式的范围。
- 确认第一阶段领域块范围。

### Milestone 1: Parser 和 AST

目标：解析 v2 顶层语法。

交付：

- 支持 `host` block。
- 支持 `profile` block。
- 支持 `component` block 和 host `components` 静态引用。
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

目标：用 ResourceGraph 生成用户可读、机器可读的结构化 plan。

交付：

- plan 输出 v2 地址。
- plan 内部包含字段级 `DiffNode`。
- 终端树状 diff renderer。
- 稳定 JSON renderer。
- 静态 HTML preview renderer。
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
- `nftables`
- 对应语义依赖推导。
- 对应 plan/apply 测试。

### Milestone 8: APT 和发布组件

目标：支持更完整的软件来源和二进制发布。

交付：

- `apt.repository`
- repository key 管理。
- package 显式引用 repository。
- host-scoped APT cache refresh 节点。
- `binary`、`archive`、`file`、`ca_certificate` component artifact。
- 按 host 架构选择 source。
- 下载校验、解压和原子安装。
- component 内部领域配置展开与冲突检测。

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
- 小型 golden examples 覆盖 BBR、APT repository、BIRD2 component、binary component、
  nftables、plan preview、systemd-networkd/WireGuard。
- golden examples 应进入 parser、HostSpec、ResourceGraph 和 plan snapshot 测试。
- `v2-fleet` 只作为组合压力示例，不应成为所有单元测试的唯一输入。
- 迁移说明只解释旧实验格式已废弃，不提供兼容承诺。

## 验收标准

v2 第一版至少应满足：

- 可以用 `host` 配置单台主机 BBR。
- 可以用 `profile` 复用 BBR 和 base packages。
- host 能覆盖 profile 中的 map 和 list。
- `force([])` 能清空继承的 list。
- 同一个 component 可以挂载到多台 host，并按 architecture 选择唯一 source。
- component 与 host/profile 产生相同远端 identity 时 validate 报错。
- plan 输出用户层 v2 地址。
- apply 能正确执行 kernel module、sysctl、package。
- 每台 host 使用独立 state。
- validate 能发现 imports 循环、字段冲突、资源图环。
- 不需要写任何 `低阶 provider 资源` 资源即可完成基础系统配置。
