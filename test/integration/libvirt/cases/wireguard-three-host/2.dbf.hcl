component "wireguard_networkd" {
  input "private_key_source" {
    type        = string
    description = "Local path to the WireGuard private key source."
    sensitive   = true
  }

  input "interface" {
    type = object({
      name        = string
      address     = list(string)
      listen_port = optional(number, 51820)
      route_table = optional(string, "off")
    })

    nullable = false
  }

  input "peers" {
    type = map(object({
      public_key           = string
      allowed_ips          = list(string)
      endpoint             = optional(string)
      persistent_keepalive = optional(number, 25)
    }))

    default  = {}
    nullable = false

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
          Address = input.interface.address
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
    path      = "/var/lib/debianform-integration/wireguard-three-a-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-three-a-state.lock"
  }

  component "wg_ab" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-a.key"
      interface = {
        name        = "wg-ab"
        address     = ["10.90.0.1/30"]
        listen_port = 51820
        route_table = "off"
      }
      peers = {
        wg_b = {
          public_key  = "zo060cy2M+x7cMF4FKXHbs0CloUFDTRHRboFhw5YfVk="
          allowed_ips = ["10.90.0.2/32"]
          endpoint    = "__DBF_WG_B_VM_IP__:51820"
        }
      }
    }
  }

  component "wg_ac" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-a.key"
      interface = {
        name        = "wg-ac"
        address     = ["10.90.0.5/30"]
        listen_port = 51821
        route_table = "off"
      }
      peers = {
        wg_c = {
          public_key  = "Xf7dO2vUf2+ijuFdlp1bsOpTd01Ii9r53xxuASSz7yI="
          allowed_ips = ["10.90.0.6/32"]
          endpoint    = "__DBF_WG_C_VM_IP__:51820"
        }
      }
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
    path      = "/var/lib/debianform-integration/wireguard-three-b-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-three-b-state.lock"
  }

  component "wireguard" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-b.key"
      interface = {
        name        = "wg-mesh"
        address     = ["10.90.0.2/30", "10.90.0.9/30"]
        listen_port = 51820
        route_table = "off"
      }
      peers = {
        wg_a = {
          public_key  = "B6N8vBQgk8i3VdwbEOhstCY3StFqqFPtC9/AsrhtHHw="
          allowed_ips = ["10.90.0.1/32"]
          endpoint    = "__DBF_WG_A_VM_IP__:51820"
        }
        wg_c = {
          public_key  = "Xf7dO2vUf2+ijuFdlp1bsOpTd01Ii9r53xxuASSz7yI="
          allowed_ips = ["10.90.0.10/32"]
          endpoint    = "__DBF_WG_C_VM_IP__:51821"
        }
      }
    }
  }
}

host "wg-c" {
  ssh {
    host          = "__DBF_WG_C_SSH_HOST__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/wireguard-three-c-state.json"
    lock_path = "/var/lock/debianform-integration/wireguard-three-c-state.lock"
  }

  component "wireguard" {
    source = component.wireguard_networkd

    inputs = {
      private_key_source = "secrets/wg-c.key"
      interface = {
        name        = "wg-mesh"
        address     = ["10.90.0.6/30", "10.90.0.10/30"]
        listen_port = 51820
        route_table = "off"
      }
      peers = {
        wg_a = {
          public_key  = "B6N8vBQgk8i3VdwbEOhstCY3StFqqFPtC9/AsrhtHHw="
          allowed_ips = ["10.90.0.5/32"]
          endpoint    = "__DBF_WG_A_VM_IP__:51821"
        }
        wg_b = {
          public_key  = "zo060cy2M+x7cMF4FKXHbs0CloUFDTRHRboFhw5YfVk="
          allowed_ips = ["10.90.0.9/32"]
          endpoint    = "__DBF_WG_B_VM_IP__:51820"
        }
      }
    }
  }
}
