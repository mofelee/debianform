python3 "$CASE_DIR/assert-plan.py" destroy "$LOG_DIR/2.pre-apply-plan.json"

assert_remote "reverse dependency removal stopped the runner while the artifact was still installed" \
  "test -f /tmp/dbf-artifact-stop.ok"
assert_remote "component artifact dependency services were stopped and disabled" \
  "! systemctl is-active --quiet dbf-artifact-alpha.service && ! systemctl is-enabled --quiet dbf-artifact-alpha.service && ! systemctl is-active --quiet dbf-artifact-beta.service && ! systemctl is-enabled --quiet dbf-artifact-beta.service"
assert_remote "component artifact binary and generated unit files were removed" \
  "test ! -e /usr/local/bin/dbf-artifact-tool && test ! -e /etc/systemd/system/dbf-artifact-alpha.service && test ! -e /etc/systemd/system/dbf-artifact-beta.service"
assert_remote "component artifact dependency final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/component-artifact-dependency-state.json"
run_remote "remove component artifact dependency integration artifacts after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /tmp/dbf-artifact-alpha.marker /tmp/dbf-artifact-beta.marker /tmp/dbf-artifact-stop.ok /usr/local/bin/dbf-artifact-tool"
assert_remote "component artifact dependency cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /tmp/dbf-artifact-alpha.marker && test ! -e /tmp/dbf-artifact-beta.marker && test ! -e /tmp/dbf-artifact-stop.ok && test ! -e /usr/local/bin/dbf-artifact-tool"
