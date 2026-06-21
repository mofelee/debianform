assert_remote "nftables service was disabled after removal from config" \
  "! systemctl is-enabled nftables"
assert_remote "nftables service was stopped after removal from config" \
  "! systemctl is-active nftables"
assert_remote "nftables managed main file was destroyed" \
  "test ! -e /etc/nftables.conf"
assert_remote "nftables managed snippet was destroyed" \
  "test ! -e /etc/nftables.d/debianform-input.nft"
assert_remote "nftables final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/nftables-state.json"
run_remote "remove nftables integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "nftables integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
