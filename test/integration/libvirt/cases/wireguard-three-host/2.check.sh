assert_remote wg-a "wg-a has both WireGuard interfaces" \
  "ip -4 address show dev wg-ab | grep -F '10.90.0.1/30' && ip -4 address show dev wg-ac | grep -F '10.90.0.5/30'"
assert_remote wg-b "wg-b has one WireGuard interface with two addresses" \
  "ip -4 address show dev wg-mesh | grep -F '10.90.0.2/30' && ip -4 address show dev wg-mesh | grep -F '10.90.0.9/30'"
assert_remote wg-c "wg-c has one WireGuard interface with two addresses" \
  "ip -4 address show dev wg-mesh | grep -F '10.90.0.6/30' && ip -4 address show dev wg-mesh | grep -F '10.90.0.10/30'"
assert_remote wg-a "wg-a can reach wg-b through the first component instance" \
  "ping -c 5 -W 1 10.90.0.2"
assert_remote wg-a "wg-a can reach wg-c through the second component instance" \
  "ping -c 5 -W 1 10.90.0.6"
assert_remote wg-b "wg-b can reach both peers through one interface" \
  "ping -c 5 -W 1 10.90.0.1 && ping -c 5 -W 1 10.90.0.10"
assert_remote wg-c "wg-c can reach both peers through one interface" \
  "ping -c 5 -W 1 10.90.0.5 && ping -c 5 -W 1 10.90.0.9"
assert_remote wg-b "wg-b WireGuard interface is managed by networkd" \
  "networkctl status wg-mesh --no-pager | grep -E 'State:.*(routable|configured|degraded)'"
assert_remote wg-c "wg-c WireGuard interface is managed by networkd" \
  "networkctl status wg-mesh --no-pager | grep -E 'State:.*(routable|configured|degraded)'"
