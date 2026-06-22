assert_remote "keep-mode removal leaves Aliyun apt source content in place" \
  "grep -F 'URIs: https://mirrors.aliyun.com/debian/' /etc/apt/sources.list.d/debian.sources"
assert_remote "keep-mode removal leaves the source file different from the saved original" \
  "! cmp -s /etc/apt/sources.list.d/debian.sources /tmp/debianform-original-debian.sources"
assert_remote "keep-mode final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/apt-source-state.json"
run_remote "remove apt source integration state and saved original after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration /tmp/debianform-original-debian.sources"
assert_remote "apt source integration cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration && test ! -e /tmp/debianform-original-debian.sources"
