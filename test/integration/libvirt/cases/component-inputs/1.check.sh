assert_remote "component input listeners file includes optional defaults" \
  "grep -F '\"tls\":false' /etc/debianform-component-inputs/listeners.json && grep -F '\"note\":null' /etc/debianform-component-inputs/listeners.json && grep -F '\"tags\":{}' /etc/debianform-component-inputs/listeners.json"
assert_remote "sensitive component input file was written with strict mode" \
  "test \"\$(stat -c '%a' /etc/debianform-component-inputs/environment.json)\" = '600' && grep -F 'component-input-secret' /etc/debianform-component-inputs/environment.json"
assert_remote "component input state records generated files without sensitive plaintext" \
  "grep -F 'host.cihost.components.proxy.files.file[\"/etc/debianform-component-inputs/environment.json\"]' /var/lib/debianform-integration/component-inputs-state.json && ! grep -F 'component-input-secret' /var/lib/debianform-integration/component-inputs-state.json"
