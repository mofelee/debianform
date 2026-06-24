# 12. 日常运维：plan 审阅、漂移处理、锁、state 和故障恢复

本章把前面章节反复用到的操作整理成日常 runbook：如何审阅 plan，如何生成 JSON/HTML plan，
如何用 `check` 检测漂移，如何看 state，如何处理 state lock。

本章示例已在 Debian 13 amd64 测试主机上验证通过。示例只管理一个小文件：
`/etc/debianform-manual/operations.txt`。

## 创建工作目录

```bash
mkdir -p debianform-manual/12-operations
cd debianform-manual/12-operations
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/12-state.json"
    lock_path = "/var/lock/debianform/manual/12-state.lock"
  }

  files {
    file "/etc/debianform-manual/operations.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "operations baseline\n"
    }
  }
}
```

## 审阅文本 plan

运行：

```bash
dbf validate
dbf plan --offline
dbf plan
```

常用判断方式：

- `--offline` 不连接主机，适合快速看资源地址、desired 内容和大致变更。
- 在线 `plan` 会连接主机、读取 state 和 observed 状态，适合 apply 前审阅真实动作。
- 在线 `plan`、`apply` 和 `check` 会把探测/执行进度写到 stderr；plan 正文仍在 stdout。
- `+` 是 create，`~` 是 update，`-` 是 delete，`!` 是 operation。

首次离线 plan 应该显示：

```text
Summary: 1 create, 0 update, 0 delete, 0 no-op, 0 operations
```

## 生成 JSON plan

运行：

```bash
dbf plan --offline --format json > plan.json
python3 - <<'PY'
import json

with open("plan.json", encoding="utf-8") as f:
    doc = json.load(f)

assert doc["format_version"] == "debianform.plan.alpha1", doc["format_version"]
assert doc["summary"]["create"] == 1, doc["summary"]
assert doc["changes"][0]["address"] == 'host.manual1.files.file["/etc/debianform-manual/operations.txt"]'
print("json plan ok")
PY
```

JSON plan 适合 CI、审计和自定义检查。格式细节见 `docs/plan-format.md`。

## 生成 HTML plan

运行：

```bash
dbf plan --offline --html plan.html
test -s plan.html
```

预期输出：

```text
wrote HTML plan to plan.html
```

HTML plan 适合在变更评审里作为附件打开。`--html` 只支持 `dbf plan`，不能和显式 `--format` 同时使用。

## 执行 apply

运行：

```bash
dbf apply --auto-approve
dbf check
```

`apply` 的输出通常会出现两段 plan：

- 第一段是确认前的在线 plan。
- 持有 state lock 后，DebianForm 会重新读取 state 和 observed 状态，再打印第二段 plan 并执行。

执行阶段 stderr 会显示当前 host、资源地址、动作以及长步骤心跳，例如 `start update ...`、
`still update ...`、`done update ...`，用来判断它正在处理哪一步。

成功后：

```text
apply complete
```

`check` 应该回到 no changes：

```text
Summary: 0 create, 0 update, 0 delete, 1 no-op, 0 operations
```

## 检查远端 state

运行：

```bash
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/12-state.json", encoding="utf-8") as f:
    st = json.load(f)

assert st["version"] == 2, st
assert st["host"] == "manual1", st
assert st["serial"] >= 1, st

key = "host.manual1.files.file[\"/etc/debianform-manual/operations.txt\"]"
res = st["resources"][key]
assert res["kind"] == "file", res
assert res["ownership"] == "managed", res
assert "content" not in res.get("desired", {}), res
print("state ok")
PY'
```

state 是 DebianForm 的进度和管辖记录，不是完整配置副本。文件内容不会以明文写入 state；
state 里保存的是 `content_sha256` 和 `content_bytes` 等摘要。

## 漂移检测

手动修改远端文件：

```bash
ssh manual1 'printf "manual drift\n" > /etc/debianform-manual/operations.txt'
```

运行：

```bash
dbf check
```

预期失败，返回非零，并显示远端 sha 和期望摘要不同：

```text
~ host.manual1.files.file["/etc/debianform-manual/operations.txt"]
  update file /etc/debianform-manual/operations.txt

Summary: 0 create, 1 update, 0 delete, 0 no-op, 0 operations
dbf: remote state does not match configuration
```

`check` 只检测，不修改远端。

## 漂移修复

运行：

```bash
dbf apply --auto-approve
dbf check
ssh manual1 'cat /etc/debianform-manual/operations.txt'
```

预期：

```text
operations baseline
```

## state lock

`apply` 会在每台目标 host 上获取 state lock。默认路径来自 host 的 `state.lock_path`：

```text
/var/lock/debianform/manual/12-state.lock
/var/lock/debianform/manual/12-state.lock.d/
```

