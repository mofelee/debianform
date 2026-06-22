# DebianForm v2 WireGuard wg-quick deployment example.
#
# This example intentionally references local secret files under examples/secrets.
# Do not commit real WireGuard private keys. Generate wg-a.conf and wg-b.conf on
# the operator machine before applying this configuration.

component "wireguard_wgquick" {
  input "config_source" {
    type = string
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
      enabled = true
      state   = "running"
    }
  }
}

host "wg-a" {
  ssh {
    host          = "wg-a.example.net"
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
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
    host          = "wg-b.example.net"
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }

  component "wireguard" {
    source = component.wireguard_wgquick

    inputs = {
      config_source = "secrets/wg-b.conf"
    }
  }
}

# Example wg-a.conf:
#
# [Interface]
# Address = 10.80.0.1/30
# ListenPort = 51820
# PrivateKey = <wg-a-private-key>
#
# [Peer]
# PublicKey = <wg-b-public-key>
# AllowedIPs = 10.80.0.2/32
# Endpoint = wg-b.example.net:51820
# PersistentKeepalive = 25
#
# Example wg-b.conf uses Address = 10.80.0.2/30, wg-b's private key,
# wg-a's public key, AllowedIPs = 10.80.0.1/32, and wg-a's endpoint.
