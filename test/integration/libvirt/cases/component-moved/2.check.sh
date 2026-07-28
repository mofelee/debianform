python3 "$SCRIPT_DIR/assert-moved-plan.py" \
  "$LOG_DIR/2.pre-apply-plan.json" \
  cihost \
  host.cihost.components.bird2_babel \
  host.cihost.components.bird2_ospfv3 \
  7 \
  'host.cihost.components.bird2_ospfv3.files.file["/etc/debianform-moved/bird.conf"]' \
  'host.cihost.components.bird2_ospfv3.script["reload_bird"]'
assert_remote "rename applied the one real OSPFv3 configuration update" \
  "grep -F 'protocol ospf v3 edge' /etc/debianform-moved/bird.conf && ! grep -Fq 'protocol babel edge' /etc/debianform-moved/bird.conf"
assert_remote "the real file update triggered reload_bird exactly once" \
  "test \"\$(cat /var/lib/debianform-moved/reload.count)\" = 2 && test \"\$(cat /var/lib/debianform-moved/component.name)\" = bird2_ospfv3"
assert_remote "unchanged file retained its remote identity across the rename" \
  "test \"\$(stat -c '%d:%i' /etc/debianform-moved/stable.conf)\" = \"\$(cat /var/lib/debianform-moved/stable.identity)\""
assert_remote "package and service stayed installed and active" \
  "dpkg-query -W -f='\${Status}' openssh-server | grep -Fx 'install ok installed' && systemctl is-enabled --quiet ssh.service && systemctl is-active --quiet ssh.service"
assert_remote "state moved every resource to the new component prefix" \
  "! grep -Fq 'host.cihost.components.bird2_babel' /var/lib/debianform-integration/component-moved-state.json && grep -F 'host.cihost.components.bird2_ospfv3.packages.install' /var/lib/debianform-integration/component-moved-state.json && grep -F 'host.cihost.components.bird2_ospfv3.services.service' /var/lib/debianform-integration/component-moved-state.json && grep -F 'host.cihost.components.bird2_ospfv3.script' /var/lib/debianform-integration/component-moved-state.json"
