assert_remote "component network files were removed" \
  "test ! -e /etc/systemd/network/20-dbf-wan.network && test ! -e /etc/systemd/network/30-dbf-policy.network"
assert_remote "removal did not unconditionally execute the removed root definition" \
  "test \"\$(cat /var/lib/debianform-shared-script-networkd/reload.count)\" = 2"
run_remote "remove shared-script integration artifacts" \
  "ip link delete dbf-wan0 2>/dev/null || true; ip link delete dbf-policy0 2>/dev/null || true; rm -rf /var/lib/debianform-shared-script-networkd /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "shared-script integration artifacts are gone" \
  "test ! -e /var/lib/debianform-shared-script-networkd && test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
