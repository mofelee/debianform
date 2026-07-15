assert_remote "core file drift was repaired and content updated" \
  "test \"\$(cat /tmp/debianform-core-libvirt.txt)\" = 'core libvirt updated'"
assert_remote "core file mode was updated" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-core-libvirt.txt)\" = '600 root root'"
if [[ "$EXPECTED_DISTRIBUTION" == "ubuntu" ]]; then
  assert_remote "non-network drift repair left stock Netplan files byte-for-byte unchanged" \
    "set -eu; export LC_ALL=C; find /etc/netplan -maxdepth 1 -type f -name '*.yaml' -print0 | sort -z | xargs -0 -r sha256sum > /tmp/debianform-netplan.current; cmp -s /tmp/debianform-netplan.sha256 /tmp/debianform-netplan.current; rm -f /tmp/debianform-netplan.current"
fi
