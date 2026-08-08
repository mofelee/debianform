python3 "$CASE_DIR/assert-plan.py" initial "$LOG_DIR/1.pre-apply-plan.json"

grep -F 'depends_on: host.cihost.packages.install["cron"]' \
  "$LOG_DIR/1.pre-apply-plan.txt" >/dev/null
grep -F 'depends_on: <code>host.cihost.files.file[&#34;/etc/default/cron&#34;]' \
  "$LOG_DIR/1.pre-apply-plan.html" >/dev/null

assert_remote "dependency workflow installed cron" \
  "dpkg-query -W -f='\${Status}' cron | grep -Fx 'install ok installed'"
assert_remote "dependency workflow wrote the managed conffile" \
  "grep -Fx '# Managed by DebianForm'\''s resource-dependencies integration case.' /etc/default/cron && grep -Fx 'READ_ENV=\"yes\"' /etc/default/cron"
assert_remote "dependency workflow enabled and started cron" \
  "systemctl is-enabled --quiet cron.service && systemctl is-active --quiet cron.service"
assert_remote "dependency workflow persisted package to file ordering" \
  "grep -F '\"host.cihost.packages.install[\\\"cron\\\"]\"' /var/lib/debianform-integration/resource-dependencies-state.json"
assert_remote "dependency workflow persisted file to service ordering" \
  "grep -F '\"host.cihost.files.file[\\\"/etc/default/cron\\\"]\"' /var/lib/debianform-integration/resource-dependencies-state.json"
