assert_remote wg-a "wireguard-tools is installed on wg-a" \
  "dpkg-query -W -f='\${Status}' wireguard-tools | grep -F 'install ok installed'"
assert_remote wg-b "wireguard-tools is installed on wg-b" \
  "dpkg-query -W -f='\${Status}' wireguard-tools | grep -F 'install ok installed'"
assert_remote wg-a "wg-a config was deployed as a secret with strict permissions" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard/wg0.conf)\" = '600 root root'"
assert_remote wg-b "wg-b config was deployed as a secret with strict permissions" \
  "test \"\$(stat -c '%a %U %G' /etc/wireguard/wg0.conf)\" = '600 root root'"
assert_remote wg-a "wg-a service remains stopped after config-only step" \
  "! systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-b "wg-b service remains stopped after config-only step" \
  "! systemctl is-active --quiet wg-quick@wg0.service"
assert_remote wg-a "wg-a state records secret and package resources without private key plaintext" \
  "grep -F 'host.wg-a.components.wireguard.secrets.file[\\\"/etc/wireguard/wg0.conf\\\"]' /var/lib/debianform-integration/wireguard-a-state.json && grep -F 'host.wg-a.components.wireguard.packages.install[\\\"wireguard-tools\\\"]' /var/lib/debianform-integration/wireguard-a-state.json && ! grep -F 'PrivateKey' /var/lib/debianform-integration/wireguard-a-state.json"
assert_remote wg-b "wg-b state records secret and package resources without private key plaintext" \
  "grep -F 'host.wg-b.components.wireguard.secrets.file[\\\"/etc/wireguard/wg0.conf\\\"]' /var/lib/debianform-integration/wireguard-b-state.json && grep -F 'host.wg-b.components.wireguard.packages.install[\\\"wireguard-tools\\\"]' /var/lib/debianform-integration/wireguard-b-state.json && ! grep -F 'PrivateKey' /var/lib/debianform-integration/wireguard-b-state.json"
