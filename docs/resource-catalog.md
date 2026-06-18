# DebianForm 运维资源设计目录

本文档是 DebianForm 的资源参考和后续实现路线图，用于回答三个问题：

1. Debian 日常运维能力应该由哪个 DebianForm 资源负责。
2. 资源应该暴露哪些 HCL 字段，以及如何读取、比较和应用状态。
3. Ansible 中常见的模块和工作流，如何映射为 Terraform/OpenTofu 风格的声明式配置。

本文档不是当前实现能力的完整承诺。具体状态以每个资源旁的标记为准，已实现行为仍以
[requirements.md](requirements.md) 和代码为准。

新增资源、provider 和示例必须遵循
[module-design.md](module-design.md)：用户声明系统事实，provider 推导依赖图和内部动作。

## 目录

- [1. 状态与优先级](#1-状态与优先级)
- [2. 设计边界](#2-设计边界)
- [3. Terraform 风格资源模型](#3-terraform-风格资源模型)
- [4. Ansible 概念映射](#4-ansible-概念映射)
- [5. 快速资源索引](#5-快速资源索引)
- [6. 软件包与 APT](#6-软件包与-apt)
- [7. 文件与内容分发](#7-文件与内容分发)
- [8. 用户、组与 SSH](#8-用户组与-ssh)
- [9. systemd 与主机基础配置](#9-systemd-与主机基础配置)
- [10. 网络与防火墙](#10-网络与防火墙)
- [11. 内核与系统参数](#11-内核与系统参数)
- [12. 存储与挂载](#12-存储与挂载)
- [13. 安全与信任](#13-安全与信任)
- [14. 日志与周期任务](#14-日志与周期任务)
- [15. 动作命令](#15-动作命令)
- [16. 诊断命令](#16-诊断命令)
- [17. 不建议提供的抽象](#17-不建议提供的抽象)
- [18. 推荐实现顺序](#18-推荐实现顺序)
- [19. 资源验收标准](#19-资源验收标准)
- [20. 参考资料](#20-参考资料)

## 1. 状态与优先级

资源状态标记：

| 标记 | 含义 |
| --- | --- |
| `已实现` | 当前代码已经支持，可用于 MVP |
| `增强` | 资源已经存在，但本文档提出了尚未实现的字段或行为 |
| `P1` | 下一阶段优先实现的高频能力 |
| `P2` | 常用能力，P1 稳定后实现 |
| `P3` | 有价值但风险较高、使用频率较低或可由现有资源替代 |
| `不规划` | 明确不准备抽象为资源 |

能力类型：

| 类型 | 含义 |
| --- | --- |
| `声明资源` | 描述长期期望状态，参与 plan、apply、check 和 state |
| `原生文件` | 管理 Debian 原生配置内容，只薄封装校验和激活 |
| `动作命令` | 执行一次性操作，不伪装成长期资源 |
| `诊断命令` | 只读查询，不写入 state |

## 2. 设计边界

### 2.1 应该成为资源的能力

满足以下大部分条件时，应实现为资源：

- 存在明确、可重复读取的当前状态。
- 可以描述稳定的期望状态。
- 重复执行不会持续产生副作用。
- 可以在 apply 前准确显示计划。
- 能够定义资源身份，例如包名、用户名、文件路径或 unit 名称。
- 删除或停用行为能够被明确表达。

典型例子是软件包、文件、用户、systemd unit、挂载点和 sysctl。

### 2.2 应该管理原生配置的能力

如果底层格式已经稳定、文档完整，并且字段很多或变化快，DebianForm 不应该重新发明 DSL。

适合直接管理原生内容的组件：

- systemd unit、drop-in、timer、socket。
- systemd-networkd `.network`、`.netdev`、`.link`。
- nftables。
- sysctl.d、modules-load.d、tmpfiles.d、sysusers.d。
- sshd、journald、resolved、logrotate。
- APT deb822 `.sources` 和 preferences。

DebianForm 对这类资源负责：

- 生成稳定资源地址。
- 写入原生文件并管理 owner、group、mode。
- 保存内容 hash 和远端路径到 state。
- 在替换前进行组件原生校验。
- 只在内容改变时触发 reload、restart 或 handler。

### 2.3 不应该成为资源的能力

以下操作没有稳定期望状态，应该是动作或诊断命令：

- 任意 shell 命令。
- 整机升级。
- 立即重启。
- 查看日志、磁盘、内存、进程和 socket。
- 一次性数据库迁移。
- 以“最后一次运行成功”为目标的脚本。

## 3. Terraform 风格资源模型

DebianForm 使用 HCL，并借用 Terraform/OpenTofu 的资源模型，但不是 OpenTofu provider，也不要求配置能被 `tofu` 直接执行。

### 3.1 资源 block

```hcl
debian_package "nginx" {
  host   = "server1"
  name   = "nginx"
  ensure = "present"
}
```

资源类型和本地名称共同构成地址：

```text
debian_package.nginx
```

本地名称只用于配置引用和 state。远端真实身份应放在 `name`、`path`、`device` 等字段中。

### 3.2 通用 meta-argument

所有资源应逐步统一支持：

| 字段 | 状态 | 含义 |
| --- | --- | --- |
| `host` | 已实现 | 目标主机或 SSH config alias |
| `for_each` | 已实现 | 从 map 或字符串 set 创建多个实例 |
| `depends_on` | 已实现 | 声明显式依赖 |
| `notify` | 已实现 | 资源实际改变后触发 handler |
| `lifecycle.prevent_destroy` | 规划 | 阻止危险删除 |
| `timeouts` | 规划 | 为资源读取和应用设置超时 |

不计划照搬所有 Terraform lifecycle 选项。`create_before_destroy` 对大多数系统配置没有清晰含义，
`ignore_changes` 也容易隐藏真实 drift。

条件表达式支持 Terraform/OpenTofu 风格的 `condition ? true_value : false_value`，并支持 `==` 和
`!=` 作为基础条件比较。条件必须是布尔值，且只求值被选中的分支。

未来可考虑：

```hcl
debian_mount "data" {
  host   = "server1"
  path   = "/srv/data"
  source = "/dev/vdb1"
  fstype = "ext4"

  lifecycle {
    prevent_destroy = true
  }
}
```

### 3.3 `for_each`

`for_each` 应与 OpenTofu 一样使用稳定 key。实例 key 会进入资源地址，不允许使用敏感值或 apply
阶段才知道的值。

```hcl
locals {
  base_packages = toset([
    "curl",
    "jq",
    "vim",
  ])
}

debian_package "base" {
  for_each = local.base_packages

  host = "server1"
  name = each.key
}
```

资源地址：

```text
debian_package.base["curl"]
debian_package.base["jq"]
debian_package.base["vim"]
```

map 适合值包含多个属性的情况：

```hcl
debian_user "operator" {
  for_each = {
    alice = {
      uid    = 2001
      groups = ["sudo"]
    }
    bob = {
      uid    = 2002
      groups = []
    }
  }

  host   = "server1"
  name   = each.key
  uid    = each.value.uid
  groups = each.value.groups
}
```

### 3.4 依赖图

资源必须按语义依赖构建有向无环图。不能仅依赖文件声明顺序，也不能把常规正确性
建立在用户手写 `depends_on` 上。

```hcl
debian_service "nginx" {
  host  = "server1"
  name  = "nginx"
  state = "running"

  depends_on = [
    debian_package.nginx,
    debian_file.nginx_config,
  ]
}
```

后续 provider 应声明 `Provides`、`Requires`、`Triggers` 和 `Locks`，让引擎自动推导
资源依赖、内部动作和并发互斥。显式属性引用应自动形成依赖；`depends_on` 只用于
无法由资源语义或值引用推断的隐藏依赖。

### 3.5 `notify` 与 handler

handler 对应 Ansible handler，但执行规则由资源图控制：

```hcl
handler "reload_sshd" {
  host    = "server1"
  command = "systemctl reload ssh"
}

debian_sshd_config_file "hardening" {
  host    = "server1"
  name    = "10-hardening.conf"
  content = file("${path.module}/files/10-hardening.conf")

  notify = [
    handler.reload_sshd,
  ]
}
```

同一 handler 在一次 apply 中最多运行一次，并且只在通知它的资源实际变化后运行。

### 3.6 `ensure` 与 `state`

为了避免字段含义混乱，统一约定：

- `ensure` 表示对象是否存在：`present`、`absent`。
- `state` 表示已经存在对象的运行时状态：`running`、`stopped`、`mounted` 等。
- `enabled` 表示是否在开机或组件加载时启用。
- 动作值如 `restarted`、`reloaded` 只能用于明确的动作字段，不能被当成稳定状态。

例如：

```hcl
debian_service "nginx" {
  host    = "server1"
  name    = "nginx"
  enabled = true
  state   = "running"
}
```

### 3.7 删除语义

DebianForm 的默认删除语义比 Terraform 保守：

- 配置中显式写 `ensure = "absent"` 才删除远端对象。
- 仅从配置中移除 block，默认不删除远端对象。
- 对危险资源应支持 `lifecycle.prevent_destroy = true`。
- plan 必须明确区分“从 state 遗忘”和“删除远端对象”。

未来可以增加类似 OpenTofu `removed` block 的显式语法，但 MVP 不自动销毁未声明的系统对象。

### 3.8 资源输出与 state 引用

每个资源都必须有稳定地址。后续资源属性引用至少应支持：

| 属性 | 适用资源 | 含义 |
| --- | --- | --- |
| `id` | 所有资源 | 远端对象规范 ID |
| `host` | 所有资源 | 目标主机 |
| `path` | 文件型资源 | 最终远端路径 |
| `sha256` | 内容型资源 | 最终内容 hash |
| `version` | 软件包和下载 | 实际版本 |
| `uid`、`gid` | 用户和组 | 实际数字 ID |

例如，原生配置文件仍然可以被稳定引用：

```hcl
depends_on = [
  debian_systemd_unit.node_exporter,
]
```

state 中应保存资源地址和用于 drift 判断的最小元数据，不能保存私钥、密码或完整敏感内容。

### 3.9 plan 输出

每个变更必须属于以下一种：

| 动作 | 含义 |
| --- | --- |
| `create` | 远端对象不存在，将创建 |
| `update` | 对象存在，但属性不符合配置 |
| `delete` | 配置显式要求删除 |
| `replace` | 无法原地更新，需要安全替换 |
| `action` | reload、restart 等一次性动作 |
| `no-op` | 当前状态已经符合配置 |

文件型资源应显示 metadata 差异和内容摘要，敏感内容只能显示 hash，不能输出原文。

## 4. Ansible 概念映射

DebianForm 可以参考 Ansible 的模块覆盖面，但配置模型采用 Terraform/OpenTofu 风格。

| Ansible 概念 | DebianForm 对应 | 主要区别 |
| --- | --- | --- |
| inventory host | SSH config alias 或 `host` block | 不维护独立 inventory 格式 |
| module task | `debian_*` 资源 | 描述长期状态，不描述执行步骤 |
| task loop | `for_each` | key 进入稳定资源地址 |
| task order | 语义依赖图，`depends_on` 作为逃生口 | 不把文件顺序或手写步骤当作主要依赖 |
| handler/notify | `handler` + `notify` | 一次 apply 中去重并延迟执行 |
| check mode | `dbf plan` | 同时结合共享 state 和远端实况 |
| diff mode | plan 详细差异 | 资源必须实现稳定 diff |
| facts | 未来 `data` 或 `dbf facts` | 只读，不作为 managed resource |
| command/shell | `dbf exec` 或 handler | 不鼓励伪装成幂等资源 |
| role | 未来 module/目录复用 | MVP 只加载当前目录配置 |
| tags/limit | `--host`，未来 selector | 不把 tag 作为资源身份 |

Ansible 的一个 task 通常表示“运行一次模块”；DebianForm 的一个资源表示“这个远端对象应持续处于某状态”。

## 5. 快速资源索引

| 领域 | DebianForm 资源 | 状态 | Ansible 参考 |
| --- | --- | --- | --- |
| 软件包 | `debian_package` | 已实现/增强 | `ansible.builtin.apt` |
| APT 仓库 | `debian_apt_repository` | 已实现/增强 | `ansible.builtin.deb822_repository` |
| APT source 文件 | `debian_apt_source` | 低层 escape hatch | `copy`/`template` |
| APT pin | `debian_apt_preferences_file` | P2 | `copy`/`template` |
| 文件 | `debian_file` | 已实现/增强 | `copy`/`template` |
| 目录 | `debian_directory` | 已实现/增强 | `file` |
| 符号链接 | `debian_symlink` | P2 | `file state=link` |
| 下载 | `debian_download` | P1 | `get_url` |
| 解压 | `debian_archive` | P1 | `unarchive` |
| 用户 | `debian_user` | P1 | `user` |
| 组 | `debian_group` | P1 | `group` |
| SSH key | `debian_authorized_key` | P1 | `ansible.posix.authorized_key` |
| sudoers | `debian_sudoers_file` | P2 | `copy` + `visudo -cf` |
| systemd 服务 | `debian_service` | 已实现/增强 | `systemd_service` |
| systemd unit | `debian_systemd_unit` | P1 | `copy` + `systemd_service` |
| tmpfiles | `debian_tmpfiles_file` | P2 | `copy` + `systemd-tmpfiles` |
| sysusers | `debian_sysusers_file` | P2 | `copy` + `systemd-sysusers` |
| 主机名 | `debian_hostname` | P1 | `hostname` |
| 时区 | `debian_timezone` | P1 | `community.general.timezone` |
| networkd | `debian_networkd_file` | 已实现 | `copy` |
| resolved | `debian_resolved_file` | P2 | `copy` |
| nftables | `debian_nftables_file` | 已实现 | `copy` + handler |
| 内核模块 | `debian_kernel_module` | 已实现 | `community.general.modprobe` |
| sysctl | `debian_sysctl` | 已实现 | `ansible.posix.sysctl` |
| 挂载 | `debian_mount` | P1 | `ansible.posix.mount` |
| swap file | `debian_swap_file` | P2 | 多模块组合 |
| CA 证书 | `debian_ca_certificate` | P1 | `copy` + handler |
| sshd | `debian_sshd_config_file` | P2 | `copy` + validate |
| journald | `debian_journald_file` | P2 | `copy` + handler |
| logrotate | `debian_logrotate_file` | P2 | `copy` + validate |
| cron | `debian_cron_file` | P3 | `cron` |
| alternatives | `debian_alternative` | P2 | `community.general.alternatives` |

## 6. 软件包与 APT

### 6.1 `debian_package`

**状态：** 已实现，建议增强。

**底层：** `dpkg-query`、`apt-get`、`apt-mark`。

建议字段：

| 字段 | 含义 |
| --- | --- |
| `host` | 目标主机 |
| `name` | 包名 |
| `ensure` | `present`、`absent`、未来可支持 `latest` |
| `version` | 精确 Debian 包版本 |
| `update_cache` | 兼容/低层字段；新模块应由 APT repository 变化触发 host 级 `apt_update[host]` |
| `cache_valid_time` | 兼容/低层字段；新模块应由 host 级 APT cache 策略控制 |
| `install_recommends` | 是否安装 Recommends |
| `hold` | 是否通过 `apt-mark hold` 锁定 |
| `purge` | 删除时是否清理配置 |
| `allow_downgrade` | 是否允许降级，默认 `false` |
| `lock_timeout` | 等待 dpkg/apt 锁的时间 |

```hcl
debian_package "nginx" {
  host               = "server1"
  name               = "nginx"
  ensure             = "present"
  update_cache       = true
  cache_valid_time   = 3600
  install_recommends = false
  hold               = false
}
```

读取状态：

- 使用 `dpkg-query` 判断是否安装和实际版本。
- 使用 `apt-mark showhold` 判断 hold。
- 不通过解析本地化的 `apt` 人类输出判断状态。

应用要求：

- 使用非交互 `apt-get`。
- 新模块不应要求用户在每个 package 上手写 `update_cache`。
- plan 必须显示安装、升级、降级、hold、unhold、remove 或 purge。
- 降级和可能删除依赖的软件包必须显式允许。
- 整机升级不能通过 `debian_package "*"` 隐式完成，应使用动作命令。

### 6.2 `debian_apt_repository`

**状态：** 已实现，建议增强；常规场景优先使用。

**类型：** 高层声明资源。

**底层：** `/etc/apt/keyrings`、`/etc/apt/sources.list.d/*.sources`、deb822、`apt-get update`。

Debian 13 应优先使用 deb822 `.sources`，不新增对废弃 `apt-key` 工作流的抽象。常规用户
不应单独声明 keyring 目录、key 文件、source 文件和 `apt-get update` 顺序。

```hcl
debian_apt_repository "docker" {
  host = "server1"
  name = "docker"

  types         = ["deb"]
  uris          = ["https://download.docker.com/linux/debian"]
  suites        = ["trixie"]
  components    = ["stable"]
  architectures = ["amd64"]
  enabled       = true

  key = {
    url  = "https://download.docker.com/linux/debian/gpg"
    path = "/etc/apt/keyrings/docker.asc"
  }
}
```

建议字段：

- `name`：生成 `/etc/apt/sources.list.d/<name>.sources`。
- `content` 或结构化的 deb822 字段二选一。
- `types`、`uris`、`suites`、`components`、`architectures`。
- `key`：可选 object，支持 `url`、`content`、`path`；本地文件内容通过 `content = file("...")` 提供。
- `signed_by`：低层 escape hatch，只接受绝对 keyring 路径或内嵌 key。
- `enabled`、`ensure`。

应用要求：

- 通过 HTTPS 获取 `key.url` 时，provider 默认安装或要求 `ca-certificates`，并把它放入
  内部依赖图，而不是要求用户手写 `debian_package.ca_certificates`。
- `key.content` 来自本地或内嵌内容时，不触发远端下载，也不自动要求
  `ca-certificates`。
- 结构化字段必须序列化为稳定顺序的 deb822。
- 修改 key 或 source 后先验证语法，再触发 host 级 `apt_update[host]`，同一 host 去重一次。
- `trusted = true`、`allow_insecure = true` 一类选项默认不提供；确有需求时必须明显标注风险。

### 6.3 `debian_apt_source`

**状态：** 低层 escape hatch，当前已实现部分能力。

只管理 deb822 source 文件。它适合兼容旧配置或用户明确要直接管理原生 source 文件的
场景；新示例和常规模块应优先使用 `debian_apt_repository`。

### 6.4 `debian_apt_preferences_file`

**状态：** P2。

直接管理 `/etc/apt/preferences.d/<name>` 原生内容。

```hcl
debian_apt_preferences_file "docker" {
  host = "server1"
  name = "docker"

  content = <<-EOF
    Package: docker-ce*
    Pin: origin download.docker.com
    Pin-Priority: 700
  EOF
}
```

该资源不重新抽象 APT pinning 语法。它只负责文件、权限、hash 和 `apt-cache policy` 可选验证。

## 7. 文件与内容分发

### 7.1 `debian_file`

**状态：** 已实现，建议增强。

建议增加：

- `ensure = "present" | "absent"`。
- `content`、`source` 二选一。
- `validate_command`，其中 `%s` 代表临时文件路径。
- `sensitive = true`，隐藏内容 diff。
- 原子写入：同目录临时文件、fsync、rename。
- 符号链接处理策略，默认拒绝跟随未知链接。

```hcl
debian_file "nginx_config" {
  host             = "server1"
  path             = "/etc/nginx/nginx.conf"
  source           = "${path.module}/files/nginx.conf"
  owner            = "root"
  group            = "root"
  mode             = "0644"
  backup           = true
  validate_command = "nginx -t -c %s"

  notify = [
    handler.reload_nginx,
  ]
}
```

不建议优先实现 Ansible `lineinfile`/`blockinfile` 的完全等价物。对 Debian 原生组件优先管理完整文件或
drop-in，避免多个资源竞争同一个文件。

### 7.2 `debian_directory`

**状态：** 已实现，建议增强。

建议字段：

- `ensure`、`owner`、`group`、`mode`。
- `parents = true`。
- `recursive = false`，默认不递归修改已有内容权限。

删除非空目录必须显式设置 `recursive = true`，并在 plan 中标记为危险操作。

### 7.3 `debian_symlink`

**状态：** P2。

```hcl
debian_symlink "current" {
  host   = "server1"
  path   = "/opt/myapp/current"
  target = "/opt/myapp/releases/1.4.0"
}
```

读取时使用 `readlink`，不能跟随链接后只比较目标内容。替换真实文件或目录为链接必须显式允许。

### 7.4 `debian_download`

**状态：** P1，已在旧需求中规划。

```hcl
debian_download "node_exporter" {
  host   = "server1"
  url    = "https://example.com/node_exporter"
  path   = "/usr/local/bin/node_exporter"
  sha256 = "..."
  owner  = "root"
  group  = "root"
  mode   = "0755"
}
```

要求：

- `sha256` 应默认必填；无 hash 时 plan 必须警告。
- 下载到临时路径，校验后原子替换。
- 支持 HTTP 重定向、超时和有限重试。
- state 保存 URL、目标路径和最终 hash，不保存认证 header。
- 远端已有文件 hash 相同则不下载。

### 7.5 `debian_archive`

**状态：** P1。

管理一个由固定 hash 归档展开得到的目录。

```hcl
debian_archive "node_exporter" {
  host        = "server1"
  source      = "/var/cache/debianform/node_exporter.tar.gz"
  destination = "/opt/node_exporter/1.9.0"
  sha256      = "..."
  strip_components = 1

  depends_on = [
    debian_download.node_exporter_archive,
  ]
}
```

要求：

- 支持 tar.gz、tar.xz、tar.zst 和 zip 的明确白名单。
- 防止归档通过 `../` 或绝对路径逃逸 destination。
- 使用 marker/state hash 判断是否需要重新展开。
- 不默认删除 destination 中不属于归档的额外文件；如需精确镜像，必须单独显式启用。

## 8. 用户、组与 SSH

### 8.1 `debian_group`

**状态：** P1。

```hcl
debian_group "deploy" {
  host   = "server1"
  name   = "deploy"
  gid    = 2000
  system = true
  ensure = "present"
}
```

读取使用 `getent group`。删除组前必须检查是否仍被用户作为主组使用。

### 8.2 `debian_user`

**状态：** P1。

```hcl
debian_user "deploy" {
  host       = "server1"
  name       = "deploy"
  uid        = 2000
  group      = "deploy"
  groups     = ["adm"]
  shell      = "/bin/bash"
  home       = "/home/deploy"
  create_home = true
  ensure     = "present"

  depends_on = [
    debian_group.deploy,
  ]
}
```

建议字段：

- `uid`、`group`、`groups`、`append_groups`。
- `home`、`create_home`、`move_home`。
- `shell`、`comment`、`system`。
- `locked`、`expires`。
- `remove_home`，删除用户时默认 `false`。

密码字段不应直接接受明文。第一版只支持锁定账户或已经由外部工具生成的密码 hash，并标记为敏感。

### 8.3 `debian_authorized_key`

**状态：** P1。

```hcl
debian_authorized_key "deploy" {
  for_each = {
    alice = {
      key     = "ssh-ed25519 AAAA... alice@example"
      options = []
    }
    ci = {
      key     = "ssh-ed25519 AAAA... ci@example"
      options = ["restrict"]
    }
  }

  host    = "server1"
  user    = "deploy"
  key     = each.value.key
  ensure  = "present"
  options = each.value.options

  depends_on = [
    debian_user.deploy,
  ]
}
```

资源身份应由用户和 key fingerprint 组成，而不是注释文本。默认每个资源只管理一把 key，避免
`exclusive = true` 在多个 `for_each` 实例之间互相删除。

## 9. systemd 与主机基础配置

### 9.1 `debian_service`

**状态：** 已实现，建议增强。

底层使用 `systemctl show`、`is-enabled`、`start`、`stop`、`enable`、`disable`、`mask`。

建议稳定字段：

- `ensure = "present"` 只表示 unit 应可被 systemd 找到。
- `state = "running" | "stopped"`。
- `enabled`、`masked`。
- `daemon_reload` 不作为长期状态，通常由 unit 文件资源触发。

`restarted` 和 `reloaded` 是动作，不应在每次 `check` 中被判断为 drift。它们应通过 handler 或显式
`action` 字段执行一次。

### 9.2 `debian_systemd_unit`

**状态：** P1。

直接管理 `.service`、`.timer`、`.socket`、`.path`、`.mount` 和 drop-in。

```hcl
debian_systemd_unit "node_exporter" {
  host    = "server1"
  name    = "node-exporter.service"
  enabled = true
  state   = "running"

  content = <<-UNIT
    [Unit]
    Description=Prometheus Node Exporter
    After=network-online.target

    [Service]
    User=node-exporter
    ExecStart=/usr/local/bin/node_exporter
    Restart=on-failure

    [Install]
    WantedBy=multi-user.target
  UNIT
}
```

要求：

- 默认路径 `/etc/systemd/system/<name>`。
- drop-in 可通过 `unit` + `dropin` 或显式 `path` 表达。
- 安装前执行 `systemd-analyze verify`。
- 内容变化后执行 `systemctl daemon-reload`。
- 只有需要时才 enable/disable/start/stop。
- unit 内容变化是否 restart 服务由 `restart_on_change` 或 handler 明确决定，默认不偷偷重启。

systemd timer 不需要单独发明 cron 风格 DSL：

```hcl
debian_systemd_unit "backup_timer" {
  host    = "server1"
  name    = "backup.timer"
  enabled = true
  state   = "running"

  content = <<-UNIT
    [Unit]
    Description=Run backup hourly

    [Timer]
    OnCalendar=hourly
    Persistent=true

    [Install]
    WantedBy=timers.target
  UNIT
}
```

### 9.3 `debian_tmpfiles_file`

**状态：** P2。

管理 `/etc/tmpfiles.d/<name>.conf`，使用原生 tmpfiles.d 格式。

- 校验：`systemd-tmpfiles --create --dry-run` 或等价安全检查。
- 激活：默认只写文件；`apply = true` 时执行指定文件。
- 不隐藏删除、清空和年龄清理规则的风险。

### 9.4 `debian_sysusers_file`

**状态：** P2。

管理 `/etc/sysusers.d/<name>.conf`，使用原生 sysusers.d 格式。

- 校验后再安装。
- `apply = true` 时调用 `systemd-sysusers`。
- sysusers 适合系统账户；需要完整生命周期控制时使用 `debian_user`/`debian_group`。

### 9.5 `debian_hostname`

**状态：** P1。

```hcl
debian_hostname "main" {
  host = "server1"
  name = "web01"
}
```

管理 static hostname，底层可使用 `/etc/hostname` 和 `hostnamectl`。不自动改 DNS。

### 9.6 `debian_timezone`

**状态：** P1。

```hcl
debian_timezone "main" {
  host = "server1"
  name = "Asia/Shanghai"
}
```

读取 `/etc/localtime` 链接或 `timedatectl show`，应用时使用 Debian/systemd 原生机制。

### 9.7 `debian_alternative`

**状态：** P2。

对 `update-alternatives` 的薄封装：

```hcl
debian_alternative "editor" {
  host = "server1"
  name = "editor"
  path = "/usr/bin/vim.basic"
}
```

不负责安装目标二进制，依赖对应 package 或 file/download 资源。

## 10. 网络与防火墙

### 10.1 `debian_networkd_file`

**状态：** 已实现。

继续坚持管理原生 `.network`、`.netdev`、`.link`，不尝试覆盖全部 networkd 字段。

增强方向：

- 安装前执行 `systemd-analyze verify` 或 networkd 支持的验证方式。
- `activate = false` 保持默认。
- plan 明确提示网络激活可能中断当前 SSH。
- 支持一个批次写入多个文件后只 reload 一次。

### 10.2 `debian_resolved_file`

**状态：** P2。

管理 `/etc/systemd/resolved.conf.d/<name>.conf` 原生 drop-in。

默认只写文件，通过 handler reload/restart `systemd-resolved`。不直接接管
`/etc/resolv.conf` 的符号链接，除非用户显式声明。

### 10.3 `debian_hosts_file`

**状态：** P3。

建议只管理一个独立生成的完整 `/etc/hosts` 文件，或暂时使用 `debian_file`。不优先实现逐行编辑，
因为行级资源容易相互覆盖。

### 10.4 `debian_nftables_file`

**状态：** 已实现。

继续使用 nft 原生语法：

- 安装前 `nft -c -f`。
- `activate = false` 默认不立即替换规则集。
- apply 计划必须标出 `flush ruleset` 等高风险内容。
- 后续可支持应用失败时恢复旧规则，但不能声称所有 SSH 失联都可自动回滚。

不规划 UFW/iptables 风格的跨防火墙 DSL。

## 11. 内核与系统参数

### 11.1 `debian_kernel_module`

**状态：** 已实现。

保持 `modprobe` + `/etc/modules-load.d/*.conf` 的薄封装。

增强方向：

- 支持 `options`，写入 `/etc/modprobe.d/*.conf`。
- unload 前检查引用和使用情况。
- 对内建模块正确识别，不能因 `/proc/modules` 中不存在而持续 plan。

### 11.2 `debian_sysctl`

**状态：** 已实现。

保持 `sysctl -w` + `/etc/sysctl.d/*.conf` 的薄封装。

要求：

- 分别比较运行时值和持久化文件。
- `apply` 和 `persist` 独立。
- 不存在的 key 应返回清晰错误。
- 同一路径由多个 sysctl 资源管理时必须检测冲突，或要求每个资源使用独立文件。

## 12. 存储与挂载

### 12.1 `debian_mount`

**状态：** P1。

管理 `/etc/fstab` 条目和当前 mount 状态。

```hcl
debian_mount "data" {
  host    = "server1"
  path    = "/srv/data"
  source  = "UUID=1111-2222"
  fstype  = "ext4"
  options = ["defaults", "noatime"]
  dump    = 0
  pass    = 2
  enabled = true
  state   = "mounted"
}
```

要求：

- 使用 `findmnt --json` 或稳定机器输出读取当前 mount。
- 使用 libmount 可接受的方式生成或更新 fstab，不能简单做字符串替换。
- `state = "mounted"` 表示当前挂载且持久化。
- `state = "unmounted"` 只卸载；是否删除 fstab 条目由 `enabled`/`ensure` 明确表达。
- 改变已挂载文件系统的 source/fstype/options 必须在 plan 中标记风险。
- 删除挂载点目录不属于该资源职责。

### 12.2 `debian_swap_file`

**状态：** P2。

```hcl
debian_swap_file "main" {
  host     = "server1"
  path     = "/swapfile"
  size     = "4GiB"
  priority = 10
  enabled  = true
  active   = true
}
```

要求：

- 已存在且大小不符时默认不破坏性重建，必须显式允许 replace。
- 使用安全权限并在 `mkswap` 前确认目标是普通文件。
- 管理 fstab 条目和 `swapon` 状态。

### 12.3 文件系统和 LVM

**状态：** P3。

`mkfs`、分区和 LVM 属于高破坏性操作。只有在以下能力成熟后再考虑：

- `prevent_destroy`。
- 精确设备身份和 UUID 检查。
- 明确 replace plan。
- 自动化虚拟机集成测试。
- 默认拒绝已有签名的块设备。

MVP 和 P1 不实现。

## 13. 安全与信任

### 13.1 `debian_ca_certificate`

**状态：** P1。

```hcl
debian_ca_certificate "internal" {
  host    = "server1"
  name    = "internal-root-ca"
  source  = "${path.module}/files/internal-root-ca.crt"
  ensure  = "present"
  activate = true
}
```

底层管理 `/usr/local/share/ca-certificates/*.crt` 并按需执行 `update-ca-certificates`。

要求：

- 校验证书可解析且不是私钥。
- 内容变化或删除后才更新 trust store。
- state 只保存证书 fingerprint/hash。

### 13.2 `debian_sudoers_file`

**状态：** P2。

直接管理 `/etc/sudoers.d/<name>`：

- owner/group 必须为 root。
- mode 默认 `0440`。
- 安装前必须执行 `visudo -cf <temporary-file>`。
- 失败时不能替换有效配置。

### 13.3 `debian_sshd_config_file`

**状态：** P2。

管理 `/etc/ssh/sshd_config.d/<name>.conf` 原生 drop-in。

要求：

- 使用 `sshd -t` 验证完整配置。
- 默认只 reload，不 restart。
- 修改监听地址、端口、认证方式或 root 登录策略时，plan 显示 SSH 失联警告。
- 不自动修改当前客户端的 SSH config。

### 13.4 `debian_limits_file`

**状态：** P3。

管理 `/etc/security/limits.d/<name>.conf` 原生内容。由于 systemd 服务通常应使用 unit 中的
`LimitNOFILE=` 等字段，只有 PAM session 场景才使用该资源。

## 14. 日志与周期任务

### 14.1 `debian_journald_file`

**状态：** P2。

管理 `/etc/systemd/journald.conf.d/<name>.conf` 原生 drop-in。

- 安装前验证配置。
- 默认通过 handler reload/restart。
- 不抽象 journald 的全部字段。

### 14.2 `debian_logrotate_file`

**状态：** P2。

管理 `/etc/logrotate.d/<name>`：

- 使用 `logrotate --debug` 或等价方式验证。
- 默认不强制执行轮转。
- `postrotate` 等命令保留原生语法。

### 14.3 `debian_cron_file`

**状态：** P3。

systemd timer 是 DebianForm 的首选周期任务机制。只有兼容已有 cron 环境时，才管理
`/etc/cron.d/<name>` 原生内容。

不优先提供 cron 字段 DSL，因为这会重复 systemd timer 和 cron 自身语法。

## 15. 动作命令

以下能力不进入资源 state。

### 15.1 `dbf exec`

```bash
dbf exec --host server1 -- uname -a
```

用途：

- 临时运维。
- 故障处理。
- 尚未资源化能力的逃生口。

要求：

- 默认一次只针对显式主机。
- 清晰显示 host、exit code、stdout、stderr。
- 不声称幂等，不出现在 plan 中。
- 后续批量执行必须有并发限制和失败策略。

### 15.2 `dbf upgrade`

```bash
dbf upgrade --host server1 --mode upgrade
dbf upgrade --host server1 --mode full-upgrade
```

整机升级属于动作，因为“最新”会随仓库时间变化。

要求：

- 明确区分 `upgrade` 和 `full-upgrade`。
- 先显示 apt 模拟结果。
- 默认拒绝隐式删除包。
- 支持检查 `/var/run/reboot-required`。
- 自动重启必须单独显式开启。

### 15.3 `dbf reboot`

```bash
dbf reboot --host server1 --wait
```

要求：

- 显式确认或 `--auto-approve`。
- 等待 SSH 断开并重新上线。
- 可设置超时。
- 不进入长期 state。

## 16. 诊断命令

诊断能力只读，不写 state：

```bash
dbf facts --host server1
dbf doctor --host server1
dbf diff --host server1
```

### 16.1 `dbf facts`

建议输出结构化信息：

- Debian 版本和 codename。
- kernel、architecture、boot ID。
- systemd 版本。
- 主机名、时区。
- 网络接口和地址摘要。
- 文件系统和 mount 摘要。

应提供 JSON 输出，供脚本使用。

### 16.2 `dbf doctor`

检查 DebianForm 运行前提：

- SSH 是否确实以 root 登录。
- 远端 Debian 版本是否受支持。
- state 路径和锁目录是否可写。
- `flock`、`base64`、`sh` 等基础命令是否存在。
- apt/dpkg 是否被锁。
- 磁盘是否接近满。
- 是否存在待重启标记。

### 16.3 不包装常用只读命令

不需要为以下命令逐个创造 DebianForm 子命令：

- `journalctl`、`dmesg`。
- `df`、`du`、`free`。
- `ps`、`top`。
- `ss`、`ip`。
- `lsblk`、`findmnt`。
- `lsof`。

用户可直接 SSH，或通过 `dbf exec` 使用这些命令。

## 17. 不建议提供的抽象

### 17.1 通用 `debian_command` 资源

不建议：

```hcl
debian_command "setup" {
  command = "curl ... | sh"
}
```

原因：

- 无法可靠读取当前状态。
- 无法生成真实 plan。
- 容易把运行成功误认为配置正确。
- state 只能记录命令运行过，不能说明远端对象当前是否符合要求。

命令只能用于 handler、显式动作或带有可靠 guard 的临时兼容层。

### 17.2 完全复制 Ansible 模块

不需要复制以下 Ansible 使用模式：

- `lineinfile` 和 `blockinfile` 作为首选配置方式。
- `shell`/`command` 注册输出后驱动大量条件 task。
- 依赖 YAML task 顺序表达资源关系。
- 为不同发行版提供统一 package/service 抽象。

DebianForm 只服务 Debian，应直接利用 Debian 和 systemd 的稳定接口。

### 17.3 高层应用 DSL

不内置 `debian_nginx_site`、`debian_postgresql_database`、`debian_docker_compose` 等应用特定资源。

这些能力后续应通过可复用 module、原生文件和基础资源组合，而不是无限扩大核心资源数量。

## 18. 推荐实现顺序

### P1：覆盖新主机最常见的基础工作

1. `debian_systemd_unit`
2. `debian_group`
3. `debian_user`
4. `debian_authorized_key`
5. `debian_apt_repository`
6. `debian_download`
7. `debian_archive`
8. `debian_mount`
9. `debian_hostname`
10. `debian_timezone`
11. `debian_ca_certificate`

同时增强：

- `debian_package`：hold、latest、cache_valid_time、lock_timeout。
- `debian_file`：absent、validate、sensitive、原子写入。
- `debian_service`：masked、稳定状态与一次性动作分离。

### P2：覆盖长期维护

1. `debian_sudoers_file`
2. `debian_sshd_config_file`
3. `debian_tmpfiles_file`
4. `debian_sysusers_file`
5. `debian_journald_file`
6. `debian_logrotate_file`
7. `debian_resolved_file`
8. `debian_swap_file`
9. `debian_alternative`
10. `debian_symlink`

### P3：有明确需求后再做

- `debian_cron_file`
- `debian_hosts_file`
- `debian_limits_file`
- 文件系统创建
- 分区和 LVM
- module 复用系统
- facts 数据源

## 19. 资源验收标准

每个新资源合并前必须满足：

### 19.1 配置和校验

- 字段有明确类型、默认值和互斥关系。
- `validate` 能在 SSH 前发现配置错误。
- 资源身份稳定，不依赖远端随机输出。
- `for_each` 实例地址稳定。

### 19.2 Read

- 从远端真实状态读取，不只相信 state。
- 使用机器可读接口；优先 JSON、`--show`、`getent`、`dpkg-query` 等稳定输出。
- 明确区分不存在、权限错误和命令不可用。
- 不受远端 locale 影响。

### 19.3 Plan

- 第二次 plan 必须是 no-op。
- 显示 create/update/delete/action。
- 高风险变更有明确警告。
- 敏感内容不进入终端或 state。
- plan 不执行写操作。

### 19.4 Apply

- 重复 apply 幂等。
- 文件使用临时文件、校验和原子替换。
- 失败时保留原配置或清晰报告部分修改。
- 只有实际变化才触发 notify。
- 写 state 前再次确认远端操作成功。

### 19.5 Check

- 人工修改远端状态后能够检测 drift。
- 不修改远端。
- 不因为无关字段或输出顺序产生假 drift。

### 19.6 测试

- 单元测试覆盖字段校验、diff 和命令生成。
- 至少一个 Debian 13 集成测试覆盖 create、no-op、update、delete。
- 危险资源必须在临时 VM 或可恢复快照中测试。
- 网络、SSH、防火墙和 mount 资源必须测试失败路径。

## 20. 参考资料

### OpenTofu

- [Resource blocks](https://opentofu.org/docs/language/resources/syntax/)
- [`for_each` meta-argument](https://opentofu.org/docs/language/meta-arguments/for_each/)
- [`depends_on` meta-argument](https://opentofu.org/docs/language/meta-arguments/depends_on/)
- [Lifecycle meta-argument](https://opentofu.org/docs/language/meta-arguments/lifecycle/)
- [State](https://opentofu.org/docs/language/state/)

### Ansible

- [ansible.builtin module index](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/index.html)
- [`ansible.builtin.apt`](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/apt_module.html)
- [`ansible.builtin.deb822_repository`](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/deb822_repository_module.html)
- [`ansible.builtin.systemd_service`](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/systemd_service_module.html)
- [`ansible.builtin.user`](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/user_module.html)
- [`ansible.builtin.get_url`](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/get_url_module.html)
- [`ansible.posix.authorized_key`](https://docs.ansible.com/projects/ansible/latest/collections/ansible/posix/authorized_key_module.html)
- [`ansible.posix.mount`](https://docs.ansible.com/projects/ansible/latest/collections/ansible/posix/mount_module.html)

### Debian 与 systemd

- [Debian releases](https://www.debian.org/releases/)
- [APT sources.list(5), including deb822 `.sources`](https://manpages.debian.org/trixie/apt/sources.list.5.en.html)
- [systemd.unit(5)](https://manpages.debian.org/trixie/systemd/systemd.unit.5.en.html)
- [systemd.timer(5)](https://manpages.debian.org/trixie/systemd/systemd.timer.5.en.html)
- [systemd-tmpfiles(8)](https://manpages.debian.org/trixie/systemd/systemd-tmpfiles.8.en.html)
- [systemd-sysusers(8)](https://manpages.debian.org/trixie/systemd/systemd-sysusers.8.en.html)
- [findmnt(8)](https://manpages.debian.org/trixie/mount/findmnt.8.en.html)
