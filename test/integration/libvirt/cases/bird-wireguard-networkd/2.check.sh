assert_remote "a no-op apply did not reload, reconfigure, or re-export BIRD routes" \
  "test \"\$(cat /var/lib/debianform-bird-networkd/reexport.count)\" = 1"
assert_remote "all managed interfaces remain configured after the no-op apply and check" \
  "ip link show bird-loop0 >/dev/null && ip link show wg-core >/dev/null && ip link show wg-edge >/dev/null"

core_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-core.key")"
edge_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-edge.key")"
assert_local_tree_excludes "no-op output excludes the core private key" "$core_key" "$LOG_DIR"
assert_local_tree_excludes "no-op output excludes the edge private key" "$edge_key" "$LOG_DIR"
