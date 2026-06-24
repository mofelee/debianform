# DebianForm 用户手册

这个目录存放面向使用者的系列教程。每一章都按“可以直接运行”的标准编写：包含完整 `.dbf.hcl`
示例、验证命令、预期结果和清理/回滚提示。教程从最小闭环开始，逐步覆盖常见运维任务。

所有章节示例都应满足：

- 能在低风险 Debian 13 amd64 测试主机上运行。
- 命令从一个空工作目录开始，避免依赖读者已有文件。
- 示例里出现的 `.dbf.hcl`、shell 命令和检查命令都经过真实验证。
- 如果某章需要公网软件源或外部下载，会在开头明确说明。
- 如果发现 DebianForm 行为与教程目标不匹配，先修复系统或文档，再继续写下一章。

## 任务列表

- [x] [01. 准备测试主机并完成第一次 apply/check](01-first-apply.zh.md)
- [x] [02. 管理文件、目录和漂移修复](02-files-and-drift.zh.md)
- [x] [03. 管理用户、组和 SSH authorized keys](03-users-and-ssh-keys.zh.md)
- [x] [04. 安装软件包和配置 APT 源](04-apt-and-packages.zh.md)
- [x] [05. 管理 systemd service unit 和服务状态](05-systemd-service.zh.md)
- [x] [06. 管理内核模块、sysctl 和 BBR](06-kernel-and-sysctl.zh.md)
- [x] [07. 管理 nftables 防火墙](07-nftables.zh.md)
- [x] [08. 管理 Docker Engine、daemon 配置和用户权限](08-docker-engine.zh.md)
- [x] [09. 部署 Docker Compose 项目](09-docker-compose.zh.md)
- [x] [10. 使用 profile、variable 和多环境参数](10-profiles-and-variables.zh.md)
- [x] [11. 使用 component 安装二进制或源码构建工具](11-components.zh.md)
- [x] [12. 日常运维：plan 审阅、漂移处理、锁、state 和故障恢复](12-operations.zh.md)

后续如果继续扩展用户手册，先把新章节追加到这个任务列表，再按顺序补正文和验证记录。

## 阅读顺序

建议从第 1 章开始顺序阅读。后续章节会复用前面建立的习惯：

```text
mkdir workdir
cat > site.dbf.hcl
dbf validate
dbf plan --offline
dbf plan
dbf apply --auto-approve
dbf plan
dbf check
ssh host '...'
```

每章都尽量保持独立，方便读者只复制当前章节的文件到新目录运行。

## 测试约定

本手册使用 disposable libvirt Debian 测试主机验证示例。验证时使用 root SSH，因为 DebianForm 当前需要
安装包、写 `/etc`、管理 systemd，并在 `/var/lib/debianform` 和 `/var/lock/debianform` 写 state/lock。

示例中的主机名统一写为 `manual1`。读者只需要在自己的 `~/.ssh/config` 中把 `manual1` 指向测试主机。
