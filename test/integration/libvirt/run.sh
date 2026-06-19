#!/usr/bin/env bash

set -euo pipefail

readonly DEBIAN_CLOUD_URL="https://cloud.debian.org/images/cloud/trixie/latest"
readonly DEBIAN_CLOUD_IMAGE="debian-13-genericcloud-amd64.qcow2"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT_DIR="$ROOT_DIR/test/integration/libvirt"
CASES_DIR="$SCRIPT_DIR/cases"
WORK_ROOT="${DBF_INTEGRATION_WORKDIR:-$(mktemp -d "${TMPDIR:-/tmp}/debianform-integration.XXXXXX")}"
ARTIFACT_ROOT="${DBF_INTEGRATION_ARTIFACT_DIR:-${TMPDIR:-/tmp}/debianform-integration-artifacts}"
DBF_BIN="$WORK_ROOT/dbf"
BASE_IMAGE="$WORK_ROOT/$DEBIAN_CLOUD_IMAGE"

log() {
  printf '[integration] %s\n' "$*"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

cleanup() {
  local status=$?
  trap - EXIT
  if [[ "${DBF_INTEGRATION_KEEP_WORKDIR:-0}" != "1" ]]; then
    rm -rf "$WORK_ROOT"
  else
    log "preserving work directory: $WORK_ROOT"
  fi
  exit "$status"
}
trap cleanup EXIT

for command in cloud-localds curl go qemu-img sha512sum ssh ssh-keygen sudo virsh; do
  require_command "$command"
done

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  printf 'libvirt integration tests require Linux x86_64\n' >&2
  exit 1
fi

mkdir -p "$WORK_ROOT" "$ARTIFACT_ROOT"
chmod 0755 "$WORK_ROOT"

log "building dbf"
(
  cd "$ROOT_DIR"
  go build -trimpath -o "$DBF_BIN" ./cmd/dbf
)

log "resolving Debian 13 genericcloud image"
curl --fail --location --retry 3 --show-error --silent \
  "$DEBIAN_CLOUD_URL/SHA512SUMS" \
  --output "$WORK_ROOT/SHA512SUMS"
EXPECTED_SHA512="$(
  awk -v image="$DEBIAN_CLOUD_IMAGE" '$2 == image { print $1; exit }' \
    "$WORK_ROOT/SHA512SUMS"
)"
test -n "$EXPECTED_SHA512"

IMAGE_CACHE_DIR="${DBF_INTEGRATION_IMAGE_CACHE:-}"
CACHED_IMAGE="${IMAGE_CACHE_DIR:+$IMAGE_CACHE_DIR/$DEBIAN_CLOUD_IMAGE}"

if [[ -n "$CACHED_IMAGE" && -f "$CACHED_IMAGE" ]] &&
  printf '%s  %s\n' "$EXPECTED_SHA512" "$CACHED_IMAGE" | sha512sum --check --status; then
  log "using cached Debian image from $IMAGE_CACHE_DIR"
  cp "$CACHED_IMAGE" "$BASE_IMAGE"
else
  log "downloading official Debian image"
  curl --fail --location --retry 3 --show-error --silent \
    "$DEBIAN_CLOUD_URL/$DEBIAN_CLOUD_IMAGE" \
    --output "$BASE_IMAGE"
fi

printf '%s  %s\n' "$EXPECTED_SHA512" "$BASE_IMAGE" | sha512sum --check

if [[ -n "$CACHED_IMAGE" && ! -f "$CACHED_IMAGE" ]]; then
  mkdir -p "$IMAGE_CACHE_DIR"
  cp "$BASE_IMAGE" "$CACHED_IMAGE.partial"
  mv "$CACHED_IMAGE.partial" "$CACHED_IMAGE"
fi
chmod 0644 "$BASE_IMAGE"

declare -a CASE_DIRS=()
if [[ -n "${DBF_INTEGRATION_CASE:-}" ]]; then
  case_dir="$CASES_DIR/$DBF_INTEGRATION_CASE"
  if [[ ! -d "$case_dir" ]]; then
    printf 'integration case not found: %s\n' "$DBF_INTEGRATION_CASE" >&2
    exit 1
  fi
  CASE_DIRS+=("$case_dir")
else
  while IFS= read -r case_dir; do
    CASE_DIRS+=("$case_dir")
  done < <(find "$CASES_DIR" -mindepth 1 -maxdepth 1 -type d | sort)
fi

if (( ${#CASE_DIRS[@]} == 0 )); then
  printf 'no integration cases found under %s\n' "$CASES_DIR" >&2
  exit 1
fi

for case_dir in "${CASE_DIRS[@]}"; do
  case_name="$(basename "$case_dir")"
  log "running case $case_name in a fresh VM"
  DBF_INTEGRATION_DBF_BIN="$DBF_BIN" \
  DBF_INTEGRATION_BASE_IMAGE="$BASE_IMAGE" \
  DBF_INTEGRATION_CASE_WORK="$WORK_ROOT/cases/$case_name" \
  DBF_INTEGRATION_CASE_ARTIFACTS="$ARTIFACT_ROOT/$case_name" \
    "$SCRIPT_DIR/run-case.sh" "$case_dir"
done

log "all integration cases passed"
