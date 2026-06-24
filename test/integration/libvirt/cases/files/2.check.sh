assert_remote "core file drift was repaired and content updated" \
  "test \"\$(cat /tmp/debianform-core-libvirt.txt)\" = 'core libvirt updated'"
assert_remote "core file mode was updated" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-core-libvirt.txt)\" = '600 root root'"
