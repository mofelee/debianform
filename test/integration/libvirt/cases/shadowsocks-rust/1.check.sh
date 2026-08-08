assert_remote "ssserver binary was installed from the GitHub release asset" \
  "test \"\$(/usr/local/bin/ssserver --version)\" = 'shadowsocks 1.24.0'"
assert_remote "shadowsocks-rust config was deployed with expected ownership and mode" \
  "test \"\$(stat -c '%a %U %G' /etc/shadowsocks-rust/server.json)\" = '640 root shadowsocks'"
assert_remote "shadowsocks-rust user has the managed primary and supplementary groups" \
  "test \"\$(id -gn shadowsocks)\" = 'shadowsocks' && id -nG shadowsocks | tr ' ' '\\n' | grep -qx 'shadowsocks-observers'"
assert_remote "shadowsocks-rust service unit was generated" \
  "grep -F 'ExecStart=/usr/local/bin/ssserver -c /etc/shadowsocks-rust/server.json' /etc/systemd/system/shadowsocks-rust.service"
assert_remote "shadowsocks-rust service is active" \
  "systemctl is-active --quiet shadowsocks-rust.service"
assert_remote "shadowsocks-rust service is enabled" \
  "systemctl is-enabled --quiet shadowsocks-rust.service"
assert_remote "shadowsocks-rust listens on TCP port 18388" \
  "grep -qi ':47D4 ' /proc/net/tcp /proc/net/tcp6"
assert_remote "shadowsocks-rust state records artifact install and service resources" \
  "grep -F 'host.cihost.components.shadowsocks_rust.artifact.install[\\\"/usr/local/bin/ssserver\\\"]' /var/lib/debianform-integration/shadowsocks-rust-state.json && grep -F 'host.cihost.components.shadowsocks_rust.services.service[\\\"shadowsocks-rust\\\"]' /var/lib/debianform-integration/shadowsocks-rust-state.json"
assert_remote "shadowsocks-rust runtime facts were discovered from the target host" \
  "grep -F '\"architecture\": \"${DBF_INTEGRATION_TARGET_ARCHITECTURE}\"' /var/lib/debianform-integration/shadowsocks-rust-state.json && grep -F '\"codename\": \"${DBF_INTEGRATION_TARGET_CODENAME}\"' /var/lib/debianform-integration/shadowsocks-rust-state.json"
assert_remote "component staging root is on a larger filesystem than constrained /tmp" \
  "tmp_device=\$(stat -c '%d' /tmp); staging_device=\$(stat -c '%d' /var/lib/debianform-integration/component-staging); tmp_blocks=\$(df -Pk /tmp | awk 'NR == 2 { print \$2 }'); staging_blocks=\$(df -Pk /var/lib/debianform-integration/component-staging | awk 'NR == 2 { print \$2 }'); test \"\$tmp_device\" != \"\$staging_device\" && test \"\$tmp_blocks\" -le 1024 && test \"\$staging_blocks\" -gt \"\$tmp_blocks\""
assert_remote "installed binary is larger than the constrained /tmp filesystem" \
  "test \"\$(stat -c '%s' /usr/local/bin/ssserver)\" -gt 1048576"
assert_remote "component staging workspaces were removed after installation" \
  "test -z \"\$(find /var/lib/debianform-integration/component-staging -mindepth 1 -maxdepth 1 -print -quit)\""
run_remote "unmount constrained component staging test /tmp" \
  "umount /tmp"
assert_remote "constrained component staging test /tmp was unmounted" \
  "! grep -q '^dbf-issue87-tmpfs /tmp tmpfs ' /proc/mounts"
