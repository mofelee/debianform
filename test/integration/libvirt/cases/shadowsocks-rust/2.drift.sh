assert_remote "shadowsocks-rust is running before the stop configuration is applied" \
  "systemctl is-active --quiet shadowsocks-rust.service"
