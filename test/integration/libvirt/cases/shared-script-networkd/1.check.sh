assert_remote "both component-owned network files exist" \
  "test -f /etc/systemd/network/20-dbf-wan.network && test -f /etc/systemd/network/30-dbf-policy.network"
assert_remote "both initial file changes caused one host script execution" \
  "test \"\$(cat /var/lib/debianform-shared-script-networkd/reload.count)\" = 1"
assert_remote "host script has no component namespace" \
  "test -z \"\$(cat /var/lib/debianform-shared-script-networkd/component.name)\""
assert_remote "the first execution received both trigger paths" \
  "grep -Fx '/etc/systemd/network/20-dbf-wan.network' /var/lib/debianform-shared-script-networkd/trigger.paths && grep -Fx '/etc/systemd/network/30-dbf-policy.network' /var/lib/debianform-shared-script-networkd/trigger.paths"
assert_remote_eventually "networkd configured both component interfaces" \
  "ip -4 address show dev dbf-wan0 | grep -F '192.0.2.2/32' && ip -4 address show dev dbf-policy0 | grep -F '198.51.100.2/32'"
assert_remote_eventually "raw policy route and rule are active" \
  "ip -4 rule show | grep -F 'from 198.51.100.2 lookup 100' && ip -4 route show table 100 | grep -F '203.0.113.0/24 dev dbf-policy0'"
