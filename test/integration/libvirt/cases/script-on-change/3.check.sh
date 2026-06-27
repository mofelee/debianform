assert_remote "script on_change updated config after input change" \
  "grep -F 'MESSAGE=changed' /etc/debianform-script-on-change/app.env"
assert_remote "script on_change ran again after config change" \
  "test \"\$(cat /var/lib/debianform-script-on-change/reload.count)\" = '2'"
assert_remote "script on_change state records component script resources" \
  "grep -F 'host.cihost.components.app.files.file[\\\"/etc/debianform-script-on-change/app.env\\\"]' /var/lib/debianform-integration/script-on-change-state.json"
