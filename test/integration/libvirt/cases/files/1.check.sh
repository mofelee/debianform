assert_remote "v2 managed file exists with initial content" \
  "test \"\$(cat /tmp/debianform-v2-libvirt.txt)\" = 'v2 libvirt smoke'"
assert_remote "v2 managed file has expected mode" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-v2-libvirt.txt)\" = '644 root root'"
assert_remote "v2 state records the managed file address" \
  "grep -F 'host.cihost.files.file[\"/tmp/debianform-v2-libvirt.txt\"]' /var/lib/debianform-integration/v2-state.json"
