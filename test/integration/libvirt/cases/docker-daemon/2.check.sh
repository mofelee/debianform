assert_remote "docker daemon json drift was repaired with updated content" \
  "grep -F '\"max-file\": \"3\"' /etc/docker/daemon.json && grep -F '\"max-size\": \"20m\"' /etc/docker/daemon.json && ! grep -F 'manual-drift' /etc/docker/daemon.json"
assert_remote "docker.service remains active after daemon update" \
  "systemctl is-active --quiet docker.service"
assert_remote "docker daemon updated state still records daemon file" \
  "grep -F 'host.cihost.docker.daemon.file[\\\"/etc/docker/daemon.json\\\"]' /var/lib/debianform-integration/docker-daemon-state.json"
