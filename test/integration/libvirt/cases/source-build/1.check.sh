assert_remote "source-built binary prints expected output" \
  "test \"\$(/usr/local/bin/hello-from-source)\" = 'hello from debianform source build'"

assert_remote "source-build state records download build and install resources" \
  "grep -F 'host.cihost.components.hello_from_source.artifact.download[\"default\"]' /var/lib/debianform-integration/source-build-state.json && grep -F 'host.cihost.components.hello_from_source.artifact.build[' /var/lib/debianform-integration/source-build-state.json && grep -F 'host.cihost.components.hello_from_source.artifact.install[\"/usr/local/bin/hello-from-source\"]' /var/lib/debianform-integration/source-build-state.json"
