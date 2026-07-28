moved {
  from = component.bird2_babel
  to   = component.bird2_ospfv3
}

host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/component-moved-state.json"
    lock_path = "/var/lock/debianform-integration/component-moved-state.lock"
  }

  component "bird2_ospfv3" {
    source = component.bird_stack
  }
}
