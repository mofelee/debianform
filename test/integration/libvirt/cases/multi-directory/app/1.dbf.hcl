host "cihost" {
  imports = [profile.multi_directory_base]

  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/../id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/multi-directory-state.json"
    lock_path = "/var/lock/debianform-integration/multi-directory-state.lock"
  }

  files {
    file "/tmp/debianform-multi-directory.txt" {
      mode    = var.file_mode
      content = "${local.shared_prefix} env=${var.message} step=1\n"
    }
  }
}
