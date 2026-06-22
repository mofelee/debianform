assert_remote "raw service is active" \
  "systemctl is-active --quiet dbf-raw.service"
assert_remote "raw service is enabled" \
  "systemctl is-enabled --quiet dbf-raw.service"
assert_remote "structured service is active" \
  "systemctl is-active --quiet dbf-structured.service"
assert_remote "structured service is enabled" \
  "systemctl is-enabled --quiet dbf-structured.service"
assert_remote "raw unit file is managed as pure text" \
  "grep -F 'Description=DebianForm Raw Service Unit' /etc/systemd/system/dbf-raw.service && grep -F 'ExecStart=/usr/local/bin/dbf-service-unit-worker raw' /etc/systemd/system/dbf-raw.service"
assert_remote "structured unit file is generated from service_unit" \
  "grep -F 'Description=DebianForm Structured Service Unit' /etc/systemd/system/dbf-structured.service && grep -F 'ExecStart=/usr/local/bin/dbf-service-unit-worker structured' /etc/systemd/system/dbf-structured.service"
assert_remote "structured unit includes generated environment" \
  "grep -F 'Environment=DBF_EXTRA=from-structured' /etc/systemd/system/dbf-structured.service && grep -F 'Environment=DBF_SERVICE_MODE=structured' /etc/systemd/system/dbf-structured.service"
assert_remote "raw service observed runtime environment" \
  "test \"\$(cat /run/debianform-service-unit/raw.mode)\" = 'raw'"
assert_remote "structured service observed runtime environment" \
  "test \"\$(cat /run/debianform-service-unit/structured.mode)\" = 'structured' && test \"\$(cat /run/debianform-service-unit/structured.extra)\" = 'from-structured'"
assert_remote "state records raw and structured service unit resources" \
  "grep -F 'host.cihost.systemd.unit[\\\"dbf-raw.service\\\"]' /var/lib/debianform-integration/service-unit-state.json && grep -F 'host.cihost.systemd.unit[\\\"dbf-structured.service\\\"]' /var/lib/debianform-integration/service-unit-state.json"
