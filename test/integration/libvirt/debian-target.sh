#!/usr/bin/env bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/target.sh"

dbf_integration_resolve_debian_target() {
  local version=13
  if (( $# > 0 )); then
    version=$1
  fi
  if [[ "$version" != "12" && "$version" != "13" ]]; then
    printf 'unsupported Debian integration version: %s (expected 12 or 13)\n' "$version" >&2
    return 1
  fi
  dbf_integration_resolve_target "debian-$version"

  DBF_INTEGRATION_DEBIAN_VERSION="$DBF_INTEGRATION_TARGET_VERSION"
  DBF_INTEGRATION_DEBIAN_CODENAME="$DBF_INTEGRATION_TARGET_CODENAME"
  DBF_INTEGRATION_DEBIAN_ARCHITECTURE="$DBF_INTEGRATION_TARGET_ARCHITECTURE"
  DBF_INTEGRATION_DEBIAN_CLOUD_URL="$DBF_INTEGRATION_TARGET_CLOUD_URL"
  DBF_INTEGRATION_DEBIAN_CLOUD_IMAGE="$DBF_INTEGRATION_TARGET_CLOUD_IMAGE"

  export DBF_INTEGRATION_DEBIAN_VERSION
  export DBF_INTEGRATION_DEBIAN_CODENAME
  export DBF_INTEGRATION_DEBIAN_ARCHITECTURE
  export DBF_INTEGRATION_DEBIAN_CLOUD_URL
  export DBF_INTEGRATION_DEBIAN_CLOUD_IMAGE
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  if (( $# > 0 )); then
    dbf_integration_resolve_debian_target "$1"
  else
    dbf_integration_resolve_debian_target
  fi
  printf 'version=%s\n' "$DBF_INTEGRATION_DEBIAN_VERSION"
  printf 'codename=%s\n' "$DBF_INTEGRATION_DEBIAN_CODENAME"
  printf 'architecture=%s\n' "$DBF_INTEGRATION_DEBIAN_ARCHITECTURE"
  printf 'cloud_url=%s\n' "$DBF_INTEGRATION_DEBIAN_CLOUD_URL"
  printf 'cloud_image=%s\n' "$DBF_INTEGRATION_DEBIAN_CLOUD_IMAGE"
fi
