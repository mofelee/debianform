assert_remote "compose project is absent after state absent apply" \
  "out=\"\$(docker compose -p app -f /opt/debianform-compose-app/compose.yaml ps --format json 2>/dev/null | tr -d '[:space:]')\"; test -z \"\$out\" || test \"\$out\" = '[]'"
assert_remote "generated compose systemd service is disabled and inactive" \
  "! systemctl is-enabled --quiet debianform-compose-app.service && ! systemctl is-active --quiet debianform-compose-app.service"
assert_remote "compose files and systemd unit remain managed before final destroy" \
  "test -e /opt/debianform-compose-app/compose.yaml && test -e /opt/debianform-compose-app/.env && test -e /etc/systemd/system/debianform-compose-app.service && grep -F 'host.cihost.docker.compose[\\\"app\\\"].file' /var/lib/debianform-integration/docker-compose-state.json && ! grep -F 'host.cihost.docker.compose[\\\"app\\\"].project' /var/lib/debianform-integration/docker-compose-state.json"
