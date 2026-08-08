python3 "$CASE_DIR/assert-plan.py" no-op "$LOG_DIR/2.pre-apply-plan.json"

assert_remote "no-op reapply kept the DebianForm-managed conffile" \
  "grep -Fx '# Managed by DebianForm'\''s resource-dependencies integration case.' /etc/apache2/ports.conf && grep -Fx 'Listen 8080' /etc/apache2/ports.conf"
assert_remote "no-op reapply kept apache2 enabled and running" \
  "systemctl is-enabled --quiet apache2.service && systemctl is-active --quiet apache2.service"
