assert_remote "v2 managed file was destroyed after removal from config" \
  "test ! -e /tmp/debianform-v2-libvirt.txt"
assert_remote "v2 final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/v2-state.json"
run_remote "remove v2 integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "v2 integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
