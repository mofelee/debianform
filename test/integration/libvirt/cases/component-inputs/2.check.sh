assert_remote "component input files were destroyed after removal from config" \
  "test ! -e /etc/debianform-component-inputs/listeners.json && test ! -e /etc/debianform-component-inputs/environment.json"
assert_remote "component input final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/component-inputs-state.json"
run_remote "remove component input integration state after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /etc/debianform-component-inputs"
assert_remote "component input integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration && test ! -e /etc/debianform-component-inputs"
