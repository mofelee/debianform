assert_remote wg-a "wg-a networkd service is active" \
  "systemctl is-active --quiet systemd-networkd.service"
assert_remote wg-b "wg-b networkd service is active" \
  "systemctl is-active --quiet systemd-networkd.service"
assert_remote wg-a "wg-a has the expected WireGuard tunnel address" \
  "ip -4 address show dev wg0 | grep -F '10.80.0.1/30'"
assert_remote wg-b "wg-b has the expected WireGuard tunnel address" \
  "ip -4 address show dev wg0 | grep -F '10.80.0.2/30'"
assert_remote wg-a "wg-a WireGuard interface is managed by networkd" \
  "networkctl status wg0 --no-pager | grep -E 'State:.*(routable|configured|degraded)'"
assert_remote wg-b "wg-b WireGuard interface is managed by networkd" \
  "networkctl status wg0 --no-pager | grep -E 'State:.*(routable|configured|degraded)'"
assert_remote wg-a "wg-a can reach wg-b through the WireGuard tunnel" \
  "ping -c 5 -W 1 10.80.0.2"
assert_remote wg-b "wg-b can reach wg-a through the WireGuard tunnel" \
  "ping -c 5 -W 1 10.80.0.1"
assert_remote wg-a "wg-a state records running networkd desired state without private key plaintext" \
  "grep -F 'host.wg-a.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'PrivateKey =' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'oC8QjNRJyIfLSq9M9ueL2r/CIDRZrpbj+bF5x04kBVc=' /var/lib/debianform-integration/wireguard-a-state.json"
assert_remote wg-b "wg-b state records running networkd desired state without private key plaintext" \
  "grep -F 'host.wg-b.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'PrivateKey =' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'mDr+zYznUq+J5L2Qm9ezR8FFcpFw69yiLLogYxYJBGc=' /var/lib/debianform-integration/wireguard-b-state.json"
