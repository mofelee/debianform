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

v2 顶层保留三个主要对象：

```hcl
locals {}

profile "name" {}

host "name" {}
```

`host` 是唯一真正会被 apply 的对象。`profile` 只贡献配置，不独立执行。

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

  state {}
  ssh {}

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
- `state`
- `ssh`
- `kernel`
- `packages`
- `files`
- `directories`
- `users`
- `groups`
- `services`
- `systemd`

`apt`、`networking`、`security` 可以放到后续阶段。

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
- v2 可以重新设计 state address，不需要兼容 v1 state address。

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

- `profile` 可以包含与 `host` 相同的领域块，但不能包含 `ssh` 和 `state`。
- `profile` 不会独立 apply。
- `imports` 必须是静态 profile 引用。
- `imports` 按声明顺序合并。
- `host` 自己的配置最后合并，优先级高于所有 profile。
- profile 可以 import profile，但必须检测循环引用。

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
  remove  = ["telnet"]
}
```

要求：

- `install` 编译成 ensure present。
- `remove` 编译成 ensure absent。
- 同一个包不能同时出现在 `install` 和 `remove`。
- list 合并默认 append 去重。
- 包名必须非空。
- 后续可支持对象形式，例如 pin version，但第一阶段不做版本锁定。

## Files 功能

`files` 管理普通文件。

建议语法：

```hcl
files {
  "/etc/motd" = {
    content = "hello\n"
    mode    = "0644"
    owner   = "root"
    group   = "root"
  }
}
```

要求：

- key 是远端绝对路径。
- `content` 和 `source` 二选一。
- `owner` 默认 `root`。
- `group` 默认 `root`。
- `mode` 默认 `0644`。
- `ensure` 默认 `present`，支持 `absent`。
- 同一路径合并后只能有一个最终定义。

## Directories 功能

`directories` 管理目录。

```hcl
directories {
  "/opt/app" = {
    owner = "app"
    group = "app"
    mode  = "0755"
  }
}
```

要求：

- key 是远端绝对路径。
- `owner` 默认 `root`。
- `group` 默认 `root`。
- `mode` 默认 `0755`。
- `ensure` 默认 `present`，支持 `absent`。
- 后续再设计 recursive 行为，第一阶段不递归修改已有内容。

## Groups 功能

`groups` 管理 Unix group。

```hcl
groups {
  deploy = {
    gid    = 1500
    system = false
  }
}
```

要求：

- key 是 group 名。
- `gid` 可选。
- `system` 默认 false。
- `ensure` 默认 `present`，支持 `absent`。
- group 名不能为空。

## Users 功能

`users` 管理 Unix user。

```hcl
users {
  deployer = {
    uid    = 1500
    gid    = "deploy"
    groups = ["sudo"]
    home   = "/home/deployer"
    shell  = "/bin/bash"
  }
}
```

要求：

- key 是 user 名。
- `uid` 可选。
- `gid` 可选，可以引用同一 host 中声明的 group。
- `groups` 默认 append 去重。
- `home` 可选。
- `shell` 可选。
- `ensure` 默认 `present`，支持 `absent`。
- 如果 `gid` 引用同一配置内的 group，编译器自动生成 user 依赖 group。

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
- 不要过早封装 nftables、networkd、ssh hardening。
- 可以先通过 `files`、`systemd`、`services` 覆盖相关用例。

## 中间表达

v2 必须定义中间表达 `HostSpec`。

编译链路：

```text
HCL AST
  -> profile/host merge
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
- 支持 `imports` 静态引用。
- 支持 `ssh`、`state`、`kernel`、`packages` 的最小字段。
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
- 填充 ssh/state 默认值。
- 归一化 kernel modules/sysctl。
- 归一化 packages install/remove。
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
- release binary 高阶语法设计。

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
- plan 输出用户层 v2 地址。
- apply 能正确执行 kernel module、sysctl、package。
- 每台 host 使用独立 state。
- validate 能发现 imports 循环、字段冲突、资源图环。
- 不需要写任何 `debian_*` 资源即可完成基础系统配置。
