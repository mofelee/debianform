run_remote "prepare alternate component staging root and constrain /tmp" \
  "set -eu
if ! command -v tar >/dev/null 2>&1 || ! command -v xz >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y --no-install-recommends tar xz-utils
fi
install -d -m 0700 /var/lib/debianform-integration/component-staging
mount -t tmpfs -o size=1m,mode=1777 dbf-issue87-tmpfs /tmp"
assert_remote "component staging fixture has extraction tools and a constrained /tmp" \
  "command -v tar >/dev/null && command -v xz >/dev/null && grep -q '^dbf-issue87-tmpfs /tmp tmpfs ' /proc/mounts && test -d /var/lib/debianform-integration/component-staging"
