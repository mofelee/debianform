run_remote wg-a "remove wg-a networkd netdev file to create drift" \
  "rm -f /etc/systemd/network/10-wg0.netdev && networkctl reload"
assert_remote wg-a "wg-a networkd file drift is present before repair" \
  "test ! -e /etc/systemd/network/10-wg0.netdev"
