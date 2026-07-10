# 09. Backend、SSH 与远端 State

本章解释 backend、SSH runner 和 state 的边界。它们负责“如何访问主机”和“如何保存 DebianForm 的管理记录”，
不负责解析配置，也不负责决定资源动作。

## 数据流

```text
ir.Program
  -> HostsFromProgram
  -> SSHRunner
  -> SSHBackend.Read/Write/Lock
  -> state.Decode/Encode
```

在线 plan 只读 state；apply 会 lock、读写 state。

## Runner 接口

`Runner` 是 provider 和 backend 与远端命令交互的底层接口：

- `Run(ctx, host, script)`：通过 `ssh <host> sh -s` 执行脚本。
- `RunInput(ctx, host, remoteCommand, input)`：执行远端命令并把 input 写到 stdin。
- `RunCommand(ctx, host, remoteCommand)`：执行一条远端命令。

`SSHRunner` 是当前实现。测试中也可以替换成 memory/fake runner。

## SSH 参数解析

`HostsFromProgram` 从 `ir.Program` 中提取每台 host 的：

- name
- SSH host/address
- port
- user
- identity file

`SSHRunner.SSHArgs` 生成 ssh 参数：

- 默认 user 是 root。
- `BatchMode=yes`，避免交互。
- `NumberOfPasswordPrompts=0`、`PasswordAuthentication=no` 和
  `KbdInteractiveAuthentication=no`，避免密码和 askpass fallback。
- `StrictHostKeyChecking=accept-new`。
- `DBF_SSH_CONFIG` 可指定 ssh config 文件。
- 非 22 端口会加 `-p`。
- identity file 支持 `~` 展开。

DebianForm 把复杂连接细节交给 OpenSSH 和 ssh config，项目内只做必要覆盖。
如果 SSH config 使用 `ProxyCommand ssh ...` 手写内层 ssh，建议在内层命令也显式加
`-o BatchMode=yes -o NumberOfPasswordPrompts=0 -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no`，
或优先使用 `ProxyJump`。

## Backend 接口

`Backend` 定义 state 后端：

- `Read(ctx, host)`：读取 host state。
- `Write(ctx, host, st)`：写入 host state。
- `Lock(ctx, host, timeout)`：获取 host state lock。

当前主要实现是 `SSHBackend`。测试里还有 memory backend。

backend 的 `Read` 返回值不是信任边界。engine 在 plan、check、apply 和 host facts 持久化路径中，
都会按请求的 host 再次执行 state 校验，防止其他 backend 把 incompatible 或 foreign state 带入
provider planning。

## SSHBackend.Read

`Read` 在远端检查 `host.State.Path` 是否存在：

- 不存在或空输出 -> `state.Empty(host.Name)`。
- 有内容 -> `state.Decode`。

`state.Decode` 会严格调用 `Normalize`：只接受当前 version 2，要求顶层 `host` 与请求 host 一致，
并检查每条 resource 的 `host`。兼容的 version 2 resource 可以省略 `host`，此时会显式补成顶层
host；缺失/旧/未知较新 version、缺失/foreign 顶层 host 和 foreign resource host 都会报错。
当前没有旧 schema 自动迁移器，校验失败的 state 不会被写回。

`Read` 会容忍 stdout 前有非 JSON 内容：它会找到第一个 `{` 后再 decode。这是对远端环境噪声的防御。

## SSHBackend.Write

`Write` 的流程：

1. 用请求的 `host.Name` 校验并归一化 state；MemoryBackend 也遵循同一写入边界。
2. `state.Encode(st)` 得到 JSON。
3. base64 编码 JSON，避免 shell heredoc 内容转义问题。
4. 远端创建 state 目录。
5. 写到临时文件 `state.path.tmp`。
6. `mv tmp state.path` 原子替换。

`state.Encode` 会更新时间和 serial，因此每次写入都会推进 state 序号。

## Lock

`SSHBackend.Lock` 使用三个远端路径：

