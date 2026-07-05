assert_remote "timezone changed to desired timezone" \
  "test \"\$(timedatectl show -p Timezone --value)\" = 'Asia/Shanghai'"
assert_remote "default locale LANG changed to desired locale" \
  "grep -Eq '^LANG=\"?en_US.UTF-8\"?$' /etc/default/locale"
assert_remote "generated locale is available" \
  "locale -a | grep -Eiq '^en_US\\.(utf8|UTF-8)$'"
assert_remote "unmanaged LC_TIME setting was preserved" \
  "grep -F 'LC_TIME=C.UTF-8' /etc/default/locale"
assert_remote "state records the system timezone resource" \
  "grep -F 'host.cihost.system.timezone' /var/lib/debianform-integration/system-settings-state.json"
assert_remote "state records the system locale resource" \
  "grep -F 'host.cihost.system.locale' /var/lib/debianform-integration/system-settings-state.json"
