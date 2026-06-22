component "wireguard_wgquick" {
  input "config_source" {
    type = string
  }

  input "service_enabled" {
    type    = bool
    default = false
  }

  input "service_state" {
    type    = string
    default = "stopped"
  }

  packages {
    install = [
      "wireguard-tools",
      "iproute2",
      "iputils-ping",
    ]
  }

  directories {
    directory "/etc/wireguard" {
      owner = "root"
      group = "root"
      mode  = "0700"
    }
  }

  secrets {
    file "/etc/wireguard/wg0.conf" {
      source = input.config_source
      owner  = "root"
      group  = "root"
      mode   = "0600"
    }
  }

  services {
    service "wg-quick@wg0" {
      package = "wireguard-tools"
      enabled = input.service_enabled
      state   = input.service_state
    }
  }
}

host "wg-a" {
  ssh {
    host          = "__DBF_WG_A_SSH_HOST__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/wireguard-a-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-a-state.lock"
  }

  component "wireguard" {
    source = component.wireguard_wgquick

    inputs = {
      config_source = "secrets/wg-a.conf"
    }
  }
}

host "wg-b" {
  ssh {
    host          = "__DBF_WG_B_SSH_HOST__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/wireguard-b-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-b-state.lock"
  }

  component "wireguard" {
    source = component.wireguard_wgquick

    inputs = {
      config_source = "secrets/wg-b.conf"
    }
  }
}
