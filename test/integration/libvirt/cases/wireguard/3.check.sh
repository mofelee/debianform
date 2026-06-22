assert_remote wg-a "wg-a service drift was repaired" \
  "systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-b "wg-b service remains active after wg-a repair" \
  "systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-a "wg-a can reach wg-b after drift repair" \
  "ping -c 5 -W 1 10.80.0.2"
assert_remote wg-b "wg-b can reach wg-a after drift repair" \
  "ping -c 5 -W 1 10.80.0.1"
