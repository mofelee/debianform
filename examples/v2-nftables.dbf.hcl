# DebianForm v2 nftables 设计示例。
#
# 当前 v1 执行器还不能 apply 此语法。可执行的 v1 原生 nftables 示例见
# system-native.dbf.hcl。
#
# 设计边界：
# - v2 的主路径是 nftables 原生配置，不是通用 firewall 抽象。
# - DebianForm 管理 ruleset 文件、snippet 文件、校验和激活。
# - 多个 nftables 文件变化时，同一 host 只校验和激活一次主 ruleset。
# - plan 应展示 nft 文件的行级 diff；HTML preview 可折叠大段上下文。

host "edge1" {
  ssh {
    host = "edge1"
  }

  system {
    hostname     = "edge1"
    architecture = "amd64"
    codename     = "trixie"
  }

  packages {
    install = [
      "nftables",
    ]
  }

  nftables {
    enable = true

    main {
      path     = "/etc/nftables.conf"
      validate = true
      activate = true

      content = <<-EOF
        flush ruleset

        include "/etc/nftables.d/*.nft"
      EOF
    }

    file "10-base" {
      path = "/etc/nftables.d/10-base.nft"

      content = <<-EOF
        table inet filter {
          chain input {
            type filter hook input priority 0; policy drop;

            ct state established,related accept
            iifname "lo" accept

            counter drop
          }

          chain forward {
            type filter hook forward priority 0; policy drop;
          }

          chain output {
            type filter hook output priority 0; policy accept;
          }
        }
      EOF
    }

    file "20-services" {
      path = "/etc/nftables.d/20-services.nft"

      content = <<-EOF
        add rule inet filter input tcp dport { 22, 80, 443 } accept
      EOF
    }

    file "30-wireguard" {
      path = "/etc/nftables.d/30-wireguard.nft"

      content = <<-EOF
        add rule inet filter input udp dport 51820 accept
      EOF
    }
  }
}

# 预期资源图：
#
# host.edge1.packages.install["nftables"]
#   -> host.edge1.nftables.file["main"]
#   -> host.edge1.nftables.file["10-base"]
#   -> host.edge1.nftables.file["20-services"]
#   -> host.edge1.nftables.file["30-wireguard"]
#   -> host.edge1.nftables.validate
#   -> host.edge1.nftables.activate
#
# 示例 plan 片段：
#
#   ~ host.edge1.nftables.file["20-services"]
#     ~ content
#       - add rule inet filter input tcp dport { 22, 80 } accept
#       + add rule inet filter input tcp dport { 22, 80, 443 } accept
#
#     validates: nft -c -f /etc/nftables.conf
#     activates: nft -f /etc/nftables.conf
