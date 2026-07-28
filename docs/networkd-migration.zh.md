<p align="right">
  <a href="networkd-migration.md">English</a> | <strong>简体中文</strong>
</p>

# 迁移 systemd-networkd ownership

本指南覆盖迁入 Preview 原生 `systemd.networkd` provider 的过程。它不会把该 provider 提升为
Beta 或 Stable，也不允许 DebianForm 从 Netplan、NetworkManager 或其他配置管理器接管
ownership。

## 范围与支持等级

原生 networkd resource 保留现有 `networkd_netdev`、`networkd_network` address、ownership
preflight、drift detection、reload 聚合、lifecycle protection 和 runtime netdev cleanup。
通用 `section` block 与 raw `content`/`source` 是这些 resource 的内容形式，不是
`files.file` 的别名。

任何可能影响 SSH 或路由的 ownership 变更都应使用维护窗口和恢复通道。DebianForm 不提供
事务式网络回滚。

## 从兼容语法迁移到通用 section

把现有原生 `netdev` 或 `network` resource 从兼容属性改为通用 section 时，DebianForm resource
address 与 provider identity 不变。apply 前应：

- 保持 resource label 与 `path` 不变；
- 渲染两种形式并比较完整文件字节、owner、group 和 mode；
- 确认 plan 只有原地 update，没有 destroy/create action；
- 保留旧配置用于回滚。

通用 section 按声明顺序输出 section，setting key 按字典序输出。兼容属性保留历史渲染顺序，
因此机械改写语法不等于已经证明字节相同。raw 与 structured 之间切换也需要同样审查。

## 从 files.file 迁移到原生 networkd

把 `files.file` 改为 `systemd.networkd.netdev` 或 `systemd.networkd.network` 会改变 state address
和 provider kind。DebianForm 不会自动 adopt 旧 record；当前 `moved` block 支持 component
instance move，不支持 leaf resource ownership 转移。由于 path ownership 必须唯一，两种声明
也不能同时管理同一个远端 path。

### 交接前

- 确认文件只由一个声明拥有，并检查当前远端 state ownership；
- 运行 `dbf check`，保存 text 与 JSON plan；
- 备份远端 state 文件及所有受影响 networkd 文件，并保留权限；
- 验证本地控制台、带外控制台或第二条管理路径可用；
- 记录当前 `networkctl status`、地址、路由和 routing daemon 状态；
- 在交接获批前，为旧 resource 设置 `lifecycle { prevent_destroy = true }`。

Ubuntu 目标必须先完成只读 ownership preflight。active Netplan ownership 是 blocker，不能作为
迁移输入。

### 经过审查的交接

删除旧 `files.file` 声明，并用相同 `path` 添加原生 resource。plan 应显示旧 file resource 被
destroy，新原生 resource 按 observed state 被 create 或 adopt。如果 plan 包含意外 runtime
netdev deletion、path 变化或无关网络变更，不要 apply。

通过恢复通道执行交接，然后验证：

```bash
dbf plan -f site.dbf.hcl
dbf apply -f site.dbf.hcl
dbf check -f site.dbf.hcl
networkctl status
ip address show
ip route show table all
```

交接期间可能短暂删除并重建 managed file。DebianForm 不承诺从 `files.file` 零停机迁移；
apply 中断后必须人工恢复。

### 回滚

在变更后检查通过前，保留旧配置、state 备份和 networkd 文件备份。需要回滚时，通过恢复通道
恢复一套彼此一致的配置、state 和文件，reload networkd，恢复 routing daemon export，并在
再次 apply 前运行 `dbf plan`。不要只恢复 state 文件而让新 ownership 继续生效。

## Ubuntu ownership 边界

DebianForm 不生成、编辑、禁用或迁移 Netplan。Ubuntu networkd 管理仅适用于 operator 已在
DebianForm 之外准备为持久原生 systemd-networkd 的目标。检测到 active Netplan ownership 时，
plan/apply 会在 provider 变更前失败。NetworkManager 管理同样不在范围内。
