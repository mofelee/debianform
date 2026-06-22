component "wireguard_networkd" {
  input "private_key_source" {
    type      = string
    sensitive = true
  }

  input "address" {
    type = string
  }

  input "listen_port" {
    type = number
  }

  input "peer_public_key" {
    type = string
  }

  input "peer_allowed_ip" {
    type = string
  }

  input "peer_endpoint" {
    type = string
  }

  directories {
    directory "/etc/wireguard" {
      owner = "root"
      group = "root"
      mode  = "0700"
    }
  }

  secrets {
    file "/etc/wireguard/private.key" {
      source = input.private_key_source
      owner  = "root"
      group  = "root"
      mode   = "0600"
    }
  }

  systemd {
    networkd {
      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = input.listen_port
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = "off"
        }

        wireguard_peer "peer" {
          PublicKey           = input.peer_public_key
          AllowedIPs          = [input.peer_allowed_ip]
          Endpoint            = input.peer_endpoint
          PersistentKeepalive = 25
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address = [input.address]
        }
      }
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
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-a.key"
      address            = "10.80.0.1/30"
      listen_port        = 51820
      peer_public_key    = "2Ra/MKyq6SNHwY2Zk7pFeJrpVxbL1g5pXHltd4xT5Co="
      peer_allowed_ip    = "10.80.0.2/32"
      peer_endpoint      = "__DBF_WG_B_VM_IP__:51820"
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
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-b.key"
      address            = "10.80.0.2/30"
      listen_port        = 51820
      peer_public_key    = "oqdR68M0ICIpSoQv+P8pIW5o56sWAtN9D8c27jvqqGI="
      peer_allowed_ip    = "10.80.0.1/32"
      peer_endpoint      = "__DBF_WG_A_VM_IP__:51820"
    }
  }
}
