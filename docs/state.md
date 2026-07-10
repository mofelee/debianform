# DebianForm State

DebianForm 为每台 host 保存独立的远端 JSON state：

```text
/var/lib/debianform/state/<host>.json
```

对应的租约文件、兼容锁目录和内部 guard 文件为：

```text
/var/lock/debianform/state/<host>.lock
/var/lock/debianform/state/<host>.lock.d/
/var/lock/debianform/state/<host>.lock.d/owner.v2
/var/lock/debianform/state/<host>.lock.guard
```

这些路径由默认 lock path 派生；通常不需要在配置中写 `state` block。需要自定义远端 state 或 lock
位置时，可以在 host 中覆盖：

```hcl
host "server1" {
  state {
    path      = "/var/lib/debianform/state/server1.json"
    lock_path = "/var/lock/debianform/state/server1.lock"
  }
}
```

## State Schema

state 顶层字段：

- `version`: 当前为 `2`。
- `host`: host label。
- `serial`: 每次成功写入时递增。
- `updated_at`: UTC RFC3339。
- `facts`: 运行时探测到的主机事实。DSL 中目标平台 facts 写作 `platform.architecture`
  和 `platform.codename`；state schema 仍以 `facts.system.*` 保存这些值。
  `facts.system.hostname` 是 observed value，不等同于配置中的 desired `system.hostname`。
  配置省略 `system.hostname` 时，DebianForm 不管理远端 hostname。
- `resources`: 以稳定 address 为 key 的资源记录。

资源记录保存：

- `host`；兼容的 version 2 记录可以省略，读取时会归一为顶层 `host`。
- resource kind、provider type 和仅供调试的 provider address。
- ownership。
- 经过脱敏的 desired 和 desired digest。
- 必要的 observed 摘要。
- lifecycle、更新时间和执行顺序。

secret content、sensitive component input 明文，以及由 sensitive input 派生的 files、
systemd unit、APT source/signing key 或 nftables content 不会写入 state。SSH 私钥、
命令日志和 lock 租约同样不会写入 state。敏感 content 只保存 `content_sha256`、
`content_bytes` 等摘要字段，用于 drift 比对和 no-op 判断。

## 读取校验

state 是 ownership 和远端操作目标的安全边界。不存在或内容为空的 state 文件会被视为当前 host 的
空 version 2 state；任何非空 state 必须同时满足：

- 顶层 `version` 必须恰好为 `2`。缺失、旧版本和未知较新版本都会被拒绝。
- 顶层 `host` 必须存在，并与当前请求的 host label 完全一致。
- 每条 resource 的 `host` 可以省略，也可以与顶层 `host` 一致；省略值会在内存中显式归一化，
  指向其他 host 的值会被拒绝。

这些检查在 JSON decode 后执行，engine 也会再次校验任意 backend 返回的 state。校验失败发生在
provider inspect、state write 和远端资源变更之前；CLI 不会用当前 schema 重写无法识别的 state。

当前没有旧 state schema 的自动迁移器。遇到旧版本时，应先备份原 state，并使用能读取该版本的
DebianForm 或经过审查的手工迁移流程；遇到较新版本时，应升级 DebianForm。不要通过删除或篡改
`version`、`host` 绕过检查。

## Ownership

- `managed`: DebianForm 创建或管理的资源；从配置移除后销毁。
- `adopted`: 远端原本已存在的资源；从配置移除后只清理 state。
- `external`: 只用于依赖或观测；不销毁远端对象。

显式 `ensure = "absent"` 与从配置移除不同。前者请求删除远端对象，后者根据
ownership 决定 destroy 或 forget。`lifecycle.prevent_destroy` 会在 plan 阶段阻止
delete 和 destroy。

## Locking

获取锁时，DebianForm 使用不会在正常 unlock 时删除的 `<lock_path>.guard` 和 `flock`
串行化 lease 的读取、续租、接管和删除。version 2 JSON lease 先完整写入同目录临时文件，再通过
`mv` 原子发布；其中包含 base64 编码的 owner、pid、随机 token、`lease_expires_at_unix` 和完整性校验值。
fresh acquisition 仍用排他的 `mkdir <lock_path>.d` 作为旧版/新版共同识别的原子桥，赢家随后写入
带 token 和 checksum 的 `.d/owner.v2` marker。获取 SSH 调用返回后还会同步续租并重新验证一次
lease 与 marker，确认远端 lease 仍归当前 token 才把 lock 返回给 engine。
每次成功续租也会刷新 marker 的 mtime，使 incomplete recovery 的宽限期从最近一次 holder 活动计算。

等待锁的 timeout 与 lease TTL 相互独立。默认 lease TTL 是 2 分钟，每 30 秒续租一次；因此长时间
apply 不会因为 `--lock-timeout` 到期而丢锁。兼容字段 `expires_at_unix` 固定为 `0`，防止不认识
version 2 协议的旧客户端把仍在续租的 lease 当成 stale lock。

- 同一 host 的第二个 apply 会等待并在 lock timeout 后失败。
- version 2 lock 过期后只能在 guard 临界区内重新校验并原子接管，同时向 stderr 输出 stale lock 提示。
- 新鲜的半写或损坏 version 2 lease 会保守等待；只有 `.d/owner.v2` 可验证的 incomplete lock
  才能在超过恢复宽限期后接管。
- 无 marker 的 lock dir、legacy 或未知格式 lease 不会自动接管，必须先确认没有 holder，再人工备份和清理。
- unlock 必须匹配 token；token 不匹配时不会删除 lock 文件或目录。
- lock 文件不参与 plan diff，也不写入 state。

state 使用临时文件写入并通过同目录 `mv` 原子替换。apply 每成功执行一个资源节点后
立即写回 state，因此中途失败时 state 只包含已经成功的节点。

state schema 兼容性、版本检测、自动迁移、备份和回滚边界见
[compatibility policy](compatibility-policy.zh.md)。
