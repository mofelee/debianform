assert_remote "shadowsocks-rust binary was destroyed after removal from config" \
  "test ! -e /usr/local/bin/ssserver"
assert_remote "shadowsocks-rust config and unit were destroyed after removal from config" \
  "test ! -e /etc/shadowsocks-rust/server.json && test ! -e /etc/systemd/system/shadowsocks-rust.service"
assert_remote "shadowsocks-rust final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/shadowsocks-rust-state.json"
run_remote "remove shadowsocks-rust integration state and component cache after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /var/cache/debianform"
assert_remote "shadowsocks-rust integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
