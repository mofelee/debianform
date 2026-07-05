host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/script-on-change-state.json"
    lock_path = "/var/lock/debianform-integration/script-on-change-state.lock"
  }
}
