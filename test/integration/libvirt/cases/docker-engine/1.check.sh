assert_remote "docker official repository source exists" \
  "grep -F 'URIs: https://download.docker.com/linux/debian' /etc/apt/sources.list.d/docker_official.sources && grep -F 'Suites: ${DBF_INTEGRATION_DEBIAN_CODENAME}' /etc/apt/sources.list.d/docker_official.sources"
assert_remote "docker official signing key exists with expected mode" \
  "test \"\$(stat -c '%a %U %G' /etc/apt/keyrings/docker.asc)\" = '644 root root'"
assert_remote "docker official packages are installed" \
  "dpkg-query -W -f='\${Status}\n' docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin | grep -c '^install ok installed$' | grep -qx '5'"
assert_remote "docker.service is active and enabled" \
  "systemctl is-active --quiet docker.service && systemctl is-enabled --quiet docker.service"
assert_remote "docker compose plugin is available" \
  "docker compose version"
assert_remote "docker engine state records high-level docker resources and runtime facts" \
  "grep -F 'host.cihost.docker.package[\\\"docker-ce\\\"]' /var/lib/debianform-integration/docker-engine-state.json && grep -F 'host.cihost.docker.service[\\\"docker\\\"]' /var/lib/debianform-integration/docker-engine-state.json && grep -F '\"codename\": \"${DBF_INTEGRATION_DEBIAN_CODENAME}\"' /var/lib/debianform-integration/docker-engine-state.json"
