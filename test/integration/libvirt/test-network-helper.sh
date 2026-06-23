#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_WORK="$(mktemp -d "${TMPDIR:-/tmp}/dbf-network-helper.XXXXXX")"
NETWORK_NAME="dbf-v2-network-helper-test-net"
NETWORK_DEFINED=0
BRIDGE_NAME=""
SUBNET_OCTET=""
ATTEMPTS="$CASE_WORK/attempts.log"
START_ATTEMPTS=0

cleanup() {
  rm -rf "$CASE_WORK"
}
trap cleanup EXIT

log() {
  printf '[network-helper-test] %s\n' "$*"
}

fail() {
  printf '[network-helper-test] ERROR: %s\n' "$*" >&2
  return 1
}

virsh_system() {
  case "$1" in
    net-define)
      printf 'define:%s:%s\n' "$SUBNET_OCTET" "$BRIDGE_NAME" >>"$ATTEMPTS"
      return 0
      ;;
    net-start)
      START_ATTEMPTS=$((START_ATTEMPTS + 1))
      printf 'start:%s\n' "$SUBNET_OCTET" >>"$ATTEMPTS"
      if (( START_ATTEMPTS == 1 )); then
        printf 'error: internal error: Network is already in use by interface virbr0\n' >&2
        return 1
      fi
      return 0
      ;;
    net-destroy)
      printf 'destroy:%s\n' "$SUBNET_OCTET" >>"$ATTEMPTS"
      return 0
      ;;
    net-undefine)
      printf 'undefine:%s\n' "$SUBNET_OCTET" >>"$ATTEMPTS"
      return 0
      ;;
    *)
      fail "unexpected virsh command: $*"
      ;;
  esac
}

source "$SCRIPT_DIR/network.sh"

dbf_integration_start_network "$CASE_WORK/network.xml"

grep -Fx 'define:200:virbr-dbf-200' "$ATTEMPTS" >/dev/null
grep -Fx 'start:200' "$ATTEMPTS" >/dev/null
grep -Fx 'destroy:200' "$ATTEMPTS" >/dev/null
grep -Fx 'undefine:200' "$ATTEMPTS" >/dev/null
grep -Fx 'define:201:virbr-dbf-201' "$ATTEMPTS" >/dev/null
grep -Fx 'start:201' "$ATTEMPTS" >/dev/null
grep -F "192.168.201.1" "$CASE_WORK/network.xml" >/dev/null
test "$NETWORK_DEFINED" -eq 1
