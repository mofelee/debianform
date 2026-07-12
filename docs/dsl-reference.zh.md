# DebianForm DSL Reference

本文档按当前实现列出 `.dbf.hcl` 的已支持指令、字段、默认值和限制。教程式用法见
[用户手册](user-manual/README.zh.md)；命令行选项见 [CLI 手册](cli.zh.md)；
能力状态见 [支持矩阵](support-matrix.zh.md)。

带 `<!-- dbf-test:... -->` 标记的示例会被 `go test ./cmd/dbf` 抽取到临时目录，
并运行标记中的命令验证。

## 顶层 Block

| Block | 用途 |
| --- | --- |
| `locals` | 定义本文件后续表达式可引用的 `local.<name>`。不支持 nested block。 |
| `variable "<name>"` | 声明外部变量。支持 CLI、环境变量和 var-file 赋值。 |
| `profile "<name>"` | 可复用 host 配置片段，可被 profile/host import。 |
| `component "<name>"` | 可复用资源模板，可带 typed input 和 artifact 安装。 |
| `host "<name>"` | 目标主机配置入口。 |

`profile`、`component`、`host` 和显式 `host.component` instance 的 `<name>` 必须是合法的 HCL
native identifier。Unicode identifier 和首字符之后的连字符可用，例如 `主机一`、`edge-1`；空字符串、
`.`、`..`、FQDN、斜杠、引号或空白不能作为 label。host label 是稳定的逻辑名称，并会进入 resource
address 及默认 state/lock 文件名。远端 FQDN 或 IP 应写在 `ssh.host`，例如：

```hcl
host "web-prod" {
  ssh {
    host = "web.example.com"
  }
}
```

`profile` 和 `host` 都支持 `imports = [profile.name]`，导入顺序在前，当前 block 在后。
map 递归合并，list 去重追加，标量由后者覆盖。`profile` 不能声明
`system.hostname`、`platform.architecture`、`platform.codename`，也不能挂载 component。

`host` 支持两种 component 挂载方式。简写会用 component 名作为 instance 名；显式写法可
自定义 instance 名并传入 input。

<!-- dbf-test:name=component-mount-syntax;commands=validate,plan-offline,component-inspect:base -->

```hcl
component "base" {
  input "key" {
    type    = string
    default = "default"
  }

  files {
    file "/etc/component-mount.txt" {
      content = input.key
    }
  }
}

host "short_component_mount" {
  components = [component.base]
}

host "custom_component_mount" {
  component "custom_name" {
    source = component.base
    inputs = {
      key = "value"
    }
  }
}
```

`assert` 可出现在 `profile` 或 `host` 中：

<!-- dbf-test:name=assert-syntax;commands=validate,plan-offline -->

```hcl
host "assert_example" {
  system {
    hostname = "assert-example"
  }

  assert {
    condition = self.system.hostname != ""
    message   = "hostname must be set"
  }
}
```

assert 在合并后的 host spec 上求值，只能读取 `self`，当前支持 `contains()`。

## 变量和表达式

`variable` 字段：

| 字段 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `type` | 是 | 无 | `string`、`number`、`bool`、`any`、`list(T)`、`set(T)`、`map(T)`、`object({...})`、`tuple([...])`。 |
| `default` | 否 | 无 | 不设置时必须由外部传值。 |
| `description` | 否 | `""` | inspect 输出用说明。 |
| `nullable` | 否 | `true` | `false` 时拒绝 `null`。 |
| `sensitive` | 否 | `false` | plan/state/错误中脱敏。 |
| `ephemeral` | 否 | `false` | 支持边界由各资源字段定义。`files.file.content` 通过 `content_version` 使用 write-only 语义；APT source/signing key 与 nftables content 当前拒绝 ephemeral。 |
| `const` | 否 | `false` | 当前作为变量元数据输出；不要依赖它阻止外部覆盖。 |
| `deprecated` | 否 | `""` | 外部显式传值时输出 warning。 |
| `validation` | 否 | 无 | 包含 `condition` 和 `error_message`。只能读取当前 `var.<name>`。 |

`component input` 字段与 variable 类似，但不支持 `ephemeral` 和 `const`，validation 只能读取
当前 `input.<name>`。component 资源表达式可读取 `input.<name>`、顶层 `var.<name>` 和
`target`；`target` 是挂载目标 host 的合并后 spec，常用字段包括 `target.name`、
`target.platform.architecture` 和 `target.platform.codename`。旧的
`target.system.architecture` / `target.system.codename` 已移除，继续使用会报错并提示迁移到
`target.platform.*`。

