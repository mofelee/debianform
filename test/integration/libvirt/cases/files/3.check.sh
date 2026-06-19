assert_remote \
  "managed directory was destroyed" \
  "! test -e /var/lib/debianform-files"
assert_remote \
  "primary managed file was destroyed" \
  "! test -e /var/lib/debianform-files/primary.conf"
assert_remote \
  "secondary managed file was destroyed" \
  "! test -e /var/lib/debianform-files/secondary.conf"
