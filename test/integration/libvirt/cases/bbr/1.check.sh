assert_remote "tcp_bbr module is loaded" \
  "awk '\$1 == \"tcp_bbr\" { found = 1 } END { exit found ? 0 : 1 }' /proc/modules"
assert_remote "default qdisc is fq at runtime" \
  "test \"\$(sysctl -n net.core.default_qdisc)\" = 'fq'"
assert_remote "TCP congestion control is bbr at runtime" \
  "test \"\$(sysctl -n net.ipv4.tcp_congestion_control)\" = 'bbr'"
assert_remote "tcp_bbr module is persisted" \
  "test \"\$(cat /etc/modules-load.d/dbf-tcp_bbr.conf)\" = 'tcp_bbr'"
assert_remote "default qdisc sysctl is persisted" \
  "test \"\$(cat /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf)\" = 'net.core.default_qdisc = fq'"
assert_remote "TCP congestion control sysctl is persisted" \
  "test \"\$(cat /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf)\" = 'net.ipv4.tcp_congestion_control = bbr'"
assert_remote "bbr state records the module address" \
  "grep -F 'host.cihost.kernel.module[\\\"tcp_bbr\\\"]' /var/lib/debianform-integration/bbr-state.json"
assert_remote "bbr state records the congestion control sysctl address" \
  "grep -F 'host.cihost.kernel.sysctl[\\\"net.ipv4.tcp_congestion_control\\\"]' /var/lib/debianform-integration/bbr-state.json"
