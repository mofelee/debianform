assert_remote "core managed file exists with initial content" \
  "test \"\$(cat /tmp/debianform-core-libvirt.txt)\" = 'core libvirt smoke'"
assert_remote "core managed file has expected mode" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-core-libvirt.txt)\" = '644 root root'"
assert_remote "state records the managed file address" \
  "grep -F 'host.cihost.files.file[\\\"/tmp/debianform-core-libvirt.txt\\\"]' /var/lib/debianform-integration/core-state.json"
