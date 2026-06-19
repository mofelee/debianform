host "debian_ci" {
  address       = "__DBF_VM_IP__"
  identity_file = "${path.module}/id_ed25519"
}

state "ssh" {
  host      = "debian_ci"
  path      = "/var/lib/debianform-integration/state.json"
  lock_path = "/var/lock/debianform-integration/state.lock"
}

# Hostname destroy cannot infer the original value, so restore the known blank-VM
# hostname declaratively before removing the resource in the final step.
debian_hostname "main" {
  host     = "debian_ci"
  hostname = "debianform-ci"
}