- lock 文件：`host.State.LockPath`
- lock dir：`host.State.LockPath + ".d"`
- ownership marker：`host.State.LockPath + ".d/owner.v2"`
- guard 文件：`host.State.LockPath + ".guard"`

获取逻辑：

- 用非阻塞 `flock` 获取稳定 guard，并在每次尝试前后检查等待 deadline。
- version 2 JSON lease 包含 owner、pid、token、`lease_expires_at_unix`、兼容值
  `expires_at_unix: 0` 和 checksum，写完临时文件后通过 `mv` 原子发布。
- fresh acquisition 必须排他创建 lock dir；失败就释放 guard 并重读，不能把别人的目录当成成功。
- 赢得 lock dir 后写入 token/checksum marker。只有 marker 可验证的 incomplete v2 lock 才能在 grace 后恢复；
  无 marker 的目录可能属于仍在写 metadata 的旧客户端，永不自动接管。
- 等待 timeout 与 lease TTL 分离；默认 lease 为 2 分钟，每 30 秒续租。
- stale takeover 在 guard 内重新读取并验证完整记录，然后原子替换，不能删除另一个 contender 的新 lease。
- fresh 的半写/损坏 v2 记录先等待恢复宽限期；legacy 或未知记录只允许人工确认后清理。
- 超过等待 timeout 仍拿不到就失败。
- acquisition 远端脚本返回后同步执行一次有 deadline 的 renew/token 校验，再向调用方返回 lock。

`Unlock` 会在同一个 guard 下验证完整记录和 token，匹配后才删除 lock 文件和 lock dir。guard 文件
保持不变，避免等待者锁住不同 inode。token mismatch 会报错，避免误删其他 apply 的 lock。
Engine 调用 unlock 时会为每个 host 单独创建短 deadline，并通过 `context.WithoutCancel` 避免继承
caller 已触发的取消或 deadline；所有 host 都会尝试 cleanup，错误带 host 信息汇总后返回。

## State 格式

`state.State` 字段：

- `Version`
- `Host`
- `Serial`
- `UpdatedAt`
- `Facts`
- `Resources`

`Resources` 的 key 是 graph address。

## Resource state

`state.Resource` 字段：

- `Host`
- `Kind`
- `ProviderType`
- `ProviderAddress`
- `Ownership`
- `Lifecycle`
- `Desired`
- `DesiredDigest`
- `Observed`
- `UpdatedAt`
- `Order`

state 中的 desired 是 sanitize 后的 desired；digest 是根据 sanitize 规则处理后的 desired digest。

## Digest 和 sanitize

`DesiredDigest` 调用 `Digest(SanitizeDesired(desired))`。`SanitizeDesired` 会：

- 如果有 `content` 字符串，删除明文 content，换成 `content_sha256` 和 `content_bytes`。
- 如果资源标记 `sensitive`，删除 `source_path` 和 `summary`。

`SanitizeObserved` 当前复用同样规则。

这个设计让 state 能判断内容是否变化，同时不保存文件内容和敏感源路径。

## Ownership

ownership 影响 orphan 处理：

- `managed`：DebianForm 创建或管理的资源，配置移除后默认 destroy。
- `adopted`：主机上已有且与 desired 一致，apply 只写入 state；配置移除后默认 forget。

provider/engine 在 plan 和 apply 中共同维护 ownership。

## 设计边界

- backend 只负责 state 存取和 lock，不负责 resource 语义。
- runner 只负责执行命令，不解释 plan/action。
- state 是 DebianForm 的记录，不是主机现实的完整快照。
- state 文件不能保存明文 secret 或 write-only content。

## 修改检查清单

- 改 state schema：同步 `docs/state.md`、state tests、兼容读取逻辑。
- 改 digest 规则：补 drift、content、sensitive、write-only 相关测试。
- 改 SSH 参数：补 `SSHArgs` 测试，并确认 ssh config 默认行为。
- 改 lock：补 stale lock、timeout、token mismatch 测试。
- 新增 backend：实现 Read/Write/Lock，并跑 engine apply/plan 测试。
