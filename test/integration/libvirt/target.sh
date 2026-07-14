#!/usr/bin/env bash

dbf_integration_resolve_target() {
  local target="${1:-debian-13}"

  case "$target" in
    debian-12)
      DBF_INTEGRATION_TARGET_DISTRIBUTION="debian"
      DBF_INTEGRATION_TARGET_VERSION="12"
      DBF_INTEGRATION_TARGET_CODENAME="bookworm"
      DBF_INTEGRATION_TARGET_ARCHITECTURE="amd64"
      DBF_INTEGRATION_TARGET_CLOUD_URL="https://cloud.debian.org/images/cloud/bookworm/latest"
      DBF_INTEGRATION_TARGET_CLOUD_IMAGE="debian-12-genericcloud-amd64.qcow2"
      DBF_INTEGRATION_TARGET_CHECKSUM_FILE="SHA512SUMS"
      DBF_INTEGRATION_TARGET_CHECKSUM_ALGORITHM="sha512"
      DBF_INTEGRATION_TARGET_APT_SOURCE_PATH="/etc/apt/sources.list.d/debian.sources"
      DBF_INTEGRATION_TARGET_APT_MIRROR="https://mirrors.aliyun.com/debian/"
      DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR="https://mirrors.aliyun.com/debian-security/"
      DBF_INTEGRATION_TARGET_APT_COMPONENTS="main contrib non-free non-free-firmware"
      ;;
    debian-13)
      DBF_INTEGRATION_TARGET_DISTRIBUTION="debian"
      DBF_INTEGRATION_TARGET_VERSION="13"
      DBF_INTEGRATION_TARGET_CODENAME="trixie"
      DBF_INTEGRATION_TARGET_ARCHITECTURE="amd64"
      DBF_INTEGRATION_TARGET_CLOUD_URL="https://cloud.debian.org/images/cloud/trixie/latest"
      DBF_INTEGRATION_TARGET_CLOUD_IMAGE="debian-13-genericcloud-amd64.qcow2"
      DBF_INTEGRATION_TARGET_CHECKSUM_FILE="SHA512SUMS"
      DBF_INTEGRATION_TARGET_CHECKSUM_ALGORITHM="sha512"
      DBF_INTEGRATION_TARGET_APT_SOURCE_PATH="/etc/apt/sources.list.d/debian.sources"
      DBF_INTEGRATION_TARGET_APT_MIRROR="https://mirrors.aliyun.com/debian/"
      DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR="https://mirrors.aliyun.com/debian-security/"
      DBF_INTEGRATION_TARGET_APT_COMPONENTS="main contrib non-free non-free-firmware"
      ;;
    ubuntu-24.04)
      DBF_INTEGRATION_TARGET_DISTRIBUTION="ubuntu"
      DBF_INTEGRATION_TARGET_VERSION="24.04"
      DBF_INTEGRATION_TARGET_CODENAME="noble"
      DBF_INTEGRATION_TARGET_ARCHITECTURE="amd64"
      DBF_INTEGRATION_TARGET_CLOUD_URL="https://cloud-images.ubuntu.com/noble/current"
      DBF_INTEGRATION_TARGET_CLOUD_IMAGE="noble-server-cloudimg-amd64.img"
      DBF_INTEGRATION_TARGET_CHECKSUM_FILE="SHA256SUMS"
      DBF_INTEGRATION_TARGET_CHECKSUM_ALGORITHM="sha256"
      DBF_INTEGRATION_TARGET_APT_SOURCE_PATH="/etc/apt/sources.list.d/ubuntu.sources"
      DBF_INTEGRATION_TARGET_APT_MIRROR="https://mirrors.aliyun.com/ubuntu/"
      DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR="https://mirrors.aliyun.com/ubuntu/"
      DBF_INTEGRATION_TARGET_APT_COMPONENTS="main restricted universe multiverse"
      ;;
    *)
      printf 'unsupported integration target: %s (expected debian-12, debian-13, or ubuntu-24.04)\n' "$target" >&2
      return 1
      ;;
  esac

  DBF_INTEGRATION_TARGET="$target"
  DBF_INTEGRATION_TARGET_SLUG="${DBF_INTEGRATION_TARGET_DISTRIBUTION}${DBF_INTEGRATION_TARGET_VERSION//./-}"

  export DBF_INTEGRATION_TARGET
  export DBF_INTEGRATION_TARGET_SLUG
  export DBF_INTEGRATION_TARGET_DISTRIBUTION
  export DBF_INTEGRATION_TARGET_VERSION
  export DBF_INTEGRATION_TARGET_CODENAME
  export DBF_INTEGRATION_TARGET_ARCHITECTURE
  export DBF_INTEGRATION_TARGET_CLOUD_URL
  export DBF_INTEGRATION_TARGET_CLOUD_IMAGE
  export DBF_INTEGRATION_TARGET_CHECKSUM_FILE
  export DBF_INTEGRATION_TARGET_CHECKSUM_ALGORITHM
  export DBF_INTEGRATION_TARGET_APT_SOURCE_PATH
  export DBF_INTEGRATION_TARGET_APT_MIRROR
  export DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR
  export DBF_INTEGRATION_TARGET_APT_COMPONENTS
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  dbf_integration_resolve_target "${1:-debian-13}"
  printf 'target=%s\n' "$DBF_INTEGRATION_TARGET"
  printf 'distribution=%s\n' "$DBF_INTEGRATION_TARGET_DISTRIBUTION"
  printf 'version=%s\n' "$DBF_INTEGRATION_TARGET_VERSION"
  printf 'codename=%s\n' "$DBF_INTEGRATION_TARGET_CODENAME"
  printf 'architecture=%s\n' "$DBF_INTEGRATION_TARGET_ARCHITECTURE"
  printf 'cloud_url=%s\n' "$DBF_INTEGRATION_TARGET_CLOUD_URL"
  printf 'cloud_image=%s\n' "$DBF_INTEGRATION_TARGET_CLOUD_IMAGE"
  printf 'checksum_file=%s\n' "$DBF_INTEGRATION_TARGET_CHECKSUM_FILE"
  printf 'checksum_algorithm=%s\n' "$DBF_INTEGRATION_TARGET_CHECKSUM_ALGORITHM"
  printf 'apt_source_path=%s\n' "$DBF_INTEGRATION_TARGET_APT_SOURCE_PATH"
  printf 'apt_mirror=%s\n' "$DBF_INTEGRATION_TARGET_APT_MIRROR"
  printf 'apt_security_mirror=%s\n' "$DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR"
  printf 'apt_components=%s\n' "$DBF_INTEGRATION_TARGET_APT_COMPONENTS"
fi
