# DebianForm v2 WireGuard deployment with native systemd-networkd.
#
# Do not commit real WireGuard private keys. Generate wg-a.key and wg-b.key on
# your own machine, keep them outside git, and point the two hosts at them.

component "wireguard_networkd" {
  input "private_key_source" {
    type        = string
    description = "Local path to the WireGuard private key source."
    sensitive   = true
  }

  input "interface" {
    type = object({
      name        = optional(string, "wg0")
      address     = string
      listen_port = optional(number, 51820)
      route_table = optional(string, "off")
    })

    description = "WireGuard interface settings. route_table = \"off\" stops networkd from adding routes for AllowedIPs."
    nullable    = false
  }

  input "peer" {
    type = object({
      public_key           = string
      allowed_ips          = list(string)
      endpoint             = string
      persistent_keepalive = optional(number, 25)
    })

    description = "WireGuard peer settings."
    nullable    = false

    validation {
      condition     = length(input.peer.allowed_ips) > 0
      error_message = "peer.allowed_ips must contain at least one CIDR."
    }
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
          Name = input.interface.name
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = input.interface.listen_port
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = input.interface.route_table
        }

        wireguard_peer "peer" {
          PublicKey           = input.peer.public_key
          AllowedIPs          = input.peer.allowed_ips
          Endpoint            = input.peer.endpoint
          PersistentKeepalive = input.peer.persistent_keepalive
        }
      }

      network "20-wg0" {
        match = {
          Name = input.interface.name
        }

        network = {
          Address = [input.interface.address]
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
      interface = {
        address     = "10.80.0.1/30"
        route_table = "off"
      }
      peer = {
        public_key  = "<wg-b-public-key>"
        allowed_ips = ["10.80.0.2/32"]
        endpoint    = "wg-b.example.net:51820"
      }
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
      interface = {
        address     = "10.80.0.2/30"
        route_table = "off"
      }
      peer = {
        public_key  = "<wg-a-public-key>"
        allowed_ips = ["10.80.0.1/32"]
        endpoint    = "wg-a.example.net:51820"
      }
    }
  }
}
