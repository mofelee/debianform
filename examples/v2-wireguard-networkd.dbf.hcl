# DebianForm v2 WireGuard deployment with native systemd-networkd.
#
# Do not commit real WireGuard private keys. Generate wg-a.key and wg-b.key on
# your own machine, keep them outside git, and point the two hosts at them.

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
      group = "systemd-network"
      mode  = "0750"
    }
  }

  secrets {
    file "/etc/wireguard/private.key" {
      source = input.private_key_source
      owner  = "root"
      group  = "systemd-network"
      mode   = "0640"
    }
  }

  systemd {
    networkd {
      enable = true

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
    host = "wg-a.example.net"
    user = "root"
  }

  component "wireguard" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-a.key"
      address            = "10.80.0.1/30"
      listen_port        = 51820
      peer_public_key    = "<wg-b-public-key>"
      peer_allowed_ip    = "10.80.0.2/32"
      peer_endpoint      = "wg-b.example.net:51820"
    }
  }
}

host "wg-b" {
  ssh {
    host = "wg-b.example.net"
    user = "root"
  }

  component "wireguard" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-b.key"
      address            = "10.80.0.2/30"
      listen_port        = 51820
      peer_public_key    = "<wg-a-public-key>"
      peer_allowed_ip    = "10.80.0.1/32"
      peer_endpoint      = "wg-a.example.net:51820"
    }
  }
}
