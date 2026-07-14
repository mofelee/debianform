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
      path = "/etc/apt/sources.list.d/debianform-integration.sources"

      content = <<-EOF
        Types: deb
        URIs: __DBF_TARGET_APT_MIRROR__
        Suites: __DBF_TARGET_CODENAME__ __DBF_TARGET_CODENAME__-updates
        Components: __DBF_TARGET_APT_COMPONENTS__

        Types: deb
        URIs: __DBF_TARGET_APT_SECURITY_MIRROR__
        Suites: __DBF_TARGET_CODENAME__-security
        Components: __DBF_TARGET_APT_COMPONENTS__
      EOF

      on_destroy = "keep"
    }
  }
}
