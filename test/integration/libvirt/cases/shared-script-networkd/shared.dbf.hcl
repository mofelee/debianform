script "reload_networkd" {
  mode = "once"
  run  = <<-SCRIPT
    set -eu
    mkdir -p /var/lib/debianform-shared-script-networkd
    for link in dbf-wan0 dbf-policy0; do
      if ! ip link show "$link" >/dev/null 2>&1; then
        ip link add "$link" type dummy
      fi
      ip link set "$link" up
    done
    systemctl start systemd-networkd.service
    networkctl reload
    networkctl reconfigure dbf-wan0 dbf-policy0
    count_file=/var/lib/debianform-shared-script-networkd/reload.count
    count=0
    if [ -f "$count_file" ]; then
      count="$(cat "$count_file")"
    fi
    printf '%s\n' "$((count + 1))" > "$count_file"
    printf '%s\n' "$DBF_COMPONENT_NAME" > /var/lib/debianform-shared-script-networkd/component.name
    printf '%s\n' "$DBF_TRIGGER_PATHS" > /var/lib/debianform-shared-script-networkd/trigger.paths
  SCRIPT
}

component "wan_network" {
  files {
    file "/etc/systemd/network/20-dbf-wan.network" {
      content = <<-EOF
        [Match]
        Name=dbf-wan0

        [Network]
        Address=192.0.2.2/32
      EOF
      on_change = script.reload_networkd
    }
  }
}

component "policy_route" {
  files {
    file "/etc/systemd/network/30-dbf-policy.network" {
      content = <<-EOF
        [Match]
        Name=dbf-policy0

        [Network]
        Address=198.51.100.2/32

        [Route]
        Destination=203.0.113.0/24
        Scope=link
        Table=100

        [RoutingPolicyRule]
        From=198.51.100.2/32
        Table=100
      EOF
      on_change = script.reload_networkd
    }
  }
}
