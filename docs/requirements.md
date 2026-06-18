# debianform 需求文档

## 1. 项目目标

`debianform` 是一个面向 Debian 的简单声明式配置管理工具，命令行为 `dbf`。

它使用 HCL 作为配置语言，用 Go 实现执行引擎，通过 SSH 以 `root` 用户连接远程 Debian 主机，并把配置文件中声明的资源应用到目标系统。

项目希望提供类似 NixOS/Terraform 的声明式体验，但第一阶段保持实现简单，不依赖 Ansible，不追求完整替代 NixOS、Terraform 或通用配置管理系统。

## 2. 设计原则

- 简单优先：减少概念、减少运行时依赖、减少远程主机准备工作。
- Root-only：远程执行必须使用 `root` SSH，不支持普通用户加 `sudo` 的复杂路径。
- Agentless：远程主机不安装长期运行的 agent。
- 资源声明：用户以资源为单位描述目标状态，而不是描述整台系统镜像。
- 幂等执行：资源在修改前应读取远端当前状态，只在需要时变更。
- 可预览：支持在实际修改前展示计划。
- Debian-first：只关注当前支持的 Debian 稳定版本，不做跨发行版抽象。

## 3. 非目标

- 不做完整 NixOS 式系统重建。
- 不做通用 Linux 配置管理平台。
- 不兼容 Ansible playbook。
- 不直接运行 OpenTofu，也不实现 OpenTofu provider 协议。
- 不支持非 Debian 系统。
- 第一版不支持复杂集群编排、滚动发布、服务发现或密钥管理平台。
- 第一版不支持非 root 远程执行。

## 4. 配置语言

配置文件使用 HCL2 风格语法。工具借用 HCL 的表达能力和可读性，但不要求配置文件能被 OpenTofu 直接执行。

配置文件后缀：

- `.dbf.hcl`

示例：

```hcl
state "ssh" {
  host      = "server1"
  path      = "/var/lib/debianform/state.json"
  lock_path = "/var/lock/debianform/state.lock"
}

debian_package "nginx" {
  host   = "server1"
  ensure = "present"
}

debian_file "/etc/nginx/sites-enabled/default" {
  host    = "server1"
  content = file("${path.module}/files/nginx-default.conf")
  owner   = "root"
  group   = "root"
  mode    = "0644"
}

debian_service "nginx" {
  host    = "server1"
  enabled = true
  state   = "running"
}
```

## 5. CLI 需求

第一版 CLI 暂定为：

```bash
dbf fmt
dbf validate
dbf plan
dbf apply
dbf check
```

命令含义：

- `fmt`：格式化 HCL 配置。
- `validate`：解析配置并检查资源字段是否合法。
- `plan`：连接远程主机，读取当前状态，生成变更计划。
- `apply`：执行变更计划。
- `check`：只检查远端状态是否符合配置，不做修改。

可选参数：

```bash
dbf plan -f main.dbf.hcl
dbf apply -f main.dbf.hcl
dbf apply --host server1
```

## 6. 连接模型

远程连接只支持 SSH。

第一版要求：

- 必须以 `root` 用户连接。
- 支持 SSH key。
- 支持读取本地 `~/.ssh/config`。
- 支持直接使用 SSH config 中的 `Host` 名称作为 `host` 字段，例如 `host = "server1"`。
- 支持配置文件中指定 `address`、`port`、`identity_file`。
- 默认不在远程主机安装 agent。

最简示例：

```hcl
debian_package "curl" {
  host = "server1"
}
```

如果 `server1` 已经在 `~/.ssh/config` 中定义，`dbf` 可以直接使用该配置连接。

显式主机定义示例：

```hcl
host "web01" {
  address       = "192.0.2.10"
  port          = 22
  identity_file = "~/.ssh/id_ed25519"
}
```

SSH config alias 映射示例：

```hcl
host "web01" {
  ssh_config_host = "server1"
}
```

主机解析规则：

