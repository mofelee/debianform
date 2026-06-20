# DebianForm v2 systemd-networkd + WireGuard 设计示例。
#
# v2 编译器尚未接入当前 CLI；本文件是设计夹具。
#
# 设计边界：
# - networkd 保持接近原生 .netdev/.network 结构。
# - WireGuard private key 使用 secrets.file，不在 plan/state 中落明文。
# - RouteTable = "off" 明确禁止根据 AllowedIPs 自动写系统路由表。

host "wg1" {
  ssh {
    host = "wg1"
  }

  system {
    hostname     = "wg1"
    architecture = "amd64"
    codename     = "trixie"
  }

  packages {
    install = [
      "wireguard-tools",
    ]
  }

  secrets {
    file "/etc/wireguard/private.key" {
      source = "secrets/wg1-private.key"
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
          Kind = "wireguard"
          Name = "wg0"
        }

        wireguard = {
          ListenPort     = 51820
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = "off"
        }

        wireguard_peer "peer1" {
          PublicKey = "REPLACE_WITH_PEER_PUBLIC_KEY"

          AllowedIPs = [
            "10.100.0.2/32",
          ]

          Endpoint            = "peer1.example.com:51820"
          PersistentKeepalive = 25
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address      = ["10.100.0.1/24"]
          IPv6AcceptRA = "no"
        }
      }
    }
  }

  assert {
    condition = self.systemd.networkd.netdev["10-wg0"].wireguard.RouteTable == "off"
    message   = "WireGuard must not auto-install routes from AllowedIPs."
  }
}
