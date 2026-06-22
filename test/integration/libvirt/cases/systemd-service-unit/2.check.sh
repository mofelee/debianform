assert_remote "raw service is stopped" \
  "! systemctl is-active --quiet dbf-raw.service"
assert_remote "raw service is disabled" \
  "! systemctl is-enabled --quiet dbf-raw.service"
assert_remote "structured service is stopped" \
  "! systemctl is-active --quiet dbf-structured.service"
assert_remote "structured service is disabled" \
  "! systemctl is-enabled --quiet dbf-structured.service"
assert_remote "unit files remain managed after stopping services" \
  "test -f /etc/systemd/system/dbf-raw.service && test -f /etc/systemd/system/dbf-structured.service"
assert_remote "state records stopped service desired state" \
  "grep -F 'host.cihost.services.service[\\\"dbf-raw\\\"]' /var/lib/debianform-integration/service-unit-state.json && grep -F 'host.cihost.services.service[\\\"dbf-structured\\\"]' /var/lib/debianform-integration/service-unit-state.json"
