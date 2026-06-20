# DebianForm v1 Archive

This is the archived v1 README. It is retained only as historical reference while v2 is redesigned.
Commands and links in this document assume the current directory is `legacy/v1/`.

The active v2 overview is in the repository root `README.md`.

DebianForm 是一个面向 Debian 主机的轻量级声明式配置管理工具，命令行为
`dbf`。它使用 HashiCorp HCL v2 描述目标状态，通过 SSH 以 `root` 用户连接远端
主机，并使用带 `flock` 锁的远端状态文件记录已管理资源。

DebianForm 的目标不是替代 NixOS、Terraform/OpenTofu 或 Ansible，而是为常见
Debian 运维任务提供一个依赖少、可预览、可重复执行的工具：

- 本地只需要 `dbf` 和 OpenSSH 客户端。
- 远端不安装常驻 agent。
- 配置描述“系统应该是什么状态”，而不是罗列 shell 执行步骤。
- `plan` 在修改前读取远端真实状态并展示差异。
- `apply` 在状态锁内重新计算计划，避免应用过期结果。
- `check` 可用于 CI 中检测配置漂移。

> 当前项目处于早期阶段，只支持本文明确列出的语法和资源。DebianForm 使用
> HCL，但它不是 Terraform/OpenTofu provider，配置文件不能直接交给
> `terraform` 或 `tofu` 执行。

## 目录

