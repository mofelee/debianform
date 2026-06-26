assert_remote "multi-directory step 3 removed managed file" \
  "test ! -e /tmp/debianform-multi-directory.txt"
assert_remote "multi-directory final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/multi-directory-state.json"
run_remote "remove multi-directory integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "multi-directory integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
