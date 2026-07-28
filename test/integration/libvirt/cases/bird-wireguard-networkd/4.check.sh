assert_remote "edge networkd files and private key were removed" \
  "test ! -e /etc/systemd/network/50-wg-edge.netdev && test ! -e /etc/systemd/network/60-wg-edge.network && test ! -e /etc/wireguard/wg-edge.key"
assert_remote "edge runtime netdev was removed after reload" \
  "! ip link show wg-edge >/dev/null 2>&1"
assert_remote_file_matches "surviving core network matches the reviewed metric update" \
  "$CASE_DIR/golden/40-wg-core.metric-200.network" "/etc/systemd/network/40-wg-core.network"
assert_remote_eventually "surviving core interface was reconfigured after edge deletion" \
  "ip link show wg-core >/dev/null && ip -4 route show dev wg-core | grep -E '^10\\.10\\.0\\.1 .*scope link metric 200[[:space:]]*$'"
assert_remote "edge deletion and core update shared one additional BIRD re-export" \
  "test \"\$(cat /var/lib/debianform-bird-networkd/reexport.count)\" = 3"

python3 "$CASE_DIR/assert-activation-plan.py" delete-edge "$LOG_DIR/4.pre-apply-plan.json"

core_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-core.key")"
edge_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-edge.key")"
assert_remote_file_excludes "state still excludes the core private key after edge deletion" \
  "$core_key" "/var/lib/debianform-integration/bird-wireguard-networkd-state.json"
assert_local_tree_excludes "edge deletion output excludes the core private key" "$core_key" "$LOG_DIR"
assert_local_tree_excludes "edge deletion output excludes the edge private key" "$edge_key" "$LOG_DIR"
