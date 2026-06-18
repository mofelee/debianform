state "ssh" {
  host      = "server1"
  path      = "/var/lib/debianform/state.json"
  lock_path = "/var/lock/debianform/state.lock"
}

debian_kernel_module "br_netfilter" {
  host    = "server1"
  name    = "br_netfilter"
  persist = true
  path    = "/etc/modules-load.d/kubernetes.conf"
}

debian_sysctl "ip_forward" {
  host  = "server1"
  key   = "net.ipv4.ip_forward"
  value = "1"
  path  = "/etc/sysctl.d/99-kubernetes.conf"
}

debian_nftables_file "main" {
  host     = "server1"
  path     = "/etc/nftables.conf"
  validate = true
  activate = false

  content = <<-EOF
    flush ruleset

    table inet filter {
      chain input {
        type filter hook input priority 0; policy accept;
      }

      chain forward {
        type filter hook forward priority 0; policy accept;
      }

      chain output {
        type filter hook output priority 0; policy accept;
      }
    }
  EOF
}
