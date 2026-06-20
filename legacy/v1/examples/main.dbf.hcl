state "ssh" {
  host      = "server1"
  path      = "/var/lib/debianform/state.json"
  lock_path = "/var/lock/debianform/state.lock"
}

debian_package "base" {
  for_each = {
    curl = true
    jq   = true
  }

  host = "server1"
  name = each.key
}

debian_file "motd" {
  host    = "server1"
  path    = "/etc/motd"
  content = "Managed by debianform\n"
  mode    = "0644"
}

debian_networkd_file "native" {
  for_each = {
    "10-eth0.network" = <<-EOF
      [Match]
      Name=eth0

      [Network]
      DHCP=yes
    EOF
  }

  host    = "server1"
  name    = each.key
  content = each.value
}
