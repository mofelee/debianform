#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CASES_DIR="$ROOT_DIR/test/integration/libvirt/cases"
EXPECTED_CASE_COUNT=20
DBF_BIN="${DBF_INTEGRATION_DBF_BIN:-}"
TEMP_DBF=""
TEMP_PLAN=""

cleanup() {
  if [[ -n "$TEMP_DBF" ]]; then
    rm -f "$TEMP_DBF"
  fi
  if [[ -n "$TEMP_PLAN" ]]; then
    rm -f "$TEMP_PLAN"
  fi
}
trap cleanup EXIT

if [[ -z "$DBF_BIN" ]]; then
  TEMP_DBF="$(mktemp "${TMPDIR:-/tmp}/dbf-core-integration-layout.XXXXXX")"
  (
    cd "$ROOT_DIR"
    go build -o "$TEMP_DBF" ./cmd/dbf
  )
  DBF_BIN="$TEMP_DBF"
fi

bash -n "$ROOT_DIR/test/integration/libvirt/run-case.sh"
bash -n "$ROOT_DIR/test/integration/libvirt/run-two-host-case.sh"
bash -n "$ROOT_DIR/test/integration/libvirt/run-three-host-case.sh"
bash -n "$ROOT_DIR/test/integration/libvirt/debian-target.sh"
bash -n "$ROOT_DIR/test/integration/libvirt/network.sh"
bash -n "$ROOT_DIR/test/integration/libvirt/test-network-helper.sh"
bash "$ROOT_DIR/test/integration/libvirt/test-network-helper.sh"

target_12="$(bash "$ROOT_DIR/test/integration/libvirt/debian-target.sh" 12)"
target_13="$(bash "$ROOT_DIR/test/integration/libvirt/debian-target.sh" 13)"
grep -qx 'codename=bookworm' <<<"$target_12"
grep -qx 'cloud_image=debian-12-genericcloud-amd64.qcow2' <<<"$target_12"
grep -qx 'codename=trixie' <<<"$target_13"
grep -qx 'cloud_image=debian-13-genericcloud-amd64.qcow2' <<<"$target_13"
if bash "$ROOT_DIR/test/integration/libvirt/debian-target.sh" 11 >/dev/null 2>&1; then
  printf 'debian-target.sh unexpectedly accepted Debian 11\n' >&2
  exit 1
fi
if bash "$ROOT_DIR/test/integration/libvirt/debian-target.sh" "" >/dev/null 2>&1; then
  printf 'debian-target.sh unexpectedly accepted an empty Debian version\n' >&2
  exit 1
fi

TEMP_PLAN="$(mktemp "${TMPDIR:-/tmp}/dbf-core-noop-plan.XXXXXX.json")"
printf '%s\n' '{"format_version":"debianform.plan.alpha1","summary":{"create":0,"update":0,"delete":0,"no_op":1,"operations":0}}' >"$TEMP_PLAN"
python3 "$ROOT_DIR/test/integration/libvirt/assert-noop-plan.py" "$TEMP_PLAN"
printf '%s\n' '{"format_version":"debianform.plan.alpha1","summary":{"create":0,"update":1,"delete":0,"no_op":0,"operations":0}}' >"$TEMP_PLAN"
if python3 "$ROOT_DIR/test/integration/libvirt/assert-noop-plan.py" "$TEMP_PLAN" >/dev/null 2>&1; then
  printf 'assert-noop-plan.py unexpectedly accepted an update plan\n' >&2
  exit 1
fi
printf '%s\n' '{}' >"$TEMP_PLAN"
if python3 "$ROOT_DIR/test/integration/libvirt/assert-noop-plan.py" "$TEMP_PLAN" >/dev/null 2>&1; then
  printf 'assert-noop-plan.py unexpectedly accepted a document without a summary\n' >&2
  exit 1
fi

