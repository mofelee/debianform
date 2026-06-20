# DebianForm v2 State

DebianForm v2 为每台 host 保存独立的远端 JSON state：

```text
/var/lib/debianform/state/<host>.json
```

对应的租约文件和原子锁目录为：

```text
/var/lock/debianform/state/<host>.lock
/var/lock/debianform/state/<host>.lock.d/
```

用户可以在 host 中覆盖路径：

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
- `facts`: 运行时探测到的主机事实，例如 `system.architecture`、`system.codename`
  和远端 hostname。
- `resources`: 以稳定 v2 address 为 key 的资源记录。

资源记录保存：

- resource kind、provider type 和仅供调试的 provider address。
- ownership。
- 经过脱敏的 desired 和 desired digest。
- 必要的 observed 摘要。
- lifecycle、更新时间和执行顺序。

secret content、SSH 私钥、命令日志和 lock 租约不会写入 state。

## Ownership

- `managed`: DebianForm 创建或管理的资源；从配置移除后销毁。
- `adopted`: 远端原本已存在的资源；从配置移除后只清理 state。
- `external`: 只用于依赖或观测；不销毁远端对象。

显式 `ensure = "absent"` 与从配置移除不同。前者请求删除远端对象，后者根据
ownership 决定 destroy 或 forget。`lifecycle.prevent_destroy` 会在 plan 阶段阻止
delete 和 destroy。

## Locking

获取锁时，DebianForm 在远端原子创建 `<lock_path>.d`。成功后写入 JSON 租约文件，
包含 owner、pid、随机 token 和过期时间。

- 同一 host 的第二个 apply 会等待并在 lock timeout 后失败。
- lock 过期后可以接管，并向 stderr 输出 stale lock 提示。
- unlock 必须匹配 token；token 不匹配时不会删除 lock 文件或目录。
- lock 文件不参与 plan diff，也不写入 state。

state 使用临时文件写入并通过同目录 `mv` 原子替换。apply 每成功执行一个资源节点后
立即写回 state，因此中途失败时 state 只包含已经成功的节点。
