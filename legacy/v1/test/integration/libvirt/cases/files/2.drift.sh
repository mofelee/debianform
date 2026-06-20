run_remote \
  "replace the primary file with drifted content" \
  "printf 'drift\n' > /var/lib/debianform-files/primary.conf"
