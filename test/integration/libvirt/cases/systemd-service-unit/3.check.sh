assert_remote "service unit files were destroyed after removal from config" \
  "test ! -e /etc/systemd/system/dbf-raw.service && test ! -e /etc/systemd/system/dbf-structured.service"
assert_remote "worker script was destroyed after removal from config" \
  "test ! -e /usr/local/bin/dbf-service-unit-worker"
assert_remote "services remain inactive after destroy" \
  "! systemctl is-active --quiet dbf-raw.service && ! systemctl is-active --quiet dbf-structured.service"
assert_remote "service unit final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/service-unit-state.json"
run_remote "remove service unit integration state and runtime markers after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /run/debianform-service-unit"
assert_remote "service unit integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration && test ! -e /run/debianform-service-unit"
