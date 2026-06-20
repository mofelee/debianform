# DebianForm v2 profile merge 设计示例。
#
# 本文件用于 Loop 1a 的 HostSpec golden：base profile 提供包和系统默认值，
# bbr profile 追加内核配置，host 再覆盖标量并追加主机包。

profile "base" {
  system {
    timezone = "UTC"
    locale   = "en_US.UTF-8"
  }

  packages {
    install = [
      "curl",
      "vim",
    ]
  }
}

profile "bbr" {
  imports = [profile.base]

  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  packages {
    install = ["htop"]
  }
}

host "merge1" {
  imports = [profile.bbr]

  ssh {
    host = "merge1"
  }

  system {
    hostname     = "merge1"
    architecture = "amd64"
    codename     = "trixie"
    timezone     = "Asia/Shanghai"
  }

  packages {
    install = ["sudo"]
  }
}
