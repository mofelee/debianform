assert_remote wg-a "wg-a WireGuard config was destroyed" \
  "test ! -e /etc/wireguard/wg0.conf"
assert_remote wg-b "wg-b WireGuard config was destroyed" \
  "test ! -e /etc/wireguard/wg0.conf"
assert_remote wg-a "wg-a WireGuard interface remains removed" \
  "! ip link show wg0"
assert_remote wg-b "wg-b WireGuard interface remains removed" \
  "! ip link show wg0"
assert_remote wg-a "wg-a service is inactive after destroy" \
  "! systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-b "wg-b service is inactive after destroy" \
  "! systemctl is-active --quiet wg-quick@wg0.service"
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
