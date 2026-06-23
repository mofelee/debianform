assert_remote "docker daemon json was destroyed after removal from config" \
  "test ! -e /etc/docker/daemon.json"
assert_remote "docker.service is inactive after daemon case final destroy" \
  "! systemctl is-active --quiet docker.service"
assert_remote "docker daemon final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/docker-daemon-state.json"
run_remote "remove docker daemon integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "docker daemon integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
