assert_remote "timezone remains unchanged after timezone management is removed" \
  "test \"\$(timedatectl show -p Timezone --value)\" = 'Asia/Shanghai'"
assert_remote "default locale remains unchanged after locale management is removed" \
  "grep -Eq '^LANG=\"?en_US.UTF-8\"?$' /etc/default/locale"
assert_remote "system settings final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/system-settings-state.json"
run_remote "remove system settings integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "system settings integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
