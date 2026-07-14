#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT_DIR="$ROOT_DIR/test/integration/libvirt"
CASES_DIR="$SCRIPT_DIR/cases"
source "$SCRIPT_DIR/target.sh"
if [[ -n "${DBF_INTEGRATION_TARGET:-}" ]]; then
  dbf_integration_resolve_target "$DBF_INTEGRATION_TARGET"
elif [[ -n "${DBF_INTEGRATION_DEBIAN_VERSION+x}" ]]; then
  dbf_integration_resolve_target "debian-$DBF_INTEGRATION_DEBIAN_VERSION"
else
  dbf_integration_resolve_target
fi
TARGET_CLOUD_URL="$DBF_INTEGRATION_TARGET_CLOUD_URL"
TARGET_CLOUD_IMAGE="$DBF_INTEGRATION_TARGET_CLOUD_IMAGE"
TARGET_CHECKSUM_FILE="$DBF_INTEGRATION_TARGET_CHECKSUM_FILE"
TARGET_CHECKSUM_ALGORITHM="$DBF_INTEGRATION_TARGET_CHECKSUM_ALGORITHM"
WORK_ROOT="${DBF_INTEGRATION_WORKDIR:-$(mktemp -d "${TMPDIR:-/tmp}/debianform-core-integration.XXXXXX")}"
ARTIFACT_ROOT="${DBF_INTEGRATION_ARTIFACT_DIR:-${TMPDIR:-/tmp}/debianform-core-integration-artifacts}"
DBF_BIN="$WORK_ROOT/dbf"
BASE_IMAGE="$WORK_ROOT/$TARGET_CLOUD_IMAGE"

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

for command in curl go python3 "${TARGET_CHECKSUM_ALGORITHM}sum" ssh ssh-keygen virsh; do
  require_command "$command"
done
if [[ -z "${DBF_LIBVIRT_URI:-${VIRSH_DEFAULT_CONNECT_URI:-${LIBVIRT_DEFAULT_URI:-}}}" ]]; then
  require_command cloud-localds
  require_command qemu-img
  require_command sudo
fi

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

log "resolving $DBF_INTEGRATION_TARGET $DBF_INTEGRATION_TARGET_ARCHITECTURE cloud image"
curl --fail --location --retry 3 --show-error --silent \
  "$TARGET_CLOUD_URL/$TARGET_CHECKSUM_FILE" \
  --output "$WORK_ROOT/$TARGET_CHECKSUM_FILE"
EXPECTED_DIGEST="$(
  awk -v image="$TARGET_CLOUD_IMAGE" '{ name=$2; sub(/^\*/, "", name); if (name == image) { print $1; exit } }' \
    "$WORK_ROOT/$TARGET_CHECKSUM_FILE"
)"
test -n "$EXPECTED_DIGEST"

IMAGE_CACHE_DIR="${DBF_INTEGRATION_IMAGE_CACHE:-}"
CACHED_IMAGE="${IMAGE_CACHE_DIR:+$IMAGE_CACHE_DIR/$TARGET_CLOUD_IMAGE}"

if [[ -n "$CACHED_IMAGE" && -f "$CACHED_IMAGE" ]] &&
  printf '%s  %s\n' "$EXPECTED_DIGEST" "$CACHED_IMAGE" | "${TARGET_CHECKSUM_ALGORITHM}sum" --check --status; then
  log "using cached $DBF_INTEGRATION_TARGET image from $IMAGE_CACHE_DIR"
  cp "$CACHED_IMAGE" "$BASE_IMAGE"
else
  log "downloading official $DBF_INTEGRATION_TARGET image"
  curl --fail --location --retry 3 --show-error --silent \
    "$TARGET_CLOUD_URL/$TARGET_CLOUD_IMAGE" \
    --output "$BASE_IMAGE"
fi

printf '%s  %s\n' "$EXPECTED_DIGEST" "$BASE_IMAGE" | "${TARGET_CHECKSUM_ALGORITHM}sum" --check

if [[ -n "$CACHED_IMAGE" ]] &&
  { [[ ! -f "$CACHED_IMAGE" ]] || ! printf '%s  %s\n' "$EXPECTED_DIGEST" "$CACHED_IMAGE" | "${TARGET_CHECKSUM_ALGORITHM}sum" --check --status; }; then
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
  log "running case $case_name in a fresh $DBF_INTEGRATION_TARGET VM"
  runner="$SCRIPT_DIR/run-case.sh"
  if [[ -f "$case_dir/three-host.case" ]]; then
    runner="$SCRIPT_DIR/run-three-host-case.sh"
  elif [[ -f "$case_dir/two-host.case" ]]; then
    runner="$SCRIPT_DIR/run-two-host-case.sh"
  fi
  DBF_INTEGRATION_DBF_BIN="$DBF_BIN" \
  DBF_INTEGRATION_BASE_IMAGE="$BASE_IMAGE" \
  DBF_INTEGRATION_BASE_IMAGE_DIGEST="$EXPECTED_DIGEST" \
  DBF_INTEGRATION_BASE_IMAGE_DIGEST_ALGORITHM="$TARGET_CHECKSUM_ALGORITHM" \
  DBF_INTEGRATION_CASE_WORK="$WORK_ROOT/cases/$case_name" \
  DBF_INTEGRATION_CASE_ARTIFACTS="$ARTIFACT_ROOT/$case_name" \
    "$runner" "$case_dir"
done

log "all integration cases passed"
