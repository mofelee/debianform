python3 "$CASE_DIR/assert-plan.py" destroy "$LOG_DIR/4.pre-apply-plan.json"

assert_remote "reverse removal stopped and disabled apache2 before package removal" \
  "! systemctl is-active --quiet apache2.service && ! systemctl is-enabled --quiet apache2.service"
assert_remote "reverse removal removed the managed conffile and apache2 package" \
  "test ! -e /etc/apache2/ports.conf && ! dpkg-query -W -f='\${Status}' apache2 2>/dev/null | grep -Fx 'install ok installed'"
assert_remote "package removal guard observed safe reverse dependency order" \
  "test -f /tmp/debianform-apache2-removal-order.ok"
assert_remote "reverse removal left no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/resource-dependencies-state.json"
run_remote "remove resource dependency integration artifacts after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /tmp/debianform-apache2-version.before /tmp/debianform-apache2-removal-order.ok /usr/local/sbin/dbf-assert-apache2-removal-order /etc/apt/apt.conf.d/99-debianform-resource-dependency-order"
assert_remote "resource dependency integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration && test ! -e /tmp/debianform-apache2-version.before && test ! -e /tmp/debianform-apache2-removal-order.ok && test ! -e /usr/local/sbin/dbf-assert-apache2-removal-order && test ! -e /etc/apt/apt.conf.d/99-debianform-resource-dependency-order"
