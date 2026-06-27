assert_remote "compose yaml drift was repaired with updated content" \
  "grep -F 'loop8-updated' /opt/debianform-compose-app/compose.yaml && ! grep -F 'manual-drift' /opt/debianform-compose-app/compose.yaml"
assert_remote_eventually "compose project was converged back to running after manual stop" \
  "docker compose -p app -f /opt/debianform-compose-app/compose.yaml ps --format json | grep -i 'running'"
assert_remote_eventually "generated compose systemd unit remains active and enabled after repair" \
  "systemctl is-active --quiet debianform-compose-app.service && systemctl is-enabled --quiet debianform-compose-app.service"
assert_remote "compose state still avoids leaking env file content" \
  "! grep -F 'loop8-secret-value' /var/lib/debianform-integration/docker-compose-state.json"
