assert_remote \
  "managed directory has mode 0750" \
  "test \"\$(stat -c %a /var/lib/debianform-files)\" = 750"
assert_remote \
  "primary file has expected content" \
  "test \"\$(cat /var/lib/debianform-files/primary.conf)\" = 'managed primary'"
assert_remote \
  "primary file has mode 0640" \
  "test \"\$(stat -c %a /var/lib/debianform-files/primary.conf)\" = 640"
assert_remote \
  "secondary file has expected content" \
  "test \"\$(cat /var/lib/debianform-files/secondary.conf)\" = 'managed secondary'"
assert_remote \
  "secondary file has mode 0600" \
  "test \"\$(stat -c %a /var/lib/debianform-files/secondary.conf)\" = 600"
assert_remote \
  "handler ran once for the initial apply" \
  "test \"\$(wc -l < /run/debianform-files-handler.log)\" = 1"
