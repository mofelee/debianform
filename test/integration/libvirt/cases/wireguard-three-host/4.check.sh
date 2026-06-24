assert_remote wg-a "wg-a WireGuard files were destroyed" \
  "test ! -e /etc/systemd/network/10-wg-ab.netdev && test ! -e /etc/systemd/network/10-wg-ac.netdev && test ! -e /etc/wireguard/wg-ab.key && test ! -e /etc/wireguard/wg-ac.key"
assert_remote wg-b "wg-b WireGuard files were destroyed" \
  "test ! -e /etc/systemd/network/10-wg-mesh.netdev && test ! -e /etc/wireguard/wg-mesh.key"
assert_remote wg-c "wg-c WireGuard files were destroyed" \
  "test ! -e /etc/systemd/network/10-wg-mesh.netdev && test ! -e /etc/wireguard/wg-mesh.key"
assert_remote wg-a "wg-a final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/wireguard-three-a-state.json"
assert_remote wg-b "wg-b final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/wireguard-three-b-state.json"
assert_remote wg-c "wg-c final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/wireguard-three-c-state.json"
run_remote wg-a "remove wg-a integration state after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
run_remote wg-b "remove wg-b integration state after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
run_remote wg-c "remove wg-c integration state after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
