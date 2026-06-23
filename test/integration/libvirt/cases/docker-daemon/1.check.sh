assert_remote "docker daemon json has expected content and permissions" \
  "test \"\$(stat -c '%a %U %G' /etc/docker/daemon.json)\" = '644 root root' && grep -F '\"log-driver\": \"json-file\"' /etc/docker/daemon.json && grep -F '\"max-size\": \"10m\"' /etc/docker/daemon.json"
assert_remote "docker.service is active after daemon config restart" \
  "systemctl is-active --quiet docker.service"
assert_remote "docker daemon state records daemon file and restart-managed service" \
  "grep -F 'host.cihost.docker.daemon.file[\\\"/etc/docker/daemon.json\\\"]' /var/lib/debianform-integration/docker-daemon-state.json && grep -F 'host.cihost.docker.service[\\\"docker\\\"]' /var/lib/debianform-integration/docker-daemon-state.json"
