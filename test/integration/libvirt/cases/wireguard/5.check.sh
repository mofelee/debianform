assert_remote wg-a "wg-a WireGuard private key was destroyed" \
  "test ! -e /etc/wireguard/private.key"
assert_remote wg-b "wg-b WireGuard private key was destroyed" \
  "test ! -e /etc/wireguard/private.key"
assert_remote wg-a "wg-a networkd netdev was destroyed" \
  "test ! -e /etc/systemd/network/10-wg0.netdev"
assert_remote wg-b "wg-b networkd netdev was destroyed" \
  "test ! -e /etc/systemd/network/10-wg0.netdev"
assert_remote wg-a "wg-a WireGuard interface remains removed" \
  "! ip link show wg0"
assert_remote wg-b "wg-b WireGuard interface remains removed" \
  "! ip link show wg0"
assert_remote wg-a "wg-a final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/wireguard-a-state.json"
assert_remote wg-b "wg-b final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/wireguard-b-state.json"
run_remote wg-a "remove wg-a integration state after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
run_remote wg-b "remove wg-b integration state after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote wg-a "wg-a integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
assert_remote wg-b "wg-b integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
