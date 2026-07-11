host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/apt-source-state.json"
    lock_path = "/var/lock/debianform-integration/apt-source-state.lock"
  }

  apt {
    source_file "main" {
      path = "/etc/apt/sources.list.d/debian.sources"

      content = <<-EOF
        Types: deb
        URIs: https://mirrors.aliyun.com/debian/
        Suites: __DBF_DEBIAN_CODENAME__ __DBF_DEBIAN_CODENAME__-updates
        Components: main contrib non-free non-free-firmware

        Types: deb
        URIs: https://mirrors.aliyun.com/debian-security/
        Suites: __DBF_DEBIAN_CODENAME__-security
        Components: main contrib non-free non-free-firmware
      EOF

      on_destroy = "restore"
    }
  }
}
