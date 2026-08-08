python3 "$CASE_DIR/assert-plan.py" no-op "$LOG_DIR/2.pre-apply-plan.json"

assert_remote "no-op reapply kept the DebianForm-managed conffile" \
  "grep -Fx '# Managed by DebianForm'\''s resource-dependencies integration case.' /etc/default/cron && grep -Fx 'READ_ENV=\"yes\"' /etc/default/cron"
assert_remote "no-op reapply kept cron enabled and running" \
  "systemctl is-enabled --quiet cron.service && systemctl is-active --quiet cron.service"
