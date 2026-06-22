run_remote wg-a "stop wg-a WireGuard service to create service drift" \
  "systemctl stop wg-quick@wg0.service"
assert_remote wg-a "wg-a service drift is present before repair" \
  "! systemctl is-active --quiet wg-quick@wg0.service"
