assert_remote "nftables managed snippet was repaired and updated" \
  "grep -F 'tcp dport 8080 accept' /etc/nftables.d/debianform-input.nft"
assert_remote "nftables managed snippet no longer contains drift" \
  "! grep -F 'tcp dport 1234 accept' /etc/nftables.d/debianform-input.nft"
assert_remote "live nftables ruleset was activated with updated port" \
  "nft list ruleset | grep -F 'tcp dport 8080 accept'"
assert_remote "live nftables ruleset no longer contains initial port" \
  "! nft list ruleset | grep -F 'tcp dport 443 accept'"
