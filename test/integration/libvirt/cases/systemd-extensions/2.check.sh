assert_remote "timer drift was repaired and timer stayed enabled" \
  "grep -F 'Description=DebianForm systemd extension timer v2' /etc/systemd/system/dbf-systemd-extensions-worker.timer && grep -F 'OnActiveSec=1s' /etc/systemd/system/dbf-systemd-extensions-worker.timer && grep -F 'OnUnitActiveSec=3s' /etc/systemd/system/dbf-systemd-extensions-worker.timer && grep -F 'AccuracySec=2s' /etc/systemd/system/dbf-systemd-extensions-worker.timer && systemctl is-enabled --quiet dbf-systemd-extensions-worker.timer && systemctl is-active --quiet dbf-systemd-extensions-worker.timer"
assert_remote "service unit content was updated" \
  "grep -F 'Description=DebianForm systemd extension timer worker v2' /etc/systemd/system/dbf-systemd-extensions-worker.service && grep -F 'NoNewPrivileges=yes' /etc/systemd/system/dbf-systemd-extensions-worker.service"
assert_remote "resolved drift was repaired" \
  "grep -F 'DNS=9.9.9.9' /etc/systemd/resolved.conf.d/debianform.conf && grep -F 'DNSSEC=allow-downgrade' /etc/systemd/resolved.conf.d/debianform.conf && ! grep -F 'DNS=8.8.8.8' /etc/systemd/resolved.conf.d/debianform.conf && systemctl is-active --quiet systemd-resolved.service"
assert_remote "journald drift was repaired" \
  "grep -F 'SystemMaxUse=128M' /etc/systemd/journald.conf.d/debianform.conf && ! grep -F 'SystemMaxUse=1M' /etc/systemd/journald.conf.d/debianform.conf"
assert_remote "updated timer fired v2 worker" \
  "for i in \$(seq 1 30); do test \"\$(cat /run/debianform-systemd-extensions/version 2>/dev/null)\" = v2 && exit 0; sleep 1; done; exit 1"
