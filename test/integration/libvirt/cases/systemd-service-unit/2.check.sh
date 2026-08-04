if ! grep -F 'host.cihost.components.service_unit_fixture.systemd.unit[\"dbf-structured.service\"].change_action' "$LOG_DIR/2.pre-apply-plan.json" >/dev/null; then
  fail "pre-apply plan did not expose the structured service change action"
fi
assert_remote "services remain active and enabled after unit update" \
  "systemctl is-active --quiet dbf-raw.service && systemctl is-enabled --quiet dbf-raw.service && systemctl is-active --quiet dbf-structured.service && systemctl is-enabled --quiet dbf-structured.service"
assert_remote "structured unit contains the updated environment" \
  "grep -F 'Environment=DBF_EXTRA=from-updated-unit' /etc/systemd/system/dbf-structured.service"
assert_remote "running structured process observed the updated unit" \
  "test \"\$(cat /run/debianform-service-unit/structured.extra)\" = 'from-updated-unit'"
assert_remote "structured service restarted exactly once for the unit change" \
  "test \"\$(cat /run/debianform-service-unit/raw.starts)\" = 1 && test \"\$(cat /run/debianform-service-unit/structured.starts)\" = 2 && before=\$(cat /run/debianform-service-unit/structured.pid.before-change) && current=\$(systemctl show dbf-structured.service -p MainPID --value) && test \"\$before\" != \"\$current\""
assert_remote "state records the updated change action completion" \
  "grep -F '\"change_action_digest\":' /var/lib/debianform-integration/service-unit-state.json"
