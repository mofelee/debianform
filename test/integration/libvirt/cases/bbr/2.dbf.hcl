host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/bbr-state.json"
    lock_path = "/var/lock/debianform-integration/bbr-state.lock"
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
