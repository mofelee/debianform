run_remote "make an installed cron version upgradeable" \
  "set -eu; dpkg-query -W -f='\${Version}\n' cron > /tmp/debianform-cron-version.before; sed -i '/^Package: cron$/,/^$/ s/^Version: .*/Version: 0/' /var/lib/dpkg/status; test \"\$(dpkg-query -W -f='\${Version}' cron)\" = 0"
