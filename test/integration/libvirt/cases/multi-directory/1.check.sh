assert_remote "multi-directory step 1 wrote content from shared and app directories" \
  "test \"\$(cat /tmp/debianform-multi-directory.txt)\" = 'shared=base-profile env=app-auto step=1'"
assert_remote "multi-directory step 1 wrote configured file mode" \
  "test \"\$(stat -c '%a %U %G' /tmp/debianform-multi-directory.txt)\" = '640 root root'"
assert_remote "multi-directory step 1 state records managed file" \
  "grep -F 'host.cihost.files.file[\\\"/tmp/debianform-multi-directory.txt\\\"]' /var/lib/debianform-integration/multi-directory-state.json"
