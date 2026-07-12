host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }
  state {
    path      = "/var/lib/debianform-integration/shared-script-networkd-state.json"
    lock_path = "/var/lock/debianform-integration/shared-script-networkd-state.lock"
  }
  platform {
    architecture = "amd64"
    codename     = "__DBF_DEBIAN_CODENAME__"
  }
  components = [component.wan_network, component.policy_route]
}
