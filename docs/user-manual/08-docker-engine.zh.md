# 08. 管理 Docker Engine、daemon 配置和用户权限

本章演示 DebianForm 的高层 Docker 能力：安装 Docker 官方源和软件包，启用 Docker 服务，
写 `/etc/docker/daemon.json`，并把一个受管用户加入 `docker` 组。

本章示例已在 Debian 13 amd64 测试主机上验证通过。首次运行会从 Docker 官方 APT 源下载并安装
Docker Engine，需要网络访问。

编写本章时修复了一个系统问题：当 `docker.users` 引用同一份配置中新建的用户时，在线 plan 现在会允许
先计划 membership，再由 apply 按依赖顺序先创建用户、再加入 docker 组。

## 创建工作目录

```bash
mkdir -p debianform-manual/08-docker-engine
cd debianform-manual/08-docker-engine
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/08-state.json"
    lock_path = "/var/lock/debianform/manual/08-state.lock"
  }

  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  users {
    user "dockerdeploy" {
      system = true
      home   = "/var/lib/dockerdeploy"
      shell  = "/usr/sbin/nologin"
    }
  }

  docker {
    enable = true
    users  = ["dockerdeploy"]

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "10m"
          "max-file" = "2"
        }
      }
    }
  }
}
```

`docker { enable = true }` 会展开为多项资源：

- Docker 官方 APT signing key。
- Docker 官方 APT repository。
- APT cache refresh。
- Docker 冲突包清理。
- `docker-ce`、`docker-ce-cli`、`containerd.io`、buildx 和 compose plugin。
- `docker` group。
- `docker.service`。
- 可选 daemon 配置和 restart operation。
- 可选用户 supplementary group membership。

`system.architecture` 和 `system.codename` 用于离线生成 Docker 官方 APT 源。在线 plan 可以自动发现 facts，
但教程里显式写出这两个字段，方便 `dbf plan --offline` 也能运行。

## 使用 Docker 官方 APT 镜像站

默认 `docker.package.source = "official"` 使用：

- `repository_url = "https://download.docker.com/linux/debian"`
- `gpg_url = "https://download.docker.com/linux/debian/gpg"`

如需使用 Aliyun 的 Docker official APT 镜像，可在 `package` 中覆盖 URL：

```hcl
host "manual1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    package {
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
      gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
```

`gpg_sha256` 是可选字段。默认 Docker official GPG URL 会自动使用 Docker official key SHA256；
自定义 `gpg_url` 时 DebianForm 不会把官方 SHA 套用到镜像 URL 上。如需固定镜像站 key 内容，可添加：

```hcl
gpg_sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

自定义 `gpg_url` 且省略 `gpg_sha256` 时，DebianForm 不校验 key 文件内容的 checksum；需要内容校验和
key 内容漂移检测时应设置 `gpg_sha256`。

这些字段只对 `package.source = "official"` 有效；`debian`、`none`、`custom` 下设置会报错。
它们和 `get.docker.com --mirror` 都是为了把 Docker 安装来源切到镜像站，但 DebianForm 不运行
`get.docker.com` 脚本，而是管理 APT source、key、package、service 和 state。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

离线 plan 会显示较多资源，summary 类似：

```text
Summary: 13 create, 0 update, 0 delete, 0 no-op, 2 operations
```

两个 operation 分别是：

- `apt-get update`
- `systemctl restart docker.service`

首次安装可能需要几十秒。

## 验证 Docker

运行：

```bash
ssh manual1 'docker --version; docker compose version; systemctl is-active docker; systemctl is-enabled docker; id dockerdeploy; cat /etc/docker/daemon.json'
```

预期类似：

```text
Docker version 29.6.0, build fb59821
Docker Compose version v5.1.4
active
enabled
uid=994(dockerdeploy) gid=986(dockerdeploy) groups=986(dockerdeploy),1000(docker)
{
  "log-driver": "json-file",
  "log-opts": {
    "max-file": "2",
    "max-size": "10m"
  }
}
```

版本号和 uid/gid 可能不同。

`docker.users` 修改的是 supplementary group。已经登录的用户 session 需要重新登录后才会获得新组权限。

## 制造 daemon 配置漂移

手动把 `max-size` 改成 `20m`：

```bash
ssh manual1 'sed -i "s/10m/20m/" /etc/docker/daemon.json'
```

运行：

```bash
dbf check
```

预期失败：

```text
~ host.manual1.docker.daemon.file["/etc/docker/daemon.json"]
  update file /etc/docker/daemon.json

! host.manual1.docker.daemon.restart
  restart docker service

Summary: 0 create, 1 update, 0 delete, 12 no-op, 1 operations
dbf: remote state does not match configuration
```

daemon 配置变化后，DebianForm 会重写文件并重启 Docker。

## 修复漂移

运行：

```bash
dbf apply --auto-approve
dbf check
```

确认配置恢复且 Docker 仍在运行：

```bash
ssh manual1 'grep -F "max-size" /etc/docker/daemon.json; systemctl is-active docker'
```

预期：

```text
    "max-size": "10m"
active
```

## 本章完整命令

```bash
mkdir -p debianform-manual/08-docker-engine
cd debianform-manual/08-docker-engine

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/08-state.json"
    lock_path = "/var/lock/debianform/manual/08-state.lock"
  }

  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  users {
    user "dockerdeploy" {
      system = true
      home   = "/var/lib/dockerdeploy"
      shell  = "/usr/sbin/nologin"
    }
  }

  docker {
    enable = true
    users  = ["dockerdeploy"]

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "10m"
          "max-file" = "2"
        }
      }
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'docker --version; docker compose version; systemctl is-active docker; systemctl is-enabled docker; id dockerdeploy; cat /etc/docker/daemon.json'

ssh manual1 'sed -i "s/10m/20m/" /etc/docker/daemon.json'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F "max-size" /etc/docker/daemon.json; systemctl is-active docker'
```

## 清理

如果要卸载 Docker 并清理本章 state：

```bash
ssh manual1 'systemctl disable --now docker.service 2>/dev/null || true; systemctl disable --now containerd.service 2>/dev/null || true; userdel dockerdeploy 2>/dev/null || true; rm -rf /var/lib/dockerdeploy /var/lib/debianform/manual/08-state.json /var/lock/debianform/manual/08-state.lock /var/lock/debianform/manual/08-state.lock.d; rm -f /etc/docker/daemon.json /etc/apt/sources.list.d/docker_official.sources /etc/apt/keyrings/docker.asc; DEBIAN_FRONTEND=noninteractive apt-get remove -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin'
```

继续第 9 章 Docker Compose 时不要清理本章，因为第 9 章会复用 Docker Engine。
