assert_remote "source-built binary prints expected output" \
  "test \"\$(/usr/local/bin/hello-from-source)\" = 'hello from debianform source build'"

assert_remote "source-build state records download build and install resources" \
  "python3 - <<'PY'
import json

with open('/var/lib/debianform-integration/source-build-state.json', encoding='utf-8') as f:
    resources = json.load(f).get('resources', {})

assert 'host.cihost.components.hello_from_source.artifact.download[\"default\"]' in resources, resources
assert any(key.startswith('host.cihost.components.hello_from_source.artifact.build[') for key in resources), resources
assert 'host.cihost.components.hello_from_source.artifact.install[\"/usr/local/bin/hello-from-source\"]' in resources, resources
PY"