object type 中可使用 `optional(T)` 或 `optional(T, default)`：

<!-- dbf-test:name=variable-object-syntax;commands=validate,plan-offline,variable-inspect -->

```hcl
variable "settings" {
  type = object({
    port = number
    tls  = optional(bool, false)
  })

  default = {
    port = 8080
  }
}

host "variable_object_example" {
  files {
    file "/etc/settings.json" {
      content = jsonencode(var.settings)
    }
  }
}
```

顶层 variable 外部值来源、优先级和 sensitive 运行时来源见 [CLI 手册](cli.zh.md#通用配置选择)。

普通表达式支持 `path.module`、`local.<name>`、`var.<name>`，以及 `file()`、
`templatefile()`、`jsonencode()`、`toset()`。在 `profile` 或 `host` 覆盖导入内容时，
可用 `force()` 覆盖 list，`before()` / `after()` 调整 list 合并顺序，`unset()` 移除
map/object 字段。

<!-- dbf-test:name=expression-and-merge-syntax;commands=validate,plan-offline;files=template.txt -->

```hcl
locals {
  modules = ["loop"]
}

variable "message" {
  type    = string
  default = "hello"
}

profile "base" {
  kernel {
    modules = ["tcp_bbr"]
    sysctl = {
      "net.core.default_qdisc" = "fq"
    }
  }
}

host "expression_example" {
  imports = [profile.base]

  kernel {
    modules = after(local.modules)
    sysctl = {
      "net.core.default_qdisc" = unset()
    }
  }

  files {
    file "/etc/expression-example.json" {
      content = jsonencode({
        module_dir = path.module
        message    = templatefile("template.txt", { message = var.message })
        modules    = toset(local.modules)
      })
    }
  }
}

host "force_example" {
  imports = [profile.base]

  kernel {
    modules = force(local.modules)
  }
}
```

## Host Domain

除特别说明外，下面的 domain 可出现在 `host` 和 `profile` 中；`component` 只能使用
`apt`、`packages`、`files`、`secrets`、`directories`、`groups`、`users`、`systemd`、`services`。

### ssh

`host`/`profile` 可用。默认 `host = host block label`、`port = 22`、`user = "root"`。
`user` 只能省略、设为空字符串或 `"root"`。

| 字段 | 说明 |
| --- | --- |
| `host` | SSH 连接名或地址。 |
| `port` | SSH 端口。 |
| `user` | 管理用户；当前只支持 root。 |
| `identity_file` | SSH identity file 路径。 |

### state

`host`/`profile` 可用。默认：

```text
/var/lib/debianform/state/<host>.json
/var/lock/debianform/state/<host>.lock
```

| 字段 | 说明 |
| --- | --- |
| `path` | 远端 state JSON 路径。 |
| `lock_path` | 远端 state lock 路径。 |

### system

`host` 可用。`profile` 只能设置 `timezone`、`locale`；`hostname` 是 host-only。
`hostname` 是期望托管的系统 hostname；省略时 DebianForm 不管理远端 hostname。
`timezone` 和 `locale` 也是 desired state；显式声明后 DebianForm 会在在线 `plan` / `apply` /
`check` 中检测和收敛远端主机。省略时不管理，不会把远端重置为默认值。
`architecture` 和 `codename` 不属于 `system`；旧的 `system.architecture` /
`system.codename` 已移除，继续使用会报错并提示迁移到 `platform`。

| 字段 | 说明 |
| --- | --- |
| `hostname` | 期望托管的系统 hostname；省略表示不管理 hostname。 |
| `timezone` | 期望系统 timezone，例如 `UTC`、`Asia/Shanghai`；必须是目标主机 `/usr/share/zoneinfo` 中存在的相对名称。 |
| `locale` | 期望系统默认 locale，即 `/etc/default/locale` 中的 `LANG`，例如 `C.UTF-8`、`en_US.UTF-8`。DebianForm 会在需要时安装/生成 locale，并保留未管理的 `LC_*` 行。 |

### platform

`host` 可用，`profile` 不可用。描述目标主机 platform facts，主要用于离线 plan、assert、
Docker 官方源和按架构选择的 component source。在线 `plan` / `apply` / `check` 会探测这些 facts；
真实主机配置通常不需要手写。

| 字段 | 说明 |
| --- | --- |
| `architecture` | Debian architecture，例如 `amd64`、`arm64`。 |
| `codename` | Debian codename，例如 Debian 12 的 `bookworm` 或 Debian 13 的 `trixie`。 |

### kernel

`host`/`profile` 可用。

| 字段 | 说明 |
| --- | --- |
| `modules` | 非空字符串列表；每项加载并持久化为 kernel module。 |
| `sysctl` | map(string)；每项写入持久化 sysctl 并应用运行时值。 |

### packages

`host`、`profile`、`component` 可用。

| 写法 | 说明 |
| --- | --- |
| `install = ["curl"]` | 安装 package。 |
| `package "<name>" { repositories = ["repo"] }` | 安装 package，并依赖指定 `apt.repository`。 |

`package` block 可带 `lifecycle { prevent_destroy = true }`。

### apt

`host`、`profile`、`component` 可用。

`repository "<name>"` 字段：

| 字段 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `uris` | present 时是 | 无 | 非空字符串列表。 |
| `suites` | present 时是 | 无 | 非空字符串列表。 |
| `components` | present 时是 | 无 | 非空字符串列表。 |
| `architectures` | 否 | `[]` | deb822 Architectures。 |
| `ensure` | 否 | `"present"` | `"present"` 或 `"absent"`。 |
| `signing_key` | 否 | 无 | nested block；present 时若声明 signing_key，需提供 `url` 或 `content`。 |

`signing_key` 字段：

| 字段 | 说明 |
| --- | --- |
| `url` | 下载 key；必须同时提供 `sha256`。 |
| `content` | 内联 key 内容；引用 sensitive 值时自动脱敏；当前不支持 ephemeral 值。若同时提供 `sha256` 会校验内容摘要。 |
| `sha256` | 64 位 hex。 |
| `path` | keyring 路径；默认 `/etc/apt/keyrings/<safe-name>.asc`。 |

`source_file "<label>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | 无 | 必须是绝对路径。 |
| `content` / `source` | 无 | present 时必须二选一；`content` 引用 sensitive 值时自动脱敏，当前不支持 ephemeral 值；`source` 相对配置文件目录解析。 |
| `owner` | `"root"` | 文件 owner。 |
| `group` | `"root"` | 文件 group。 |
| `mode` | `"0644"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |
| `on_destroy` | `"keep"` | `"keep"` 或 `"restore"`；sensitive content 不支持 `"restore"`，避免在 state 中保存待恢复明文。 |

`repository` 和 `source_file` 都支持 `lifecycle { prevent_destroy = true }`。

### files

`host`、`profile`、`component` 可用。

`file "<path-or-label>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | label | label 不是绝对路径时必须显式设置。最终路径必须是绝对路径。 |
| `content` / `source` | 无 | present 时必须二选一。 |
| `content_version` | `""` | `content` 含 ephemeral 值时必填，用于 write-only 变更判断。 |
| `owner` | `"root"` | 文件 owner。 |
| `group` | `"root"` | 文件 group。 |
| `mode` | `"0644"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |
| `sensitive` | `false` | 明确脱敏；含 sensitive/ephemeral 内容时会自动脱敏。 |
| `on_change` | 无 | 仅 component 内可用；可引用 component-local 或根 script，实际变化时生成并执行 operation。 |

支持 `lifecycle { prevent_destroy = true }`。

### secrets

`host`、`profile`、`component` 可用，但属于兼容旧写法；新配置优先使用
`variable + files.file content + content_version`。

`file "<path-or-label>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | label | 最终路径必须是绝对路径。 |
| `source` | 无 | present 时必填；相对配置文件目录解析。 |
| `owner` | `"root"` | 文件 owner。 |
| `group` | `"root"` | 文件 group。 |
| `mode` | `"0600"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |

支持 `lifecycle { prevent_destroy = true }`。使用时会输出 deprecation warning。

### directories

`host`、`profile`、`component` 可用。

`directory "<absolute-path>"` 字段：

| 字段 | 默认 |
| --- | --- |
| `owner` | `"root"` |
| `group` | `"root"` |
| `mode` | `"0755"` |
| `ensure` | `"present"` |

支持 `lifecycle { prevent_destroy = true }`。

### groups

`host`、`profile`、`component` 可用。

`group "<name>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `gid` | `""` | 可选 gid。 |
| `system` | `false` | 创建 system group。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |

支持 `lifecycle { prevent_destroy = true }`。

### users

`host`、`profile`、`component` 可用。

`user "<name>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `uid` | `""` | 可选 uid。 |
| `home` | `""` | 可选 home。 |
| `shell` | `""` | 可选 shell。 |
| `group` | `""` | primary group；非同名 group 必须被声明。 |
| `groups` | `[]` | supplementary groups。 |
| `system` | `false` | 创建 system user。 |
| `ssh_authorized_keys` | `[]` | authorized keys 列表。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |

支持 `lifecycle { prevent_destroy = true }`。

### systemd

`host`、`profile`、`component` 可用。

`unit "<name>"` 管理原始 unit 文件，路径固定为 `/etc/systemd/system/<name>`。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `content` / `source` | 无 | present 时必须二选一。 |
| `owner` | `"root"` | 文件 owner。 |
| `group` | `"root"` | 文件 group。 |
| `mode` | `"0644"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |

`service_unit "<name>"` 生成或管理 `.service` unit，unit 名会自动补 `.service`。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `content` / `source` | 无 | raw unit 模式；不能和结构化字段混用。 |
| `description` | unit 名去掉 `.service` | `[Unit] Description=`。 |
| `run` | 无 | 结构化模式必填；字符串或 argv 字符串列表。 |
| `type` | 无 | `simple`、`exec`、`forking`、`oneshot`、`dbus`、`notify`、`notify-reload`、`idle`。 |
| `user` / `group` | 无 | `[Service] User=` / `Group=`。 |
| `working_dir` | 无 | `[Service] WorkingDirectory=`。 |
| `environment` | `{}` | map(string)，渲染为 `Environment=`。 |
| `restart` | 无 | `no`、`on-success`、`on-failure`、`on-abnormal`、`on-watchdog`、`on-abort`、`always`。 |
| `restart_delay` | 无 | `[Service] RestartSec=`。 |
| `wants` / `after` | `[]` | `[Unit] Wants=` / `After=`。 |
| `wanted_by` | `["multi-user.target"]` | `[Install] WantedBy=`；空列表会省略 Install section。 |
| `stdout` / `stderr` | 无 | StandardOutput/StandardError。 |
| `owner` | `"root"` | unit 文件 owner。 |
| `file_group` | `"root"` | unit 文件 group。 |
| `mode` | `"0644"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |

`unit` 和 `service_unit` 支持 `lifecycle { prevent_destroy = true }`。

#### systemd.networkd

`systemd { networkd { ... } }` 支持生成 networkd 文件并管理 `systemd-networkd.service`。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enable` | `null` | 是否启用 systemd-networkd。 |

`netdev "<label>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | `/etc/systemd/network/<label>.netdev` | 必须是绝对路径。 |
| `netdev` | `{}` | 渲染为 `[NetDev]`；present 时必须含 `Name` 和 `Kind`。 |
| `wireguard` | `{}` | 渲染为 `[WireGuard]`；不允许 inline `PrivateKey`，用 `PrivateKeyFile`。 |
| `wireguard_peer "<label>"` | 无 | nested block，渲染为 `[WireGuardPeer]`；不允许 inline `PresharedKey`。 |
| `owner` / `group` | `"root"` / `"root"` | 文件 owner/group。 |
| `mode` | `"0644"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |

`network "<label>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | `/etc/systemd/network/<label>.network` | 必须是绝对路径。 |
| `match` | `{}` | 渲染为 `[Match]`；present 时必填。 |
| `network` | `{}` | 渲染为 `[Network]`；present 时必填。 |
| `owner` / `group` | `"root"` / `"root"` | 文件 owner/group。 |
| `mode` | `"0644"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |

networkd section value 可以是 string、number、bool 或这些类型的 list；bool 会渲染为
`yes`/`no`。`netdev`、`network` 支持 `lifecycle { prevent_destroy = true }`。

### services

`host`、`profile`、`component` 可用。

`service "<name>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `package` | `""` | 可选 package 依赖。 |
| `enabled` | `null` | `true`/`false` 时管理 enablement；省略则不管理。 |
| `state` | `""` | `running`、`stopped`、`restarted`、`reloaded`；省略则不管理运行状态。 |

service unit 名会自动补 `.service`。支持 `lifecycle { prevent_destroy = true }`。

### nftables

`host`、`profile` 可用。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enable` | `null` | 是否启用 nftables service。 |

`main { ... }` 管理 `/etc/nftables.conf`，`file "<label>" { ... }` 默认管理
`/etc/nftables.d/<label>.nft`。两者字段相同：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | 见上 | 必须是绝对路径。 |
| `content` / `source` | 无 | present 时必须二选一；`content` 引用 sensitive 值时自动脱敏，当前不支持 ephemeral 值。 |
| `owner` | `"root"` | 文件 owner。 |
| `group` | `"root"` | 文件 group。 |
| `mode` | `"0644"` | 四位八进制字符串。 |
| `ensure` | `"present"` | `"present"` 或 `"absent"`。 |
| `sensitive` | `false` | 显式要求输出脱敏；`content` 引用 sensitive 值时会自动启用。 |
| `validate` | `true` | 触发 `nft -c -f`。 |
| `activate` | `true` | 触发 nftables reload/activate。 |

支持 `lifecycle { prevent_destroy = true }`。

### docker

`host` 可用。

顶层字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enable` | `false` | 启用 Docker Engine 管理。 |
| `users` | `[]` | 加入 `docker` supplementary group 的用户列表。 |

`package` block 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `source` | `"official"` | `"official"`、`"none"`、`"custom"`。省略时使用 Docker 官方 APT 源。 |
| `channel` | `"stable"` | 当前只实际使用 stable。 |
| `version` | `null` | 已解析但版本 pinning 尚未实现。 |
| `repository_url` | `"https://download.docker.com/linux/debian"` | 仅 `source = "official"` 可用；替换 Docker official APT repository base URL。 |
| `gpg_url` | `"https://download.docker.com/linux/debian/gpg"` | 仅 `source = "official"` 可用；替换 Docker official APT signing key URL。 |
| `gpg_sha256` | 默认 `gpg_url` 时为 Docker official key SHA256；自定义 `gpg_url` 时为空 | 仅 `source = "official"` 可用；可选。自定义 `gpg_url` 时不会自动套用官方 SHA，可显式设置此字段启用 checksum 校验。 |
| `remove_conflicts` | `"auto"` | `"auto"`、`true`/`"true"`、`false`/`"false"`。 |

Aliyun Docker official APT 镜像示例：

```hcl
docker {
  package {
    repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
    gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
  }
}
```

这些字段只控制 Docker Engine 官方 APT 源和 signing key，不是 Docker registry mirror。它们和
`get.docker.com --mirror` 的目标相近，都是让 Docker 安装来源使用镜像站；区别是 DebianForm
不会运行 `get.docker.com` 安装脚本，而是声明式管理 APT source、key、package、service 和 state。
自定义 `gpg_url` 且省略 `gpg_sha256` 时，DebianForm 不校验 key 文件内容的 checksum；需要内容校验和
key 内容漂移检测时应设置 `gpg_sha256`。

`service` block 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enable` | `true` | 管理 docker.service enablement。 |
| `state` | `"running"` | `"running"` 或 `"stopped"`。 |

`daemon { settings = {...} }` 会生成 `/etc/docker/daemon.json`；settings 必须是 JSON-compatible
map，不能含 sensitive/ephemeral 值。

`compose "<name>"` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enable` | `true` | 是否管理该 Compose project。 |
| `state` | `"running"` | `"running"`、`"stopped"`、`"absent"`。 |
| `directory` | 无 | enable 时必须是绝对路径。 |
| `project` | compose label | Docker Compose project 名。 |
| `pull` | `"missing"` | `"never"`、`"missing"`、`"always"`。 |
| `recreate` | `"auto"` | `"auto"`、`"always"`、`"never"`。 |
| `remove_orphans` | `false` | 是否移除 orphan container。 |
| `after` | `["docker.service", "network-online.target"]` | 生成 service unit 的 After。 |
| `wanted_by` | `["multi-user.target"]` | 生成 service unit 的 WantedBy。 |

`compose.file` 必须恰好一个，不能带 label；`env_file "<label>"` 可有多个。二者字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | 无 | 必须是绝对路径。 |
| `content` / `source` | 无 | 必须二选一。 |
| `owner` | `"root"` | 文件 owner。 |
| `group` | `"root"` | 文件 group。 |
| `mode` | file `"0644"`，env_file `"0600"` | 四位八进制字符串。 |

`compose.service` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enable` | `true` | 是否生成并管理 systemd unit/service。 |
| `name` | `debianform-compose-<compose-label>` | unit base name，会自动补 `.service`。 |

Compose label、project、service name 必须以字母或数字开头，后续只能使用字母、数字、
`_`、`.`、`@`、`%`、`+`、`-`。

## Component Artifact

`component` 可只封装资源，也可声明 artifact 下载/构建/安装。artifact 相关字段：

| 字段/block | 说明 |
| --- | --- |
| `type` | `"binary"`、`"archive"`、`"file"`、`"ca_certificate"`、`"source"`。一旦声明 source/extract/build/install/version，`type` 必填。 |
| `version` | 进入 plan/state 的版本元数据。 |
| `source ["architecture"]` | 至少一个；可无 label 表示架构无关，或按 `platform.architecture` 选择。不能混用有 label 和无 label source。 |
| `extract` | `binary`、`archive`、`source` 可用；`archive` 必填。 |
| `build` | 仅 `source` 可用且必填。 |
| `install` | artifact component 必填。 |

`source` 字段：

| 字段 | 说明 |
| --- | --- |
| `url` | 非空 URL。 |
| `sha256` | 64 位 hex。 |

`extract` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `format` | 从 URL 推断 | `binary`: `zip`、`tar.gz`、`tar.xz`、`bz2`、`gz`；`archive`: `tar.gz`；`source`: `zip`、`tar.gz`、`tar.xz`。 |
| `strip_components` | `0` | 必须大于等于 0。 |
| `include` | `""` | `binary` 的 zip/tar 格式必填；`archive` 和 `source` 不支持。 |

`build` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `commands` | 无 | 必须至少一个 argv list；不经过 shell 拼接。 |
| `packages` | `[]` | build package 列表。 |
| `working_dir` | `""` | 相对路径。 |
| `output` | 无 | 相对路径，必填。 |
| `source_name` | `""` | 单文件源码构建时的相对文件名。 |

`install` 字段：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `path` | 无 | 必须是绝对路径。 |
| `owner` | `"root"` | 文件 owner。 |
| `group` | `"root"` | 文件 group。 |
| `mode` | `file`/`ca_certificate` 为 `"0644"`，其他为 `"0755"` | 四位八进制字符串。 |

`ca_certificate` 安装后会触发 `update-ca-certificates` operation。

### script / files.file.on_change

`script "<name>"` 可以声明在程序根部（与 `host`、`component` 同级），作为按目标 host
实例化的共享定义。不同 component 的文件可以引用同一个根声明：

<!-- dbf-test:name=shared-networkd-reload-syntax;commands=validate,plan-offline -->

```hcl
script "reload_networkd" {
  mode = "once"
  commands = [
    ["systemctl", "start", "systemd-networkd.service"],
    ["networkctl", "reload"],
  ]
}

component "wan" {
  files {
    file "/etc/systemd/network/20-wan.network" {
      content   = "[Match]\nName=enp1s0\n"
      on_change = script.reload_networkd
    }
  }
}

component "policy_route" {
  files {
    file "/etc/systemd/network/30-policy-routing.network" {
      content   = "[Match]\nName=enp2s0\n"
      on_change = script.reload_networkd
    }
  }
}

host "router1" {
  components = [component.wan, component.policy_route]
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }
}
```

引用和遮蔽规则是确定的：

- `script.<name>` 优先解析当前 component 的本地声明；没有同名本地声明时解析根声明。
- `global.script.<name>` 始终显式解析根声明，可在本地同名声明存在时消歧。
- 聚合键是解析后的声明身份，不是 label 文本或命令内容。同一 host 上引用同一根声明的资源合并；
  不同声明即使命令相同也不合并；同一声明在不同 host 上分别实例化。

根 script 当前只允许 `mode = "once"`，支持与 component script 相同的 `interpreter`、
`outputs`、`run`、`content`、`commands` 字段。它可以读取 `var.<name>`、`local.<name>`
和每 host 的 `target`，但不能读取任何 component 专属的 `input.*`。根 script 是定义，
只有引用它的资源实际变化时才生成执行步骤；未引用或无触发变化时不会执行。稳定 operation
地址形如 `host.router1.script["reload_networkd"]`，其 `depends_on` 和 `triggered_by`
包含所有引用资源并去重。

上述 raw file 模式适合结构化 DSL 尚未覆盖的 `[Route]` / `[RoutingPolicyRule]` 等内容；
已有的 `systemd.networkd` 结构化资源仍使用 provider 自身的 host 级 reload 聚合，不需要此 hook。

component-local script 的既有语义保持不变：

component-local 的 `script "<name>"` 与 `files`、`services` 等 block 同级。
`files.file.on_change` 可引用本地 script：

<!-- dbf-test:name=component-script-on-change-syntax;commands=validate,plan-offline -->

```hcl
component "managed_app" {
  input "service_name" {
    type = string
  }

  script "reload" {
    mode        = "once"
    interpreter = ["/bin/sh", "-eu"]
    outputs     = ["/etc/managed-app/rendered.env"]
    run         = "cp /etc/managed-app/config.env /etc/managed-app/rendered.env && systemctl reload ${input.service_name}.service"
  }

  files {
    file "/etc/managed-app/config.env" {
      content   = "LISTEN_ADDR=127.0.0.1:8080\n"
      on_change = script.reload
    }
  }
}

