assert_remote "script on_change managed config was removed after component removal" \
  "test ! -e /etc/debianform-script-on-change/app.env"
run_remote "remove script on_change integration artifacts after verification" \
  "rm -rf /var/lib/debianform-script-on-change /var/lib/debianform-integration /var/lock/debianform-integration /etc/debianform-script-on-change"
assert_remote "script on_change integration cleanup completed" \
  "test ! -e /var/lib/debianform-script-on-change && test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration && test ! -e /etc/debianform-script-on-change"
