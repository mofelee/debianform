host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/nftables-state.json"
    lock_path = "/var/lock/debianform-integration/nftables-state.lock"
  }

  packages {
    install = ["nftables"]
  }

  nftables {
    enable = true

    main {
      content = <<-EOF
        flush ruleset
        include "/etc/nftables.d/debianform-*.nft"
      EOF
    }

    file "20-debianform-input" {
      path = "/etc/nftables.d/debianform-input.nft"

      content = <<-EOF
        table inet debianform_integration {
          chain input {
            type filter hook input priority 0; policy accept;

            ct state established,related accept
            iifname "lo" accept
            tcp dport 22 accept
            tcp dport 8080 accept
          }
        }
      EOF
    }
  }
}
