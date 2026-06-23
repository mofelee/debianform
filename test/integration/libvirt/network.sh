#!/usr/bin/env bash

dbf_integration_candidate_subnets() {
  local octet

  if [[ -n "${DBF_INTEGRATION_SUBNET_OCTET:-}" ]]; then
    if [[ ! "$DBF_INTEGRATION_SUBNET_OCTET" =~ ^[0-9]+$ ]] ||
      (( DBF_INTEGRATION_SUBNET_OCTET < 1 || DBF_INTEGRATION_SUBNET_OCTET > 254 )); then
      fail "DBF_INTEGRATION_SUBNET_OCTET must be between 1 and 254"
    fi
    printf '%s\n' "$DBF_INTEGRATION_SUBNET_OCTET"
    return 0
  fi

  for (( octet = 200; octet <= 254; octet++ )); do
    printf '%s\n' "$octet"
  done
  for (( octet = 100; octet <= 199; octet++ )); do
    printf '%s\n' "$octet"
  done
}

dbf_integration_write_network_xml() {
  local network_xml=$1

  cat >"$network_xml" <<EOF
<network>
  <name>$NETWORK_NAME</name>
  <forward mode='nat'/>
  <bridge name='$BRIDGE_NAME' stp='on' delay='0'/>
  <ip address='192.168.$SUBNET_OCTET.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.$SUBNET_OCTET.10' end='192.168.$SUBNET_OCTET.250'/>
    </dhcp>
  </ip>
</network>
EOF
}

dbf_integration_is_retryable_network_error() {
  local log_file=$1

  grep -Eiq \
    'already in use|overlap|conflict|address.*in use|file exists' \
    "$log_file"
}

dbf_integration_start_network() {
  local network_xml=$1
  local define_log="$CASE_WORK/network-define.log"
  local start_log="$CASE_WORK/network-start.log"
  local tried=0

  while IFS= read -r SUBNET_OCTET; do
    tried=$((tried + 1))
    BRIDGE_NAME="virbr-dbf-$SUBNET_OCTET"
    dbf_integration_write_network_xml "$network_xml"

    if ! virsh_system net-define "$network_xml" >"$define_log" 2>&1; then
      cat "$define_log" >&2
      fail "failed to define libvirt network $NETWORK_NAME"
    fi
    NETWORK_DEFINED=1

    if virsh_system net-start "$NETWORK_NAME" >"$start_log" 2>&1; then
      log "libvirt network $NETWORK_NAME uses 192.168.$SUBNET_OCTET.0/24 on $BRIDGE_NAME"
      return 0
    fi

    virsh_system net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
    virsh_system net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
    NETWORK_DEFINED=0

    if [[ -n "${DBF_INTEGRATION_SUBNET_OCTET:-}" ]] ||
      ! dbf_integration_is_retryable_network_error "$start_log"; then
      cat "$start_log" >&2
      fail "failed to start libvirt network $NETWORK_NAME"
    fi

    log "libvirt subnet 192.168.$SUBNET_OCTET.0/24 is unavailable; trying another"
  done < <(dbf_integration_candidate_subnets)

  if [[ -s "$start_log" ]]; then
    cat "$start_log" >&2
  fi
  fail "could not find an available libvirt subnet after $tried attempts"
}
