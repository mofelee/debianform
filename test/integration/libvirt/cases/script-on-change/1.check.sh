assert_remote "script on_change wrote initial config" \
  "grep -F 'MESSAGE=hello' /etc/debianform-script-on-change/app.env"
assert_remote "script on_change ran once on first apply" \
  "test \"\$(cat /var/lib/debianform-script-on-change/reload.count)\" = '1'"
assert_remote "script on_change received component context" \
  "test \"\$(cat /var/lib/debianform-script-on-change/script.name)\" = 'record_change' && test \"\$(cat /var/lib/debianform-script-on-change/component.name)\" = 'app'"
assert_remote "script on_change received trigger path context" \
  "test \"\$(cat /var/lib/debianform-script-on-change/trigger.path)\" = '/etc/debianform-script-on-change/app.env' && test \"\$(cat /var/lib/debianform-script-on-change/trigger.paths)\" = '/etc/debianform-script-on-change/app.env'"
