#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CASES_DIR="$ROOT_DIR/test/integration/libvirt/cases"
DBF_BIN="${DBF_INTEGRATION_DBF_BIN:-}"
TEMP_DBF=""

cleanup() {
  if [[ -n "$TEMP_DBF" ]]; then
    rm -f "$TEMP_DBF"
  fi
}
trap cleanup EXIT

if [[ -z "$DBF_BIN" ]]; then
  TEMP_DBF="$(mktemp "${TMPDIR:-/tmp}/dbf-v2-integration-layout.XXXXXX")"
  (
    cd "$ROOT_DIR"
    go build -o "$TEMP_DBF" ./cmd/dbf
  )
  DBF_BIN="$TEMP_DBF"
fi

failed=0
case_count=0
while IFS= read -r case_dir; do
  case_count=$((case_count + 1))
  case_name="$(basename "$case_dir")"
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

    validation="$("$DBF_BIN" validate -f "$config")"
    printf '[layout:%s] %s\n' "$case_name" "$validation"
  done
done < <(find "$CASES_DIR" -mindepth 1 -maxdepth 1 -type d | sort)

if (( case_count == 0 )); then
  printf 'no integration cases found under %s\n' "$CASES_DIR" >&2
  exit 1
fi

exit "$failed"
