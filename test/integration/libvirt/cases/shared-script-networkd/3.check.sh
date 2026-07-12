assert_remote "one drifted file caused exactly one additional execution" \
  "test \"\$(cat /var/lib/debianform-shared-script-networkd/reload.count)\" = 2"
assert_remote "the drift execution received only the WAN trigger path" \
  "test \"\$(cat /var/lib/debianform-shared-script-networkd/trigger.paths)\" = '/etc/systemd/network/20-dbf-wan.network'"
assert_remote_eventually "networkd restored the drifted file and active addresses" \
  "grep -F 'Name=dbf-wan0' /etc/systemd/network/20-dbf-wan.network && ip -4 address show dev dbf-wan0 | grep -F '192.0.2.2/32' && ip -4 address show dev dbf-policy0 | grep -F '198.51.100.2/32'"
