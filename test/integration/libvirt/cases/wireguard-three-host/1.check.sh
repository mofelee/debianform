assert_remote wg-a "wg-a has two component-expanded WireGuard key files" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard/wg-ab.key)\" = '640 root systemd-network' && test \"\$(stat -c '%a %U %G' /etc/wireguard/wg-ac.key)\" = '640 root systemd-network'"
assert_remote wg-a "wg-a rendered two WireGuard interfaces from the same component" \
  "test -e /etc/systemd/network/10-wg-ab.netdev && test -e /etc/systemd/network/10-wg-ac.netdev && grep -F 'Name=wg-ab' /etc/systemd/network/10-wg-ab.netdev && grep -F 'Name=wg-ac' /etc/systemd/network/10-wg-ac.netdev"
assert_remote wg-a "wg-a shared directory was not duplicated into a conflicting remote path" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard)\" = '750 root systemd-network'"
assert_remote wg-b "wg-b one interface renders two WireGuard peers" \
  "test \"\$(grep -c '^\\[WireGuardPeer\\]' /etc/systemd/network/10-wg-mesh.netdev)\" = '2' && grep -F 'AllowedIPs=10.90.0.1/32' /etc/systemd/network/10-wg-mesh.netdev && grep -F 'AllowedIPs=10.90.0.10/32' /etc/systemd/network/10-wg-mesh.netdev"
assert_remote wg-c "wg-c one interface renders two WireGuard peers" \
  "test \"\$(grep -c '^\\[WireGuardPeer\\]' /etc/systemd/network/10-wg-mesh.netdev)\" = '2' && grep -F 'AllowedIPs=10.90.0.5/32' /etc/systemd/network/10-wg-mesh.netdev && grep -F 'AllowedIPs=10.90.0.9/32' /etc/systemd/network/10-wg-mesh.netdev"
assert_remote wg-a "wg-a state records both component instances" \
  "grep -F 'host.wg-a.components.wg_ab.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-three-a-state.json && grep -F 'host.wg-a.components.wg_ac.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-three-a-state.json"
assert_remote wg-b "wg-b state records the two-peer component" \
  "grep -F 'host.wg-b.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-three-b-state.json && ! grep -F 'AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=' /var/lib/debianform-integration/wireguard-three-b-state.json"
assert_remote wg-c "wg-c state records the two-peer component" \
  "grep -F 'host.wg-c.components.wireguard.systemd.networkd.netdev' /var/lib/debianform-integration/wireguard-three-c-state.json && ! grep -F 'AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=' /var/lib/debianform-integration/wireguard-three-c-state.json"
