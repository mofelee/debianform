assert_remote "core managed file was destroyed after removal from config" \
  "test ! -e /tmp/debianform-core-libvirt.txt"
assert_remote "core final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/core-state.json"
if [[ "$EXPECTED_DISTRIBUTION" == "ubuntu" ]]; then
  assert_remote "non-network destroy left stock Netplan files byte-for-byte unchanged" \
    "set -eu; export LC_ALL=C; find /etc/netplan -maxdepth 1 -type f -name '*.yaml' -print0 | sort -z | xargs -0 -r sha256sum > /tmp/debianform-netplan.current; cmp -s /tmp/debianform-netplan.sha256 /tmp/debianform-netplan.current; rm -f /tmp/debianform-netplan.current /tmp/debianform-netplan.sha256 /tmp/debianform-networkd-active /tmp/debianform-networkd-enabled"
fi
run_remote "remove integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "core integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
