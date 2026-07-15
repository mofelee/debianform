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

  files {
    file "/etc/systemd/network/91-dbf-netplan-raw.network" {
      content = <<-EOF
        [Match]
        Name=dbf-netplan-raw0

        [Network]
        Address=198.51.100.1/32
      EOF
    }
  }

  systemd {
    networkd {
      network "90-dbf-netplan-structured" {
        path = "/etc/systemd/network/90-dbf-netplan-structured.network"

        match = {
          Name = "dbf-netplan-structured0"
        }

        network = {
          Address = ["192.0.2.1/32"]
        }
      }
    }
  }
}
