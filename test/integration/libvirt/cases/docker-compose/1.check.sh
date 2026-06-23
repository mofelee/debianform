assert_remote "docker compose config can render the managed project" \
  "docker compose -p app -f /opt/debianform-compose-app/compose.yaml config | grep -F 'com.example.debianform: loop8'"
assert_remote "compose working directory and files have expected permissions" \
  "test \"\$(stat -c '%a %U %G' /opt/debianform-compose-app)\" = '755 root root' && test \"\$(stat -c '%a %U %G' /opt/debianform-compose-app/compose.yaml)\" = '644 root root' && test \"\$(stat -c '%a %U %G' /opt/debianform-compose-app/.env)\" = '600 root root'"
assert_remote "compose project is running" \
  "docker compose -p app -f /opt/debianform-compose-app/compose.yaml ps --format json | grep -i 'running'"
assert_remote "generated compose systemd unit contains expected docker compose command" \
  "grep -F 'Description=DebianForm Compose Project app' /etc/systemd/system/debianform-compose-app.service && grep -F 'ExecStart=/usr/bin/docker compose -p app -f /opt/debianform-compose-app/compose.yaml up -d' /etc/systemd/system/debianform-compose-app.service"
assert_remote "generated compose systemd service is active and enabled" \
  "systemctl is-active --quiet debianform-compose-app.service && systemctl is-enabled --quiet debianform-compose-app.service"
assert_remote "compose state records project, unit, service, and write-only env file without plaintext secret" \
  "grep -F 'host.cihost.docker.compose[\\\"app\\\"].project' /var/lib/debianform-integration/docker-compose-state.json && grep -F 'host.cihost.docker.compose[\\\"app\\\"].systemd_unit' /var/lib/debianform-integration/docker-compose-state.json && grep -F 'host.cihost.docker.compose[\\\"app\\\"].service' /var/lib/debianform-integration/docker-compose-state.json && ! grep -F 'loop8-secret-value' /var/lib/debianform-integration/docker-compose-state.json"
