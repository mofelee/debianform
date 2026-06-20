assert_remote \
  "managed user was destroyed" \
  "! getent passwd debianform-app >/dev/null"
assert_remote \
  "managed group was destroyed" \
  "! getent group debianform-deploy >/dev/null"
assert_remote \
  "managed home directory was destroyed" \
  "! test -e /home/debianform-app"
assert_remote \
  "managed authorized_keys file was destroyed with its home" \
  "! test -e /home/debianform-app/.ssh/authorized_keys"