host "app1" {
  component "app" {
    source = component.managed_app

    inputs = {
      service_name = "managed-app"
    }
  }
}
```

`script` 字段：

| 字段 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `mode` | 否 | `"once"` | `"once"` 或 `"each"`。 |
| `interpreter` | 否 | `["/bin/sh", "-eu"]` | 非空 string list。 |
| `outputs` | 否 | `[]` | 脚本生成的普通文件路径列表；必须是绝对路径。 |
| `run` | 三选一 | 无 | 单条脚本命令字符串。 |
| `content` | 三选一 | 无 | 多行脚本文本。 |
| `commands` | 三选一 | 无 | 命令矩阵，例如 `[["systemctl", "reload", "app.service"]]`。 |

`run`、`content`、`commands` 必须且只能声明一个。component script 字段可读取 component
求值上下文里的 `input.<name>`、`var.<name>` 和 `target`。`host` / `profile` 不能声明
`script`，`files.file.on_change` 仍只能出现在 component 内。

当前实现范围是 DSL 解析、validate、HostSpec 编译、ResourceGraph/plan operation 展示、
apply 脚本执行、`once` / `each` 触发语义、运行时触发上下文，以及 `outputs` 文件漂移检测。
完整脚本内容作为内部执行载荷传给 provider，不会出现在 plan text/json/html 中。

声明 `outputs` 后，apply 会在脚本执行完成后记录输出文件 hash；后续 check/plan 会在输出缺失、
变成目录、hash 漂移或脚本声明变化时重新触发该 script。`outputs` 只声明脚本副作用的检查边界，
不会让 DebianForm 直接写入或删除这些输出文件。

`mode = "once"` 时，同一轮 apply 中同一个 script 被多个实际变更文件触发也只运行一次。
`mode = "each"` 时，每个实际变更文件各运行一次；online plan 会为每个触发源显示唯一的
operation 地址，形如：

```text
host.app1.components.app.script["reload"].trigger["host.app1.components.app.files.file[\"/etc/app.conf\"]"]
```

脚本执行时会注入这些环境变量：

| 环境变量 | 说明 |
| --- | --- |
| `DBF_SCRIPT_NAME` | script 名称。 |
| `DBF_COMPONENT_NAME` | component instance 名称；host-scoped 根 script 中为空字符串。 |
| `DBF_TRIGGER_ADDRESS` | 当前触发资源地址；`each` 模式下总是单个地址。 |
| `DBF_TRIGGER_PATH` | 当前触发文件路径；`each` 模式下总是单个路径。 |
| `DBF_TRIGGER_ADDRESSES` | 当前 script 本轮触发地址列表，换行分隔。 |
| `DBF_TRIGGER_PATHS` | 当前 script 本轮触发文件路径列表，换行分隔。 |

如果脚本需要对每个文件单独处理，使用 `mode = "each"` 并读取 `DBF_TRIGGER_PATH`；如果只是
reload/restart 服务，通常使用默认的 `mode = "once"`。

## 综合示例

下面的示例覆盖当前主要 DSL 指令。它只用于本地 `validate` 和 `plan --offline`，
不会连接远端。

<!-- dbf-test:name=dsl-reference;commands=validate,plan-offline;files=token.txt,local-source.txt -->

```hcl
locals {
  app_dir = "/opt/reference-app"
}

variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "dev"
  nullable    = false

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "runtime_token" {
  type        = string
  sensitive   = true
  ephemeral   = true
  default     = "not-a-real-secret-token"
  description = "Write-only runtime token."
}

profile "base" {
  system {
    timezone = "UTC"
    locale   = "C.UTF-8"
  }

  directories {
    directory "/opt/reference-app" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/reference-app/config.json" {
      content = jsonencode({
        environment = var.environment
        app_dir     = local.app_dir
      })
      mode = "0644"
    }
  }

  assert {
    condition = self.system.timezone == "UTC"
    message   = "base directory must be declared"
  }
}

component "app_sidecar" {
  input "port" {
    type        = number
    default     = 8080
    nullable    = false
    description = "Sidecar listen port."

    validation {
      condition     = input.port >= 1 && input.port <= 65535
      error_message = "port must be between 1 and 65535."
    }
  }

  files {
    file "/etc/reference-app/sidecar.env" {
      content = "PORT=${input.port}\n"
      mode    = "0644"
    }
  }

  services {
    service "reference-sidecar" {
      enabled = false
      state   = "stopped"
    }
  }
}

component "local_tool" {
  type    = "file"
  version = "1.0.0"

  source {
    url    = "file://local-source.txt"
    sha256 = "dbecfcfc1c83e7491897111315e80f6b9fabab3d144695cecb21bae7aeda8ba4"
  }

  install {
    path = "/usr/local/share/reference-tool.txt"
    mode = "0644"
  }
}

