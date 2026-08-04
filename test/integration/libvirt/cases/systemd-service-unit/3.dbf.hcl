host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/service-unit-state.json"
    lock_path = "/var/lock/debianform-integration/service-unit-state.lock"
  }

  component "service_unit_fixture" {
    source = component.service_unit_fixture

    inputs = {
      structured_extra = "from-updated-unit"
      service_enabled  = false
      service_state    = "stopped"
    }
  }
}
