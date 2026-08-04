assert_remote "services are running before the unit change is applied" \
  "systemctl is-active --quiet dbf-raw.service && systemctl is-active --quiet dbf-structured.service"
run_remote "capture structured service PID before unit change" \
  "systemctl show dbf-structured.service -p MainPID --value > /run/debianform-service-unit/structured.pid.before-change"
