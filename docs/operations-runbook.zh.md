# DebianForm Operations Runbook

本文档覆盖 DebianForm 日常运维恢复流程：state lock、apply 中途失败、state 与远端不一致、
资源移除、恢复和常见故障排查。新用户第一次使用仍建议先走
[quickstart](quickstart.zh.md)；发布操作见 [release quick runbook](release-quick-runbook.zh.md)。

DebianForm 仍处于 public preview / beta 阶段。真实主机恢复前，先在低风险主机或测试窗口中
验证命令输出。

## 基本原则

- 先保留证据：保存 `dbf plan`、`dbf check` 输出和远端 state/lock 文件副本。
- 先读 plan，再决定修配置、恢复远端状态或 apply。
- 不直接手改 state，除非已经备份且确认 CLI 无法恢复。
- 同一台 host 同一时间只运行一个 `dbf apply`。
- 涉及删除、destroy、service stop、package remove 的 plan 必须人工确认。
- 包含 secret 的配置和日志不要粘贴到公开 issue；plan/state 会脱敏，但 shell 历史和外部命令日志仍需要自行保护。

常用变量示例：

```bash
export DBF_FILE=site.dbf.hcl
export DBF_HOST=server1
export DBF_TARGET=192.0.2.10
export DBF_STATE=/var/lib/debianform/state/server1.json
export DBF_LOCK=/var/lock/debianform/state/server1.lock
```

## 现场快照

遇到失败时先保存当前状态：

```bash
dbf validate -f "$DBF_FILE"
dbf plan -f "$DBF_FILE" --host "$DBF_HOST" > dbf-plan.txt 2> dbf-plan.err || true
dbf check -f "$DBF_FILE" --host "$DBF_HOST" > dbf-check.txt 2> dbf-check.err || true
ssh root@"$DBF_TARGET" "test -f '$DBF_STATE' && cp -a '$DBF_STATE' '$DBF_STATE.bak.$(date -u +%Y%m%dT%H%M%SZ)' || true"
ssh root@"$DBF_TARGET" "test -f '$DBF_LOCK' && cat '$DBF_LOCK' || true"
```

`check` 发现 drift 时会返回非零并输出：

```text
dbf: remote state does not match configuration
```

当前错误文本仍保留历史格式名；语义是远端状态和当前配置不一致。

这通常表示远端状态和配置不一致，或有尚未 apply 的变更。先读 `dbf-check.txt` 里的 plan。

## State 和 Lock 路径

默认每台 host 使用独立远端 state：

```text
/var/lib/debianform/state/<host>.json
```

默认 lock 文件、兼容锁目录和内部 guard：

```text
/var/lock/debianform/state/<host>.lock
/var/lock/debianform/state/<host>.lock.d/
/var/lock/debianform/state/<host>.lock.d/owner.v2
/var/lock/debianform/state/<host>.lock.guard
```

配置中可以覆盖：

```hcl
host "server1" {
  state {
    path      = "/var/lib/debianform/state/server1.json"
    lock_path = "/var/lock/debianform/state/server1.lock"
  }
}
```

state 使用 JSON schema version `2`。apply 每成功执行一个资源节点后立即写回 state，因此中途失败时，
state 只记录已经成功的节点。

## Stale Lock

现象：

```text
timed out waiting for state lock /var/lock/debianform/state/server1.lock
```

处理步骤：

1. 确认没有另一个 `dbf apply` 仍在运行：

   ```bash
   ps aux | grep '[d]bf apply'
   ssh root@"$DBF_TARGET" "cat '$DBF_LOCK' 2>/dev/null || true"
   ```

2. 如果只是等待时间不足，重新运行并加大超时。`--lock-timeout` 只控制等待，不会改变 holder
   默认 2 分钟 lease；运行中的 holder 会每 30 秒续租：

   ```bash
   dbf apply -f "$DBF_FILE" --host "$DBF_HOST" --lock-timeout 10m
   ```

