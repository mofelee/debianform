APP_PUBLIC_KEY="$(cat "$CASE_DIR/app_key.pub")"

assert_remote \
  "managed group exists with GID 4242" \
  "test \"\$(getent group debianform-deploy | cut -d: -f3)\" = 4242"
assert_remote \
  "managed user exists with UID 4250" \
  "test \"\$(getent passwd debianform-app | cut -d: -f3)\" = 4250"
assert_remote \
  "managed user has the expected primary group" \
  "test \"\$(id -gn debianform-app)\" = debianform-deploy"
assert_remote \
  "managed user has the nologin shell" \
  "test \"\$(getent passwd debianform-app | cut -d: -f7)\" = /usr/sbin/nologin"
assert_remote \
  "managed home directory has mode 0750" \
  "test \"\$(stat -c %a /home/debianform-app)\" = 750"
assert_remote \
  "authorized key is present" \
  "grep -qF '$APP_PUBLIC_KEY' /home/debianform-app/.ssh/authorized_keys"
assert_remote \
  "authorized_keys has mode 0600" \
  "test \"\$(stat -c %a /home/debianform-app/.ssh/authorized_keys)\" = 600"
