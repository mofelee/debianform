host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/bird-wireguard-networkd-state.json"
    lock_path = "/var/lock/debianform-integration/bird-wireguard-networkd-state.lock"
  }

  platform {
    architecture = "amd64"
    codename     = "__DBF_TARGET_CODENAME__"
  }

  component "routing" {
    source = component.bird_networkd

    inputs = {
      base_ensure           = "absent"
      edge_ensure           = "absent"
      loop_reconfigure      = []
      core_reconfigure      = []
      edge_reconfigure      = []
      core_route_metric     = 200
      core_key_source       = "secrets/wg-core.key"
      edge_key_source       = "secrets/wg-edge.key"
      core_private_key_file = "/etc/wireguard/wg-core.key"
      edge_private_key_file = "/etc/wireguard/wg-edge.key"
    }
  }
}
