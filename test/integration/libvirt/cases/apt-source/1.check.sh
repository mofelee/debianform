assert_remote \
  "APT source file exists" \
  "test -f /etc/apt/sources.list.d/example.sources"
assert_remote \
  "APT source contains the expected URI" \
  "grep -qx 'URIs: https://example.invalid/debian' /etc/apt/sources.list.d/example.sources"
assert_remote \
  "APT source contains the expected suite" \
  "grep -qx 'Suites: trixie' /etc/apt/sources.list.d/example.sources"
assert_remote \
  "APT source contains the expected Signed-By path" \
  "grep -qx 'Signed-By: /etc/apt/keyrings/example.gpg' /etc/apt/sources.list.d/example.sources"
assert_remote \
  "APT source file has mode 0644" \
  "test \"\$(stat -c %a /etc/apt/sources.list.d/example.sources)\" = 644"
