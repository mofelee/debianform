assert_remote "per-instance unit files were destroyed after removal from config" \
  "test ! -e /etc/systemd/system/dbf-transport-alpha.service && test ! -e /etc/systemd/system/dbf-transport-beta.service"
assert_remote "per-instance services are inactive and disabled" \
  "! systemctl is-active --quiet dbf-transport-alpha.service && ! systemctl is-enabled --quiet dbf-transport-alpha.service && ! systemctl is-active --quiet dbf-transport-beta.service && ! systemctl is-enabled --quiet dbf-transport-beta.service"
assert_remote "per-instance directories and files were destroyed" \
  "test ! -e /var/lib/debianform-multi/alpha && test ! -e /var/lib/debianform-multi/beta && test ! -e /etc/debianform-multi-alpha.conf && test ! -e /etc/debianform-multi-beta.conf"
assert_remote "multi-instance final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/multi-instance-state.json"
run_remote "remove multi-instance integration state after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /var/lib/debianform-multi"
assert_remote "multi-instance integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lib/debianform-multi"
