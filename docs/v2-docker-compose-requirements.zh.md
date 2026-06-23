# DebianForm v2 Docker / Docker Compose 管理需求文档

实现拆分与当前状态见：`docs/v2-docker-compose-implementation-plan.zh.md`。

当前实现状态：Compose systemd unit 先使用固定模板，生成 `Type=oneshot`、
`RemainAfterExit=yes`、`ExecStart=docker compose ... up -d` 和 `ExecStop=docker compose ... stop`。
MVP 暂不支持为 Compose unit 自定义 `Type`、`Restart`、`User`、`Group` 或额外 `Exec*` 行。

## 1. 背景

DebianForm 是一个面向 Debian 系统运维的声明式配置工具，设计上借鉴 Terraform 的 `plan / apply / state` 思路，以及 NixOS 的系统声明式配置体验。

Docker 和 Docker Compose 是 Debian 服务器上非常常见的应用部署方式。DebianForm 需要提供一套简洁、声明式、可组合的 Docker 管理语法，让用户可以通过 DebianForm 完成以下事情：

* 安装并启用 Docker Engine
* 使用 Docker 官方 APT 源，而不是默认使用 Debian 仓库里的旧版 `docker.io`
* 管理 Docker daemon 配置
* 管理 Docker Compose project
* 生成 systemd 托管服务
* 在 `plan / apply / check` 阶段清晰表达变更
* 尽量复用 Docker / Compose 的原生格式，不重新发明 Compose schema

## 2. 设计目标

### 2.1 用户体验目标

最小可用语法应该足够简单：

```hcl
docker {
  enable = true
}
```

这句配置应表示：

* 安装 Docker 官方 APT 源
* 安装 Docker 官方包
* 安装 Docker Compose plugin
* 启用 `docker.service`
* 启动 `docker.service`

用户不需要额外声明 `package.source = "official"` 才能获得官方 Docker。

### 2.2 默认行为目标

`docker { enable = true }` 默认使用 Docker 官方源，而不是 Debian 官方仓库中的 `docker.io`。

默认安装包包括：

```text
docker-ce
docker-ce-cli
containerd.io
docker-buildx-plugin
docker-compose-plugin
```

默认启用服务：

```text
docker.service
```

默认 Compose 命令使用：

```bash
docker compose
```

而不是旧版：

```bash
docker-compose
```

### 2.3 抽象层级目标

DebianForm 不应该直接模仿 Terraform Docker provider，暴露低阶资源：

```hcl
docker_container "nginx" {}
docker_image "nginx" {}
docker_network "frontend" {}
docker_volume "data" {}
```

第一版不推荐直接管理单个 container、image、network、volume。

DebianForm 应该管理两个高阶对象：

1. Docker 作为 Debian 系统能力
2. Compose project 作为应用部署单元

也就是说，Docker 管理的核心语义应该是：

```hcl
docker {
  enable = true

  compose "app" {
    state = "running"
  }
}
```

而不是：

```hcl
docker {
  container "app" {}
}
```

## 3. 非目标

第一版不实现以下能力：

* 不重新设计完整 Compose schema
* 不直接管理单个 Docker container
* 不直接管理单个 Docker image
* 不直接管理单个 Docker network
* 不直接管理单个 Docker volume
* 不实现 Swarm / Kubernetes 抽象
* 不实现私有 registry 的完整生命周期管理
* 不替代 `compose.yaml`
* 不发明 DebianForm 自己的容器编排 DSL

这些能力可以作为后续版本扩展，但不属于 MVP。

## 4. 顶层 Docker 语法

### 4.1 最小语法

```hcl
docker {
  enable = true
}
```

等价于：

```hcl
docker {
  enable = true

  package {
    source  = "official"
    channel = "stable"
  }

  service {
    enable = true
    state  = "running"
  }
}
```

### 4.2 完整语法草案

```hcl
docker {
  enable = true

  package {
    source  = "official"
    channel = "stable"
    version = null

    remove_conflicts = "auto"
  }

  service {
    enable = true
    state  = "running"
  }

  daemon {
    settings = {
      "log-driver" = "json-file"
      "log-opts" = {
        "max-size" = "100m"
        "max-file" = "3"
      }
    }
  }

  users = ["deploy"]
}
```

## 5. `enable` 字段语义

