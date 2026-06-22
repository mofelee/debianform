assert_remote "keep-mode apt source file uses Aliyun Debian mirror" \
  "grep -F 'URIs: https://mirrors.aliyun.com/debian/' /etc/apt/sources.list.d/debian.sources"
assert_remote "keep-mode apt source file uses Aliyun Debian security mirror" \
  "grep -F 'URIs: https://mirrors.aliyun.com/debian-security/' /etc/apt/sources.list.d/debian.sources"
assert_remote "keep-mode apt source differs from the saved original" \
  "! cmp -s /etc/apt/sources.list.d/debian.sources /tmp/debianform-original-debian.sources"
assert_remote "keep-mode state records keep destroy behavior" \
  "grep -F '\"on_destroy\": \"keep\"' /var/lib/debianform-integration/apt-source-state.json"
