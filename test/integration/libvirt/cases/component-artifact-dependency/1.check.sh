python3 "$CASE_DIR/assert-plan.py" initial "$LOG_DIR/1.pre-apply-plan.json"

grep -F 'depends_on: host.cihost.components.artifact_tool.artifact.install["/usr/local/bin/dbf-artifact-tool"]' \
  "$LOG_DIR/1.pre-apply-plan.txt" >/dev/null
grep -F 'depends_on: <code>host.cihost.components.artifact_tool.artifact.install[&#34;/usr/local/bin/dbf-artifact-tool&#34;]' \
  "$LOG_DIR/1.pre-apply-plan.html" >/dev/null

assert_remote "cross-component artifact install placed the executable" \
  "test -x /usr/local/bin/dbf-artifact-tool"
assert_remote "cross-component artifact dependency services are active and enabled" \
  "systemctl is-active --quiet dbf-artifact-alpha.service && systemctl is-enabled --quiet dbf-artifact-alpha.service && systemctl is-active --quiet dbf-artifact-beta.service && systemctl is-enabled --quiet dbf-artifact-beta.service"
assert_remote "cross-component artifact dependency services executed the installed binary" \
  "grep -Fx 'tag=alpha' /tmp/dbf-artifact-alpha.marker && grep -Fx 'tag=beta' /tmp/dbf-artifact-beta.marker"
assert_remote "state persisted the cross-component artifact install dependency" \
  "grep -F '\"host.cihost.components.artifact_tool.artifact.install[\\\"/usr/local/bin/dbf-artifact-tool\\\"]\"' /var/lib/debianform-integration/component-artifact-dependency-state.json"