- [设计原则](#设计原则)
- [快速开始](#快速开始)
- [安装](#安装)
- [命令行](#命令行)
- [配置文件与 SSH](#配置文件与-ssh)
- [核心配置块](#核心配置块)
- [HCL 语言能力](#hcl-语言能力)
- [高阶模块](#高阶模块)
- [软件包与服务](#软件包与服务)
- [发布二进制与 systemd unit](#发布二进制与-systemd-unit)
- [账户与主机](#账户与主机)
- [文件与目录](#文件与目录)
- [Debian 原生配置](#debian-原生配置)
- [低阶 APT 模块](#低阶-apt-模块)
- [依赖与 Handler](#依赖与-handler)
- [资源删除](#资源删除)
- [状态、锁与并发](#状态锁与并发)
- [完整示例](#完整示例)
- [开发、测试与发布](#开发测试与发布)
- [当前边界](#当前边界)

## 设计原则

DebianForm 的推荐配置遵循以下顺序：

1. 优先使用能表达完整运维目标的高阶资源。
2. 使用资源字段表达语义依赖。
3. 使用 Debian 原生配置资源管理复杂或变化较快的格式。
4. 只有在引擎无法推断隐藏关系时才写 `depends_on`。
5. 不把任意 shell 命令伪装成长期资源。

例如，从第三方 APT 仓库安装 BIRD2 时，推荐声明仓库、包和服务：

```hcl
debian_apt_repository "cznic_bird2" {
  host       = "bird_host"
  uris       = "https://pkg.labs.nic.cz/bird2"
  suites     = "trixie"
  components = "main"

  key = {
    url  = "https://pkg.labs.nic.cz/gpg"
    path = "/etc/apt/keyrings/cznic.asc"
  }
}

debian_package "bird2" {
  host = "bird_host"
}

debian_service "bird" {
  host    = "bird_host"
  package = "bird2"
  enabled = true
  state   = "running"
}
```

引擎会自动处理以下关系：

- HTTPS signing key 需要 `ca-certificates`。
- keyring 目录和 deb822 `.sources` 文件需要创建。
- APT repository 变化后需要执行 `apt-get update`。
- 同一主机上的软件包应在 repository 更新之后安装。
- `bird` 服务应在 `bird2` 软件包之后处理。

因此，常规配置不需要手工拼接 key 文件、APT source、缓存刷新和服务启动顺序。

## 快速开始

### 1. 准备环境

本地要求：

- Go 1.26 或兼容版本，用于从源码构建。
- OpenSSH 客户端中的 `ssh`。

远端 Debian 主机要求：

- 可以通过 SSH 以 `root` 登录。
- `/bin/sh` 和资源所使用的 Debian 系统命令可用。
- 状态主机提供 `flock`、`base64`，并允许写入状态路径。

建议先通过 `~/.ssh/config` 验证连接：

```sshconfig
Host server1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
```

```bash
ssh server1 true
```

`dbf` 会强制使用 `root`，并启用 `BatchMode=yes`。SSH 连接不能依赖交互式密码
输入。

### 2. 安装 `dbf`

```bash
make build
sudo make install
dbf --version
```

### 3. 创建配置

在空目录中创建 `main.dbf.hcl`：

```hcl
state "ssh" {
  host      = "server1"
  path      = "/var/lib/debianform/state.json"
  lock_path = "/var/lock/debianform/state.lock"
}

debian_package "curl" {
  host = "server1"
}

debian_file "motd" {
  host    = "server1"
  path    = "/etc/motd"
  content = "Managed by DebianForm\n"
  mode    = "0644"
}
```

资源块的本地名称默认也是远端对象名，因此
`debian_package "curl"` 不必重复填写 `name = "curl"`。

### 4. 验证并应用

```bash
dbf validate
dbf plan
dbf apply
dbf check
```

`apply` 会先展示计划，并要求输入 `yes`。自动化环境可以使用：

```bash
dbf apply --auto-approve
```

## 安装

### 使用 Make

默认安装到 `/usr/local/bin/dbf`：

```bash
sudo make install
```

安装位置遵循标准 Make 变量：

```bash
sudo make install PREFIX=/usr
make install PREFIX="$HOME/.local"
make install DESTDIR=/tmp/debianform-package
```

- `PREFIX` 默认为 `/usr/local`。
- `BINDIR` 默认为 `$(PREFIX)/bin`。
- `DESTDIR` 适合制作系统软件包。

只构建当前目录下的二进制：

```bash
make build
./dbf version
```

### 使用 Go

安装仓库最新可用版本：

```bash
go install github.com/mofelee/debianform/cmd/dbf@latest
```

安装指定发布标签：

```bash
go install github.com/mofelee/debianform/cmd/dbf@vX.Y.Z
```

二进制会写入 `GOBIN`，或者默认的 `GOPATH/bin`。

## 命令行

### 命令总览

```text
dbf validate [-f file]
dbf plan     [-f file] [--host name]
dbf apply    [-f file] [--host name] [--auto-approve] [--lock-timeout 5m]
dbf check    [-f file] [--host name]
dbf fmt      [-f file]
dbf version
dbf --version
```

### `dbf validate`

解析并检查配置，不连接远端主机：

```bash
dbf validate
dbf validate -f examples/main.dbf.hcl
```

验证内容包括：

- HCL 语法。
- 必填配置块与字段。
- `for_each` 展开。
- `depends_on` 和 `notify` 引用格式。
- handler 是否存在。
- 已知枚举值和资源类型。

### `dbf plan`

读取远端状态文件和目标主机真实状态，打印待执行变更：

```bash
dbf plan
dbf plan -f examples/bird2.dbf.hcl
dbf plan --host server1
```

计划符号：

| 符号 | 含义 |
| --- | --- |
| `+` | 创建资源 |
| `~` | 更新资源 |
| `-` | 删除资源或销毁已从配置移除的资源 |
| `!` | 资源变化后将运行 handler |

### `dbf apply`

应用配置：

```bash
dbf apply
dbf apply --auto-approve
dbf apply --lock-timeout 10m
dbf apply --host server1
```

默认流程：

1. 在未加锁状态下生成预览计划。
2. 用户输入 `yes` 确认。
3. 获取远端状态锁。
4. 重新读取状态并重新计算计划。
5. 按依赖顺序执行普通资源。
6. 执行本次变更触发的 handler。
7. 写回状态并释放锁。

`--auto-approve` 只跳过确认，不跳过状态锁内的重新规划。

### `dbf check`

执行与 `plan` 相同的漂移检查，但发现任何变更时返回非零状态码：

```bash
dbf check
dbf check --host server1
```

适合用于 CI、定时巡检和外部监控。

### `dbf fmt`

当前 MVP 中 `fmt` 只解析和验证配置，不会改写文件：

```bash
dbf fmt
```

命令成功时会输出配置解析成功。不要把它当作 HCL 自动格式化器。

### 版本命令

```bash
dbf --version
dbf version
```

`--version` 输出简短版本；`version` 还会输出 commit、构建时间、Go 版本和目标平台。

### 通用参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-f path` | 空 | 只加载指定配置文件 |
| `--host name` | 空 | 只处理指定目标主机的资源 |
| `--auto-approve` | `false` | 跳过 `apply` 的交互确认 |
| `--lock-timeout duration` | `5m` | 等待远端状态锁的最长时间 |

Go `flag` 接受 `-host` 和 `--host` 两种写法。

## 配置文件与 SSH

### 文件加载规则

不传 `-f` 时，`dbf`：

1. 查找当前目录中的所有 `*.dbf.hcl` 文件。
2. 按文件名排序。
3. 合并为同一组根配置。
4. 不递归加载子目录。

常见目录结构：

```text
production/
├── 00-state.dbf.hcl
├── 10-repositories.dbf.hcl
├── 20-packages.dbf.hcl
├── 30-services.dbf.hcl
├── files/
│   └── nginx.conf
└── templates/
    └── app.conf.tftpl
```

文件名用于提高可读性和稳定展示顺序，不能代替资源依赖。需要顺序时应使用语义字段
或 `depends_on`。

传入 `-f` 时只加载一个文件：

```bash
dbf plan -f production/20-packages.dbf.hcl
```

该文件必须独立包含所需的 `state "ssh"` 块。

### SSH 主机解析

资源中的 `host` 和 state 中的 `host` 可以直接使用 SSH config alias：

```hcl
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}
```

也可以在 HCL 中声明主机：

```hcl
host "web01" {
  address       = "192.0.2.10"
  port          = "22"
  identity_file = "/home/operator/.ssh/id_ed25519"
}

state "ssh" {
  host = "web01"
  path = "/var/lib/debianform/state.json"
}
```

或者把内部名称映射到已有 SSH config alias：

```hcl
host "production_web" {
  ssh_config_host = "web01-via-bastion"
}
```

`host` 块字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `address` | 否 | IP 地址或 DNS 名称 |
| `ssh_config_host` | 否 | 已有 SSH config 的 `Host` alias，优先于 `address` |
| `port` | 否 | SSH 端口，字符串或数字均可 |
| `identity_file` | 否 | 私钥路径，建议使用绝对路径 |

DebianForm 最终执行的连接固定包含：

```text
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -l root ...
```

复杂的跳板机、代理和连接复用建议继续放在 `~/.ssh/config` 中。

## 核心配置块

### `state "ssh"`

每组配置必须提供 SSH 状态后端：

```hcl
state "ssh" {
  host      = "server1"
  path      = "/var/lib/debianform/state.json"
  lock_path = "/var/lock/debianform/state.lock"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 保存状态和锁的 SSH 主机 |
| `path` | 是 | 无 | JSON 状态文件路径 |
| `lock_path` | 否 | `${path}.lock` | `flock` 锁文件路径 |

状态主机可以与被管理主机相同，也可以是单独的管理节点。

### `locals`

`locals` 用于复用静态值：

```hcl
locals {
  host  = "server1"
  suite = "trixie"
  packages = toset([
    "curl",
    "jq",
    "vim",
  ])
}
```

引用方式：

```hcl
debian_package "base" {
  for_each = local.packages

  host = local.host
  name = each.key
}
```

### `host`

`host "name"` 为逻辑主机名定义 SSH 连接参数。未声明的名称会直接作为 SSH
目标使用，因此已有 SSH config 时通常不需要 `host` 块。

### `handler`

handler 是资源变更后延迟执行的命令：

```hcl
handler "reload_nginx" {
  host    = "server1"
  command = "systemctl reload nginx"
}
```

handler 本身不参与常规漂移比较，必须由资源的 `notify` 触发。

## HCL 语言能力

### 支持的数据与表达式

当前支持：

- 字符串、布尔值、数字。
- list、map/object 和 heredoc。
- HCL 模板插值。
- `locals` 与 `local.name`。
- `path.module`。
- `for_each`、`each.key`、`each.value`。
- 条件表达式 `condition ? true_value : false_value`。
- `==` 和 `!=`。
- `file()`、`templatefile()`、`toset()`。
- `depends_on` 和 `notify` 中的静态资源地址。

普通字符串必须加引号：

```hcl
name = "nginx"
```

裸地址只在 `depends_on` 和 `notify` 中有效：

```hcl
depends_on = [
  debian_package.nginx,
]
```

### `path.module`

`path.module` 是当前配置文件所在目录：

```hcl
source = "${path.module}/files/nginx.conf"
```

当多个 `.dbf.hcl` 文件来自不同目录时，每个块使用其所属文件的目录。

### `file()`

读取本地文件内容。相对路径以当前配置文件目录为基准：

```hcl
content = file("files/sshd_config")
```

也可以用于高阶资源中的嵌套字段：

```hcl
key = {
  content = file("keys/vendor.asc")
}
```

### `templatefile()`

使用变量渲染 HCL 模板：

```hcl
content = templatefile("templates/app.conf.tftpl", {
  listen_address = "127.0.0.1"
  listen_port    = 8080
})
```

模板文件支持：

- `${name}` 插值。
- `%{ if ... }` 条件。
- `%{ for ... }` 循环。

传入的第二个参数必须是 object 或 map。模板内部只能访问显式传入的变量。

### `toset()`

把字符串列表转换为稳定字符串集合：

```hcl
locals {
  packages = toset(["curl", "jq", "vim"])
}
```

集合项必须是非空字符串，并且不能重复。

### `for_each`

资源可以从 map 或字符串集合批量展开。

字符串集合：

```hcl
debian_package "base" {
  for_each = toset([
    "curl",
    "jq",
  ])

  host = "server1"
  name = each.key
}
```

展开后的地址：

```text
debian_package.base["curl"]
debian_package.base["jq"]
```

对象 map：

```hcl
debian_user "operator" {
  for_each = {
    alice = {
      uid    = 2001
      groups = ["sudo"]
    }
    bob = {
      uid    = 2002
      groups = ["developers"]
    }
  }

  host   = "server1"
  name   = each.key
  uid    = each.value.uid
  groups = each.value.groups
}
```

`for_each` key 会进入状态地址。修改 key 等同于删除旧资源并创建新资源，不会自动迁移
state。

### 条件表达式

```hcl
debian_service "worker" {
  host    = "server1"
  name    = local.environment == "ci" ? "ci-worker" : "worker"
  enabled = local.environment != "disabled"
}
```

条件必须产生布尔值，只会求值选中的分支。

### 资源名称与地址

资源块格式：

```hcl
debian_package "web_server" {
  host = "server1"
  name = "nginx"
}
```

- `debian_package` 是资源类型。
- `web_server` 是本地名称。
- `nginx` 是远端真实对象名。
- 状态地址是 `debian_package.web_server`。

本地名称只能包含字母、数字和下划线，并且不能以数字开头。远端名称可以通过
`name`、`path`、`key` 等字段表达。

除少数没有 `name` 概念的资源外，未设置 `name` 时按以下顺序取默认值：

1. `for_each` 的 `each.key`。
2. 资源块的本地名称。

## 高阶模块

高阶模块放在本手册前部，因为它们更接近用户真正想管理的系统事实，并能减少手写
执行顺序。

### `debian_apt_repository`

推荐使用该资源管理完整的第三方 APT repository。它可以同时处理：

- deb822 `.sources` 文件。
- signing key 内容或远端下载。
- `/etc/apt/keyrings` 目录。
- HTTPS 下载所需的 `ca-certificates`。
- repository 变化后的 `apt-get update`。
- 同主机软件包资源的自动依赖。

```hcl
debian_apt_repository "vendor" {
  host          = "server1"
  types         = "deb"
  uris          = "https://packages.example.com/debian"
  suites        = "trixie"
  components    = "main"
  architectures = "amd64"

  key = {
    url  = "https://packages.example.com/signing.asc"
    path = "/etc/apt/keyrings/vendor.asc"
  }
}
```

字段：

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `uris` | 是 | 无 | deb822 `URIs` |
| `suites` | 是 | 无 | deb822 `Suites` |
| `components` | 是 | 无 | deb822 `Components` |
| `types` | 否 | `deb` | deb822 `Types` |
| `architectures` | 否 | 空 | deb822 `Architectures` |
| `signed_by` | 否 | 空 | 不使用 `key` 时指定现有 key 路径 |
| `key` | 否 | 空 | signing key 对象 |
| `path` | 否 | `/etc/apt/sources.list.d/<name>.sources` | source 文件路径 |
| `mode` | 否 | `0644` | source 文件权限 |
| `ensure` | 否 | `present` | `present` 或 `absent` |

`key` 对象字段：

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `url` | 二选一 | 无 | 在远端下载 key |
| `content` | 二选一 | 无 | 直接提供 key 内容，常与 `file()` 配合 |
| `path` | 否 | `/etc/apt/keyrings/<name>.asc` | 远端 key 文件路径 |

`url` 和 `content` 不能同时设置。

使用本地 key：

```hcl
debian_apt_repository "vendor" {
  host       = "server1"
  uris       = "https://packages.example.com/debian"
  suites     = "trixie"
  components = "main"

  key = {
    content = file("keys/vendor.asc")
  }
}
```

通过 `key.url` 下载时，远端优先使用 `curl`，其次使用 `wget`，再回退到
`/usr/lib/apt/apt-helper`。如果三者都不可用，会安装 `curl`。

当前对 `key.url` 只检查远端 key 文件是否存在；需要检测 key 内容漂移时，应使用
`key.content = file(...)`，此模式会比较内容 hash。

### 第三方仓库、软件包和服务

完整 BIRD2 示例：

```hcl
locals {
  host  = "server1"
  suite = "trixie"
}

state "ssh" {
  host = local.host
  path = "/var/lib/debianform/bird2-state.json"
}

debian_apt_repository "cznic_bird2" {
  host       = local.host
  uris       = "https://pkg.labs.nic.cz/bird2"
  suites     = local.suite
  components = "main"

  key = {
    url  = "https://pkg.labs.nic.cz/gpg"
    path = "/etc/apt/keyrings/cznic.asc"
  }
}

debian_package "bird2" {
  host = local.host
}

debian_service "bird" {
  host    = local.host
  package = "bird2"
  enabled = true
  state   = "running"
}
```

使用：

```bash
dbf validate -f examples/bird2.dbf.hcl
dbf plan -f examples/bird2.dbf.hcl
dbf apply -f examples/bird2.dbf.hcl
dbf check -f examples/bird2.dbf.hcl
```

把 `trixie` 改成目标 Debian 系统对应的 codename。

### BBR 组合

内核模块和 sysctl 是较底层资源，但 DebianForm 对 BBR 提供了一个特定的语义依赖：
当配置 `net.ipv4.tcp_congestion_control = bbr` 且同一主机存在 `tcp_bbr` 模块资源
时，sysctl 会自动依赖该模块。

```hcl
debian_kernel_module "tcp_bbr" {
  host    = "server1"
  name    = "tcp_bbr"
  persist = true
}

debian_sysctl "bbr_qdisc" {
  host  = "server1"
  key   = "net.core.default_qdisc"
  value = "fq"
}

debian_sysctl "bbr_congestion_control" {
  host  = "server1"
  key   = "net.ipv4.tcp_congestion_control"
  value = "bbr"
}
```

完整配置见 [examples/bbr.dbf.hcl](examples/bbr.dbf.hcl)。

## 软件包与服务

### `debian_package`

管理 Debian 软件包：

```hcl
debian_package "nginx" {
  host   = "server1"
  name   = "nginx"
  ensure = "present"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `name` | 否 | 资源名或 `each.key` | 软件包名 |
| `ensure` | 否 | `present` | `present` 或 `absent` |
| `version` | 否 | 空 | 要求安装的精确 Debian 包版本 |
| `update_cache` | 否 | `false` | 安装前手工执行 `apt-get update` |

如果同一主机配置了 `debian_apt_repository`，所有 `ensure = "present"` 的软件包会
自动依赖这些 repository，通常不需要设置 `update_cache`。

批量安装：

```hcl
debian_package "base" {
  for_each = toset([
    "curl",
    "jq",
    "vim",
  ])

  host = "server1"
}
```

### `debian_service`

管理 systemd 服务：

```hcl
debian_service "nginx" {
  host    = "server1"
  name    = "nginx"
  package = "nginx"
  enabled = true
  state   = "running"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `name` | 否 | 资源名或 `each.key` | systemd unit 名称 |
| `package` | 否 | 空 | 提供该服务的软件包名，用于推导依赖 |
| `enabled` | 否 | 不管理 | `true` 启用，`false` 禁用 |
| `state` | 否 | 不管理 | `running`、`stopped`、`restarted`、`reloaded` |

`package = "nginx"` 会查找同一主机中对象名为 `nginx` 的
`debian_package` 并自动建立依赖。

`restarted` 和 `reloaded` 每次 `plan` 都会产生变更，适合明确要求执行动作的场景。
长期运行状态应优先使用 `running` 或 `stopped`。

## 发布二进制与 systemd unit

### `debian_release_binary`

从远端 `tar.xz` 发布包中安装单个二进制，并同时校验发布归档和最终二进制的
SHA-256：

```hcl
debian_release_binary "tool" {
  host   = "server1"
  path   = "/usr/local/bin/tool"
  member = "tool"

  sources = {
    amd64 = {
      url            = "https://example.com/tool-amd64.tar.xz"
      archive_sha256 = "<64 hex characters>"
      binary_sha256  = "<64 hex characters>"
    }
    arm64 = {
      url            = "https://example.com/tool-arm64.tar.xz"
      archive_sha256 = "<64 hex characters>"
      binary_sha256  = "<64 hex characters>"
    }
  }
}
```

字段：

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `path` | 是 | 无 | 最终二进制路径 |
| `member` | 是 | 无 | 归档中的成员名称 |
| `source` | 二选一 | 无 | 单一架构 source 对象 |
| `sources` | 二选一 | 无 | 按 `dpkg --print-architecture` 选择的 source map |
| `archive_format` | 否 | `tar.xz` | 当前仅支持 `tar.xz` |
| `owner` | 否 | `root` | 最终文件 owner |
| `group` | 否 | `root` | 最终文件 group |
| `mode` | 否 | `0755` | 最终文件权限 |

每个 source 必须包含 `url`、`archive_sha256` 和 `binary_sha256`。应用时仅在目标主机
缺少相关工具时安装 `ca-certificates`、`curl`、`tar` 和 `xz-utils`。从配置移除资源
会删除最终二进制，不会自动卸载这些辅助软件包。

### `debian_systemd_unit`

管理 `/etc/systemd/system` 下的 unit，并在内容变化或删除后执行
`systemctl daemon-reload`：

```hcl
debian_systemd_unit "tool" {
  host = "server1"
  name = "tool.service"

  content = <<-EOF
    [Service]
    ExecStart=/usr/local/bin/tool
  EOF
}
```

字段与 `debian_file` 的 `content`、`source`、`owner`、`group`、`mode` 相同。
`path` 默认为 `/etc/systemd/system/<name>`。同一主机上对象名相同的
`debian_service` 会自动依赖该 unit。

## 账户与主机

### `debian_group`

管理 Unix 组：

```hcl
debian_group "deploy" {
  host   = "server1"
  gid    = 1500
  system = false
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `name` | 否 | 资源名或 `each.key` | 组名 |
| `gid` | 否 | 不管理 | GID |
| `system` | 否 | `false` | 创建时使用系统组 |
| `ensure` | 否 | `present` | `present` 或 `absent` |

`system` 只影响新建组，不会把现有普通组转换为系统组。

### `debian_user`

管理 Unix 用户：

```hcl
debian_user "deployer" {
  host   = "server1"
  uid    = 2001
  gid    = "deploy"
  groups = ["sudo"]
  home   = "/home/deployer"
  shell  = "/bin/bash"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `name` | 否 | 资源名或 `each.key` | 用户名 |
| `uid` | 否 | 不管理 | UID |
| `gid` | 否 | 不管理 | 主组名称或 GID |
| `groups` | 否 | 不管理 | 补充组列表 |
| `home` | 否 | 不管理 | home 路径 |
| `shell` | 否 | 不管理 | 登录 shell |
| `system` | 否 | `false` | 新建时创建系统用户 |
| `ensure` | 否 | `present` | `present` 或 `absent` |

普通用户创建时使用 `useradd -m`；系统用户创建时使用 `useradd -r`。
删除用户使用 `userdel`，不会自动删除 home。

当前实现只有在 `groups` 为非空列表时才管理补充组；空列表与省略字段都会保留现状。

### `debian_authorized_key`

管理用户 `authorized_keys` 中的一把公钥：

```hcl
debian_authorized_key "deployer_ci" {
  host = "server1"
  user = "deployer"
  key  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... ci@example"
}
```

从本地文件读取：

```hcl
debian_authorized_key "deployer_ci" {
  host   = "server1"
  user   = "deployer"
  source = "${path.module}/keys/deployer.pub"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `user` | 是 | 无 | 远端用户名 |
| `key` | 二选一 | 无 | 公钥内容 |
| `source` | 二选一 | 无 | 本地公钥文件路径 |
| `path` | 否 | `<home>/.ssh/authorized_keys` | 远端目标文件 |
| `ensure` | 否 | `present` | `present` 或 `absent` |

公钥身份按“类型 + base64 主体”判断，末尾注释变化不视为漂移。添加 key 时会把
`.ssh` 目录设为 `0700`，把 `authorized_keys` 设为 `0600`。

### `debian_hostname`

管理静态主机名：

```hcl
debian_hostname "main" {
  host     = "server1"
  hostname = "web01"
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `host` | 是 | 目标主机 |
| `hostname` | 是 | 目标静态主机名 |

应用时调用 `hostnamectl set-hostname`。从配置移除该资源时不会猜测旧主机名，因此
销毁操作为空。

## 文件与目录

### `debian_file`

管理普通文件内容与元数据：

```hcl
debian_file "nginx_conf" {
  host   = "server1"
  path   = "/etc/nginx/nginx.conf"
  source = "${path.module}/files/nginx.conf"
  owner  = "root"
  group  = "root"
  mode   = "0644"
  backup = true
}
```

也可以直接提供内容：

```hcl
debian_file "motd" {
  host    = "server1"
  path    = "/etc/motd"
  content = <<-EOF
    Managed by DebianForm
  EOF
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `path` | 是 | 无 | 远端文件路径 |
| `content` | 二选一 | 无 | 文件内容 |
| `source` | 二选一 | 无 | 本地源文件路径 |
| `owner` | 否 | `root` | owner |
| `group` | 否 | `root` | group |
| `mode` | 否 | `0644` | 权限 |
| `backup` | 否 | `false` | 覆盖前创建带时间戳的 `.bak` 备份 |

`content` 和 `source` 必须且只能设置一个。写入使用临时文件和 `install`，只有内容
hash 或元数据不一致时才更新。

### `debian_directory`

管理目录：

```hcl
debian_directory "app_data" {
  host   = "server1"
  path   = "/var/lib/example"
  owner  = "example"
  group  = "example"
  mode   = "0750"
  ensure = "present"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `path` | 是 | 无 | 目录路径 |
| `owner` | 否 | 不管理 | owner |
| `group` | 否 | 不管理 | group |
| `mode` | 否 | 不管理 | 权限 |
| `ensure` | 否 | `present` | `present` 或 `absent` |

`ensure = "absent"` 和从配置中移除资源都会执行递归删除。引擎拒绝管理空路径和根
目录 `/`，但其他目录删除仍需谨慎。

`owner` 和 `group` 都省略时不会修改所有权；只设置其中一个时，另一个会在实际
`chown` 命令中使用 `root`。需要保留非 root 的另一侧时，请同时显式填写。

## Debian 原生配置

对于 systemd-networkd、nftables、sysctl 和 modules-load 等已有稳定原生格式的
系统组件，DebianForm 采用薄封装：管理文件、检查真实状态，并按资源字段选择是否
校验或激活。

### `debian_networkd_file`

管理 systemd-networkd 原生 `.network`、`.netdev` 或 `.link` 文件：

```hcl
debian_networkd_file "eth0" {
  host = "server1"
  name = "10-eth0.network"

  content = <<-EOF
    [Match]
    Name=eth0

    [Network]
    DHCP=yes
  EOF

  activate = true
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `name` | 条件必填 | 资源名或 `each.key` | `/etc/systemd/network` 下的文件名 |
| `path` | 条件必填 | `/etc/systemd/network/<name>` | 自定义完整路径 |
| `content` | 二选一 | 无 | 文件内容 |
| `source` | 二选一 | 无 | 本地源文件路径 |
| `owner` | 否 | `root` | owner |
| `group` | 否 | `root` | group |
| `mode` | 否 | `0644` | 权限 |
| `backup` | 否 | `false` | 覆盖前备份 |
| `activate` | 否 | `false` | 执行 `systemctl reload-or-restart systemd-networkd` |

必须能通过 `name` 或 `path` 确定目标路径。

`activate = true` 会让每次 `plan` 都安排一次更新，以便每次 `apply` 都重新写入文件
并 reload 或 restart systemd-networkd。

批量管理：

```hcl
debian_networkd_file "native" {
  for_each = {
    "10-eth0.network" = file("network/10-eth0.network")
    "20-wg0.netdev"   = file("network/20-wg0.netdev")
  }

  host    = "server1"
  name    = each.key
  content = each.value
}
```

### `debian_nftables_file`

管理 nftables 原生配置：

```hcl
debian_nftables_file "main" {
  host     = "server1"
  path     = "/etc/nftables.conf"
  validate = true
  activate = false

  content = <<-EOF
    flush ruleset

    table inet filter {
      chain input {
        type filter hook input priority 0; policy accept;
      }
    }
  EOF
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `name` | 否 | 资源名或 `each.key` | 生成默认路径时使用 |
| `path` | 否 | 见下文 | 远端配置路径 |
| `content` | 二选一 | 无 | nft 配置内容 |
| `source` | 二选一 | 无 | 本地源文件路径 |
| `owner` | 否 | `root` | owner |
| `group` | 否 | `root` | group |
| `mode` | 否 | `0644` | 权限 |
| `validate` | 否 | `true` | 安装前执行 `nft -c -f` |
| `activate` | 否 | `false` | 安装后执行 `nft -f` |

默认路径：

- 对象名为 `main`：`/etc/nftables.conf`。
- 其他对象名：`/etc/nftables.d/<name>.nft`。

校验针对临时文件执行，校验失败时不会安装新配置。

### `debian_kernel_module`

管理当前内核模块及可选持久化配置：

```hcl
debian_kernel_module "br_netfilter" {
  host    = "server1"
  name    = "br_netfilter"
  persist = true
  path    = "/etc/modules-load.d/kubernetes.conf"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `name` | 否 | 资源名或 `each.key` | 内核模块名 |
| `ensure` | 否 | `present` | `present` 或 `absent` |
| `persist` | 否 | `true` | 管理 modules-load 文件 |
| `path` | 否 | `/etc/modules-load.d/dbf-<资源名>.conf` | 持久化文件路径 |

`present` 调用 `modprobe`；`absent` 删除持久化文件并尝试 `modprobe -r`。

默认 `path` 使用资源块本地名称，不使用 `for_each` key。一个
`debian_kernel_module` 块通过 `for_each` 管理多个模块时，应为各实例显式生成不同
的 `path`，避免它们写入同一个文件。

### `debian_sysctl`

管理运行时 sysctl 和持久化文件：

```hcl
debian_sysctl "ip_forward" {
  host    = "server1"
  key     = "net.ipv4.ip_forward"
  value   = "1"
  apply   = true
  persist = true
  path    = "/etc/sysctl.d/99-kubernetes.conf"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `key` | 是 | 无 | sysctl key |
| `value` | 是 | 无 | 目标值 |
| `apply` | 否 | `true` | 管理运行时值 |
| `persist` | 否 | `true` | 管理 sysctl.d 文件 |
| `path` | 否 | `/etc/sysctl.d/99-dbf-<资源名>.conf` | 持久化文件路径 |

运行时应用使用 `sysctl -w key=value`。从配置移除资源时只删除持久化文件，不会猜测
并恢复旧的运行时值。

与内核模块相同，默认 `path` 使用资源块本地名称。通过 `for_each` 创建多个 sysctl
实例时，应显式为每个实例设置唯一 `path`。

完整原生系统配置见
[examples/system-native.dbf.hcl](examples/system-native.dbf.hcl)。

## 低阶 APT 模块

### `debian_apt_source`

`debian_apt_source` 只管理一个 deb822 `.sources` 文件。它不管理 signing key，也
不会为其他软件包自动表达完整的 repository 生命周期。新配置应优先使用
`debian_apt_repository`。

```hcl
debian_apt_source "vendor" {
  host          = "server1"
  types         = "deb"
  uris          = "https://packages.example.com/debian"
  suites        = "trixie"
  components    = "main"
  architectures = "amd64"
  signed_by     = "/etc/apt/keyrings/vendor.asc"
}
```

| 字段 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `host` | 是 | 无 | 目标主机 |
| `uris` | 是 | 无 | deb822 `URIs` |
| `suites` | 是 | 无 | deb822 `Suites` |
| `components` | 是 | 无 | deb822 `Components` |
| `types` | 否 | `deb` | deb822 `Types` |
| `architectures` | 否 | 空 | deb822 `Architectures` |
| `signed_by` | 否 | 空 | deb822 `Signed-By` |
| `path` | 否 | `/etc/apt/sources.list.d/<name>.sources` | 远端路径 |
| `mode` | 否 | `0644` | 文件权限 |
| `ensure` | 否 | `present` | `present` 或 `absent` |

该资源不会自动执行 `apt-get update`。如果确实需要低阶组合，应显式处理 key、缓存
刷新和依赖关系。

## 依赖与 Handler

### 自动语义依赖

当前引擎会自动推导四类关系：

1. 同一主机中，所有要安装的软件包依赖所有存在的
   `debian_apt_repository`。
2. `debian_service.package` 匹配同主机软件包对象名时，服务依赖软件包。
3. BBR congestion control sysctl 依赖同主机的 `tcp_bbr` 内核模块。
4. `debian_service` 与 `debian_systemd_unit` 的对象名匹配时，服务依赖 unit。

自动依赖只在同一主机中建立。

### `depends_on`

无法由字段表达的隐藏依赖可以显式声明：

```hcl
debian_service "nginx" {
  host    = "server1"
  enabled = true
  state   = "running"

  depends_on = [
    debian_package.nginx,
    debian_file.nginx_conf,
  ]
}
```

引用单个 `for_each` 实例：

```hcl
depends_on = [
  debian_file.site["primary"],
]
```

引用不带实例 key 的整个 `for_each` 资源时，会匹配全部实例：

```hcl
depends_on = [
  debian_file.site,
]
```

依赖图必须无环。发现环时 `plan` 和 `apply` 会报错。

### `notify` 与 handler

```hcl
handler "reload_nginx" {
  host    = "server1"
  command = "systemctl reload nginx"
}

debian_file "nginx_conf" {
  host   = "server1"
  path   = "/etc/nginx/nginx.conf"
  source = "${path.module}/files/nginx.conf"

  notify = [
    handler.reload_nginx,
  ]
}
```

执行规则：

- 只有通知资源本次实际发生变化时才触发。
- 普通资源全部成功后再执行 handler。
- 同一 handler 被多个资源通知时，一次 `apply` 只运行一次。
- 多个 handler 按声明顺序运行。
- 普通资源中途失败时，不运行尚未开始的 handler。
- handler 命令通过远端 `/bin/sh` 执行。

handler 是有意开放的命令入口，应保持简短并尽量使用幂等的 reload 操作。

## 资源删除

### 两种删除方式

部分资源支持显式 `ensure = "absent"`：

- `debian_apt_repository`
- `debian_apt_source`
- `debian_package`
- `debian_directory`
- `debian_kernel_module`
- `debian_group`
- `debian_user`
- `debian_authorized_key`

所有受 state 跟踪的资源都支持“从配置移除资源块后销毁”：

先从 `.dbf.hcl` 中删除资源块，然后执行：

```bash
dbf plan
dbf apply
```

销毁按上次记录的应用顺序反向执行，使依赖方尽量先于被依赖方删除。

### 各资源的销毁行为

| 资源 | 从配置移除后的行为 |
| --- | --- |
| `debian_apt_repository` | 删除 source 和所管理的 key，然后执行 `apt-get update` |
| `debian_apt_source` | 删除 `.sources` 文件 |
| `debian_package` | 执行 `apt-get remove -y` |
| `debian_service` | 执行 `systemctl disable --now` |
| `debian_release_binary` | 删除最终二进制 |
| `debian_systemd_unit` | 删除 unit 并执行 `systemctl daemon-reload` |
| `debian_file` | 删除文件 |
| `debian_directory` | 递归删除目录 |
| `debian_networkd_file` | 删除文件，不自动 reload networkd |
| `debian_nftables_file` | 删除文件，不自动重新加载规则 |
| `debian_kernel_module` | 删除持久化文件并尝试卸载模块 |
| `debian_sysctl` | 删除持久化文件，不恢复运行时值 |
| `debian_group` | 执行 `groupdel` |
| `debian_user` | 执行 `userdel`，保留 home |
| `debian_authorized_key` | 删除匹配“类型 + key 主体”的行 |
| `debian_hostname` | 不执行反向操作 |

删除是实际系统操作，不只是从 state 中移除记录。尤其需要注意目录、软件包、用户、
网络和防火墙资源。

### `--host` 与删除

使用 `--host server1` 时，只规划和应用该主机的资源，也只销毁 state 中属于该主机
且已从配置移除的资源。其他主机的 state 记录会保留。

## 状态、锁与并发

### 状态内容

远端 JSON state 记录：

- 资源地址与类型。
- 目标主机。
- 对象名或路径。
- 内容 hash、key 信息等销毁所需字段。
- 上次更新时间。
- 上次应用顺序。
- handler 最近一次触发原因和命令 hash。

state 用于确定资源所有权和处理“配置中已删除的资源”，但 `plan` 仍会读取远端真实
状态来检测漂移。

### 状态锁

`apply` 通过 SSH 启动持有 `flock` 的远端进程：

1. 创建状态目录和 lock 文件。
2. 对 lock 文件获取独占锁。
3. 保持 SSH 会话直到 apply 完成。
4. 写回 state 后释放锁。

默认等待 5 分钟：

```bash
dbf apply --lock-timeout 30s
dbf apply --lock-timeout 15m
```

`plan` 和 `check` 不获取写锁，因此只能视为某一时刻的观察结果。真正执行时
`apply` 会在锁内重新规划。

### 一个配置使用一个 state

同一组配置文件应共享一个 state。不要让两个互不知情的 state 管理同一个远端对象，
否则双方都可能把对方的变更视为漂移。

## 完整示例

仓库包含以下可运行示例：

| 文件 | 内容 |
| --- | --- |
| [examples/bird2.dbf.hcl](examples/bird2.dbf.hcl) | 高阶 APT repository、BIRD2 软件包和服务 |
| [examples/bbr.dbf.hcl](examples/bbr.dbf.hcl) | TCP BBR 内核模块与 sysctl |
| [examples/shadowsocks-rust.dbf.hcl](examples/shadowsocks-rust.dbf.hcl) | shadowsocks-rust v1.24.0、发布二进制校验和 systemd 服务 |
| [examples/main.dbf.hcl](examples/main.dbf.hcl) | 软件包、文件和 networkd 基础示例 |
| [examples/system-native.dbf.hcl](examples/system-native.dbf.hcl) | 内核模块、sysctl 和 nftables |
| [examples/ksvm-smoke.dbf.hcl](examples/ksvm-smoke.dbf.hcl) | 单主机文件 smoke test |
| [examples/ksvm-fleet-smoke.dbf.hcl](examples/ksvm-fleet-smoke.dbf.hcl) | `for_each` 多主机 smoke test |
| [examples/ksvm-handler-smoke.dbf.hcl](examples/ksvm-handler-smoke.dbf.hcl) | handler 去重与变更触发 |

v2 仍处于设计阶段，以下文件用于冻结目标 DSL，当前执行器不能 apply：

| 文件 | 内容 |
| --- | --- |
| [examples/v2-bbr.dbf.hcl](examples/v2-bbr.dbf.hcl) | BBR kernel module、sysctl 和语义依赖 |
| [examples/v2-apt-repository.dbf.hcl](examples/v2-apt-repository.dbf.hcl) | host 直配 APT repository、显式 package 来源依赖和 cache refresh |
| [examples/v2-bird2.dbf.hcl](examples/v2-bird2.dbf.hcl) | component、APT repository、显式 package 来源依赖和服务 |
| [examples/v2-component-binary.dbf.hcl](examples/v2-component-binary.dbf.hcl) | binary component、按架构 source 选择、下载校验和原子安装 |
| [examples/v2-nftables.dbf.hcl](examples/v2-nftables.dbf.hcl) | 原生 nftables ruleset、snippet、校验、激活和 plan diff |
| [examples/v2-plan-preview.dbf.hcl](examples/v2-plan-preview.dbf.hcl) | 结构化 plan、文本 diff、sensitive 摘要和 operation 展示目标 |
| [examples/v2-systemd-networkd-wireguard.dbf.hcl](examples/v2-systemd-networkd-wireguard.dbf.hcl) | systemd-networkd、WireGuard、secret 文件和 RouteTable=off |
| [examples/v2-fleet.dbf.hcl](examples/v2-fleet.dbf.hcl) | profile、component、多主机、systemd 和 networkd 完整组合 |

运行示例前必须修改：

- SSH host alias。
- state 和 lock 路径。
- Debian suite。
- 网络、服务和软件包等与目标主机相关的值。

建议先执行：

```bash
dbf validate -f examples/main.dbf.hcl
dbf plan -f examples/main.dbf.hcl
```

## 开发、测试与发布

### 常用开发命令

```bash
make build
make test
make test-unit
make clean
```

| 命令 | 说明 |
| --- | --- |
| `make build` | 构建带版本元数据的 `dbf` |
| `make test` | 执行 `go test ./...` |
| `make test-unit` | 执行 race detector 和全部 Go 测试 |
| `make test-legacy-v1-integration` | 每个场景启动一台全新 Debian 13 VM |
| `make test-legacy-v1-integration-case CASE=files` | 只运行指定场景 |
| `make test-legacy-v1-integration-layout` | 不启动 VM，校验场景目录、HCL 和 shell 语法 |
| `make clean` | 清理构建产物 |

主 CI 已不再运行 v1 libvirt 集成测试。需要检查归档场景协议时，可以手动运行：

```bash
make test-legacy-v1-integration-layout
```

### Debian 13 libvirt 集成测试

集成测试使用官方 Debian 13 `trixie` genericcloud 镜像，校验 `SHA512SUMS`。每个
`test/integration/libvirt/cases/<name>/` 目录会创建独立 NAT 网络、cloud-init seed、
qcow2 overlay 和空白 VM，不复用其他场景修改过的系统盘。

场景使用连续编号的 `1.dbf.hcl`、`2.dbf.hcl` 等文件表达多步骤变更。每一步都有
对应的 `.plan` 精确列出全部计划地址，并用 `.check.sh` 将远端检查逐项命名。可选
`.drift.sh` 负责制造漂移，并要求 `dbf check` 在修复前失败。

最后一个配置必须包含零个资源，用于销毁该场景创建的全部资源。运行器会检查最终
销毁计划与 `.plan` 完全一致、应用后的 state 为空，并执行最终 `.check.sh` 检查
主机残留。详细协议见
[test/integration/libvirt/README.md](test/integration/libvirt/README.md)。

在 x86_64 Linux 上安装依赖：

```bash
sudo apt-get install \
  cloud-image-utils curl dnsmasq-base libvirt-clients \
  libvirt-daemon-system openssh-client ovmf \
  qemu-system-x86 qemu-utils

sudo systemctl start libvirtd
make test-legacy-v1-integration
```

只运行一个场景：

```bash
make test-legacy-v1-integration-case CASE=identity
```

`/dev/kvm` 可读写时使用 KVM，否则回退到 QEMU 软件模拟。

可用环境变量：

| 变量 | 说明 |
| --- | --- |
| `DBF_INTEGRATION_CASE` | 只运行指定场景目录 |
| `DBF_INTEGRATION_WORKDIR` | 指定测试工作目录 |
| `DBF_INTEGRATION_KEEP_WORKDIR=1` | 测试后保留工作目录 |
| `DBF_INTEGRATION_ARTIFACT_DIR` | 指定失败诊断输出目录 |
| `DBF_INTEGRATION_IMAGE_CACHE` | 指定 Debian 镜像缓存目录 |
| `DBF_INTEGRATION_DISABLE_KVM=1` | 强制使用 QEMU |

### 模块实现标准

新增资源应同时具备：

- 可读取的真实当前状态。
- 稳定的期望状态和资源身份。
- 清晰的 `plan`、`apply` 与 `destroy` 行为。
- 幂等执行。
- 对内容、owner、mode 或领域字段的漂移检测。
- 必要的语义依赖，而不是依赖文件顺序。
- 单元测试和至少一个代表性配置示例。

如果一个能力只是一次性动作，例如整机升级、立即重启、数据库迁移或任意 shell
脚本，不应伪装成声明式资源。

### 版本与发布

Git tag 是版本号的主要来源。发布标签使用语义化版本，例如 `v0.2.0`。

`make build` 注入：

- 精确 Git tag；当前 commit 没有精确 tag 时为 `dev`。
- Git commit。
- UTC 构建时间。

创建发布：

```bash
git tag -a vX.Y.Z -m "debianform vX.Y.Z"
git push origin vX.Y.Z
make build
./dbf version
```

CI 可以显式提供可复现元数据：

```bash
make build \
  VERSION=vX.Y.Z \
  COMMIT="$(git rev-parse --short=12 HEAD)" \
  BUILD_DATE="2026-06-18T00:00:00Z"
```

通过 `go install ...@vX.Y.Z` 安装时，`dbf version` 也会尝试读取 Go 内嵌的 VCS
元数据。

## 当前边界

当前明确不支持：

- 非 Debian 系统。
- 非 `root` SSH 执行和自动 `sudo`。
- 远端常驻 agent。
- Terraform/OpenTofu provider 协议。
- Terraform 的完整函数、变量、module 和 lifecycle 语义。
- secrets 管理与加密 state。
- 复杂集群编排、滚动发布和服务发现。
- 任意 shell 命令资源。
- 自动迁移 `for_each` key 或资源地址。
- 自动恢复无法推断的删除前状态，例如旧 hostname 或旧 sysctl 值。
- 对所有未知资源属性进行严格报错；请按本文字段表书写并用 `plan` 复核行为。

当前已实现资源，按推荐使用层次排序：

1. `debian_apt_repository`
2. `debian_release_binary`
3. `debian_systemd_unit`
4. `debian_package`
5. `debian_service`
6. `debian_group`
7. `debian_user`
8. `debian_authorized_key`
9. `debian_hostname`
10. `debian_file`
11. `debian_directory`
12. `debian_networkd_file`
13. `debian_nftables_file`
14. `debian_kernel_module`
15. `debian_sysctl`
16. `debian_apt_source`

超出本文列表的资源类型会在配置验证阶段被拒绝。
