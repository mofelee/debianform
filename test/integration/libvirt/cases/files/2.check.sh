assert_remote "v2 file drift was repaired and content updated" \
  "test \"\$(cat /tmp/debianform-v2-libvirt.txt)\" = 'v2 libvirt updated'"
assert_remote "v2 file mode was updated" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-v2-libvirt.txt)\" = '600 root root'"
