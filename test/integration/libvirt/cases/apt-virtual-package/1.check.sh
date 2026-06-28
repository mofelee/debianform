assert_remote "dnsutils virtual package installed bind9-dnsutils provider" \
  "dpkg-query -W -f='\${Status}' bind9-dnsutils | grep -F 'install ok installed'"
assert_remote "dnsutils itself is still not a binary package" \
  "! dpkg-query -W dnsutils >/dev/null 2>&1"
assert_remote "apt virtual package state records the provider" \
  "grep -F 'host.cihost.packages.install[\\\"dnsutils\\\"]' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"package\": \"bind9-dnsutils\"' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"virtual\": true' /var/lib/debianform-integration/apt-virtual-package-state.json"
assert_remote "apt virtual package state keeps declared virtual package desired" \
  "grep -F '\"name\": \"dnsutils\"' /var/lib/debianform-integration/apt-virtual-package-state.json"
