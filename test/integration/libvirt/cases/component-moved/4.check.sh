assert_remote "managed component files and directory were removed" \
  "test ! -e /etc/debianform-moved"
assert_remote "adopted package and service remained available after component removal" \
  "dpkg-query -W -f='\${Status}' openssh-server | grep -Fx 'install ok installed' && systemctl is-active --quiet ssh.service"
assert_remote "component removal left no managed state resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/component-moved-state.json"
run_remote "remove component moved integration artifacts after verification" \
  "rm -rf /var/lib/debianform-moved /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "component moved integration cleanup completed" \
  "test ! -e /var/lib/debianform-moved && test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
