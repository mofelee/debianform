<p align="center">
  <img src="./logo.png" alt="DebianForm logo" width="320">
</p>

# DebianForm

DebianForm 是一个面向 Debian 主机的声明式配置工具：写一份 `.dbf.hcl`，先看
`plan`，再执行 `apply`，最后用 `check` 检查漂移。

它把常见服务器配置变成可读、可审计、可重复的 HCL：

- 管理文件、目录、用户、组、APT、kernel/sysctl、systemd、nftables、Docker 和 Compose。
- 默认先生成变更计划，不先碰目标机。
- 在线模式通过 SSH 读取目标主机事实、远端 state 和实际状态。
- 每台 host 有独立远端 state 和 lock，避免并发 apply 打架。
- secret 和 sensitive 内容在 plan、state、HTML/JSON 输出中脱敏。
- `.dbf.hcl` 足够直接，适合人读，也适合 LLM 生成和修改。

当前项目仍处于 public preview / beta 阶段。建议先在低风险 Debian 13 测试主机上试用；
CLI、配置格式、state 和 plan JSON 在 stable 前仍可能调整。

## 30 秒安装

macOS 或 Linux 上推荐 Homebrew：

```bash
brew install mofelee/debianform/dbf
dbf version
```

也可以使用安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
dbf version
```

## 5 分钟快速开始

准备一台低风险 Debian 13 amd64 主机，并在控制机的 `~/.ssh/config` 里给它一个稳定名字。
DebianForm 默认把 `host "server1"` 当作 `ssh server1` 使用；连接细节交给 SSH config：

```sshconfig
Host server1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
```

先确认普通 SSH 可以工作：

```bash
ssh server1 'cat /etc/debian_version && uname -m'
```

新建 `site.dbf.hcl`：

```hcl
host "server1" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}
```

然后执行：

```bash
dbf validate -f site.dbf.hcl
dbf plan -f site.dbf.hcl --offline
dbf plan -f site.dbf.hcl
dbf apply -f site.dbf.hcl
dbf plan -f site.dbf.hcl
dbf check -f site.dbf.hcl
```

这条路径覆盖了完整闭环：

- `validate`：本地解析和校验配置，不连接主机。
- `plan --offline`：本地预览资源地址和变更形状。
- `plan`：通过 SSH 读取目标主机事实、远端 state 和 observed 状态。
- `apply`：重新生成在线 plan，获取远端 lock，按资源图执行变更并写 state。
- 第二次 `plan`：预期 no-op。
- `check`：检查远端是否漂移；不一致时返回非零。

更完整的新手教程见 [Quickstart](docs/quickstart.zh.md)。真实服务部署模板见
[systemd app 示例](docs/realistic-deployment-example.zh.md)。

## 常用命令

```bash
# 校验配置
dbf validate -f site.dbf.hcl

# 本地预览，不连接目标机
dbf plan -f site.dbf.hcl --offline

# 在线 plan，读取 facts/state/observed 状态
dbf plan -f site.dbf.hcl

# 输出机器可读 plan
dbf plan -f site.dbf.hcl --format json

# 输出静态 HTML plan
dbf plan -f site.dbf.hcl --html plan.html

# 应用变更
dbf apply -f site.dbf.hcl

# CI 或临时环境跳过确认
dbf apply -f site.dbf.hcl --auto-approve

# 检查漂移
dbf check -f site.dbf.hcl

# 格式化配置
dbf fmt -f site.dbf.hcl

