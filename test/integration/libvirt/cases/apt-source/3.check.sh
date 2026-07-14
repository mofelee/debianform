assert_remote "keep-mode apt source file uses the target mirror" \
  "grep -F 'URIs: $DBF_INTEGRATION_TARGET_APT_MIRROR' /etc/apt/sources.list.d/debianform-integration.sources"
assert_remote "keep-mode apt source file uses the target security mirror" \
  "grep -F 'URIs: $DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR' /etc/apt/sources.list.d/debianform-integration.sources"
assert_remote "keep-mode apt source differs from the saved original" \
  "! cmp -s /etc/apt/sources.list.d/debianform-integration.sources /tmp/debianform-original-target.sources"
assert_remote "keep-mode state records keep destroy behavior" \
  "grep -F '\"on_destroy\": \"keep\"' /var/lib/debianform-integration/apt-source-state.json"
