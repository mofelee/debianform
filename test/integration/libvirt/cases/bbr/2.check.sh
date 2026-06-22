assert_remote "default qdisc drift was repaired" \
  "test \"\$(sysctl -n net.core.default_qdisc)\" = 'fq'"
assert_remote "TCP congestion control drift was repaired" \
  "test \"\$(sysctl -n net.ipv4.tcp_congestion_control)\" = 'bbr'"
assert_remote "tcp_bbr module persistence was repaired" \
  "test \"\$(cat /etc/modules-load.d/dbf-tcp_bbr.conf)\" = 'tcp_bbr'"
assert_remote "default qdisc persistence was repaired" \
  "test \"\$(cat /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf)\" = 'net.core.default_qdisc = fq'"
assert_remote "TCP congestion control persistence was repaired" \
  "test \"\$(cat /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf)\" = 'net.ipv4.tcp_congestion_control = bbr'"
