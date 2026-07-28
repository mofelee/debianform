script "reexport_bird" {
  mode = "once"
  run  = <<-SCRIPT
    set -eu
    install -d -m 0755 /var/lib/debianform-bird-networkd
    count_file=/var/lib/debianform-bird-networkd/reexport.count
    count=0
    if [ -f "$count_file" ]; then
      count="$(cat "$count_file")"
    fi
    printf '%s\n' "$((count + 1))" > "$count_file"
    printf '%s\n' "$DBF_TRIGGER_ADDRESSES" > /var/lib/debianform-bird-networkd/trigger.addresses
  SCRIPT
}

component "bird_networkd" {
  input "base_ensure" {
    type    = string
    default = "present"

    validation {
      condition     = contains(["present", "absent"], input.base_ensure)
      error_message = "base_ensure must be present or absent."
    }
  }

  input "edge_ensure" {
    type    = string
    default = "present"

    validation {
      condition     = contains(["present", "absent"], input.edge_ensure)
      error_message = "edge_ensure must be present or absent."
    }
  }

  input "loop_reconfigure" {
    type    = list(string)
    default = ["bird-loop0"]
  }

  input "core_reconfigure" {
    type    = list(string)
    default = ["wg-core"]
  }

  input "edge_reconfigure" {
    type    = list(string)
    default = ["wg-edge"]
  }

  input "core_route_metric" {
    type    = number
    default = 100
  }

  input "core_key_source" {
    type      = string
    sensitive = true
  }

  input "edge_key_source" {
    type      = string
    sensitive = true
  }

  input "core_private_key_file" {
    type      = string
    sensitive = true
  }

  input "edge_private_key_file" {
    type      = string
    sensitive = true
  }

  directories {
    directory "/etc/wireguard" {
      ensure = input.base_ensure
      owner  = "root"
      group  = "systemd-network"
      mode   = "0750"
    }
  }

  secrets {
    file "/etc/wireguard/wg-core.key" {
      ensure = input.base_ensure
      source = input.core_key_source
      owner  = "root"
      group  = "systemd-network"
      mode   = "0640"
    }

    file "/etc/wireguard/wg-edge.key" {
      ensure = input.edge_ensure
      source = input.edge_key_source
      owner  = "root"
      group  = "systemd-network"
      mode   = "0640"
    }
  }

  systemd {
    networkd {
      enable = true

      netdev "10-bird-loop0" {
        ensure = input.base_ensure

        section "identity" {
          name = "NetDev"
          settings = {
            Name = "bird-loop0"
            Kind = "dummy"
          }
        }
      }

      network "20-bird-loop0" {
        ensure = input.base_ensure

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
          reconfigure = input.loop_reconfigure
          post_reload = global.script.reexport_bird
        }
      }

      netdev "30-wg-core" {
        ensure = input.base_ensure
        group  = "systemd-network"
        mode   = "0640"

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
            ListenPort     = 51830
            PrivateKeyFile = input.core_private_key_file
            RouteTable     = "off"
          }
        }
        section "peer_primary" {
          name = "WireGuardPeer"
          settings = {
            PublicKey           = "2Ra/MKyq6SNHwY2Zk7pFeJrpVxbL1g5pXHltd4xT5Co="
            AllowedIPs          = ["10.100.0.1/32", "fd00:100::1/128"]
            Endpoint            = "192.0.2.10:51820"
            PersistentKeepalive = 25
          }
        }
        section "peer_backup" {
          name = "WireGuardPeer"
          settings = {
            PublicKey           = "oqdR68M0ICIpSoQv+P8pIW5o56sWAtN9D8c27jvqqGI="
            AllowedIPs          = ["10.100.0.2/32", "fd00:100::2/128"]
            Endpoint            = "192.0.2.11:51820"
            PersistentKeepalive = 25
          }
        }
      }

      network "40-wg-core" {
        ensure = input.base_ensure

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
            Metric      = input.core_route_metric
            Scope       = "link"
          }
        }
        section "peer_ipv6" {
          name     = "Route"
          settings = { Destination = "fd00:10::1/128" }
        }
        activation {
          reconfigure = input.core_reconfigure
          post_reload = global.script.reexport_bird
        }
      }

      netdev "50-wg-edge" {
        ensure = input.edge_ensure
        group  = "systemd-network"
        mode   = "0640"

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
            ListenPort     = 51831
            PrivateKeyFile = input.edge_private_key_file
            RouteTable     = "off"
          }
        }
        section "peer" {
          name = "WireGuardPeer"
          settings = {
            PublicKey           = "2Ra/MKyq6SNHwY2Zk7pFeJrpVxbL1g5pXHltd4xT5Co="
            AllowedIPs          = ["10.200.0.1/32", "fd00:200::1/128"]
            Endpoint            = "198.51.100.10:51821"
            PersistentKeepalive = 25
          }
        }
      }

      network "60-wg-edge" {
        ensure = input.edge_ensure

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
          reconfigure = input.edge_reconfigure
          post_reload = global.script.reexport_bird
        }
      }
    }
  }
}
