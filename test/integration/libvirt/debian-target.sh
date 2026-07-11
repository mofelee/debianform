#!/usr/bin/env bash

dbf_integration_resolve_debian_target() {
  local version=13
  if (( $# > 0 )); then
    version=$1
  fi

  case "$version" in
    12)
      DBF_INTEGRATION_DEBIAN_CODENAME="bookworm"
      ;;
    13)
      DBF_INTEGRATION_DEBIAN_CODENAME="trixie"
      ;;
    *)
      printf 'unsupported Debian integration version: %s (expected 12 or 13)\n' "$version" >&2
      return 1
      ;;
  esac

  DBF_INTEGRATION_DEBIAN_VERSION="$version"
  DBF_INTEGRATION_DEBIAN_ARCHITECTURE="amd64"
  DBF_INTEGRATION_DEBIAN_CLOUD_URL="https://cloud.debian.org/images/cloud/$DBF_INTEGRATION_DEBIAN_CODENAME/latest"
  DBF_INTEGRATION_DEBIAN_CLOUD_IMAGE="debian-$version-genericcloud-amd64.qcow2"

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
