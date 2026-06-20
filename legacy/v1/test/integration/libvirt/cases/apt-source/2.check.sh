assert_remote \
  "APT source content drift was repaired" \
  "! grep -q '# drift' /etc/apt/sources.list.d/example.sources"
