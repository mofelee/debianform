assert_remote "keep-mode removal leaves target apt source content in place" \
  "grep -F 'URIs: $DBF_INTEGRATION_TARGET_APT_MIRROR' /etc/apt/sources.list.d/debianform-integration.sources"
assert_remote "keep-mode removal leaves the source file different from the saved original" \
  "! cmp -s /etc/apt/sources.list.d/debianform-integration.sources /tmp/debianform-original-target.sources"
assert_remote "keep-mode final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/apt-source-state.json"
assert_remote "target native apt source remained unchanged" \
  "cmp -s '$DBF_INTEGRATION_TARGET_APT_SOURCE_PATH' /tmp/debianform-original-target.sources"
run_remote "remove apt source integration state and saved original after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /tmp/debianform-original-target.sources /etc/apt/sources.list.d/debianform-integration.sources"
assert_remote "apt source integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration && test ! -e /tmp/debianform-original-target.sources && test ! -e /etc/apt/sources.list.d/debianform-integration.sources"
