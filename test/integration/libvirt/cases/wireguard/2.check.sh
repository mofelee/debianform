assert_remote wg-a "wg-a service is active" \
  "systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-b "wg-b service is active" \
  "systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-a "wg-a service is enabled" \
  "systemctl is-enabled --quiet wg-quick@wg0.service"
assert_remote wg-b "wg-b service is enabled" \
  "systemctl is-enabled --quiet wg-quick@wg0.service"
assert_remote wg-a "wg-a has the expected WireGuard tunnel address" \
  "ip -4 address show dev wg0 | grep -F '10.80.0.1/30'"
assert_remote wg-b "wg-b has the expected WireGuard tunnel address" \
  "ip -4 address show dev wg0 | grep -F '10.80.0.2/30'"
assert_remote wg-a "wg-a sees wg-b as a WireGuard peer" \
  "wg show wg0 peers | grep -F '2Ra/MKyq6SNHwY2Zk7pFeJrpVxbL1g5pXHltd4xT5Co='"
assert_remote wg-b "wg-b sees wg-a as a WireGuard peer" \
  "wg show wg0 peers | grep -F 'oqdR68M0ICIpSoQv+P8pIW5o56sWAtN9D8c27jvqqGI='"
assert_remote wg-a "wg-a can reach wg-b through the WireGuard tunnel" \
  "ping -c 5 -W 1 10.80.0.2"
assert_remote wg-b "wg-b can reach wg-a through the WireGuard tunnel" \
  "ping -c 5 -W 1 10.80.0.1"
assert_remote wg-a "wg-a state records running service desired state without private key plaintext" \
  "grep -F 'host.wg-a.components.wireguard.services.service[\\\"wg-quick@wg0\\\"]' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'PrivateKey' /var/lib/debianform-integration/wireguard-a-state.json"
assert_remote wg-b "wg-b state records running service desired state without private key plaintext" \
  "grep -F 'host.wg-b.components.wireguard.services.service[\\\"wg-quick@wg0\\\"]' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'PrivateKey' /var/lib/debianform-integration/wireguard-b-state.json"
