assert_remote "systemd extension unit files were destroyed after removal from config" \
  "test ! -e /etc/systemd/system/dbf-systemd-extensions-worker.service && test ! -e /etc/systemd/system/dbf-systemd-extensions-worker.timer"
assert_remote "timer remains inactive and disabled after destroy" \
  "! systemctl is-active --quiet dbf-systemd-extensions-worker.timer && ! systemctl is-enabled --quiet dbf-systemd-extensions-worker.timer"
assert_remote "resolved and journald drop-ins were destroyed after removal from config" \
  "test ! -e /etc/systemd/resolved.conf.d/debianform.conf && test ! -e /etc/systemd/journald.conf.d/debianform.conf"
assert_remote "worker script was destroyed after removal from config" \
  "test ! -e /usr/local/bin/dbf-systemd-extensions-worker"
assert_remote "systemd extensions final state keeps only the resolved package prerequisite" \
  "grep -F 'host.cihost.packages.install[\\\"systemd-resolved\\\"]' /var/lib/debianform-integration/systemd-extensions-state.json && ! grep -F 'host.cihost.systemd.timer' /var/lib/debianform-integration/systemd-extensions-state.json && ! grep -F 'host.cihost.systemd.resolved' /var/lib/debianform-integration/systemd-extensions-state.json && ! grep -F 'host.cihost.systemd.journald' /var/lib/debianform-integration/systemd-extensions-state.json"
run_remote "remove systemd extension integration state and runtime markers after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /run/debianform-systemd-extensions"
assert_remote "systemd extension integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration && test ! -e /run/debianform-systemd-extensions"
