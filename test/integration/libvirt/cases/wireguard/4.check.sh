assert_remote wg-a "wg-a WireGuard interface was removed before destroy" \
  "! ip link show wg0"
assert_remote wg-b "wg-b WireGuard interface was removed before destroy" \
  "! ip link show wg0"
assert_remote wg-a "wg-a networkd files were removed before destroy" \
  "test ! -e /etc/systemd/network/10-wg0.netdev && test ! -e /etc/systemd/network/20-wg0.network"
assert_remote wg-b "wg-b networkd files were removed before destroy" \
  "test ! -e /etc/systemd/network/10-wg0.netdev && test ! -e /etc/systemd/network/20-wg0.network"
assert_remote wg-a "wg-a private key remains available for final destroy" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard/private.key)\" = '640 root systemd-network'"
assert_remote wg-b "wg-b private key remains available for final destroy" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard/private.key)\" = '640 root systemd-network'"
assert_remote wg-a "wg-a state forgets removed networkd files without private key plaintext" \
  "! grep -F 'host.wg-a.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'PrivateKey =' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'oC8QjNRJyIfLSq9M9ueL2r/CIDRZrpbj+bF5x04kBVc=' /var/lib/debianform-integration/wireguard-a-state.json"
assert_remote wg-b "wg-b state forgets removed networkd files without private key plaintext" \
  "! grep -F 'host.wg-b.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'PrivateKey =' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'mDr+zYznUq+J5L2Qm9ezR8FFcpFw69yiLLogYxYJBGc=' /var/lib/debianform-integration/wireguard-b-state.json"
