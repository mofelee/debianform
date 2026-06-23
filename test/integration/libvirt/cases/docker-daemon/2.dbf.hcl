host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/docker-daemon-state.json"
    lock_path = "/var/lock/debianform-integration/docker-daemon-state.lock"
  }

  docker {
    enable = true

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-file" = "3"
          "max-size" = "20m"
        }
      }
    }
  }
}