host "reference1" {
  imports = [profile.base]

  ssh {
    host          = "reference1"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform/reference/state.json"
    lock_path = "/var/lock/debianform/reference/state.lock"
  }

  system {
    hostname = "reference1"
  }

  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  kernel {
    modules = ["tcp_bbr"]
    sysctl = {
      "net.core.default_qdisc" = "fq"
    }
  }

  apt {
    repository "reference" {
      uris       = ["https://repo.example.invalid/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        content = "not-real-key\n"
        sha256  = "655e13e5db5c1ade95bc939d3d55d5f6d5f8d48f49ad436d3cdef1b962df8075"
      }
    }

    source_file "local" {
      path    = "/etc/apt/sources.list.d/local.sources"
      content = "Types: deb\nURIs: https://deb.debian.org/debian\nSuites: trixie\nComponents: main\n"
    }
  }

  packages {
    install = ["curl"]

    package "reference-tool" {
      repositories = ["reference"]

      lifecycle {
        prevent_destroy = true
      }
    }
  }

  groups {
    group "reference" {
      system = true
    }
  }

  users {
    user "reference" {
      system = true
      group  = "reference"
      home   = "/var/lib/reference"
      shell  = "/usr/sbin/nologin"
    }
  }

  files {
    file "/etc/reference-app/runtime-token" {
      content         = var.runtime_token
      content_version = "v1"
      mode            = "0600"
      sensitive       = true
    }

    file "/etc/reference-app/source-copy.txt" {
      source = "local-source.txt"
      mode   = "0644"
    }
  }

  secrets {
    file "/etc/reference-app/legacy-token" {
      source = "token.txt"
    }
  }

  systemd {
    unit "reference-raw.service" {
      content = "[Unit]\nDescription=Reference Raw\n[Service]\nExecStart=/bin/true\n"
    }

    service_unit "reference-app" {
      description = "Reference App"
      run         = ["/usr/bin/sleep", "infinity"]
      user        = "reference"
      group       = "reference"
      working_dir = "/var/lib/reference"
      restart     = "always"
      environment = {
        ENVIRONMENT = var.environment
      }
    }

    networkd {
      enable = true

      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }
        wireguard = {
          PrivateKeyFile = "/etc/wireguard/wg0.key"
        }
        wireguard_peer "peer-a" {
          PublicKey  = "not-a-real-public-key"
          AllowedIPs = ["10.0.0.2/32"]
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }
        network = {
          Address = ["10.0.0.1/24"]
        }
      }
    }
  }

  services {
    service "reference-app" {
      enabled = true
      state   = "running"
    }
  }

  nftables {
    enable = true

    main {
      content = "flush ruleset\ninclude \"/etc/nftables.d/*.nft\"\n"
    }

    file "20-reference" {
      content = "table inet reference { chain input { type filter hook input priority 0; policy accept; } }\n"
    }
  }

  docker {
    enable = true

    package {
      remove_conflicts = false
    }

    service {
      enable = false
      state  = "stopped"
    }

    daemon {
      settings = {
        log-driver = "json-file"
        log-opts = {
          max-size = "10m"
        }
      }
    }

    users = ["reference"]

    compose "reference" {
      directory      = "/opt/reference-compose"
      project        = "reference"
      pull           = "never"
      recreate       = "never"
      remove_orphans = true

      file {
        path    = "/opt/reference-compose/compose.yaml"
        content = "services:\n  web:\n    image: debian:trixie-slim\n    command: sleep infinity\n"
      }

      env_file "app" {
        path    = "/opt/reference-compose/.env"
        content = "ENVIRONMENT=${var.environment}\n"
      }

      service {
        enable = false
      }
    }
  }

  component "sidecar" {
    source = component.app_sidecar
    inputs = {
      port = 9000
    }
  }

  components = [
    component.local_tool,
  ]

  assert {
    condition = self.platform.architecture == "amd64"
    message   = "reference example expects amd64"
  }
}
```
