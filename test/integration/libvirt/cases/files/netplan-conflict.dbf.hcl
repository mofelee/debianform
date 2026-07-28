host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/netplan-preflight-state.json"
    lock_path = "/var/lock/debianform-integration/netplan-preflight-state.lock"
  }

  systemd {
    networkd {
      network "90-dbf-netplan-generic" {
        section "match" {
          name     = "Match"
          settings = { Name = "dbf-netplan-generic0" }
        }
        section "network" {
          name     = "Network"
          settings = { Address = ["192.0.2.1/32"] }
        }
      }

      network "91-dbf-netplan-raw-content" {
        content = <<-EOF
          [Match]
          Name=dbf-netplan-raw0

          [Network]
          Address=198.51.100.1/32
        EOF
      }

      netdev "92-dbf-netplan-raw-source" {
        source = "netplan-conflict-raw.netdev"
      }
    }
  }
}
