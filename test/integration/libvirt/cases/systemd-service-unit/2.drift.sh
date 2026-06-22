assert_remote "services are running before the stop configuration is applied" \
  "systemctl is-active --quiet dbf-raw.service && systemctl is-active --quiet dbf-structured.service"