### 5.1 `enable = true`

```hcl
docker {
  enable = true
}
```

表示当前主机应该具备 Docker 能力。

DebianForm 应该生成以下资源：

```text
apt.keyring["/etc/apt/keyrings/docker.asc"]
apt.repository["docker-official"]
apt.package["docker-ce"]
apt.package["docker-ce-cli"]
apt.package["containerd.io"]
apt.package["docker-buildx-plugin"]
apt.package["docker-compose-plugin"]
systemd.service["docker"]
```

同时确保：

* Docker 官方源已配置
* Docker 官方包已安装
* `docker.service` 已 enable
* `docker.service` 已 running
* `docker compose` 命令可用

### 5.2 `enable = false`

```hcl
docker {
  enable = false
}
```

表示 DebianForm 不管理 Docker。

默认情况下不应该卸载 Docker，也不应该停止现有 Docker 服务。

如果后续需要支持卸载行为，应通过显式字段表达，例如：

```hcl
docker {
  enable = false
  purge  = true
}
```

该能力不属于 MVP。

## 6. Docker 安装源设计

### 6.1 默认安装源

默认值：

```hcl
package {
  source = "official"
}
```

含义：

* 使用 Docker 官方 APT repository
* 安装 Docker 官方包
* 不使用 Debian 仓库中的 `docker.io`

### 6.2 支持的 source 类型

```hcl
package {
  source = "official"
}
```

表示使用 Docker 官方源。

```hcl
package {
  source = "debian"
}
```

表示使用 Debian 官方仓库中的 Docker 包。

```hcl
package {
  source = "none"
}
```

表示不安装 Docker 包，只管理配置、服务或 Compose project。

```hcl
package {
  source = "custom"
}
```

表示用户自己声明 repository、keyring 和 packages。

### 6.3 官方源语法

```hcl
docker {
  enable = true

  package {
    source  = "official"
    channel = "stable"
  }
}
```

默认安装：

```text
docker-ce
docker-ce-cli
containerd.io
docker-buildx-plugin
docker-compose-plugin
```

### 6.4 Debian 源语法

```hcl
docker {
  enable = true

  package {
    source = "debian"
  }
}
```

安装包可默认为：

```text
docker.io
docker-compose-plugin
```

该模式主要用于希望完全遵循 Debian 官方仓库的用户。

但它不是默认值。

### 6.5 不安装包

```hcl
docker {
  enable = true

  package {
    source = "none"
  }
}
```

语义：

* 不添加 Docker 官方源
* 不安装 Docker 包
* 仍然允许管理 `/etc/docker/daemon.json`
* 仍然允许管理 `docker.service`
* 仍然允许管理 Compose project

这个模式适合已经手动安装 Docker 的生产机器。

## 7. 冲突包处理

Docker 官方包可能和 Debian 里已有的 Docker 相关包冲突。

DebianForm 应支持冲突包检测。

建议字段：

```hcl
package {
  remove_conflicts = "auto"
}
```

可选值：

```text
auto
true
false
```

### 7.1 `remove_conflicts = "auto"`

默认值。

语义：

* `plan` 阶段检测冲突包
* 清晰展示将要替换的包
* `apply` 阶段按需要移除冲突包
* 不静默删除用户已有包

可能冲突包包括：

```text
docker.io
docker-doc
docker-compose
podman-docker
containerd
runc
```

### 7.2 plan 示例

```text
~ docker.package
  source: debian -> official

- apt.package docker.io
+ apt.package docker-ce
+ apt.package docker-ce-cli
+ apt.package containerd.io
+ apt.package docker-buildx-plugin
+ apt.package docker-compose-plugin
```

## 8. Docker daemon 配置

### 8.1 语法

```hcl
docker {
  enable = true

  daemon {
    settings = {
      "log-driver" = "json-file"
      "log-opts" = {
        "max-size" = "100m"
        "max-file" = "3"
      }
      "registry-mirrors" = [
        "https://mirror.example.com"
      ]
    }
  }
}
```

### 8.2 语义

`daemon.settings` 映射到：

```text
/etc/docker/daemon.json
```

DebianForm 应负责：

* 生成 JSON 文件
* 校验 JSON 合法性
* 设置文件权限
* 检测 drift
* 在配置变化后 reload 或 restart Docker

