assert_remote "all BIRD networkd files and key files were removed" \
  "test ! -e /etc/systemd/network/10-bird-loop0.netdev && test ! -e /etc/systemd/network/20-bird-loop0.network && test ! -e /etc/systemd/network/30-wg-core.netdev && test ! -e /etc/systemd/network/40-wg-core.network && test ! -e /etc/systemd/network/50-wg-edge.netdev && test ! -e /etc/systemd/network/60-wg-edge.network && test ! -e /etc/wireguard/wg-core.key && test ! -e /etc/wireguard/wg-edge.key"
assert_remote "all BIRD runtime netdevs were removed" \
  "! ip link show bird-loop0 >/dev/null 2>&1 && ! ip link show wg-core >/dev/null 2>&1 && ! ip link show wg-edge >/dev/null 2>&1"
assert_remote "final networkd deletion ran one shared BIRD re-export" \
  "test \"\$(cat /var/lib/debianform-bird-networkd/reexport.count)\" = 4"
assert_remote "state forgot every managed networkd and sensitive-file resource" \
  "! grep -F 'systemd.networkd.netdev' /var/lib/debianform-integration/bird-wireguard-networkd-state.json && ! grep -F 'systemd.networkd.network' /var/lib/debianform-integration/bird-wireguard-networkd-state.json && ! grep -F '/etc/wireguard/wg-' /var/lib/debianform-integration/bird-wireguard-networkd-state.json"

python3 "$CASE_DIR/assert-activation-plan.py" final "$LOG_DIR/5.pre-apply-plan.json"

core_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-core.key")"
edge_key="$(tr -d '\n' <"$CASE_DIR/secrets/wg-edge.key")"
assert_remote_file_excludes "final state excludes the core private key" \
  "$core_key" "/var/lib/debianform-integration/bird-wireguard-networkd-state.json"
assert_remote_file_excludes "final state excludes the edge private key" \
  "$edge_key" "/var/lib/debianform-integration/bird-wireguard-networkd-state.json"
assert_local_tree_excludes "all collected output excludes the core private key" "$core_key" "$LOG_DIR"
assert_local_tree_excludes "all collected output excludes the edge private key" "$edge_key" "$LOG_DIR"

run_remote "remove BIRD networkd integration state and counters" \
  "rm -rf /var/lib/debianform-bird-networkd /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "BIRD networkd guest artifacts are gone" \
  "test ! -e /var/lib/debianform-bird-networkd && test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
