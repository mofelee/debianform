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
- `StrictHostKeyChecking=accept-new`。
- `DBF_SSH_CONFIG` 可指定 ssh config 文件。
- 非 22 端口会加 `-p`。
- identity file 支持 `~` 展开。

DebianForm 把复杂连接细节交给 OpenSSH 和 ssh config，项目内只做必要覆盖。

## Backend 接口

`Backend` 定义 state 后端：

- `Read(ctx, host)`：读取 host state。
- `Write(ctx, host, st)`：写入 host state。
- `Lock(ctx, host, timeout)`：获取 host state lock。

当前主要实现是 `SSHBackend`。测试里还有 memory backend。

## SSHBackend.Read

`Read` 在远端检查 `host.State.Path` 是否存在：

- 不存在或空输出 -> `state.Empty(host.Name)`。
- 有内容 -> `state.Decode`。

读取后会 `Normalize`，确保 version、host、resources map 存在。

`Read` 会容忍 stdout 前有非 JSON 内容：它会找到第一个 `{` 后再 decode。这是对远端环境噪声的防御。

## SSHBackend.Write

`Write` 的流程：

1. `state.Encode(st)` 得到 JSON。
2. base64 编码 JSON，避免 shell heredoc 内容转义问题。
3. 远端创建 state 目录。
4. 写到临时文件 `state.path.tmp`。
5. `mv tmp state.path` 原子替换。

`state.Encode` 会更新时间和 serial，因此每次写入都会推进 state 序号。

## Lock

`SSHBackend.Lock` 使用两个远端路径：

- lock 文件：`host.State.LockPath`
- lock dir：`host.State.LockPath + ".d"`

获取逻辑：

- 循环尝试创建 lock dir。
- 成功后写 lock 文件，包含 owner、pid、token、expires_at、expires_at_unix。
- 如果已有 lock 过期，删除 lock dir 并接管。
- 超过 timeout 仍拿不到就失败。

`Unlock` 会检查 token 匹配后才删除 lock 文件和 lock dir。token mismatch 会报错，避免误删其他 apply 的 lock。

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
