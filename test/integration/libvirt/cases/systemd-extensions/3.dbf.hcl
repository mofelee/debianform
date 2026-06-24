host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/systemd-extensions-state.json"
    lock_path = "/var/lock/debianform-integration/systemd-extensions-state.lock"
  }

  packages {
    install = ["systemd-resolved"]
  }
}
