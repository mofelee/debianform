run_remote "drift only the WAN component file" \
  "printf '%s\n' '# drifted outside DebianForm' > /etc/systemd/network/20-dbf-wan.network"
