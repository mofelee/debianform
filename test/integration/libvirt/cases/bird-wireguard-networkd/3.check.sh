assert_remote_file_matches "single-file drift repair restored reviewed core output" \
  "$CASE_DIR/golden/40-wg-core.network" "/etc/systemd/network/40-wg-core.network"
assert_remote "single-file repair ran exactly one additional BIRD re-export" \
  "test \"\$(cat /var/lib/debianform-bird-networkd/reexport.count)\" = 2"
assert_remote_eventually "single-file repair restored the core addresses and routes" \
  "ip -4 address show dev wg-core | grep -F '10.10.0.0/31' && ip -6 address show dev wg-core | grep -F 'fd00:10::/127' && ip -4 route show dev wg-core | grep -E '^10\\.10\\.0\\.1 .*scope link metric 100[[:space:]]*$'"

python3 "$CASE_DIR/assert-activation-plan.py" drift "$LOG_DIR/3.drift-plan.json"

core_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-core.key")"
edge_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-edge.key")"
assert_local_tree_excludes "drift diagnostics and repair output exclude the core private key" \
  "$core_key" "$LOG_DIR"
assert_local_tree_excludes "drift diagnostics and repair output exclude the edge private key" \
  "$edge_key" "$LOG_DIR"
