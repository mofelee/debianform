host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/component-inputs-state.json"
    lock_path = "/var/lock/debianform-integration/component-inputs-state.lock"
  }

  system {
    architecture = "amd64"
    codename     = "trixie"
  }
}
