assert_remote \
  "managed APT source was destroyed" \
  "! test -e /etc/apt/sources.list.d/example.sources"