1. 如果 `host = "web01"` 匹配配置文件中的 `host "web01"` block，则使用该 block。
2. 否则将 `web01` 当作 SSH config 中的 `Host web01` alias。
3. 如果 SSH config 中设置了 `User`，必须是 `root`；如果未设置，`dbf` 默认使用 `root`。

## 7. 执行模型

每个资源至少包含三个阶段：

1. `Read`：读取远端当前状态。
2. `Plan`：比较配置状态和远端状态，生成差异。
3. `Apply`：执行必要修改。

执行要求：

- 默认按依赖顺序执行。
- 同一主机内资源可以先串行执行，降低复杂度。
- 多主机并发可以作为后续优化。
- 失败时应显示资源名、主机名、执行命令和错误输出。
- 修改文件前，对关键系统文件可选择自动备份。

## 8. State 模型

state 文件保存在 SSH 服务器上的指定路径中，而不是默认保存在本地工作目录。

这样多个协作者只要使用同一个 state 后端，就能读取最新状态，并通过远端锁避免同时修改同一个 state。

原则：

- 远端实际状态优先。
- state 用于记录资源 ID、上次 apply 时间、内容 hash、下载 hash 等辅助信息。
- 删除配置中的资源时，默认不自动删除远端资源，除非资源显式支持 destroy 语义。
- state 后端必须支持锁。
- `apply` 必须在持有远端独占锁期间完成读取 state、读取远端真实状态、生成计划、执行变更、写回 state。
- `plan` 可以读取 state 并展示计划，但 `apply` 不应直接执行之前生成的过期计划；`apply` 必须重新拿锁并重新计算计划。

state 配置示例：

```hcl
state "ssh" {
  host      = "server1"
  path      = "/var/lib/debianform/state.json"
  lock_path = "/var/lock/debianform/state.lock"
}
```

字段：

- `host`：必填，保存 state 的 SSH 主机。
- `path`：必填，远端 state 文件路径。
- `lock_path`：可选，远端锁文件路径，默认可以从 `path` 派生。

锁实现要求：

- 第一版使用远端 `flock` 实现独占锁。
- 锁必须绑定到当前 `dbf` 进程持有的 SSH session；本地进程退出或 SSH 断开时，远端锁应自动释放。
- 获取锁失败时应显示当前锁路径和等待状态。
- 默认可以等待锁，也应提供超时参数。
- 不允许绕过锁写入 state。

## 9. 第一版资源类型

所有资源中的 `host` 字段都使用字符串。该字符串可以是项目内 `host` block 的名字，也可以直接是 SSH config 中的 `Host` alias。

### 9.1 `host`

声明一台 Debian 主机。

`host` block 是可选的。若主机已经存在于 `~/.ssh/config` 中，可以直接在资源中使用 SSH config 的 `Host` 名称，不必额外声明。

字段：

- `address`：可选，IP 或域名。若没有填写，则使用资源中主机名对应的 SSH config alias。
- `ssh_config_host`：可选，映射到 `~/.ssh/config` 中的某个 `Host` 名称。
- `port`：可选，默认 `22`。
- `identity_file`：可选，SSH 私钥路径。

### 9.2 `debian_package`

通过 `apt` 管理软件包。

字段：

- `host`：必填。
- `ensure`：`present` 或 `absent`，默认 `present`。
- `version`：可选，指定版本。
- `update_cache`：可选，是否在安装前执行 `apt-get update`。

示例：

```hcl
debian_package "curl" {
  host   = "server1"
  ensure = "present"
}
```

### 9.3 `debian_file`

管理远程文件内容和权限。

字段：

- 资源 label：远程路径。
- `host`：必填。
- `content`：可选，文件内容。
- `source`：可选，本地文件路径。
- `owner`：可选，默认 `root`。
- `group`：可选，默认 `root`。
- `mode`：可选。
- `backup`：可选，修改前备份。

`content` 和 `source` 二选一。

### 9.4 `debian_directory`

管理远程目录。

字段：

