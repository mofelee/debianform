assert_remote "ssserver binary was installed from the GitHub release asset" \
  "test \"\$(/usr/local/bin/ssserver --version)\" = 'shadowsocks 1.24.0'"
assert_remote "shadowsocks-rust config was deployed with expected ownership and mode" \
  "test \"\$(stat -c '%a %U %G' /etc/shadowsocks-rust/server.json)\" = '640 root shadowsocks'"
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
