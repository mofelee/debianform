assert_remote "dnsutils virtual package remains satisfied by bind9-dnsutils after repeated apply" \
  "dpkg-query -W -f='\${Status}' bind9-dnsutils | grep -F 'install ok installed'"
assert_remote "apt virtual package repeated state records the provider" \
  "grep -F 'host.cihost.packages.install[\\\"dnsutils\\\"]' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"package\": \"bind9-dnsutils\"' /var/lib/debianform-integration/apt-virtual-package-state.json && grep -F '\"virtual\": true' /var/lib/debianform-integration/apt-virtual-package-state.json"
run_remote "remove apt virtual package integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "apt virtual package integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