默认文件权限：

```text
owner: root
group: root
mode: 0644
```

### 8.3 编译资源

```text
file["/etc/docker/daemon.json"]
systemd.service["docker"]
```

当 daemon 配置发生变化时，应触发：

```text
systemctl restart docker
```

后续可以考虑更细粒度的 reload 行为，但 MVP 可先统一 restart。

## 9. Docker 用户组

### 9.1 语法

```hcl
docker {
  enable = true

  users = ["deploy", "app"]
}
```

### 9.2 语义

表示将用户加入 `docker` group。

编译资源：

```text
group["docker"]
user_group_membership["deploy:docker"]
user_group_membership["app:docker"]
```

注意：

* DebianForm 只负责声明用户属于 docker group
* 不负责解决当前登录 shell 的 group session 刷新问题
* plan/apply 输出中应提示用户重新登录后权限生效

## 10. Compose Project 管理

### 10.1 设计原则

DebianForm 管理 Compose project，而不是直接管理单个容器。

Compose 的原生配置仍然由 `compose.yaml` 表达。

DebianForm 负责：

* 写入 compose 文件
* 写入 env 文件
* 校验 compose 配置
* 生成 systemd unit
* 启动、停止或删除 Compose project
* 接入 plan / apply / check / drift 检测

### 10.2 最小语法

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }
  }
}
```

### 10.3 完整语法草案

```hcl
docker {
  enable = true

  compose "app" {
    enable    = true
    state     = "running"
    directory = "/opt/app"
    project   = "app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
      owner   = "root"
      group   = "root"
      mode    = "0644"
    }

    env_file "app" {
      path    = "/opt/app/.env"
      content = file("./.env")
      owner   = "root"
      group   = "root"
      mode    = "0600"
    }

    pull           = "missing"
    recreate       = "auto"
    remove_orphans = false

    service {
      enable = true
      name   = "debianform-compose-app"
    }

    after     = ["docker.service", "network-online.target"]
    wanted_by = ["multi-user.target"]
  }
}
```

## 11. Compose 字段语义

### 11.1 `compose "<name>"`

```hcl
compose "app" {}
```

`app` 是 DebianForm 内部的 Compose project 标识。

默认情况下：

```hcl
project = "app"
```

### 11.2 `enable`

```hcl
compose "app" {
  enable = true
}
```

表示 DebianForm 管理该 Compose project。

默认值：

```hcl
enable = true
```

### 11.3 `state`

```hcl
state = "running"
```

可选值：

```text
running
stopped
absent
```

含义：

| state   | 行为                        |
| ------- | ------------------------- |
| running | 执行 `docker compose up -d` |
| stopped | 执行 `docker compose stop`  |
| absent  | 执行 `docker compose down`  |

默认值：

```hcl
state = "running"
```

### 11.4 `directory`

```hcl
directory = "/opt/app"
```

表示 Compose project 的工作目录。

DebianForm 应确保该目录存在。

默认权限：

```text
owner: root
group: root
mode: 0755
```

### 11.5 `project`

```hcl
project = "app"
```

表示 Compose project name。

如果不设置，默认使用 block label。

### 11.6 `file`

```hcl
file {
  path    = "/opt/app/compose.yaml"
  content = file("./compose.yaml")
}
```

表示 DebianForm 管理 Compose 文件。

`file.content` 可以来自：

```hcl
content = file("./compose.yaml")
```

也可以直接内联：

```hcl
content = <<-YAML
services:
  web:
    image: nginx:1.27-alpine
    ports:
      - "8080:80"
