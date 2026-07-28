assert_remote_file_matches "dummy netdev matches reviewed golden output" \
  "$CASE_DIR/golden/10-bird-loop0.netdev" "/etc/systemd/network/10-bird-loop0.netdev"
assert_remote_file_matches "dummy network matches reviewed golden output" \
  "$CASE_DIR/golden/20-bird-loop0.network" "/etc/systemd/network/20-bird-loop0.network"
assert_remote_file_matches "core WireGuard netdev matches reviewed golden output" \
  "$CASE_DIR/golden/30-wg-core.netdev" "/etc/systemd/network/30-wg-core.netdev"
assert_remote_file_matches "core WireGuard network matches reviewed golden output" \
  "$CASE_DIR/golden/40-wg-core.network" "/etc/systemd/network/40-wg-core.network"
assert_remote_file_matches "edge WireGuard netdev matches reviewed golden output" \
  "$CASE_DIR/golden/50-wg-edge.netdev" "/etc/systemd/network/50-wg-edge.netdev"
assert_remote_file_matches "edge WireGuard network matches reviewed golden output" \
  "$CASE_DIR/golden/60-wg-edge.network" "/etc/systemd/network/60-wg-edge.network"

assert_remote "networkd files and private keys retain protected modes" \
  "test \"\$(stat -c '%a %U %G' /etc/systemd/network/10-bird-loop0.netdev)\" = '644 root root' && test \"\$(stat -c '%a %U %G' /etc/systemd/network/30-wg-core.netdev)\" = '640 root systemd-network' && test \"\$(stat -c '%a %U %G' /etc/systemd/network/50-wg-edge.netdev)\" = '640 root systemd-network' && test \"\$(stat -c '%a %U %G' /etc/wireguard/wg-core.key)\" = '640 root systemd-network' && test \"\$(stat -c '%a %U %G' /etc/wireguard/wg-edge.key)\" = '640 root systemd-network'"
assert_remote "systemd-networkd is active" \
  "systemctl is-active --quiet systemd-networkd.service"
assert_remote_eventually "dummy and WireGuard interfaces have all managed addresses" \
  "ip -4 address show dev bird-loop0 | grep -F '192.0.2.1/32' && ip -6 address show dev bird-loop0 | grep -F '2001:db8:100::1/128' && ip -4 address show dev wg-core | grep -F '10.10.0.0/31' && ip -6 address show dev wg-core | grep -F 'fd00:10::/127' && ip -6 address show dev wg-core | grep -F 'fe80::10/64' && ip -4 address show dev wg-edge | grep -F '10.20.0.0/31' && ip -6 address show dev wg-edge | grep -F 'fd00:20::/127' && ip -6 address show dev wg-edge | grep -F 'fe80::20/64'"
assert_remote_eventually "core IPv4 peer route is active" \
  "ip -4 route show dev wg-core | grep -E '^10\\.10\\.0\\.1 .*scope link metric 100[[:space:]]*$'"
assert_remote_eventually "core IPv6 peer route is active" \
  "ip -6 route show dev wg-core | grep -E '^fd00:10::1 .*metric 1024 '"
assert_remote_eventually "edge IPv4 peer route is active" \
  "ip -4 route show dev wg-edge | grep -E '^10\\.20\\.0\\.1 .*scope link[[:space:]]*$'"
assert_remote_eventually "edge IPv6 peer route is active" \
  "ip -6 route show dev wg-edge | grep -E '^fd00:20::1 .*metric 1024 '"
assert_remote "AddPrefixRoute=false suppresses WireGuard prefix routes" \
  "! ip -4 route show dev wg-core | grep -F '10.10.0.0/31' && ! ip -4 route show dev wg-edge | grep -F '10.20.0.0/31'"
assert_remote "all initial changes ran the shared BIRD re-export script once" \
  "test \"\$(cat /var/lib/debianform-bird-networkd/reexport.count)\" = 1"

python3 "$CASE_DIR/assert-activation-plan.py" initial "$LOG_DIR/1.pre-apply-plan.json"

core_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-core.key")"
edge_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-edge.key")"
assert_remote_file_excludes "state excludes the core private key" \
  "$core_key" "/var/lib/debianform-integration/bird-wireguard-networkd-state.json"
assert_remote_file_excludes "state excludes the edge private key" \
  "$edge_key" "/var/lib/debianform-integration/bird-wireguard-networkd-state.json"
assert_local_tree_excludes "text, JSON, HTML, debug, apply, and check output exclude the core private key" \
  "$core_key" "$LOG_DIR"
assert_local_tree_excludes "text, JSON, HTML, debug, apply, and check output exclude the edge private key" \
  "$edge_key" "$LOG_DIR"