- 资源 label：远程路径。
- `host`：必填。
- `owner`：可选。
- `group`：可选。
- `mode`：可选。
- `ensure`：`present` 或 `absent`，默认 `present`。

### 9.5 `debian_service`

管理 systemd 服务。

字段：

- 资源 label：服务名。
- `host`：必填。
- `enabled`：可选，是否开机启动。
- `state`：可选，`running`、`stopped`、`restarted`、`reloaded`。

### 9.6 `debian_download`

下载远程二进制或文件，并校验 hash。

字段：

- `host`：必填。
- `url`：必填。
- `path`：必填，远程目标路径。
- `sha256`：强烈建议填写。
- `owner`：可选，默认 `root`。
- `group`：可选，默认 `root`。
- `mode`：可选。

示例：

```hcl
debian_download "node_exporter" {
  host   = "server1"
  url    = "https://example.com/node_exporter"
  path   = "/usr/local/bin/node_exporter"
  sha256 = "..."
  mode   = "0755"
}
```

### 9.7 `debian_networkd`

通过 `systemd-networkd` 管理网络配置。

第一版只支持 systemd-networkd，不支持 NetworkManager 或 `/etc/network/interfaces`。

字段初稿：

- `host`：必填。
- `interface`：必填。
- `dhcp`：可选，`true` 或 `false`。
- `address`：可选，CIDR 地址列表。
- `gateway`：可选。
- `dns`：可选，DNS 地址列表。

示例：

```hcl
debian_networkd "eth0" {
  host      = "server1"
  interface = "eth0"
  dhcp      = false
  address   = ["192.0.2.10/24"]
  gateway   = "192.0.2.1"
  dns       = ["1.1.1.1", "8.8.8.8"]
}
```

安全要求：

- 修改网络配置前必须生成 plan。
- 默认不自动 apply 网络变更，除非用户显式确认。
- 应支持写入配置但不立即重启网络。
- 应尽量避免让 SSH 连接中的主机失联。

## 10. 依赖关系

资源可以通过引用自然形成依赖。

示例：

```hcl
debian_service "nginx" {
  host = "server1"
  state = "running"

  depends_on = [
    debian_package.nginx,
    debian_file.nginx_default,
  ]
}
```

第一版可以先支持显式 `depends_on`，后续再增强引用分析。

## 11. 错误处理

工具输出应清楚显示：

- 哪台主机失败。
- 哪个资源失败。
- 执行了什么操作。
- 远端命令的 stderr。
- 是否已经部分修改。

`apply` 遇到错误时，第一版可以停止当前主机后续资源执行。

## 12. 安全与敏感信息

第一版不内置复杂密钥管理。

要求：

- 不在普通日志中输出私钥内容。
- 不在 state 中明文保存敏感字段。
- HCL 中如需密码类字段，先标记为不推荐。
- 优先使用 SSH key 和远端 root 权限。

## 13. Go 实现建议

建议模块划分：

```text
cmd/dbf/              CLI 入口
internal/config/      HCL 解析和配置模型
internal/engine/      plan/apply/check 执行引擎
internal/sshx/        SSH config 解析、SSH 连接和远程命令执行
internal/resource/    资源接口和内置资源
internal/state/       SSH state 后端和远端锁
```

核心接口草案：

```go
type Resource interface {
    ID() string
    Read(ctx context.Context, host HostClient) (CurrentState, error)
    Plan(ctx context.Context, current CurrentState) (Change, error)
    Apply(ctx context.Context, host HostClient, change Change) error
}
```

## 14. MVP 范围

第一阶段建议只实现：

- `host`
- SSH config alias 主机解析
- `debian_package`
- `debian_file`
- `debian_directory`
- `debian_service`
- SSH state 后端和远端锁
- `validate`
- `plan`
- `apply`
- root SSH 连接

第二阶段再实现：

- `debian_download`
- `debian_networkd`
- `check`
- 多主机并发

## 15. 待确认问题

- MVP 是否需要立即支持 `debian_networkd`，还是先等 package/file/service 稳定。
- `apply` 是否默认需要二次确认。
