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
  TEMP_DBF="$(mktemp "${TMPDIR:-/tmp}/dbf-integration-layout.XXXXXX")"
  (
    cd "$ROOT_DIR"
    go build -o "$TEMP_DBF" ./cmd/dbf
  )
  DBF_BIN="$TEMP_DBF"
fi

validate_config() {
  "$DBF_BIN" validate -f "$1"
}

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
  config_count="$(
    find "$case_dir" -maxdepth 1 -type f -name '[0-9]*.dbf.hcl' | wc -l | tr -d '[:space:]'
  )"
  if (( config_count != ${#configs[@]} )); then
    printf '%s: numbered configs must start at 1 and be contiguous\n' "$case_name" >&2
    failed=1
  fi
  if (( ${#configs[@]} < 2 )); then
    printf '%s: requires at least two numbered configs\n' "$case_name" >&2
    failed=1
    continue
  fi

  expected_step=1
  last_validation=""
  for config in "${configs[@]}"; do
    filename="$(basename "$config")"
    step="${filename%%.dbf.hcl}"
    if [[ "$step" != "$expected_step" ]]; then
      printf '%s: expected step %d, found %s\n' "$case_name" "$expected_step" "$filename" >&2
      failed=1
    fi

    for companion in "$case_dir/$step.plan" "$case_dir/$step.check.sh"; do
      if [[ ! -f "$companion" ]]; then
        printf '%s: missing %s\n' "$case_name" "$(basename "$companion")" >&2
        failed=1
      fi
    done

    if [[ -f "$case_dir/$step.check.sh" ]]; then
      bash -n "$case_dir/$step.check.sh"
      if ! grep -q 'assert_remote' "$case_dir/$step.check.sh"; then
        printf '%s: %s must contain explicit assert_remote checks\n' \
          "$case_name" "$step.check.sh" >&2
        failed=1
      fi
      if [[ -f "$case_dir/$step.plan" ]]; then
        plan_address_count="$(
          sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$case_dir/$step.plan" |
            wc -l |
            tr -d '[:space:]'
        )"
        check_count="$(grep -c '^assert_remote' "$case_dir/$step.check.sh" || true)"
        if (( check_count < plan_address_count )); then
          printf '%s: %s has %d checks for %d planned addresses\n' \
            "$case_name" "$step.check.sh" "$check_count" "$plan_address_count" >&2
          failed=1
        fi
      fi
    fi
    if [[ -f "$case_dir/$step.drift.sh" ]]; then
      bash -n "$case_dir/$step.drift.sh"
    fi

    validation="$(validate_config "$config")"
    last_validation="$validation"
    printf '[layout:%s] %s\n' "$case_name" "$validation"
    expected_step=$((expected_step + 1))
  done

  if ! grep -Eq 'configuration is valid: [0-9]+ host\(s\), 0 resource\(s\)' <<<"$last_validation"; then
    printf '%s: final config must contain zero resources\n' "$case_name" >&2
    failed=1
  fi
done < <(find "$CASES_DIR" -mindepth 1 -maxdepth 1 -type d | sort)

if (( case_count == 0 )); then
  printf 'no integration cases found under %s\n' "$CASES_DIR" >&2
  exit 1
fi

exit "$failed"
