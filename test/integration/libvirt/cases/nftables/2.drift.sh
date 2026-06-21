run_remote "write drift to the managed nftables snippet" \
  "sed -i 's/tcp dport 443 accept/tcp dport 1234 accept/' /etc/nftables.d/debianform-input.nft"
