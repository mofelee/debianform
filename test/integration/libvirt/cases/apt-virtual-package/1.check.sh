assert_remote "awk virtual package is satisfied by the mawk provider" \
  "dpkg-query -W -f='\${Status}' mawk | grep -F 'install ok installed'"
assert_remote "awk itself is not an installed binary package" \
  "! dpkg-query -W -f='\${binary:Package}\t\${Status}\n' awk 2>/dev/null | grep -F 'awk	install ok installed'"
assert_remote "apt virtual package state records the provider" \
  "grep -F 'host.cihost.packages.install[\\\"awk\\\"]' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"package\": \"mawk\"' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"virtual\": true' /var/lib/debianform-integration/apt-virtual-package-state.json"
assert_remote "apt virtual package state keeps declared virtual package desired" \
  "grep -F '\"name\": \"awk\"' /var/lib/debianform-integration/apt-virtual-package-state.json"
