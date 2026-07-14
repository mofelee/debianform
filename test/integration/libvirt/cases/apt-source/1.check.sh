assert_remote "restore-mode apt source file uses Aliyun Debian mirror" \
  "grep -F 'URIs: https://mirrors.aliyun.com/debian/' /etc/apt/sources.list.d/debian.sources"
assert_remote "restore-mode apt source file uses Aliyun Debian security mirror" \
  "grep -F 'URIs: https://mirrors.aliyun.com/debian-security/' /etc/apt/sources.list.d/debian.sources"
assert_remote "restore-mode apt source differs from the saved original" \
  "! cmp -s /etc/apt/sources.list.d/debian.sources /tmp/debianform-original-debian.sources"
assert_remote "restore-mode apt cache refresh fetched Aliyun Debian metadata" \
  "find /var/lib/apt/lists -maxdepth 1 -type f -name '*mirrors.aliyun.com_debian_dists_${DBF_INTEGRATION_TARGET_CODENAME}_InRelease' | grep -q ."
assert_remote "restore-mode state records the apt source file address" \
  "grep -F 'host.cihost.apt.source_file[\\\"main\\\"]' /var/lib/debianform-integration/apt-source-state.json"
assert_remote "restore-mode state records restore destroy behavior" \
  "grep -F '\"on_destroy\": \"restore\"' /var/lib/debianform-integration/apt-source-state.json"
