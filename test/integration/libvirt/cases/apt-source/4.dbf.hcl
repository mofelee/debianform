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
}
