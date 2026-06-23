# DebianForm v2 Plan Format

本文档定义 v2 `dbf plan --format json` 的当前 alpha 格式。它也是终端树状 diff 和
`dbf plan --html plan.html` 静态 preview 的输入模型。

v2 plan format 的目标：

- 稳定表达用户层 v2 地址。
- 保留字段级结构化 diff。
- 支持文本内容行级 diff。
- 不泄露 sensitive 明文。
- 允许 debug 模式关联低阶 provider resource。
- 让 CI、审计系统和 HTML/TUI viewer 可以复用同一份数据。

## 顶层结构

```json
{
  "format_version": "debianform.plan.v2alpha1",
  "generated_at": "2026-06-20T12:00:00Z",
  "command": {
    "file": "examples/v2-fleet.dbf.hcl",
    "host": "server1"
  },
  "summary": {
    "create": 1,
    "update": 2,
    "delete": 0,
    "no_op": 0,
    "operations": 2
  },
  "changes": [],
  "operations": [],
  "diagnostics": []
}
```

字段要求：

- `format_version` 必须随破坏式格式变更更新。
- `generated_at` 使用 UTC RFC3339。
- `changes` 保存资源或领域对象变更。
- `operations` 保存由变更触发的动作节点，例如 APT cache refresh、nftables activate。
- `diagnostics` 保存 warning 或非 fatal 信息；fatal error 不应产生成功 plan。

## Action 枚举

```text
create
update
delete
destroy
no_op
replace
read
run
```

说明：

- `delete` 表示配置仍声明该对象，但期望 absent。
- `destroy` 表示对象已从配置移除，需要根据 ownership 销毁。
- `replace` 表示不能原地 update，必须 delete/create 或原子替换。
- `run` 用于 operation node。

## PlanChange

```json
{
  "address": "host.server1.nftables.file[\"20-services\"]",
  "action": "update",
  "summary": "update nftables snippet 20-services",
  "source": {
    "file": "examples/v2-fleet.dbf.hcl",
    "line": 560,
    "path": "host.server1.nftables.file[\"20-services\"]"
  },
  "provider_address": "nftables_file.server1_20_services",
  "diff": {},
  "low_level_actions": []
}
```

要求：

- `address` 使用用户可理解的稳定 v2 地址。
- `provider_address` 只在 `dbf plan --debug` 时输出；普通 JSON、终端和 HTML 输出省略。
- `source` 指向用户 DSL 的来源位置。
- `diff` 使用 `DiffNode`。
- `low_level_actions` 用于解释实际会执行的底层动作，但不替代 operation node。

## DiffNode

```json
{
  "path": ["content"],
  "kind": "text",
  "action": "update",
  "sensitive": false,
  "before": null,
  "after": null,
  "children": [],
  "hunks": []
}
```

`kind` 枚举：

```text
object
map
set
list
scalar
text
sensitive
```

规则：

- 根节点使用 `kind = "object"`，字段变化位于排序稳定的 `children` 中。
- object/map 按 key 递归 diff。
- set 按元素 identity diff。
- list 只有顺序有语义时才按 index diff。
- labeled block list 必须先归一化成 map。
- scalar diff 使用 `before` 和 `after`。
- text diff 使用 `hunks`。
- sensitive diff 不得输出明文。
- 由 sensitive component input 派生的 file 或 systemd unit content 也必须使用
  sensitive diff，只输出 sha256 和 bytes 摘要。
- terminal 和 HTML renderer 必须消费同一棵 `DiffNode`，不能各自重新计算差异。

## Text diff

```json
{
  "path": ["content"],
  "kind": "text",
  "action": "update",
  "hunks": [
    {
      "old_start": 8,
      "old_lines": 1,
      "new_start": 8,
      "new_lines": 1,
      "lines": [
        {
          "op": "delete",
          "text": "add rule inet filter input tcp dport { 22, 80 } accept"
        },
        {
          "op": "create",
          "text": "add rule inet filter input tcp dport { 22, 80, 443 } accept"
        }
      ]
    }
  ]
}
```

`op` 使用同一套 action 枚举中的 `create`、`delete`、`no_op`。

## Sensitive diff

```json
{
  "path": ["content"],
  "kind": "sensitive",
  "action": "update",
  "sensitive": true,
  "before_summary": {
    "sha256": "old-redacted-sha256",
    "bytes": 64
  },
  "after_summary": {
    "sha256": "new-redacted-sha256",
    "bytes": 64
  }
}
```

要求：

- `before_summary` / `after_summary` 优先来自资源的 `summary`，也可以来自
  `content_sha256` 和 `content_bytes`。
- JSON 和 text renderer 都不得包含 sensitive content 明文。

- 不允许出现 secret 明文。
- `sha256` 可以用于判断变化，但不能当成安全加密存储。
- 如果 hash 本身也可能暴露风险，provider 可以只输出 `changed: true` 和长度区间。

## OperationNode

```json
{
  "address": "host.server1.nftables.activate",
  "action": "run",
  "summary": "activate nftables ruleset",
  "depends_on": [
    "host.server1.nftables.validate"
  ],
  "triggered_by": [
    "host.server1.nftables.file[\"20-services\"]"
  ],
  "command_preview": "nft -f /etc/nftables.conf",
  "source": {
    "file": "examples/v2-fleet.dbf.hcl",
    "line": 560,
    "path": "host.server1.nftables.file[\"20-services\"]"
  }
}
```

OperationNode 只描述有语义的执行动作，不是任意 shell hook。典型地址：

```text
host.server1.apt.cache_refresh
host.server1.nftables.validate
host.server1.nftables.activate
host.server1.systemd.daemon_reload
host.server1.services.service["myapp"].restart
```

## Diagnostics

```json
{
  "severity": "warning",
  "message": "The WireGuard private key source is local; keep examples/secrets out of git.",
  "source": {
    "file": "examples/v2-fleet.dbf.hcl",
    "line": 410,
    "path": "host.server1.secrets.file[\"/etc/wireguard/private.key\"]"
  }
}
```

`severity` 第一版支持：

```text
info
warning
```

error 级 diagnostics 应让命令失败，而不是产生可 apply 的 plan。
