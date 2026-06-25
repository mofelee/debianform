#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SKILL_DIR="${DBF_VIRSH_TEST_HOST_DIR:-/root/.codex/skills/virsh-test-host}"
VIRSH_HELPER="${DBF_VIRSH_TEST_HOST_HELPER:-$SKILL_DIR/scripts/virsh-test-host.sh}"

DOMAIN="${DBF_DEMO_DOMAIN:-dbf-test-readme-demo}"
ALIAS="${DBF_DEMO_HOST_ALIAS:-demo1}"
CAST_PATH="${DBF_DEMO_CAST_PATH:-$ROOT_DIR/docs/demo/debianform-quickstart.cast}"
DBF_BIN="${DBF_DEMO_DBF_BIN:-}"
WORK_DIR=""
CREATED_DOMAIN=0

cleanup() {
  local status=$?
  if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then
    rm -rf "$WORK_DIR"
  fi
  if [[ "$CREATED_DOMAIN" == "1" ]]; then
    DBF_TEST_NAME="$DOMAIN" "$VIRSH_HELPER" destroy "$DOMAIN" >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT

die() {
  printf 'record-readme-demo: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

require_command asciinema
require_command go
require_command ssh
require_command sed

[[ -x "$VIRSH_HELPER" ]] || die "virsh test host helper not found or not executable: $VIRSH_HELPER"

cd "$ROOT_DIR"
mkdir -p "$(dirname "$CAST_PATH")"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/debianform-readme-demo.XXXXXX")"
mkdir -p "$WORK_DIR/session"
if [[ -z "$DBF_BIN" ]]; then
  DBF_BIN="$WORK_DIR/dbf"
fi

printf '[demo] building dbf binary: %s\n' "$DBF_BIN"
mkdir -p "$(dirname "$DBF_BIN")"
go build -buildvcs=false -o "$DBF_BIN" ./cmd/dbf

printf '[demo] probing libvirt\n'
"$VIRSH_HELPER" probe

if "$VIRSH_HELPER" wait-ip "$DOMAIN" >/dev/null 2>&1; then
  die "domain already appears to exist: $DOMAIN; destroy it explicitly before recording"
fi

printf '[demo] creating disposable VM: %s\n' "$DOMAIN"
DBF_TEST_NAME="$DOMAIN" "$VIRSH_HELPER" create
CREATED_DOMAIN=1

printf '[demo] waiting for guest IP\n'
"$VIRSH_HELPER" wait-ip "$DOMAIN" >/dev/null

SSH_CONFIG="$WORK_DIR/ssh_config"
{
  printf 'Include ~/.ssh/config\n'
  printf '\n'
} >"$SSH_CONFIG"
DBF_TEST_SSH_HOST_ALIAS="$ALIAS" "$VIRSH_HELPER" ssh-config "$DOMAIN" >>"$SSH_CONFIG"
chmod 600 "$SSH_CONFIG"

printf '[demo] waiting for SSH readiness through alias: %s\n' "$ALIAS"
for _ in $(seq 1 60); do
  if ssh -F "$SSH_CONFIG" -o BatchMode=yes -o ConnectTimeout=10 "$ALIAS" 'true' >/dev/null 2>&1; then
    break
  fi
  printf '.'
  sleep 2
done
printf '\n'
ssh -F "$SSH_CONFIG" -o BatchMode=yes -o ConnectTimeout=10 "$ALIAS" 'true' >/dev/null

printf '[demo] recording asciinema cast: %s\n' "$CAST_PATH"
rm -f "$CAST_PATH"
asciinema rec \
  --overwrite \
  --quiet \
  --cols "${DBF_DEMO_COLS:-90}" \
  --rows "${DBF_DEMO_ROWS:-28}" \
  --idle-time-limit "${DBF_DEMO_IDLE_TIME_LIMIT:-2}" \
  --title "DebianForm quickstart" \
  --env "SHELL,TERM" \
  --command "env DBF_DEMO_ROOT_DIR='$ROOT_DIR' DBF_DEMO_WORK_DIR='$WORK_DIR/session' DBF_DEMO_DBF_BIN='$DBF_BIN' DBF_DEMO_SSH_CONFIG='$SSH_CONFIG' DBF_DEMO_HOST_ALIAS='$ALIAS' '$ROOT_DIR/docs/demo/run-readme-demo-session.sh'" \
  "$CAST_PATH"

asciinema cat "$CAST_PATH" |
  grep -F 'Done: plan, apply, no-op plan, and drift check all passed.' >/dev/null ||
  die "recorded session did not complete successfully"

printf '[demo] cast recorded: %s\n' "$CAST_PATH"
printf '[demo] render with: docs/demo/render-readme-demo.sh\n'
