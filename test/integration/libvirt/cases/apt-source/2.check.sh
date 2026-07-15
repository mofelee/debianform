assert_remote "restore-mode removal restored the original apt source file" \
  "cmp -s /etc/apt/sources.list.d/debianform-integration.sources /tmp/debianform-original-target.sources"
assert_remote "restore-mode removal no longer leaves the target mirror in the source file" \
  "! grep -F 'URIs: $DBF_INTEGRATION_TARGET_APT_MIRROR' /etc/apt/sources.list.d/debianform-integration.sources"
assert_remote "restore-mode final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/apt-source-state.json"
