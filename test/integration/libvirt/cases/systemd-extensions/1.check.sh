assert_remote "structured service unit includes service_config hardening" \
  "grep -F 'NoNewPrivileges=yes' /etc/systemd/system/dbf-systemd-extensions-worker.service && grep -F 'PrivateTmp=yes' /etc/systemd/system/dbf-systemd-extensions-worker.service"
assert_remote "timer unit was generated with install and timer sections" \
  "grep -F '[Timer]' /etc/systemd/system/dbf-systemd-extensions-worker.timer && grep -F 'Unit=dbf-systemd-extensions-worker.service' /etc/systemd/system/dbf-systemd-extensions-worker.timer && grep -F 'OnActiveSec=1s' /etc/systemd/system/dbf-systemd-extensions-worker.timer && grep -F 'OnUnitActiveSec=2s' /etc/systemd/system/dbf-systemd-extensions-worker.timer && grep -F 'WantedBy=timers.target' /etc/systemd/system/dbf-systemd-extensions-worker.timer"
assert_remote "timer is enabled and active" \
  "systemctl is-enabled --quiet dbf-systemd-extensions-worker.timer && systemctl is-active --quiet dbf-systemd-extensions-worker.timer"
assert_remote "timer fired the worker service" \
  "for i in \$(seq 1 30); do test -f /run/debianform-systemd-extensions/timer-ran && test \"\$(cat /run/debianform-systemd-extensions/timer-count)\" -ge 1 && exit 0; sleep 1; done; exit 1"
assert_remote "resolved drop-in is written and service is active" \
  "grep -F '[Resolve]' /etc/systemd/resolved.conf.d/debianform.conf && grep -F 'DNS=1.1.1.1' /etc/systemd/resolved.conf.d/debianform.conf && grep -F 'DNSStubListener=no' /etc/systemd/resolved.conf.d/debianform.conf && systemctl is-enabled --quiet systemd-resolved.service && systemctl is-active --quiet systemd-resolved.service"
assert_remote "journald drop-in is written" \
  "grep -F '[Journal]' /etc/systemd/journald.conf.d/debianform.conf && grep -F 'Compress=yes' /etc/systemd/journald.conf.d/debianform.conf && grep -F 'SystemMaxUse=64M' /etc/systemd/journald.conf.d/debianform.conf"
assert_remote "state records systemd extension resources" \
  "grep -F 'host.cihost.systemd.timer[\\\"dbf-systemd-extensions-worker.timer\\\"]' /var/lib/debianform-integration/systemd-extensions-state.json && grep -F 'host.cihost.systemd.resolved' /var/lib/debianform-integration/systemd-extensions-state.json && grep -F 'host.cihost.systemd.journald' /var/lib/debianform-integration/systemd-extensions-state.json"
