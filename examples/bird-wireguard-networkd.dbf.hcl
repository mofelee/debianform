# BIRD owns routing; systemd-networkd owns interface creation and addresses.
# Replace every example key, endpoint, address, and key-file path before apply.

variable "wg_core_private_key_file" {
  type        = string
  description = "Remote PrivateKeyFile path for wg-core."
  sensitive   = true
  default     = "/etc/wireguard/wg-core.key"
}

variable "wg_edge_private_key_file" {
  type        = string
  description = "Remote PrivateKeyFile path for wg-edge."
  sensitive   = true
  default     = "/etc/wireguard/wg-edge.key"
}

script "reexport_bird" {
  mode = "once"

  commands = [
    ["birdc", "reload", "out", "kernel4"],
    ["birdc", "reload", "out", "kernel6"],
  ]
}

host "bird-router" {
  platform {
    distribution = "debian"
    version      = "13"
    architecture = "amd64"
    codename     = "trixie"
  }

  systemd {
    networkd {
      enable = true

      netdev "10-bird-loop0" {
        section "identity" {
          name = "NetDev"
          settings = {
            Name = "bird-loop0"
            Kind = "dummy"
          }
        }
      }

      network "20-bird-loop0" {
        section "match" {
          name     = "Match"
          settings = { Name = "bird-loop0" }
        }
        section "network" {
          name     = "Network"
          settings = { LinkLocalAddressing = "no" }
        }
        section "ipv4" {
          name = "Address"
          settings = {
            Address        = "192.0.2.1/32"
            AddPrefixRoute = false
          }
        }
        section "ipv6" {
          name = "Address"
          settings = {
            Address        = "2001:db8:100::1/128"
            AddPrefixRoute = false
          }
        }
        activation {
          reconfigure = ["bird-loop0"]
          post_reload = script.reexport_bird
        }
      }

      netdev "30-wg-core" {
        group = "systemd-network"
        mode  = "0640"

        section "identity" {
          name = "NetDev"
          settings = {
            Name = "wg-core"
            Kind = "wireguard"
          }
        }
        section "wireguard" {
          name = "WireGuard"
          settings = {
            ListenPort     = 51820
            PrivateKeyFile = var.wg_core_private_key_file
            RouteTable     = "off"
          }
        }
        section "peer_primary" {
          name = "WireGuardPeer"
          settings = {
            PublicKey           = "example-core-primary-public-key"
            AllowedIPs          = ["0.0.0.0/0", "::/0"]
            Endpoint            = "core-primary.example.invalid:51820"
            PersistentKeepalive = 25
          }
        }
        section "peer_backup" {
          name = "WireGuardPeer"
          settings = {
            PublicKey           = "example-core-backup-public-key"
            AllowedIPs          = ["10.10.0.4/32", "fd00:10::4/128"]
            Endpoint            = "core-backup.example.invalid:51820"
            PersistentKeepalive = 25
          }
        }
      }

      network "40-wg-core" {
        section "match" {
          name     = "Match"
          settings = { Name = "wg-core" }
        }
        section "network" {
          name     = "Network"
          settings = { LinkLocalAddressing = "no" }
        }
        section "ipv4" {
          name = "Address"
          settings = {
            Address        = "10.10.0.0/31"
            AddPrefixRoute = false
          }
        }
        section "ipv6" {
          name = "Address"
          settings = {
            Address        = "fd00:10::/127"
            AddPrefixRoute = false
          }
        }
        section "link_local" {
          name     = "Address"
          settings = { Address = "fe80::10/64" }
        }
        section "peer_ipv4" {
          name = "Route"
          settings = {
            Destination = "10.10.0.1/32"
            Scope       = "link"
          }
        }
        section "peer_ipv6" {
          name     = "Route"
          settings = { Destination = "fd00:10::1/128" }
        }
        activation {
          reconfigure = ["wg-core"]
          post_reload = script.reexport_bird
        }
      }

      netdev "50-wg-edge" {
        group = "systemd-network"
        mode  = "0640"

        section "identity" {
          name = "NetDev"
          settings = {
            Name = "wg-edge"
            Kind = "wireguard"
          }
        }
        section "wireguard" {
          name = "WireGuard"
          settings = {
            ListenPort     = 51821
            PrivateKeyFile = var.wg_edge_private_key_file
            RouteTable     = "off"
          }
        }
        section "peer" {
          name = "WireGuardPeer"
          settings = {
            PublicKey           = "example-edge-public-key"
            AllowedIPs          = ["0.0.0.0/0", "::/0"]
            Endpoint            = "edge.example.invalid:51821"
            PersistentKeepalive = 25
          }
        }
      }

      network "60-wg-edge" {
        section "match" {
          name     = "Match"
          settings = { Name = "wg-edge" }
        }
        section "network" {
          name     = "Network"
          settings = { LinkLocalAddressing = "no" }
        }
        section "ipv4" {
          name = "Address"
          settings = {
            Address        = "10.20.0.0/31"
            AddPrefixRoute = false
          }
        }
        section "ipv6" {
          name = "Address"
          settings = {
            Address        = "fd00:20::/127"
            AddPrefixRoute = false
          }
        }
        section "link_local" {
          name     = "Address"
          settings = { Address = "fe80::20/64" }
        }
        section "peer_ipv4" {
          name = "Route"
          settings = {
            Destination = "10.20.0.1/32"
            Scope       = "link"
          }
        }
        section "peer_ipv6" {
          name     = "Route"
          settings = { Destination = "fd00:20::1/128" }
        }
        activation {
          reconfigure = ["wg-edge"]
          post_reload = script.reexport_bird
        }
      }
    }
  }
}