3. 如果有效的 version 2 lease 已过期，DebianForm 会在 guard 下重新校验后自动接管，并在 stderr 输出：

   ```text
   taking over stale lock /var/lock/debianform/state/server1.lock
   ```

4. 无 `.d/owner.v2` marker 的 lock dir、legacy/未知 lease 不会自动接管，因为旧 holder 可能仍在
   创建目录后写 metadata。只有在确认没有任何 apply 进程仍在执行、且 lock 明显无法自动
   接管时，才手动移除 lock；不要同时删除 `.guard`，因为其他进程可能已经打开它等待：

   ```bash
   ssh root@"$DBF_TARGET" "cp -a '$DBF_LOCK' '$DBF_LOCK.manual-backup.$(date -u +%Y%m%dT%H%M%SZ)' 2>/dev/null || true"
   ssh root@"$DBF_TARGET" "rm -f '$DBF_LOCK' && rm -rf '$DBF_LOCK.d'"
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   ```

如果 unlock 时出现：

```text
state lock token mismatch for /var/lock/debianform/state/server1.lock
```

说明当前进程持有的 token 与远端 lock 文件不一致。不要直接重试并发 apply；先确认是否有另一个进程
已经接管 lock，再按上面的手动清理流程处理。

## Apply 中途失败

现象：

```text
host.server1.files.file["/etc/app/config.yaml"] failed: ...
```

处理步骤：

1. 保存失败输出和远端 state 备份：

   ```bash
   ssh root@"$DBF_TARGET" "test -f '$DBF_STATE' && cp -a '$DBF_STATE' '$DBF_STATE.failed.$(date -u +%Y%m%dT%H%M%SZ)' || true"
   ```

2. 运行在线 plan，看哪些资源仍需要变更：

   ```bash
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   ```

3. 修复 provider 报错对应的根因，例如目标路径权限、APT 源不可达、systemd unit 配置错误、
   Docker Compose `config` 校验失败或远端网络问题。

4. 再次 apply：

   ```bash
   dbf apply -f "$DBF_FILE" --host "$DBF_HOST"
   ```

5. apply 完成后确认 no-op 和 check：

   ```bash
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   dbf check -f "$DBF_FILE" --host "$DBF_HOST"
   ```

不要因为 apply 中途失败就删除整个 state。state 已记录成功节点，删除会让 DebianForm 失去 ownership
上下文，下一次 plan 可能出现不必要的 adopt/create/destroy。

## 使用 apply 调试器排查远端调用

当普通进度日志不足以判断失败发生在哪条远端命令、stdin payload 或 stdout/stderr 中时，可以临时使用
`apply --debug`：

```bash
dbf apply -f "$DBF_FILE" --host "$DBF_HOST" --debug --auto-approve
```

调试器从 facts discovery 开始拦截每一次 SSH 调用，并在 stderr 输出调用编号、host、phase、
address、action、summary、远端脚本或命令，以及 stdin/stdout/stderr 摘要。常用命令：

```text
step
next 5
continue
show
show stdin
show stdout
show stderr
retry
quit
```

失败后 prompt 会变成 `(dbfdbg failed)`。此时不要用 `step`、`next` 或 `continue`；先用
`show stderr`、`show stdout` 或 `show stdin` 看失败上下文，修复临时远端问题后可输入
`retry` 重跑同一个远端调用。确认无法继续时输入 `quit`；DebianForm 会取消普通调用，但仍会尽力执行
state unlock 这类 cleanup 调用。

`apply --debug` 是高风险排障模式。完整展开内容可能包含 secret、远端脚本、stdin payload、
stdout 或 stderr；长文本和二进制默认只显示摘要，只有显式 `show ...` 才展开。需要保留日志时，
把 stdout 和 stderr 分开保存，并按敏感日志处理：

```bash
dbf apply -f "$DBF_FILE" --host "$DBF_HOST" --debug --auto-approve \
  > dbf-apply.out 2> dbf-debug.err
```

调试模式会强制远端调用串行；`--debug --parallel 2` 会失败。排查多 host 问题时建议先加
`--host "$DBF_HOST"` 缩小范围。

