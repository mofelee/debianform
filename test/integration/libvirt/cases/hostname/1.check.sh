assert_remote "static hostname changed to desired hostname" \
  "test \"\$(hostnamectl --static)\" = 'dbf-hostname'"
assert_remote "runtime hostname changed to desired hostname" \
  "test \"\$(hostname)\" = 'dbf-hostname'"
assert_remote "hostname state records the system hostname resource" \
  "grep -F 'host.cihost.system.hostname' /var/lib/debianform-integration/hostname-state.json"
assert_remote "hostname state records the system_hostname kind" \
  "grep -F '\"kind\": \"system_hostname\"' /var/lib/debianform-integration/hostname-state.json"
assert_remote "hostname convergence does not manage /etc/hosts" \
  "! grep -F 'dbf-hostname' /etc/hosts"