lock 文件是一个租约 JSON，包含 owner、pid、token 和过期时间。运行中的 apply 结束后会删除 lock。
如果没有变更，`apply` 会在打印 no-op plan 后退出，不进入持锁执行阶段。

模拟一个未过期锁：

```bash
ssh manual1 'mkdir -p /var/lock/debianform/manual/12-state.lock.d; exp=$(( $(date +%s) + 600 )); printf "{\"owner\":\"manual-lock-demo\",\"pid\":\"manual\",\"token\":\"manual\",\"expires_at\":\"manual\",\"expires_at_unix\":%s}\n" "$exp" > /var/lock/debianform/manual/12-state.lock'
ssh manual1 'printf "lock demo drift\n" > /etc/debianform-manual/operations.txt'
dbf apply --auto-approve --lock-timeout 2s
```

预期失败：

```text
dbf: ssh manual1 failed: exit status 1: timed out waiting for state lock /var/lock/debianform/manual/12-state.lock
```

先检查 lock 内容：

```bash
ssh manual1 'cat /var/lock/debianform/manual/12-state.lock'
```

只有确认没有其他 `dbf apply` 仍在运行时，才手动清理 lock：

```bash
ssh manual1 'rm -f /var/lock/debianform/manual/12-state.lock; rm -rf /var/lock/debianform/manual/12-state.lock.d'
dbf apply --auto-approve
dbf check
```

如果 lock 已过期，DebianForm 会接管 stale lock 并在 stderr 打印提示。

## 常见故障处理

`check` 失败：
先读 plan，看是文件内容、权限、服务状态、包状态还是 operation 触发。确认预期后用 `apply` 修复。

`apply` 中断：
不要先删 state。重新运行 `dbf plan` 看剩余变更。DebianForm 每成功执行一个资源会写一次 state，
下一次 apply 会从当前实际状态继续。

远端 state 看起来不对：
先备份 state 文件，再检查资源地址是否和当前 plan 一致。资源地址变更会让旧 state 资源变成 orphan。

锁超时：
确认没有其他 apply 仍在运行，再清理 `lock_path` 和 `lock_path.d/`。不要在不确认进程状态时直接删除锁。

离线 plan 报 runtime facts：
给 host 显式声明需要的 `system.architecture`、`system.codename` 等 facts，或者运行在线 `dbf plan`。

## 本章完整命令

```bash
mkdir -p debianform-manual/12-operations
cd debianform-manual/12-operations

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/12-state.json"
    lock_path = "/var/lock/debianform/manual/12-state.lock"
  }

  files {
    file "/etc/debianform-manual/operations.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "operations baseline\n"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf plan --offline --format json > plan.json
python3 - <<'PY'
import json

with open("plan.json", encoding="utf-8") as f:
    doc = json.load(f)

assert doc["format_version"] == "debianform.plan.alpha1", doc["format_version"]
assert doc["summary"]["create"] == 1, doc["summary"]
assert doc["changes"][0]["address"] == 'host.manual1.files.file["/etc/debianform-manual/operations.txt"]'
print("json plan ok")
PY
dbf plan --offline --html plan.html
test -s plan.html

dbf apply --auto-approve
dbf check
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/12-state.json", encoding="utf-8") as f:
    st = json.load(f)

assert st["version"] == 2, st
assert st["host"] == "manual1", st
assert st["serial"] >= 1, st

key = "host.manual1.files.file[\"/etc/debianform-manual/operations.txt\"]"
res = st["resources"][key]
assert res["kind"] == "file", res
assert res["ownership"] == "managed", res
assert "content" not in res.get("desired", {}), res
print("state ok")
PY'

ssh manual1 'printf "manual drift\n" > /etc/debianform-manual/operations.txt'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'cat /etc/debianform-manual/operations.txt'

ssh manual1 'mkdir -p /var/lock/debianform/manual/12-state.lock.d; exp=$(( $(date +%s) + 600 )); printf "{\"owner\":\"manual-lock-demo\",\"pid\":\"manual\",\"token\":\"manual\",\"expires_at\":\"manual\",\"expires_at_unix\":%s}\n" "$exp" > /var/lock/debianform/manual/12-state.lock'
ssh manual1 'printf "lock demo drift\n" > /etc/debianform-manual/operations.txt'
dbf apply --auto-approve --lock-timeout 2s || true
ssh manual1 'cat /var/lock/debianform/manual/12-state.lock; rm -f /var/lock/debianform/manual/12-state.lock; rm -rf /var/lock/debianform/manual/12-state.lock.d'
dbf apply --auto-approve
dbf check
```

## 清理

如果要清理本章创建的远端文件、state、lock 和本地 plan 文件：

```bash
rm -f plan.json plan.html
ssh manual1 'rm -f /etc/debianform-manual/operations.txt /var/lib/debianform/manual/12-state.json /var/lock/debianform/manual/12-state.lock; rm -rf /var/lock/debianform/manual/12-state.lock.d'
```