YAML
```

### 11.7 `env_file`

```hcl
env_file "app" {
  path    = "/opt/app/.env"
  content = file("./.env")
  mode    = "0600"
}
```

表示 DebianForm 管理 Compose 使用的 env 文件。

可以支持多个：

```hcl
env_file "app" {}
env_file "secret" {}
```

### 11.8 `pull`

```hcl
pull = "missing"
```

可选值：

```text
never
missing
always
```

含义：

| pull    | 行为              |
| ------- | --------------- |
| never   | 不主动 pull image  |
| missing | image 不存在时 pull |
| always  | 每次 apply 前 pull |

默认建议：

```hcl
pull = "missing"
```

### 11.9 `recreate`

```hcl
recreate = "auto"
```

可选值：

```text
auto
always
never
```

含义：

| recreate | 行为                               |
| -------- | -------------------------------- |
| auto     | 由 docker compose 判断是否需要 recreate |
| always   | 强制 recreate                      |
| never    | 不 recreate                       |

默认值：

```hcl
recreate = "auto"
```

### 11.10 `remove_orphans`

```hcl
remove_orphans = true
```

表示执行 compose up 时附加：

```bash
--remove-orphans
```

默认值：

```hcl
remove_orphans = false
```

## 12. systemd 集成

### 12.1 目标

每个 Compose project 应可以生成一个 systemd unit，由 systemd 负责开机启动和服务托管。

### 12.2 默认 unit 名称

```text
debianform-compose-<name>.service
```

例如：

```hcl
compose "app" {}
```

默认生成：

```text
debianform-compose-app.service
```

### 12.3 语法

```hcl
compose "app" {
  service {
    enable = true
    name   = "debianform-compose-app"
  }

  after     = ["docker.service", "network-online.target"]
  wanted_by = ["multi-user.target"]
}
```

### 12.4 默认依赖

默认：

```hcl
after = ["docker.service", "network-online.target"]
wanted_by = ["multi-user.target"]
```

### 12.5 生成 unit 示例

```ini
[Unit]
Description=DebianForm Compose Project app
Requires=docker.service
After=docker.service network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/app
ExecStart=/usr/bin/docker compose -p app -f /opt/app/compose.yaml up -d
ExecStop=/usr/bin/docker compose -p app -f /opt/app/compose.yaml stop
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
```

当 `state = "absent"` 时，可执行：

```bash
docker compose -p app -f /opt/app/compose.yaml down
```

## 13. Compose 校验

DebianForm 在 apply 前应执行 Compose 配置校验。

建议命令：

```bash
docker compose -p <project> -f <file> config
```

校验失败时：

* 停止 apply
* 输出错误
* 不启动服务
* 不修改 systemd 状态

## 14. plan 行为

### 14.1 新增 Docker

输入：

```hcl
docker {
  enable = true
}
```

plan 输出示例：

```text
+ docker
  + apt keyring /etc/apt/keyrings/docker.asc
  + apt repository docker-official
  + package docker-ce
  + package docker-ce-cli
  + package containerd.io
  + package docker-buildx-plugin
  + package docker-compose-plugin
  + service docker.service enabled
  + service docker.service running
```

### 14.2 新增 Compose project

输入：

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }
  }
}
```

plan 输出示例：

```text
+ docker.compose["app"]
  + directory /opt/app
  + file /opt/app/compose.yaml
  + systemd unit debianform-compose-app.service
  + validate compose config
  + start compose project app
```

### 14.3 修改 daemon 配置

```text
~ docker.daemon
  ~ /etc/docker/daemon.json
  ~ restart docker.service
```

### 14.4 修改 Compose 文件

```text
~ docker.compose["app"]
  ~ file /opt/app/compose.yaml
  ~ validate compose config
  ~ docker compose up -d
```

## 15. apply 行为

`apply` 应按以下顺序执行：

1. 配置 Docker 官方源
2. 安装 Docker packages
3. 创建 docker group
4. 配置用户 group membership
5. 写入 `/etc/docker/daemon.json`
6. enable/start `docker.service`
7. 创建 Compose 工作目录
8. 写入 compose 文件
9. 写入 env 文件
10. 执行 `docker compose config`
11. 写入 systemd unit
12. 执行 `systemctl daemon-reload`
13. enable/start compose systemd unit
14. 根据 `state` 执行 `up / stop / down`

## 16. check 行为

`check` 应检查：

* Docker 官方源是否存在
* Docker packages 是否已安装
* Docker 版本是否满足声明
* `docker.service` 是否 enabled
* `docker.service` 是否 running
* `/etc/docker/daemon.json` 内容是否与声明一致
* Compose 工作目录是否存在
* Compose 文件内容是否与声明一致
* env 文件内容是否与声明一致
* systemd unit 内容是否与声明一致
* Compose project 是否处于期望状态

示例输出：

```text
✓ docker official repository
✓ docker packages
✓ docker.service running
✓ /etc/docker/daemon.json
✓ compose app config
✓ compose app systemd unit
✓ compose app running
```

