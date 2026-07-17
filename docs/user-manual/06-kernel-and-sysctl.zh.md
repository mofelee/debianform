<p align="right">
  <a href="06-kernel-and-sysctl.md">English</a> | <strong>简体中文</strong>
</p>

# 06. 管理内核模块、sysctl 和 BBR

本章演示如何加载并持久化内核模块、设置 sysctl，并用 BBR 作为真实例子。这个示例会修改测试主机的
运行时网络参数：

- `net.core.default_qdisc = fq`
- `net.ipv4.tcp_congestion_control = bbr`

请只在低风险测试主机上运行。本章示例已在 Debian 13 amd64 测试主机上验证通过。

## 创建工作目录

```bash
mkdir -p debianform-manual/06-kernel-and-sysctl
cd debianform-manual/06-kernel-and-sysctl
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/06-state.json"
    lock_path = "/var/lock/debianform/manual/06-state.lock"
  }

  kernel {
    modules = [
      "tcp_bbr",
    ]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires the tcp_bbr kernel module."
  }
}
```

`kernel.modules` 会加载模块并写入 `/etc/modules-load.d/dbf-tcp_bbr.conf`。

`kernel.sysctl` 默认会同时：

- 用 `sysctl -w` 应用运行时值。
- 写入 `/etc/sysctl.d/99-dbf-*.conf` 持久化配置。

这里的 `assert` 是一个配置自检：如果以后有人删掉 `tcp_bbr` 模块声明，`validate` 会失败。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

第一次在线 apply 可能显示 3 个 update，而不是 create：

```text
Summary: 0 create, 3 update, 0 delete, 0 no-op, 0 operations
```

这是正常的。sysctl key 本来就存在于内核中，DebianForm 要做的是把当前值改成 desired 值并写入持久化文件。

## 验证结果

运行：

```bash
ssh manual1 'awk "$1 == \"tcp_bbr\" { found = 1 } END { exit found ? 0 : 1 }" /proc/modules; sysctl -n net.core.default_qdisc; sysctl -n net.ipv4.tcp_congestion_control; cat /etc/modules-load.d/dbf-tcp_bbr.conf; cat /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf; cat /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf'
```

预期输出包含：

```text
fq
bbr
tcp_bbr
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
```

第一条 `awk` 没有输出；它只用退出码确认 `tcp_bbr` 已加载。

## 制造 sysctl 漂移

手动把 TCP 拥塞控制改回 `cubic`：

```bash
ssh manual1 'sysctl -w net.ipv4.tcp_congestion_control=cubic'
```

运行：

```bash
dbf check
```

预期失败：

```text
~ host.manual1.kernel.sysctl["net.ipv4.tcp_congestion_control"]
  sysctl -w net.ipv4.tcp_congestion_control=bbr and write /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf

Summary: 0 create, 1 update, 0 delete, 2 no-op, 0 operations
dbf: remote state does not match configuration
```

## 修复漂移

运行：

```bash
dbf apply --auto-approve
dbf check
```

确认值恢复：

```bash
ssh manual1 'sysctl -n net.ipv4.tcp_congestion_control'
```

预期：

```text
bbr
```

## 本章完整命令

```bash
mkdir -p debianform-manual/06-kernel-and-sysctl
cd debianform-manual/06-kernel-and-sysctl

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/06-state.json"
    lock_path = "/var/lock/debianform/manual/06-state.lock"
  }

  kernel {
    modules = [
      "tcp_bbr",
    ]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires the tcp_bbr kernel module."
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'awk "$1 == \"tcp_bbr\" { found = 1 } END { exit found ? 0 : 1 }" /proc/modules; sysctl -n net.core.default_qdisc; sysctl -n net.ipv4.tcp_congestion_control; cat /etc/modules-load.d/dbf-tcp_bbr.conf; cat /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf; cat /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf'

ssh manual1 'sysctl -w net.ipv4.tcp_congestion_control=cubic'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'sysctl -n net.ipv4.tcp_congestion_control'
```

## 清理或回滚

如果想把本章 state 和持久化文件清掉，并把运行时拥塞控制改回 `cubic`：

```bash
ssh manual1 'rm -f /var/lib/debianform/manual/06-state.json /var/lock/debianform/manual/06-state.lock /etc/modules-load.d/dbf-tcp_bbr.conf /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf; rm -rf /var/lock/debianform/manual/06-state.lock.d; sysctl -w net.core.default_qdisc=fq_codel >/dev/null || true; sysctl -w net.ipv4.tcp_congestion_control=cubic >/dev/null || true'
```

继续后续章节时不需要清理。
