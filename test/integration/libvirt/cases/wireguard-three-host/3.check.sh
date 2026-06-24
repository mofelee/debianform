assert_remote wg-a "wg-a WireGuard interfaces were removed before final destroy" \
  "! ip link show wg-ab && ! ip link show wg-ac"
assert_remote wg-b "wg-b WireGuard interface was removed before final destroy" \
  "! ip link show wg-mesh"
assert_remote wg-c "wg-c WireGuard interface was removed before final destroy" \
  "! ip link show wg-mesh"
assert_remote wg-a "wg-a networkd files were removed while secrets remain" \
  "test ! -e /etc/systemd/network/10-wg-ab.netdev && test ! -e /etc/systemd/network/10-wg-ac.netdev && test -e /etc/wireguard/wg-ab.key && test -e /etc/wireguard/wg-ac.key"
assert_remote wg-b "wg-b networkd files were removed while secret remains" \
  "test ! -e /etc/systemd/network/10-wg-mesh.netdev && test -e /etc/wireguard/wg-mesh.key"
assert_remote wg-c "wg-c networkd files were removed while secret remains" \
  "test ! -e /etc/systemd/network/10-wg-mesh.netdev && test -e /etc/wireguard/wg-mesh.key"
assert_remote wg-a "wg-a state forgets removed networkd resources but keeps component secrets" \
  "! grep -F 'host.wg-a.components.wg_ab.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-three-a-state.json && grep -F 'host.wg-a.components.wg_ab.secrets.file' /var/lib/debianform-integration/wireguard-three-a-state.json"