## 17. drift 检测

DebianForm 应能检测以下 drift：

* 用户手动修改 `/etc/docker/daemon.json`
* 用户手动修改 compose 文件
* 用户手动修改 env 文件
* 用户手动 disable `docker.service`
* 用户手动 stop `docker.service`
* 用户手动修改 systemd unit
* Compose project 未运行
* Compose project 中存在 orphan container

drift 输出示例：

```text
! drift detected

~ /etc/docker/daemon.json
  expected hash: abc123
  actual hash:   def456

~ docker.compose["app"]
  expected state: running
  actual state:   stopped
```

## 18. 与现有 DebianForm 架构的关系

Docker DSL 属于 DebianForm v2 的高阶领域块。

用户层写：

```hcl
host "server1" {
  docker {
    enable = true
  }
}
```

编译器应将其展开为底层资源图：

```text
apt_repository
apt_keyring
apt_package
file
directory
group
user_group_membership
systemd_service
operation
```

用户不直接操作底层资源，除非后续提供 escape hatch。

## 19. 推荐 MVP 范围

第一版只实现以下能力：

```hcl
docker {
  enable = true
}
```

```hcl
docker {
  enable = true

  daemon {
    settings = {}
  }
}
```

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }
  }
}
```

MVP 必须支持：

* 官方 Docker APT 源
* 官方 Docker packages
* Docker service enable/start
* `/etc/docker/daemon.json`
* Compose 文件写入
* Compose config 校验
* Compose project up/stop/down
* systemd unit 生成
* plan/apply/check 输出

MVP 暂不支持：

* HCL 对象自动生成 compose YAML
* 镜像生命周期管理
* volume 生命周期管理
* network 生命周期管理
* registry 登录
* secrets 管理
* swarm
* rootless Docker
* Podman backend

## 20. 后续扩展方向

### 20.1 HCL 生成 Compose YAML

后续可以支持：

```hcl
docker {
  compose "app" {
    spec = {
      services = {
        web = {
          image = "nginx:1.27-alpine"
          ports = ["8080:80"]
        }
      }
    }
  }
}
```

该能力只是语法糖。

主路径仍然应该是原生 Compose 文件：

```hcl
file {
  content = file("./compose.yaml")
}
```

### 20.2 Registry 登录

后续可以支持：

```hcl
docker {
  registry "ghcr.io" {
    username = "user"
    password = secret("ghcr_token")
  }
}
```

不属于 MVP。

### 20.3 Rootless Docker

后续可以支持：

```hcl
docker {
  rootless {
    enable = true
    user   = "deploy"
  }
}
```

不属于 MVP。

### 20.4 Docker mirror

后续可以支持：

```hcl
docker {
  package {
    source = "official"

    repository {
      mirror = "https://mirror.example.com/docker"
    }
  }
}
```

不属于 MVP。

## 21. 最终推荐语法

最小 Docker：

```hcl
docker {
  enable = true
}
```

Docker daemon：

```hcl
docker {
  enable = true

  daemon {
    settings = {
      "log-driver" = "json-file"
      "log-opts" = {
        "max-size" = "100m"
        "max-file" = "3"
      }
    }
  }
}
```

Docker Compose：

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }

    env_file "app" {
      path    = "/opt/app/.env"
      content = file("./.env")
      mode    = "0600"
    }

    after     = ["docker.service", "network-online.target"]
    wanted_by = ["multi-user.target"]
  }
}
```

## 22. 核心结论

DebianForm 的 Docker 管理语义应该是：

```hcl
docker {
  enable = true
}
```

表示：

```text
让这台 Debian 主机具备 Docker 能力。
```

默认行为必须是：

```text
使用 Docker 官方源安装 Docker。
```

而不是：

```text
使用 Debian 仓库中的 docker.io。
```

Compose 的语义应该是：

```hcl
docker {
  compose "app" {}
}
```

表示：

```text
管理一个 Docker Compose project。
```

而不是：

```text
管理一组低阶 container / image / network / volume。
```

这样可以同时保持：

* DebianForm 的高阶系统声明式体验
* Terraform 风格的 plan/apply/check 能力
* NixOS 风格的 enable 配置习惯
* Docker Compose 原生生态兼容性
* 简洁、稳定、可扩展的 DSL 边界
