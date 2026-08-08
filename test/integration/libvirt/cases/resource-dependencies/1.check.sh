python3 "$CASE_DIR/assert-plan.py" initial "$LOG_DIR/1.pre-apply-plan.json"

grep -F 'depends_on: host.cihost.packages.install["apache2"]' \
  "$LOG_DIR/1.pre-apply-plan.txt" >/dev/null
grep -F 'depends_on: <code>host.cihost.files.file[&#34;/etc/apache2/ports.conf&#34;]' \
  "$LOG_DIR/1.pre-apply-plan.html" >/dev/null

assert_remote "dependency workflow installed apache2" \
  "dpkg-query -W -f='\${Status}' apache2 | grep -Fx 'install ok installed'"
assert_remote "dependency workflow wrote the managed conffile" \
  "grep -Fx '# Managed by DebianForm'\''s resource-dependencies integration case.' /etc/apache2/ports.conf && grep -Fx 'Listen 8080' /etc/apache2/ports.conf"
assert_remote "dependency workflow enabled and started apache2" \
  "systemctl is-enabled --quiet apache2.service && systemctl is-active --quiet apache2.service"
assert_remote "dependency workflow persisted package to file ordering" \
  "grep -F '\"host.cihost.packages.install[\\\"apache2\\\"]\"' /var/lib/debianform-integration/resource-dependencies-state.json"
assert_remote "dependency workflow persisted file to service ordering" \
  "grep -F '\"host.cihost.files.file[\\\"/etc/apache2/ports.conf\\\"]\"' /var/lib/debianform-integration/resource-dependencies-state.json"
