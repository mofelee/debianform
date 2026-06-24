assert_remote "core managed file was destroyed after removal from config" \
  "test ! -e /tmp/debianform-core-libvirt.txt"
assert_remote "core final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/core-state.json"
run_remote "remove integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "core integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
