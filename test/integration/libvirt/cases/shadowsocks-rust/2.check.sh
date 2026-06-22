assert_remote "shadowsocks-rust service is stopped" \
  "! systemctl is-active --quiet shadowsocks-rust.service"
assert_remote "shadowsocks-rust service is disabled" \
  "! systemctl is-enabled --quiet shadowsocks-rust.service"
assert_remote "shadowsocks-rust unit and config remain managed after stop" \
  "test -f /etc/systemd/system/shadowsocks-rust.service && test -f /etc/shadowsocks-rust/server.json"
assert_remote "shadowsocks-rust state records stopped service desired state" \
  "grep -F 'host.cihost.components.shadowsocks_rust.services.service[\\\"shadowsocks-rust\\\"]' /var/lib/debianform-integration/shadowsocks-rust-state.json"