load_step_source_args() {
  local case_dir=$1
  local step=$2
  local default_config=$3
  local out_name=$4
  local -n out=$out_name
  local source_file="$case_dir/$step.sources"
  out=()
  if [[ ! -f "$source_file" ]]; then
    out=("-f" "$default_config")
    return
  fi

  local count=0
  local source
  while IFS= read -r source || [[ -n "$source" ]]; do
    source="${source%%#*}"
    source="${source#"${source%%[![:space:]]*}"}"
    source="${source%"${source##*[![:space:]]}"}"
    if [[ -z "$source" ]]; then
      continue
    fi
    out+=("-f" "$case_dir/$source")
    count=$((count + 1))
  done <"$source_file"
  if (( count == 0 )); then
    printf '%s: %s.sources must contain at least one source\n' "$(basename "$case_dir")" "$step" >&2
    return 1
  fi
}

failed=0
case_count=0
while IFS= read -r case_dir; do
  case_count=$((case_count + 1))
  case_name="$(basename "$case_dir")"
  two_host=0
  three_host=0
  if [[ -f "$case_dir/two-host.case" ]]; then
    two_host=1
  fi
  if [[ -f "$case_dir/three-host.case" ]]; then
    three_host=1
  fi
  configs=()
  next_step=1
  while [[ -f "$case_dir/$next_step.dbf.hcl" ]]; do
    configs+=("$case_dir/$next_step.dbf.hcl")
    next_step=$((next_step + 1))
  done
  config_count="$(find "$case_dir" -maxdepth 1 -type f -name '[0-9]*.dbf.hcl' | wc -l | tr -d '[:space:]')"
  if (( config_count != ${#configs[@]} )); then
    printf '%s: numbered configs must start at 1 and be contiguous\n' "$case_name" >&2
    failed=1
  fi
  if (( ${#configs[@]} < 2 )); then
    printf '%s: requires at least two numbered configs\n' "$case_name" >&2
    failed=1
    continue
  fi

  for config in "${configs[@]}"; do
    step="$(basename "$config" .dbf.hcl)"
    for companion in "$case_dir/$step.check.sh"; do
      if [[ ! -f "$companion" ]]; then
        printf '%s: missing %s\n' "$case_name" "$(basename "$companion")" >&2
        failed=1
      fi
    done
    if [[ -f "$case_dir/$step.check.sh" ]]; then
      bash -n "$case_dir/$step.check.sh"
      if ! grep -q 'assert_remote' "$case_dir/$step.check.sh"; then
        printf '%s: %s.check.sh must contain explicit assert_remote checks\n' "$case_name" "$step" >&2
        failed=1
      fi
    fi
    if [[ -f "$case_dir/$step.drift.sh" ]]; then
      bash -n "$case_dir/$step.drift.sh"
    fi

    declare -a source_args=()
    if ! load_step_source_args "$case_dir" "$step" "$config" source_args; then
      failed=1
      continue
    fi
    validation="$("$DBF_BIN" validate "${source_args[@]}")"
    printf '[layout:%s] %s\n' "$case_name" "$validation"

    if (( two_host == 1 )); then
      if ! grep -Eq '__DBF_WG_A_SSH_HOST__|host[[:space:]]+"wg-a"' "$config"; then
        printf '%s: two-host config %s should declare or template host wg-a\n' "$case_name" "$(basename "$config")" >&2
        failed=1
      fi
      if ! grep -Eq '__DBF_WG_B_SSH_HOST__|host[[:space:]]+"wg-b"' "$config"; then
        printf '%s: two-host config %s should declare or template host wg-b\n' "$case_name" "$(basename "$config")" >&2
        failed=1
      fi
    fi
    if (( three_host == 1 )); then
      if ! grep -Eq '__DBF_WG_A_SSH_HOST__|host[[:space:]]+"wg-a"' "$config"; then
        printf '%s: three-host config %s should declare or template host wg-a\n' "$case_name" "$(basename "$config")" >&2
        failed=1
      fi
      if ! grep -Eq '__DBF_WG_B_SSH_HOST__|host[[:space:]]+"wg-b"' "$config"; then
        printf '%s: three-host config %s should declare or template host wg-b\n' "$case_name" "$(basename "$config")" >&2
        failed=1
      fi
      if ! grep -Eq '__DBF_WG_C_SSH_HOST__|host[[:space:]]+"wg-c"' "$config"; then
        printf '%s: three-host config %s should declare or template host wg-c\n' "$case_name" "$(basename "$config")" >&2
        failed=1
      fi
    fi
  done
done < <(find "$CASES_DIR" -mindepth 1 -maxdepth 1 -type d | sort)

if (( case_count != EXPECTED_CASE_COUNT )); then
  printf 'expected %d integration cases under %s, found %d\n' \
    "$EXPECTED_CASE_COUNT" "$CASES_DIR" "$case_count" >&2
  exit 1
fi

exit "$failed"
