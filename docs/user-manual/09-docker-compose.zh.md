# 09. 部署 Docker Compose 项目

本章演示 DebianForm 的 Docker Compose 能力：管理项目目录、`compose.yaml`、敏感 `.env` 文件、
生成 systemd unit，最后把 Compose project 收敛到 `running`。

本章示例已在 Debian 13 amd64 测试主机上验证通过。首次运行会从 Docker Hub 拉取
`busybox:1.36`，需要网络访问。第 8 章如果已经安装过 Docker，本章会复用现有 Docker Engine。

编写本章时修复了一个系统问题：Compose project 漂移检查现在会用 `docker compose ps --all --format json`
读取停止容器，因此手动停止容器后会准确显示 `stopped -> running`。

## 创建工作目录

```bash
mkdir -p debianform-manual/09-docker-compose
cd debianform-manual/09-docker-compose
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/09-state.json"
    lock_path = "/var/lock/debianform/manual/09-state.lock"
  }

  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    compose "manualapp" {
      state          = "running"
      directory      = "/opt/debianform-compose-manual"
      project        = "manualapp"
      pull           = "missing"
      recreate       = "auto"
      remove_orphans = true

      file {
        path = "/opt/debianform-compose-manual/compose.yaml"

        content = <<-YAML
          services:
            web:
              image: busybox:1.36
              command: ["sh", "-c", "while true; do echo debianform-compose; sleep 60; done"]
              labels:
                com.example.debianform: "manual09"
              env_file:
                - .env
        YAML
      }

      env_file "app" {
        path    = "/opt/debianform-compose-manual/.env"
        content = "TOKEN=manual09-secret-value\n"
        mode    = "0600"
      }
    }
  }
}
```

`docker.compose` 不替代 Compose YAML。YAML 仍然表达容器拓扑；DebianForm 负责管理文件、
运行 `docker compose config`、生成 systemd unit，并通过 `docker compose up/stop/down` 收敛项目状态。

关键字段：

- `directory` 是 Compose project 的工作目录。
- `file` 管理主 `compose.yaml`，默认权限是 `0644`。
- `env_file` 管理敏感文件，默认会按敏感内容处理，示例中权限是 `0600`。
- `project` 对应 `docker compose -p manualapp`。
- `state = "running"` 表示项目应该保持运行。
- `pull = "missing"` 会在缺镜像时拉取镜像。
- `remove_orphans = true` 会在收敛时清理多余服务容器。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

首次离线 plan 会显示 Docker Engine、Compose 文件、systemd unit 和 project 资源，summary 类似：

```text
Summary: 15 create, 0 update, 0 delete, 0 no-op, 3 operations
```

如果第 8 章已经安装过 Docker，真实 apply 里 Docker 包和源可能显示为 adopt existing，这是正常的：
本章使用独立 state 文件，需要把远端已有资源纳入本章 state。

## 验证 Compose 项目

运行：

```bash
dbf plan
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
ssh manual1 'systemctl is-active debianform-compose-manualapp.service; systemctl is-enabled debianform-compose-manualapp.service'
ssh manual1 'stat -c "%a %U %G %n" /opt/debianform-compose-manual /opt/debianform-compose-manual/compose.yaml /opt/debianform-compose-manual/.env'
```

预期 `dbf plan` 和 `dbf check` 都是 no changes。Compose project 应该有一个 running 容器，
systemd service 应该是 `active` 和 `enabled`，文件权限类似：

```text
755 root root /opt/debianform-compose-manual
644 root root /opt/debianform-compose-manual/compose.yaml
600 root root /opt/debianform-compose-manual/.env
```

确认 state 里记录了 Compose 资源，但没有泄露 `.env` 明文：

```bash
ssh manual1 'grep -F "host.manual1.docker.compose[\"manualapp\"].project" /var/lib/debianform/manual/09-state.json'
ssh manual1 '! grep -F "manual09-secret-value" /var/lib/debianform/manual/09-state.json'
```

## 制造运行态漂移

手动停止 Compose project：

```bash
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml stop'
```

运行：

```bash
dbf check
```

预期失败，并显示 project 从 `stopped` 漂移到期望的 `running`：

```text
~ host.manual1.docker.compose["manualapp"].project
  converge docker compose project manualapp from stopped to running

Summary: 0 create, 1 update, 0 delete, 14 no-op, 0 operations
dbf: remote state does not match configuration
```

## 修复漂移

运行：

```bash
dbf apply --auto-approve
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
```

`apply` 会重新执行 `docker compose up -d --pull missing`，然后 `check` 回到 no changes。

## 本章完整命令

```bash
mkdir -p debianform-manual/09-docker-compose
cd debianform-manual/09-docker-compose

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/09-state.json"
    lock_path = "/var/lock/debianform/manual/09-state.lock"
  }

  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    compose "manualapp" {
      state          = "running"
      directory      = "/opt/debianform-compose-manual"
      project        = "manualapp"
      pull           = "missing"
      recreate       = "auto"
      remove_orphans = true

      file {
        path = "/opt/debianform-compose-manual/compose.yaml"

        content = <<-YAML
          services:
            web:
              image: busybox:1.36
              command: ["sh", "-c", "while true; do echo debianform-compose; sleep 60; done"]
              labels:
                com.example.debianform: "manual09"
              env_file:
                - .env
        YAML
      }

      env_file "app" {
        path    = "/opt/debianform-compose-manual/.env"
        content = "TOKEN=manual09-secret-value\n"
        mode    = "0600"
      }
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
dbf plan
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
ssh manual1 'systemctl is-active debianform-compose-manualapp.service; systemctl is-enabled debianform-compose-manualapp.service'
ssh manual1 'stat -c "%a %U %G %n" /opt/debianform-compose-manual /opt/debianform-compose-manual/compose.yaml /opt/debianform-compose-manual/.env'
ssh manual1 'grep -F "host.manual1.docker.compose[\"manualapp\"].project" /var/lib/debianform/manual/09-state.json'
ssh manual1 '! grep -F "manual09-secret-value" /var/lib/debianform/manual/09-state.json'

ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml stop'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
```

## 清理

如果要删除本章 Compose project 和 state：

```bash
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml down 2>/dev/null || true; systemctl disable --now debianform-compose-manualapp.service 2>/dev/null || true; rm -f /etc/systemd/system/debianform-compose-manualapp.service; systemctl daemon-reload; rm -rf /opt/debianform-compose-manual /var/lib/debianform/manual/09-state.json /var/lock/debianform/manual/09-state.lock /var/lock/debianform/manual/09-state.lock.d'
```
