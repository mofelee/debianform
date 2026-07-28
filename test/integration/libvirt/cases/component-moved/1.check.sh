assert_remote "initial Babel-shaped configuration was applied" \
  "grep -F 'protocol babel edge' /etc/debianform-moved/bird.conf"
assert_remote "initial component script ran once and recorded the old instance" \
  "test \"\$(cat /var/lib/debianform-moved/reload.count)\" = 1 && test \"\$(cat /var/lib/debianform-moved/component.name)\" = bird2_babel"
assert_remote "initial state contains package, service, files, directory, and script outputs under the old prefix" \
  "grep -F 'host.cihost.components.bird2_babel.packages.install' /var/lib/debianform-integration/component-moved-state.json && grep -F 'host.cihost.components.bird2_babel.services.service' /var/lib/debianform-integration/component-moved-state.json && grep -F 'host.cihost.components.bird2_babel.files.file' /var/lib/debianform-integration/component-moved-state.json && grep -F 'host.cihost.components.bird2_babel.directories.directory' /var/lib/debianform-integration/component-moved-state.json && grep -F 'host.cihost.components.bird2_babel.script' /var/lib/debianform-integration/component-moved-state.json"
run_remote "record stable file identity before the component rename" \
  "stat -c '%d:%i' /etc/debianform-moved/stable.conf > /var/lib/debianform-moved/stable.identity"
