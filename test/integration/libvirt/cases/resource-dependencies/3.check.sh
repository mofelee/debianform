python3 "$CASE_DIR/assert-plan.py" policy-update "$LOG_DIR/3.pre-apply-plan.json"

assert_remote "keep policy completed a real apache2 package upgrade" \
  "test \"\$(dpkg-query -W -f='\${Version}' apache2)\" = \"\$(cat /tmp/debianform-apache2-version.before)\" && test \"\$(dpkg-query -W -f='\${Version}' apache2)\" != 0"
assert_remote "keep policy retained the DebianForm-managed conffile" \
  "grep -Fx '# Managed by DebianForm'\''s resource-dependencies integration case.' /etc/apache2/ports.conf && grep -Fx 'Listen 8080' /etc/apache2/ports.conf"
assert_remote "keep policy left apache2 enabled and running" \
  "systemctl is-enabled --quiet apache2.service && systemctl is-active --quiet apache2.service"
assert_remote "keep policy is explicit in persisted package state" \
  "grep -F '\"conffile_policy\": \"keep\"' /var/lib/debianform-integration/resource-dependencies-state.json"
run_remote "install reverse dependency removal guard" \
  "set -eu; printf '%s\n' '#!/bin/sh' 'set -eu' 'if systemctl is-active --quiet apache2.service || [ -e /etc/apache2/ports.conf ]; then' '  echo apache2 dependency removal order violated >&2' '  exit 1' 'fi' 'touch /tmp/debianform-apache2-removal-order.ok' > /usr/local/sbin/dbf-assert-apache2-removal-order; chmod 0755 /usr/local/sbin/dbf-assert-apache2-removal-order; printf '%s\n' 'DPkg::Pre-Invoke { \"/usr/local/sbin/dbf-assert-apache2-removal-order\"; };' > /etc/apt/apt.conf.d/99-debianform-resource-dependency-order"
assert_remote "reverse dependency removal guard is active" \
  "test -x /usr/local/sbin/dbf-assert-apache2-removal-order && test -f /etc/apt/apt.conf.d/99-debianform-resource-dependency-order"
