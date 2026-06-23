assert_remote "source-built binary is removed after component removal" \
  "test ! -e /usr/local/bin/hello-from-source"

assert_remote "source-build final state contains no managed resources" \
  "python3 - <<'PY'
import json
with open('/var/lib/debianform-integration/source-build-state.json', encoding='utf-8') as f:
    state = json.load(f)
resources = state.get('resources', {})
assert resources == {}, resources
PY"
