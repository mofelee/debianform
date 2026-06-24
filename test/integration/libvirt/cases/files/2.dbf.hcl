host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/core-state.json"
    lock_path = "/var/lock/debianform-integration/core-state.lock"
  }

  files {
    file "/tmp/debianform-core-libvirt.txt" {
      mode    = "0600"
      content = "core libvirt updated\n"
    }
  }
}
