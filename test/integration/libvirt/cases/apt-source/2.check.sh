assert_remote "restore-mode removal restored the original apt source file" \
  "cmp -s /etc/apt/sources.list.d/debian.sources /tmp/debianform-original-debian.sources"
assert_remote "restore-mode removal no longer leaves Aliyun in the source file" \
  "! grep -F 'mirrors.aliyun.com' /etc/apt/sources.list.d/debian.sources"
assert_remote "restore-mode final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/apt-source-state.json"
