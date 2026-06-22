assert_remote "tcp_bbr module persistence was destroyed" \
  "test ! -e /etc/modules-load.d/dbf-tcp_bbr.conf"
assert_remote "default qdisc sysctl persistence was destroyed" \
  "test ! -e /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf"
assert_remote "TCP congestion control sysctl persistence was destroyed" \
  "test ! -e /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf"
assert_remote "bbr final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/bbr-state.json"
run_remote "remove bbr integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "bbr integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
