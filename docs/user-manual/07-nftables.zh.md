<p align="right">
  <a href="07-nftables.md">English</a> | <strong>简体中文</strong>
</p>

# 07. 管理 nftables 防火墙

本章演示如何安装并启用 nftables，管理 `/etc/nftables.conf` 和 snippet 文件，并在文件漂移后重新验证、
激活规则集。

本章示例已在 Debian 13 amd64 测试主机上验证通过。为了避免把自己锁在 SSH 外，示例使用
`policy accept`，并显式允许 TCP 22。

## 创建工作目录

```bash
mkdir -p debianform-manual/07-nftables
cd debianform-manual/07-nftables
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/07-state.json"
    lock_path = "/var/lock/debianform/manual/07-state.lock"
  }

  packages {
    install = ["nftables"]
  }

  nftables {
    enable = true

    main {
      content = <<-EOF
        flush ruleset
        include "/etc/nftables.d/debianform-manual-*.nft"
      EOF
    }

    file "10-input" {
      path = "/etc/nftables.d/debianform-manual-input.nft"

      content = <<-EOF
        table inet debianform_manual {
          chain input {
            type filter hook input priority 0; policy accept;

            ct state established,related accept
            iifname "lo" accept
            tcp dport 22 accept
            counter accept
          }
        }
      EOF
    }
  }
}
```

这份配置会：

- 安装 `nftables`。
- 写 `/etc/nftables.conf`。
- 写 `/etc/nftables.d/debianform-manual-input.nft`。
- 用 `nft -c -f /etc/nftables.conf` 校验规则。
- 用 `nft -f /etc/nftables.conf` 激活规则。
- 启用并启动 `nftables.service`。

## 应用配置

运行：

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

离线 plan 应该包含 4 个资源和 2 个 operation：

```text
Summary: 4 create, 0 update, 0 delete, 0 no-op, 2 operations
```

两个 operation 是：

```text
nft -c -f /etc/nftables.conf
nft -f /etc/nftables.conf
```

文件变化时会先 validate，再 activate。

## 验证结果

运行：

```bash
ssh manual1 'systemctl is-active nftables; systemctl is-enabled nftables; nft list ruleset | grep debianform_manual -A8'
```

预期包含：

```text
active
enabled
table inet debianform_manual {
  chain input {
    type filter hook input priority filter; policy accept;
    ct state established,related accept
    iifname "lo" accept
    tcp dport 22 accept
```

`nft list ruleset` 可能会把 `priority 0` 显示成 `priority filter`，这是 nftables 的显示格式。

## 制造规则文件漂移

把 snippet 中的 SSH 端口从 22 改成 2222：

```bash
ssh manual1 'sed -i "s/tcp dport 22 accept/tcp dport 2222 accept/" /etc/nftables.d/debianform-manual-input.nft'
```

运行：

```bash
dbf check
```

预期失败：

```text
~ host.manual1.nftables.file["10-input"]
  update file /etc/nftables.d/debianform-manual-input.nft

Summary: 0 create, 1 update, 0 delete, 3 no-op, 2 operations
dbf: remote state does not match configuration
```

注意 summary 里仍然有 2 个 operations。nftables 文件变化后，DebianForm 会重新校验并激活规则集。

## 修复漂移

运行：

```bash
dbf apply --auto-approve
dbf check
```

确认文件和 live ruleset 都恢复 TCP 22：

```bash
ssh manual1 'grep -F "tcp dport 22 accept" /etc/nftables.d/debianform-manual-input.nft; nft list ruleset | grep -F "tcp dport 22 accept"'
```

预期两行都存在。

## 本章完整命令

```bash
mkdir -p debianform-manual/07-nftables
cd debianform-manual/07-nftables

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/07-state.json"
    lock_path = "/var/lock/debianform/manual/07-state.lock"
  }

  packages {
    install = ["nftables"]
  }

  nftables {
    enable = true

    main {
      content = <<-EOT
        flush ruleset
        include "/etc/nftables.d/debianform-manual-*.nft"
      EOT
    }

    file "10-input" {
      path = "/etc/nftables.d/debianform-manual-input.nft"

      content = <<-EOT
        table inet debianform_manual {
          chain input {
            type filter hook input priority 0; policy accept;

            ct state established,related accept
            iifname "lo" accept
            tcp dport 22 accept
            counter accept
          }
        }
      EOT
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'systemctl is-active nftables; systemctl is-enabled nftables; nft list ruleset | grep debianform_manual -A8'

ssh manual1 'sed -i "s/tcp dport 22 accept/tcp dport 2222 accept/" /etc/nftables.d/debianform-manual-input.nft'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F "tcp dport 22 accept" /etc/nftables.d/debianform-manual-input.nft; nft list ruleset | grep -F "tcp dport 22 accept"'
```

## 清理

如果要停用本章配置：

```bash
ssh manual1 'systemctl disable --now nftables 2>/dev/null || true; rm -f /etc/nftables.conf /etc/nftables.d/debianform-manual-input.nft /var/lib/debianform/manual/07-state.json /var/lock/debianform/manual/07-state.lock; rm -rf /var/lock/debianform/manual/07-state.lock.d; nft flush ruleset 2>/dev/null || true'
```

继续后续章节时不需要清理。
