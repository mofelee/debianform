assert_remote "compose directory and generated unit were destroyed after removal from config" \
  "test ! -e /opt/debianform-compose-app && test ! -e /etc/systemd/system/debianform-compose-app.service"
assert_remote "docker compose final state contains no managed resources" \
  "grep -F '\"resources\": {}' /var/lib/debianform-integration/docker-compose-state.json"
run_remote "remove docker compose integration state backend after verification" \
  "rm -rf /var/lib/debianform-integration /var/lock/debianform-integration"
assert_remote "docker compose integration state backend cleanup completed" \
  "test ! -e /var/lib/debianform-integration && test ! -e /var/lock/debianform-integration"