# 查看 component/variable 公开输入
dbf component inspect -f site.dbf.hcl component_name
dbf variable inspect -f site.dbf.hcl
```

不传 `-f` 时，`dbf` 读取当前目录所有 `*.dbf.hcl` 并按文件名排序。传入一个或多个
`-f file` 时，只读取这些显式文件，并按命令行顺序解析。

默认情况下，`host "<name>"` 会通过 `ssh <name>` 连接，管理用户为 root。推荐把
`HostName`、`User`、`IdentityFile`、`ProxyJump`、端口等连接细节放在 `~/.ssh/config`。
只有需要覆盖默认连接名、端口、identity file 或 state 路径时，才在 `.dbf.hcl` 中写
`ssh` 或 `state` block。

## 配置模型

DebianForm 的用户层只写 `host`、`profile`、`component`、`locals`、`variable` 和领域块。
不需要写低阶 provider 资源。

一个真实但仍很小的服务配置可以包含：

```hcl
host "app1" {
  groups {
    group "app" {
      system = true
    }
  }

  users {
    user "app" {
      system = true
      group  = "app"
      home   = "/var/lib/app"
      shell  = "/usr/sbin/nologin"
    }
  }

  files {
    file "/etc/app/config.env" {
      owner   = "root"
      group   = "app"
      mode    = "0640"
      content = "APP_ENV=prod\n"
    }
  }

  systemd {
    service_unit "app" {
      description = "App worker"
      run         = ["/usr/local/bin/app-worker"]
      user        = "app"
      group       = "app"
      restart     = "always"
    }
  }

  services {
    service "app" {
      enabled = true
      state   = "running"
    }
  }
}
```

更多可运行样例在 `examples/`。这些示例文件目前仍保留历史 `v2-` 文件名前缀。
常用本地预览命令：

```bash
dbf validate -f examples/v2-bbr.dbf.hcl
dbf plan -f examples/v2-bbr.dbf.hcl --offline
dbf validate -f examples/v2-realistic-systemd-app.dbf.hcl
dbf plan -f examples/v2-realistic-systemd-app.dbf.hcl --offline
dbf plan -f examples/v2-docker-minimal.dbf.hcl --offline
dbf plan -f examples/v2-nftables.dbf.hcl --offline
```

当前 README 覆盖的可运行示例：

- `examples/v2-bbr.dbf.hcl`
- `examples/v2-apt-repository.dbf.hcl`
- `examples/v2-bird2.dbf.hcl`
- `examples/v2-component-binary.dbf.hcl`
- `examples/v2-files-plan-preview.dbf.hcl`
- `examples/v2-mihomo.dbf.hcl`
- `examples/v2-nftables.dbf.hcl`
- `examples/v2-plan-preview.dbf.hcl`
- `examples/v2-profile-merge.dbf.hcl`
- `examples/v2-realistic-systemd-app.dbf.hcl`
- `examples/v2-systemd-service.dbf.hcl`
- `examples/v2-user-group.dbf.hcl`
- `examples/v2-variable-secret-file.dbf.hcl`

更完整的示例状态和覆盖范围见 [支持矩阵](docs/support-matrix.zh.md)。

## 支持边界

- CLI 可运行在 Linux 和 macOS 的 amd64/arm64。
- 被管理目标主机当前最高优先级是 Debian 13 amd64。
- 在线 `plan`、`apply`、`check` 当前要求目标主机可用 root SSH key 登录。
- `ssh.user` 只能省略或设置为 `"root"`；不支持 sudo、become 或非 root 管理连接。
- 服务进程仍可以通过 systemd `user`/`group` 以低权限运行；这不改变管理连接必须是 root。

平台细节见 [支持矩阵](docs/support-matrix.zh.md) 和
[平台支持策略](docs/platform-support-strategy.zh.md)。安全边界见
[安全模型](docs/security-model.zh.md)。

## 文档索引

- [Quickstart](docs/quickstart.zh.md)：从零到第一次 `apply/check`。
- [CLI 手册](docs/cli.zh.md)：所有命令、参数、输出和限制。
- [真实部署模板](docs/realistic-deployment-example.zh.md)：低权限 systemd app 模板。
- [Operations Runbook](docs/operations-runbook.zh.md)：state lock、失败恢复、drift 排查。
- [支持矩阵](docs/support-matrix.zh.md)：当前支持的系统、领域块、示例和验证覆盖。
- [兼容性政策](docs/compatibility-policy.zh.md)：beta/stable 的兼容和迁移规则。
- [Plan JSON 格式](docs/plan-format.md)：`dbf plan --format json` 的结构化输出。
- [State 格式](docs/state.md)：远端 state、lock、ownership 和脱敏规则。
- [Docs 索引](docs/README.zh.md)：所有用户文档、维护文档和归档设计稿入口。

## 发布产物校验

每个 GitHub Release 包含平台 tarball、`checksums.txt`、cosign keyless bundle、SBOM
和 GitHub provenance attestation。快速校验 checksum：

```bash
sha256sum --check checksums.txt
```

完整发布和校验流程见 [release process](docs/release-process.zh.md)。

## 开发

```bash
make build
make test
```

libvirt 集成测试位于 `test/integration/libvirt/`，用于在全新的 Debian VM 中验证
`validate`、`apply`、`check` 闭环。
