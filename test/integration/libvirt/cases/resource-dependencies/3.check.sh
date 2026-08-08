python3 "$CASE_DIR/assert-plan.py" policy-update "$LOG_DIR/3.pre-apply-plan.json"

assert_remote "keep policy completed a real cron package upgrade" \
  "test \"\$(dpkg-query -W -f='\${Version}' cron)\" = \"\$(cat /tmp/debianform-cron-version.before)\" && test \"\$(dpkg-query -W -f='\${Version}' cron)\" != 0"
assert_remote "keep policy retained the DebianForm-managed conffile" \
  "grep -Fx '# Managed by DebianForm'\''s resource-dependencies integration case.' /etc/default/cron && grep -Fx 'READ_ENV=\"yes\"' /etc/default/cron"
assert_remote "keep policy left cron enabled and running" \
  "systemctl is-enabled --quiet cron.service && systemctl is-active --quiet cron.service"
assert_remote "keep policy is explicit in persisted package state" \
  "grep -F '\"conffile_policy\": \"keep\"' /var/lib/debianform-integration/resource-dependencies-state.json"
run_remote "install reverse dependency removal guard" \
  "set -eu; printf '%s\n' '#!/bin/sh' 'set -eu' 'if systemctl is-active --quiet cron.service || [ -e /etc/default/cron ]; then' '  echo cron dependency removal order violated >&2' '  exit 1' 'fi' 'touch /tmp/debianform-cron-removal-order.ok' > /usr/local/sbin/dbf-assert-cron-removal-order; chmod 0755 /usr/local/sbin/dbf-assert-cron-removal-order; printf '%s\n' 'DPkg::Pre-Invoke { \"/usr/local/sbin/dbf-assert-cron-removal-order\"; };' > /etc/apt/apt.conf.d/99-debianform-resource-dependency-order"
assert_remote "reverse dependency removal guard is active" \
  "test -x /usr/local/sbin/dbf-assert-cron-removal-order && test -f /etc/apt/apt.conf.d/99-debianform-resource-dependency-order"
