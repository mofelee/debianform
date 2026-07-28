run_remote "drift only the core WireGuard network file" \
  "printf '%s\n' '# drifted outside DebianForm' > /etc/systemd/network/40-wg-core.network"
assert_remote "single-file networkd drift is present before check and repair" \
  "test \"\$(cat /etc/systemd/network/40-wg-core.network)\" = '# drifted outside DebianForm'"
