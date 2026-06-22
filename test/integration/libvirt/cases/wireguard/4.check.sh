assert_remote wg-a "wg-a service was stopped before destroy" \
  "! systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-b "wg-b service was stopped before destroy" \
  "! systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-a "wg-a service was disabled before destroy" \
  "! systemctl is-enabled --quiet wg-quick@wg0.service"
assert_remote wg-b "wg-b service was disabled before destroy" \
  "! systemctl is-enabled --quiet wg-quick@wg0.service"
assert_remote wg-a "wg-a WireGuard interface was removed after stopping service" \
  "! ip link show wg0"
assert_remote wg-b "wg-b WireGuard interface was removed after stopping service" \
  "! ip link show wg0"
assert_remote wg-a "wg-a config remains available for final destroy" \
  "test -e /etc/wireguard/wg0.conf && test \"\$(stat -c '%a %U %G' /etc/wireguard/wg0.conf)\" = '600 root root'"
assert_remote wg-b "wg-b config remains available for final destroy" \
  "test -e /etc/wireguard/wg0.conf && test \"\$(stat -c '%a %U %G' /etc/wireguard/wg0.conf)\" = '600 root root'"
assert_remote wg-a "wg-a state records stopped service without private key plaintext" \
  "grep -F 'host.wg-a.components.wireguard.services.service[\\\"wg-quick@wg0\\\"]' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'PrivateKey' /var/lib/debianform-integration/wireguard-a-state.json"
assert_remote wg-b "wg-b state records stopped service without private key plaintext" \
  "grep -F 'host.wg-b.components.wireguard.services.service[\\\"wg-quick@wg0\\\"]' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'PrivateKey' /var/lib/debianform-integration/wireguard-b-state.json"
