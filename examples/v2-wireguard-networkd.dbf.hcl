# DebianForm v2 WireGuard deployment with native systemd-networkd.
#
# Do not commit real WireGuard private keys. Generate keys on your own machine,
# keep them outside git, and point each interface at its local key source.

component "wireguard_networkd" {
  input "private_key_source" {
    type        = string
    description = "Local path to the WireGuard private key source."
    sensitive   = true
  }

  input "interface" {
    type = object({
      name        = string
      address     = string
      listen_port = optional(number, 51820)
      route_table = optional(string, "off")
    })

    description = "WireGuard interface settings. route_table = \"off\" stops networkd from adding routes for AllowedIPs."
    nullable    = false
  }

  input "peers" {
    type = map(object({
      public_key           = string
      allowed_ips          = list(string)
      endpoint             = optional(string)
      persistent_keepalive = optional(number, 25)
    }))

    description = "WireGuard peer settings keyed by stable peer label."
    default     = {}
    nullable    = false

    validation {
      condition     = alltrue([for peer in values(input.peers) : length(peer.allowed_ips) > 0])
      error_message = "Each peer.allowed_ips must contain at least one CIDR."
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
    file "private_key" {
      path   = "/etc/wireguard/${input.interface.name}.key"
      source = input.private_key_source
      owner  = "root"
      group  = "systemd-network"
      mode   = "0640"
    }
  }

  systemd {
    networkd {
      enable = true

      netdev "wireguard" {
        path = "/etc/systemd/network/10-${input.interface.name}.netdev"

        netdev = {
          Name = input.interface.name
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = input.interface.listen_port
          PrivateKeyFile = "/etc/wireguard/${input.interface.name}.key"
          RouteTable     = input.interface.route_table
        }

        wireguard_peer = {
          for name, peer in input.peers : name => {
            PublicKey           = peer.public_key
            AllowedIPs          = peer.allowed_ips
            Endpoint            = peer.endpoint
            PersistentKeepalive = peer.persistent_keepalive
          }
        }
      }

      network "wireguard" {
        path = "/etc/systemd/network/20-${input.interface.name}.network"

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
        name        = "wg-prod"
        address     = "10.80.0.1/30"
        route_table = "off"
      }
      peers = {
        wg_b = {
          public_key  = "<wg-b-public-key>"
          allowed_ips = ["10.80.0.2/32"]
          endpoint    = "wg-b.example.net:51820"
        }
        laptop = {
          public_key  = "<laptop-public-key>"
          allowed_ips = ["10.80.0.10/32"]
        }
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
        name        = "wg-prod"
        address     = "10.80.0.2/30"
        route_table = "off"
      }
      peers = {
        wg_a = {
          public_key  = "<wg-a-public-key>"
          allowed_ips = ["10.80.0.1/32"]
          endpoint    = "wg-a.example.net:51820"
        }
      }
    }
  }
}
