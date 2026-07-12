# Host-scoped shared script: raw networkd files from separate components trigger
# one reload operation per affected host.
script "reload_networkd" {
  mode = "once"
  commands = [
    ["systemctl", "start", "systemd-networkd.service"],
    ["networkctl", "reload"],
  ]
}

component "wan_network" {
  files {
    file "/etc/systemd/network/20-wan.network" {
      content = <<-EOF
        [Match]
        Name=enp1s0

        [Network]
        DHCP=yes
      EOF

      on_change = script.reload_networkd
    }
  }
}

component "policy_route" {
  files {
    file "/etc/systemd/network/30-policy-routing.network" {
      content = <<-EOF
        [Match]
        Name=enp2s0

        [Network]
        Address=192.0.2.2/24

        [Route]
        Gateway=192.0.2.1
        Table=100

        [RoutingPolicyRule]
        From=192.0.2.2/32
        Table=100
      EOF

      on_change = script.reload_networkd
    }
  }
}

host "router1" {
  components = [component.wan_network, component.policy_route]

  platform {
    architecture = "amd64"
    codename     = "trixie"
  }
}
