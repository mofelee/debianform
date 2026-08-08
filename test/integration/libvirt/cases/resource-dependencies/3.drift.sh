run_remote "make an installed apache2 version upgradeable" \
  "set -eu; dpkg-query -W -f='\${Version}\n' apache2 > /tmp/debianform-apache2-version.before; sed -i '/^Package: apache2$/,/^$/ s/^Version: .*/Version: 0/' /var/lib/dpkg/status; test \"\$(dpkg-query -W -f='\${Version}' apache2)\" = 0"
