assert_remote wg-a "wg-a private key was deployed as a strict secret" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard/private.key)\" = '640 root systemd-network'"
assert_remote wg-b "wg-b private key was deployed as a strict secret" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard/private.key)\" = '640 root systemd-network'"
assert_remote wg-a "wg-a networkd netdev exists" \
  "test -e /etc/systemd/network/10-wg0.netdev && grep -F 'PrivateKeyFile=/etc/wireguard/private.key' /etc/systemd/network/10-wg0.netdev"
assert_remote wg-b "wg-b networkd netdev exists" \
  "test -e /etc/systemd/network/10-wg0.netdev && grep -F 'PrivateKeyFile=/etc/wireguard/private.key' /etc/systemd/network/10-wg0.netdev"
assert_remote wg-a "wg-a state records networkd resources without private key plaintext" \
  "grep -F 'host.wg-a.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-a-state.json && grep -F 'host.wg-a.components.wireguard.secrets.file' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'PrivateKey =' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'oC8QjNRJyIfLSq9M9ueL2r/CIDRZrpbj+bF5x04kBVc=' /var/lib/debianform-integration/wireguard-a-state.json"
assert_remote wg-b "wg-b state records networkd resources without private key plaintext" \
  "grep -F 'host.wg-b.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-b-state.json && grep -F 'host.wg-b.components.wireguard.secrets.file' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'PrivateKey =' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'mDr+zYznUq+J5L2Qm9ezR8FFcpFw69yiLLogYxYJBGc=' /var/lib/debianform-integration/wireguard-b-state.json"
