variable "private_key" {
  type      = string
  sensitive = true
  default   = "not-a-real-variable-secret"
}

script "reexport" {
  mode = "once"
  run  = "birdc reload out kernel4"
}

host "networkd-redaction1" {
  platform {
    distribution = "debian"
    version      = "13"
    architecture = "amd64"
    codename     = "trixie"
  }

  systemd {
    networkd {
      netdev "10-wg0" {
        section "identity" {
          name = "NetDev"
          settings = {
            Name = "wg0"
            Kind = "wireguard"
          }
        }

        section "wireguard" {
          name = "WireGuard"
          settings = {
            PrivateKey = var.private_key
            RouteTable = "off"
          }
        }
      }

      network "20-wg0" {
        content = "[Match]\nName=wg0\n\n[Network]\nDescription=${var.private_key}\n"

        activation {
          reconfigure = ["wg0"]
          post_reload = script.reexport
        }
      }
    }
  }
}
