assert_remote "core managed file exists with initial content" \
  "test \"\$(cat /tmp/debianform-core-libvirt.txt)\" = 'core libvirt smoke'"
assert_remote "core managed file has expected mode" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-core-libvirt.txt)\" = '644 root root'"
assert_remote "state records the managed file address" \
  "grep -F 'host.cihost.files.file[\\\"/tmp/debianform-core-libvirt.txt\\\"]' /var/lib/debianform-integration/core-state.json"
if [[ "$EXPECTED_DISTRIBUTION" == "ubuntu" ]]; then
  assert_remote "non-network apply left stock Netplan files byte-for-byte unchanged" \
    "set -eu; export LC_ALL=C; find /etc/netplan -maxdepth 1 -type f -name '*.yaml' -print0 | sort -z | xargs -0 -r sha256sum > /tmp/debianform-netplan.current; cmp -s /tmp/debianform-netplan.sha256 /tmp/debianform-netplan.current; rm -f /tmp/debianform-netplan.current"
fi
