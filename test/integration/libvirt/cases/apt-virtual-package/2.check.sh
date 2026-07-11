assert_remote "awk virtual package remains satisfied by mawk after repeated apply" \
  "dpkg-query -W -f='\${Status}' mawk | grep -F 'install ok installed'"
assert_remote "apt virtual package repeated state records the provider" \
  "grep -F 'host.cihost.packages.install[\\\"awk\\\"]' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"package\": \"mawk\"' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"virtual\": true' /var/lib/debianform-integration/apt-virtual-package-state.json"
run_remote "remove apt virtual package integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "apt virtual package integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
