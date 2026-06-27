assert_remote "script on_change no-op apply did not run script again" \
  "test \"\$(cat /var/lib/debianform-script-on-change/reload.count)\" = '1'"
assert_remote "script on_change no-op apply kept config stable" \
  "grep -F 'MESSAGE=hello' /etc/debianform-script-on-change/app.env"