## State 与远端不一致

现象：

```text
dbf: remote state does not match configuration
```

处理顺序：

1. 读 `check` 输出中的 plan，确定差异类型：

   ```bash
   dbf check -f "$DBF_FILE" --host "$DBF_HOST"
   ```

2. 如果远端被手动改坏，而配置是正确来源，直接 apply 修复：

   ```bash
   dbf apply -f "$DBF_FILE" --host "$DBF_HOST"
   ```

3. 如果远端改动是期望状态，把它写回 `.dbf.hcl`，先 validate，再 plan：

   ```bash
   dbf validate -f "$DBF_FILE"
   dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
   ```

4. 如果差异来自外部系统管理的资源，优先停止让两个工具同时管理同一文件、service 或 package。
   在从 DebianForm 配置中移除前，先运行 plan 确认移除动作是 forget 还是 destroy。

5. 如果怀疑 state 文件损坏，先备份并检查 JSON：

   ```bash
   ssh root@"$DBF_TARGET" "cp -a '$DBF_STATE' '$DBF_STATE.corrupt.$(date -u +%Y%m%dT%H%M%SZ)'"
   ssh root@"$DBF_TARGET" "python3 -m json.tool '$DBF_STATE' >/dev/null"
   ```

只有在 state JSON 无法解析且没有可用备份时，才考虑临时移走 state 后重新 plan；执行前要预期会丢失
ownership 信息：

```bash
ssh root@"$DBF_TARGET" "mv '$DBF_STATE' '$DBF_STATE.disabled.$(date -u +%Y%m%dT%H%M%SZ)'"
dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
```

## 资源移除和恢复

从配置移除资源和显式声明 absent 不是同一件事：

- 从配置移除：根据 state 中的 ownership 决定 destroy 或 forget。
- `ensure = "absent"`：请求删除远端对象。
- `lifecycle.prevent_destroy = true`：在 plan 阶段阻止 delete、destroy 和 replace。

移除资源前：

```bash
dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
```

如果 plan 中出现删除或 destroy，先确认它是期望动作。对高风险资源建议在配置中加保护：

```text
files {
  file "/etc/critical.conf" {
    content = file("./critical.conf")

    lifecycle {
      prevent_destroy = true
    }
  }
}
```

如果误删配置但尚未 apply：

1. 从 Git 恢复 `.dbf.hcl`。
2. 运行 `dbf plan`，确认 plan 回到 no-op 或只包含期望更新。

如果已经 apply 造成资源被删除：

1. 从 Git 或备份恢复配置。
2. 运行 `dbf apply` 重新创建资源。
3. 对 service、Docker Compose project、nftables 等有运行态的资源，再运行 `dbf check`。

如果只希望 DebianForm 停止管理某个已存在资源，不要直接删除远端对象。当前 DSL 对不同资源的
adopt/forget 能力并不完全相同，先用 `dbf plan` 确认从配置移除后的动作；若 plan 显示 destroy，
需要改用显式 lifecycle 保护或等待对应资源提供安全 forget 入口。

## 常见故障排查

### SSH 不可达

常见输出：

```text
ssh: connect ...
Permission denied (publickey)
```

处理：

```bash
ssh -vvv \
  -o BatchMode=yes \
  -o NumberOfPasswordPrompts=0 \
  -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no \
  root@"$DBF_TARGET" true
```

确认网络、端口、root SSH key、agent、`ssh.identity_file`、`ProxyCommand`/`ProxyJump`
和目标主机 `sshd_config`。如果跳板配置使用 `ProxyCommand ssh jump ...`，内层 ssh 也要加
`-o BatchMode=yes -o NumberOfPasswordPrompts=0 -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no`，
否则可能进入密码或 askpass fallback。
如果使用 1Password SSH agent 或需要桌面授权的 agent，且多 host apply 一次弹出多次授权请求，
先用 `dbf apply --parallel 1 ...` 验证；稳定后再逐步调高。推荐在 `~/.ssh/config` 的具体
Host 条目中配置 `IdentityFile` 和 `IdentitiesOnly yes`，减少 agent 为每台主机尝试过多 key。
DebianForm 当前不支持 sudo、become 或非 root 管理连接。

