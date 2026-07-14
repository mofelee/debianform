assert_remote "restore-mode apt source file uses the target mirror" \
  "grep -F 'URIs: $DBF_INTEGRATION_TARGET_APT_MIRROR' /etc/apt/sources.list.d/debianform-integration.sources"
assert_remote "restore-mode apt source file uses the target security mirror" \
  "grep -F 'URIs: $DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR' /etc/apt/sources.list.d/debianform-integration.sources"
assert_remote "restore-mode apt source differs from the saved original" \
  "! cmp -s /etc/apt/sources.list.d/debianform-integration.sources /tmp/debianform-original-target.sources"
assert_remote "restore-mode apt cache refresh fetched target mirror metadata" \
  "find /var/lib/apt/lists -maxdepth 1 -type f -name '*mirrors.aliyun.com_*_dists_${DBF_INTEGRATION_TARGET_CODENAME}_InRelease' | grep -q ."
assert_remote "restore-mode state records the apt source file address" \
  "grep -F 'host.cihost.apt.source_file[\\\"main\\\"]' /var/lib/debianform-integration/apt-source-state.json"
assert_remote "restore-mode state records restore destroy behavior" \
  "grep -F '\"on_destroy\": \"restore\"' /var/lib/debianform-integration/apt-source-state.json"
