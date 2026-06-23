assert_remote "docker.service is inactive after final destroy" \
  "! systemctl is-active --quiet docker.service"
assert_remote "docker official source and signing key were destroyed" \
  "test ! -e /etc/apt/sources.list.d/docker_official.sources && test ! -e /etc/apt/keyrings/docker.asc"
assert_remote "docker engine final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/docker-engine-state.json"
run_remote "remove docker engine integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "docker engine integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
