APP_PUBLIC_KEY="$(cat "$CASE_DIR/app_key.pub")"

assert_remote \
  "group GID drift was repaired" \
  "test \"\$(getent group debianform-deploy | cut -d: -f3)\" = 4242"
assert_remote \
  "user shell drift was repaired" \
  "test \"\$(getent passwd debianform-app | cut -d: -f7)\" = /usr/sbin/nologin"
assert_remote \
  "home directory group drift was repaired" \
  "test \"\$(stat -c %G /home/debianform-app)\" = debianform-deploy"
assert_remote \
  "authorized key drift was repaired" \
  "grep -qF '$APP_PUBLIC_KEY' /home/debianform-app/.ssh/authorized_keys"
