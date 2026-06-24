# DebianForm BBR 设计示例。
#
# 可通过当前 CLI validate/plan。
#
# 设计边界：
# - kernel.modules 和 kernel.sysctl 保持领域结构。
# - 编译器自动推导 sysctl 依赖 tcp_bbr module。
# - plan 使用用户层地址展示字段级变化。

host "bbr1" {
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

# 预期资源图：
#
# host.bbr1.kernel.module["tcp_bbr"]
#   -> host.bbr1.kernel.sysctl["net.core.default_qdisc"]
#   -> host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]
