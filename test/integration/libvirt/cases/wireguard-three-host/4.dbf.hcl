host "wg-a" {
  ssh {
    host          = "__DBF_WG_A_SSH_HOST__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/wireguard-three-a-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-three-a-state.lock"
  }
}

host "wg-b" {
  ssh {
    host          = "__DBF_WG_B_SSH_HOST__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/wireguard-three-b-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-three-b-state.lock"
  }
}

host "wg-c" {
  ssh {
    host          = "__DBF_WG_C_SSH_HOST__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/wireguard-three-c-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-three-c-state.lock"
  }
}
