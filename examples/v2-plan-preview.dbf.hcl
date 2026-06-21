# DebianForm v2 plan preview fixture for nftables text diffs and secret summaries.
#
# 设计边界：
# - 本文件用于固定结构化 plan、终端树状 diff 和 HTML preview 的展示目标。
# - 假设远端当前 nftables snippet 只开放 22/80，目标配置新增 443。
# - 假设远端 secret hash 与本地 source 不同，plan 只能显示摘要变化，不能显示明文。

host "preview1" {
  secrets {
    file "/etc/app/token" {
      source = "../internal/v2/testdata/fixtures/app-token.txt"
      owner  = "root"
      group  = "root"
      mode   = "0600"
    }
  }

  nftables {
    enable = true

    main {
      content = <<-EOF
        flush ruleset

        include "/etc/nftables.d/*.nft"
      EOF
    }

    file "20-services" {
      content = <<-EOF
        add rule inet filter input tcp dport { 22, 80, 443 } accept
      EOF
    }
  }
}

# 示例终端 plan：
#
# Plan:
#   ~ host.preview1.nftables.file["20-services"]
#     ~ content
#       - add rule inet filter input tcp dport { 22, 80 } accept
#       + add rule inet filter input tcp dport { 22, 80, 443 } accept
#
#   ~ host.preview1.secrets.file["/etc/app/token"]
#     ~ content: <sensitive sha256 changed, 32 bytes>
#
#   ! host.preview1.nftables.validate
#     triggered_by:
#       - host.preview1.nftables.file["20-services"]
#
#   ! host.preview1.nftables.activate
#     triggered_by:
#       - host.preview1.nftables.validate
