assert_remote "nftables package is installed" \
  "dpkg-query -W -f='\${Status}' nftables | grep -F 'install ok installed'"
assert_remote "nftables service is enabled" \
  "systemctl is-enabled nftables"
assert_remote "nftables service is active" \
  "systemctl is-active nftables"
assert_remote "nftables main ruleset file exists" \
  "test -f /etc/nftables.conf"
assert_remote "nftables managed snippet exists with initial port" \
  "grep -F 'tcp dport 443 accept' /etc/nftables.d/debianform-input.nft"
assert_remote "live nftables ruleset contains initial port" \
  "nft list ruleset | grep -F 'tcp dport 443 accept'"
assert_remote "state records nftables snippet address" \
  "grep -F 'host.cihost.nftables.file[\\\"20-debianform-input\\\"]' /var/lib/debianform-integration/nftables-state.json"
assert_remote "state records nftables enable service address" \
  "grep -F 'host.cihost.nftables.enable' /var/lib/debianform-integration/nftables-state.json"
