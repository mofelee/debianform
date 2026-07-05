assert_remote "static hostname remains unchanged after hostname management is removed" \
  "test \"\$(hostnamectl --static)\" = 'dbf-hostname'"
assert_remote "hostname final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/hostname-state.json"
assert_remote "hostname convergence still did not manage /etc/hosts" \
  "! grep -F 'dbf-hostname' /etc/hosts"
run_remote "remove hostname integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "hostname integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
