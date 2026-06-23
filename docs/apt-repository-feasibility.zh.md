# DebianForm .deb 和 APT Repository 可行性评估

本文档评估 DebianForm 后续提供 `.deb` 包和 APT repository 的可行性、边界和实施路线。
当前 public beta 已有 GitHub Release tarball、curl installer 和 Homebrew tap；`.deb` 和
APT repository 尚未实现，也不是当前官方安装路径。

## 当前结论

- `.deb` 包可行，适合作为 Linux amd64/arm64 的原生安装方式。
- APT repository 可行，但需要额外签名密钥、仓库元数据生成、发布权限和长期运维承诺。
- public beta 阶段不应仓促上线 apt repository；先实现可本地验证的 `.deb` 产物，再决定是否
  发布 repository。
- 上线前必须明确 key rotation、rollback、撤回错误包、repository retention 和 release
  notes 通知流程。

## 用户价值

`.deb` 包提供：

- `dpkg -i` 或 `apt install ./dbf_<version>_<arch>.deb` 安装路径。
- 文件归属和卸载路径清晰。
- 安装 README、docs 和 examples 到 `/usr/share/debianform`。
- 可被内部 artifact repository 或配置管理系统分发。

APT repository 进一步提供：

- `apt install dbf`。
- `apt upgrade` 跟随版本更新。
- 企业或实验室环境可使用标准 Debian 软件源管理方式。

## 风险和成本

APT repository 不是单个 artifact，而是一条长期发布渠道。主要成本：

- GPG signing key 的生成、保护、轮换和吊销。
- `Release`、`InRelease`、`Packages`、by-hash 等元数据生成和校验。
- repository URL、suite、component 和 retention 策略一旦公开后很难随意改变。
- 发布失败时需要处理部分镜像、缓存和用户端 apt metadata 不一致。
- 需要额外验证 Linux amd64/arm64 `.deb` 安装、升级、降级和卸载。

因此 `.deb` 和 APT repository 应拆成两个阶段，不与 tarball/Homebrew 主路径耦合。

## 推荐仓库布局

建议使用专用发布路径，例如：

```text
https://apt.debianform.example/debianform
```

Debian suite/component：

```text
stable main
beta main
```

public beta 初期也可以只发布：

```text
beta main
```

目标架构：

```text
amd64
arm64
```

包名：

```text
dbf
```

安装内容：

```text
/usr/bin/dbf
/usr/share/debianform/README.md
/usr/share/debianform/docs/*
/usr/share/debianform/examples/*
/usr/share/doc/dbf/changelog.gz
/usr/share/doc/dbf/copyright
```

不建议在 Debian package 中默认安装 systemd unit，因为 `dbf` 是控制机 CLI，不是 daemon。

## 签名和密钥

APT repository 需要独立 signing key。建议：

- 使用专用 APT repository signing key，不复用 Git tag、cosign 或 SSH key。
- 公布 ASCII armored public key 和 key fingerprint。
- release notes 和 README 只引用 fingerprint，不只引用短 key id。
- key rotation 至少提前一个 release 公告。
- 私钥只存在于受控 release 环境或专用 secret store。

在 key 管理流程没有确定前，不发布长期 repository URL。

## 实施 Loop

### Loop A: 本地 `.deb` artifact

目标：在本地和 CI 中生成可安装 `.deb`，但不发布 APT repository。

范围：

- GoReleaser 或 nfpm 生成 `.deb`。
- 覆盖 Linux amd64 和 Linux arm64。
- package metadata 包含 description、license、homepage、maintainer。
- 包内包含 `dbf`、README、docs、examples、LICENSE 和 CHANGELOG。
- 安装后 `dbf version` 可运行。
- 卸载后 `/usr/bin/dbf` 被移除。

验收：

```bash
goreleaser release --snapshot --clean --skip publish
test -n "$(find dist -maxdepth 1 -name 'dbf_*_linux_amd64.deb' -print -quit)"
dpkg-deb --info dist/dbf_*_linux_amd64.deb
dpkg-deb --contents dist/dbf_*_linux_amd64.deb
```

真实安装验收需要 Debian VM：

```bash
apt install ./dbf_<version>_linux_amd64.deb
dbf version
apt remove dbf
```

### Loop B: Repository metadata dry-run

目标：本地生成 apt repository 目录和 metadata，不公开发布。

范围：

- 生成 `pool/`、`dists/`、`Packages`、`Release`、`InRelease`。
- 支持 amd64 和 arm64。
- 使用临时测试 key 签名。
- 在 Debian VM 中通过 `file://` 或本地 HTTP server 加源安装。

验收：

```bash
apt-get update
apt-get install dbf
dbf version
apt-get remove dbf
```

### Loop C: 发布渠道决策

目标：决定 repository 托管位置和权限模型。

候选：

- GitHub Pages。
- Cloudflare Pages/R2。
- S3-compatible bucket。
- 自建静态文件服务。

决策条件：

- 支持 HTTPS。
- 支持原子或近似原子的 metadata 发布。
- 支持回滚或保留旧版本。
- 支持 least-privilege release credential。
- 成本和运维复杂度可接受。

### Loop D: Public beta repository

目标：公开 beta APT repository。

前置条件：

- `.deb` 安装/升级/卸载在 Debian 13 amd64 真实或 libvirt VM 通过。
- arm64 artifact 至少完成构建和静态检查；真实 arm64 安装可先标记 best-effort。
- signing key fingerprint 已文档化。
- rollback 和 bad release 处理流程已写入 release runbook。
- release notes 模板包含 APT repository 验证结果。

## 不进入当前 beta 主路径的事项

- 进入 Debian official archive。
- 支持 Ubuntu、RHEL、Fedora、Arch 等非 Debian 发行版包仓库。
- 自动安装 shell completion、systemd service 或 daemon。
- 多 channel 自动 pinning 策略。

## 当前状态

截至本文档新增时：

- GitHub Release tarball、curl installer 和 Homebrew tap 是当前官方安装路径。
- `.deb` 包尚未实现。
- APT repository 尚未实现。
- 本文档完成可行性评估和实施 loop 拆分，作为后续实现依据。
