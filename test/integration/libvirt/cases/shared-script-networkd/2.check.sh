assert_remote "a no-op apply did not execute the host script" \
  "test \"\$(cat /var/lib/debianform-shared-script-networkd/reload.count)\" = 1"
assert_remote "networkd remains configured after the no-op apply" \
  "ip -4 address show dev dbf-wan0 | grep -F '192.0.2.2/32' && ip -4 address show dev dbf-policy0 | grep -F '198.51.100.2/32'"
