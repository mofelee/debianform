assert_remote \
  "primary file drift was repaired" \
  "test \"\$(cat /var/lib/debianform-files/primary.conf)\" = 'managed primary'"
assert_remote \
  "handler ran once more after drift repair" \
  "test \"\$(wc -l < /run/debianform-files-handler.log)\" = 2"
