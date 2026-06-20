host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/v2-state.json"
    lock_path = "/var/lock/debianform-integration/v2-state.lock"
  }

  files {
    file "/tmp/debianform-v2-libvirt.txt" {
      mode    = "0600"
      content = "v2 libvirt updated\n"
    }
  }
}
