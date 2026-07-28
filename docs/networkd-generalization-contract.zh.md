<p align="right">
  <a href="networkd-generalization-contract.md">English</a> | <strong>简体中文</strong>
</p>

# systemd.networkd 通用化契约

状态：Preview `systemd.networkd` provider 的实现契约。

本文锁定 issue #72 的 DSL 与生命周期决策。该能力仍为 Preview；新增能力不会把 networkd
管理提升为 Stable，也不允许 DebianForm 从 Netplan 或 NetworkManager 接管 ownership。

## DSL

每个 `netdev` 和 `network` resource 可使用三种内容形式之一：

- 兼容属性（`netdev`、`wireguard`、`wireguard_peer`、`match`、`network`）；
- 通用 `section "<identity>"` block；
- `content` 或 `source` raw 属性，且必须二选一。

三种形式互斥。`source` 与 `files.file` 一样，相对声明它的 `.dbf.hcl` 文件解析。raw 内容
可显式设置 `sensitive = true`；引用 sensitive value 的 `content` 会自动标记。

```hcl
network "wg-peer" {
  section "match" {
    name = "Match"
    settings = {
      Name = "wg-peer"
    }
  }

  section "ipv4" {
    name = "Address"
    settings = {
      Address        = "10.2.0.0/31"
      AddPrefixRoute = false
    }
  }

  activation {
    reconfigure = ["wg-peer"]
    post_reload = script.reexport_bird
  }
}
```

block label 是 DebianForm 本地稳定 identity，不会渲染；`name` 才是实际 networkd section
名。通用 section 按声明顺序渲染；setting key 按字典序渲染，list value 按 list 顺序重复
key；null setting 会省略。兼容属性保留现有 section 与 key 顺序，因此 lower 到共享表示后
不会改变已有文件字节。

section 名与 setting key 必须是非空、单行 systemd identifier。scalar value 只能是 string、
number 或 bool，且不能含 NUL 或换行；bool 渲染为 `yes`/`no`。list 只能包含这些 scalar
类型。DebianForm 接受语法有效但未知的 section 名和 key。

present structured netdev 必须有一个 `[NetDev]` section，且 `Name`、`Kind` 非空。present
raw netdev 也检查这两个字段，但不会因此维护封闭的 networkd schema。inline `PrivateKey`
或 `PresharedKey` 只有在表达式携带 sensitive mark 时才允许；仍推荐对应的 file-backed
directive。networkd resource 没有 write-only content-version 契约，因此禁止 ephemeral setting
和 raw content。

## Activation 与 identity

`activation.reconfigure` 是 interface name 的有序 list。`activation.post_reload` 接受
`script.<name>` 或 `global.script.<name>`，引用解析和 declaration identity 规则与 component
`files.file.on_change` 相同，并在 validation 阶段解析。root script 必须使用 `mode = "once"`。

一次 apply 中，changed networkd resource 汇总为一条 host activation chain：

1. 写入或删除所有 changed managed file；
2. 配置要求时确保 `systemd-networkd.service` 正在运行；
3. 只执行一次 `networkctl reload`；
4. 删除已移除 netdev 的 runtime link；
5. 对每个受影响 interface 执行一次确定顺序的 `networkctl reconfigure <interface>`；
6. 每个受影响 post-reload script 执行一次。

interface name 会 union、去重并排序。post-reload declaration 按 declaration identity 去重，而
不是按命令文本去重；不同 declaration 仍是不同 operation。reconfigure 和 post-reload 都依赖
reload，post-reload 还依赖所有受影响的 reconfigure operation。没有 changed trigger 就不会
执行 activation。`check` 仍只观察；offline plan 仍会展示声明的 operation graph。

现有 `networkd_netdev`、`networkd_network` kind 和 resource address 不变。通用 section 与 raw
内容只是 content 细节，不成为独立 state resource。raw resource 仍保留 networkd ownership
preflight、lifecycle protection、desired hash、check/drift、reload aggregation 和 netdev runtime
cleanup。

## 迁移边界

把现有 native networkd resource 从兼容语法改成等价通用 section 属于原地 content update；
apply 前必须审查字节是否一致。把 `files.file` 改成 native networkd resource 不是自动 state
move：operator 必须备份 state 和网络文件、确认 path ownership 唯一、通过本地控制台或其他
恢复通道执行 plan，并保留 rollback 配置。DebianForm 不会推断 ownership，也不会执行未经
审查的 runtime interface 删除。
