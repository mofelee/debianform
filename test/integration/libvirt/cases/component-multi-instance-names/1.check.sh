assert_remote "per-instance unit files use resolved names" \
  "test -f /etc/systemd/system/dbf-transport-alpha.service && test -f /etc/systemd/system/dbf-transport-beta.service"
assert_remote "per-instance services are active and enabled" \
  "systemctl is-active --quiet dbf-transport-alpha.service && systemctl is-enabled --quiet dbf-transport-alpha.service && systemctl is-active --quiet dbf-transport-beta.service && systemctl is-enabled --quiet dbf-transport-beta.service"
assert_remote "per-instance unit descriptions were generated from resolved names" \
  "grep -F 'Description=DebianForm multi-instance transport alpha' /etc/systemd/system/dbf-transport-alpha.service && grep -F 'Description=DebianForm multi-instance transport beta' /etc/systemd/system/dbf-transport-beta.service"
assert_remote "per-instance directories use resolved paths" \
  "test -d /var/lib/debianform-multi/alpha && test -d /var/lib/debianform-multi/beta"
assert_remote "per-instance files use resolved paths" \
  "grep -F 'tag=alpha' /etc/debianform-multi-alpha.conf && grep -F 'tag=beta' /etc/debianform-multi-beta.conf"
assert_remote "state records resolved per-instance resource addresses" \
  "grep -F 'host.cihost.components.alpha.systemd.unit[\\\"dbf-transport-alpha.service\\\"]' /var/lib/debianform-integration/multi-instance-state.json && grep -F 'host.cihost.components.beta.systemd.unit[\\\"dbf-transport-beta.service\\\"]' /var/lib/debianform-integration/multi-instance-state.json && grep -F 'host.cihost.components.alpha.directories.directory[\\\"/var/lib/debianform-multi/alpha\\\"]' /var/lib/debianform-integration/multi-instance-state.json && grep -F 'host.cihost.components.beta.services.service[\\\"dbf-transport-beta\\\"]' /var/lib/debianform-integration/multi-instance-state.json"
