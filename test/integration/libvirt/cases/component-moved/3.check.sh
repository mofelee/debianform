python3 "$SCRIPT_DIR/assert-noop-plan.py" "$LOG_DIR/3.pre-apply-plan.json"
assert_remote "removing the moved block kept the migrated state and remote objects stable" \
  "test \"\$(cat /var/lib/debianform-moved/reload.count)\" = 2 && test \"\$(cat /var/lib/debianform-moved/component.name)\" = bird2_ospfv3 && ! grep -Fq 'host.cihost.components.bird2_babel' /var/lib/debianform-integration/component-moved-state.json"
assert_remote "removed moved block did not replace the unchanged file" \
  "test \"\$(stat -c '%d:%i' /etc/debianform-moved/stable.conf)\" = \"\$(cat /var/lib/debianform-moved/stable.identity)\""
