assert_remote "multi-directory step 2 updated content using cross-directory files" \
  "test \"\$(cat /tmp/debianform-multi-directory.txt)\" = 'shared=base-profile env=app-auto step=2'"
assert_remote "multi-directory step 2 updated file mode" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-multi-directory.txt)\" = '600 root root'"
assert_remote "multi-directory step 2 state still records managed file" \
  "grep -F 'host.cihost.files.file[\\\"/tmp/debianform-multi-directory.txt\\\"]' /var/lib/debianform-integration/multi-directory-state.json"