### 权限不足

常见输出：

```text
Permission denied
Read-only file system
Operation not permitted
```

处理：先确认 DebianForm 使用 root SSH 连接，目标路径所在文件系统可写，并且目标主机没有被维护窗口、
只读快照或外部配置管理锁住。DebianForm 不会通过 sudo 提权；如果普通 SSH 登录后仍需要 sudo，
当前版本不能管理这台主机。

### 非 root 管理连接

常见输出：

```text
ssh.user must be "root" or omitted
```

处理：把配置中的 `ssh.user` 删除或设为 `"root"`，并确认 root key 登录可用。

### 离线 plan 缺 runtime facts

常见输出：

```text
offline plan cannot resolve runtime facts
must declare platform.architecture
must declare platform.codename
```

处理：对真实主机运行在线 plan，或只在本地 fixture 中声明匹配的 `host.platform`：

```text
platform {
  distribution = "debian"
  version      = "13"
  architecture = "amd64"
  codename     = "trixie"
}
```

### 声明 facts 与远端不匹配

常见输出：

```text
declared platform.architecture "arm64" does not match detected architecture "amd64"
declared platform.codename "bookworm" does not match detected codename "trixie"
```

处理：如果管理真实主机，优先删除手写 platform facts，让在线探测填充；如果是离线 fixture，
把 `distribution`、`version`、`architecture` 和 `codename` 修正为目标环境的真实值。

### 目标系统 facts 探测失败

常见输出：

```text
discover host facts for server1: architecture is empty
discover host facts for server1: codename is empty
```

处理：确认目标主机 tuple 在当前支持范围内，并且 `dpkg --print-architecture` 和
`/etc/os-release` 可读：

```bash
ssh root@"$DBF_TARGET" 'dpkg --print-architecture; . /etc/os-release; printf "%s %s %s\n" "$ID" "$VERSION_ID" "$VERSION_CODENAME"'
```

如果输出为空，先修复目标系统基础环境。当前 allowlist 包含 Debian 12/13，以及 Ubuntu 24.04
和 26.04 的已列架构；其他 tuple 会在 provider observation 前被拒绝。

### Check 失败

常见输出：

```text
dbf: remote state does not match configuration
```

处理：这是 drift 或未 apply 变更，不是单纯语法错误。读 check 输出中的 plan，再选择 apply、
修配置或恢复远端状态。

### prevent_destroy 阻止删除

常见输出：

```text
lifecycle.prevent_destroy
```

处理：先确认 plan 里的 delete/destroy/replace 是否真的期望发生。若是误删配置，恢复配置；若确实要删除，
在同一个变更中移除 `prevent_destroy` 并再次 plan，确认删除动作后再 apply。

### Docker Compose config 校验失败

常见输出会来自 Docker Compose 本身。处理：

```bash
ssh root@"$DBF_TARGET" "cd /opt/app && docker compose -p app -f /opt/app/compose.yaml config"
```

先修复 Compose YAML、env file 或镜像引用，再重新 `dbf apply`。DebianForm 在 config 校验失败时不会启动
Compose project。

## 恢复完成标准

一次恢复结束前至少确认：

```bash
dbf validate -f "$DBF_FILE"
dbf plan -f "$DBF_FILE" --host "$DBF_HOST"
dbf check -f "$DBF_FILE" --host "$DBF_HOST"
```

期望结果：

- `validate` 成功。
- 在线 `plan` summary 中 create/update/delete/operations 都为 0，除非仍有明确待执行变更。
- `check` 返回 0。
- 没有遗留 state lock：

  ```bash
  ssh root@"$DBF_TARGET" "test ! -e '$DBF_LOCK' && test ! -e '$DBF_LOCK.d'"
  ```
