#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_SOURCE="${1:?usage: run-three-host-case.sh CASE_DIR}"
CASE_NAME="$(basename "$CASE_SOURCE")"
DBF_BIN="${DBF_INTEGRATION_DBF_BIN:?missing DBF_INTEGRATION_DBF_BIN}"
BASE_IMAGE="${DBF_INTEGRATION_BASE_IMAGE:?missing DBF_INTEGRATION_BASE_IMAGE}"
CASE_WORK="${DBF_INTEGRATION_CASE_WORK:?missing DBF_INTEGRATION_CASE_WORK}"
ARTIFACT_DIR="${DBF_INTEGRATION_CASE_ARTIFACTS:?missing DBF_INTEGRATION_CASE_ARTIFACTS}"
CASE_DIR="$CASE_WORK/scenario"
LOG_DIR="$CASE_WORK/logs"
DBF_HOME="$CASE_WORK/home"
SSH_KEY="$CASE_WORK/id_ed25519"
LIBVIRT_URI="${DBF_LIBVIRT_URI:-${VIRSH_DEFAULT_CONNECT_URI:-${LIBVIRT_DEFAULT_URI:-}}}"
REMOTE_HYPERVISOR=""
REMOTE_TMP_DIR=""
REMOTE_POOL="${DBF_INTEGRATION_POOL:-vm}"
REMOTE_BASE_IMAGE="${DBF_INTEGRATION_REMOTE_BASE_IMAGE:-}"

RUN_SUFFIX="${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}-${RANDOM}"
SAFE_CASE_NAME="$(tr -c 'a-zA-Z0-9' '-' <<<"$CASE_NAME" | sed 's/-*$//')"
NETWORK_NAME="dbf-v2-${SAFE_CASE_NAME}-${RUN_SUFFIX}-net"
BRIDGE_NAME=""
SUBNET_OCTET=""
VIRT_TYPE="qemu"
NETWORK_DEFINED=0
CURRENT_STEP=""
ASSERTION_COUNT=0

HOST_LABELS=(a b c)
HOST_ALIASES=(wg-a wg-b wg-c)
declare -A VM_NAMES VM_MACS VM_IPS VM_DISKS VM_SEEDS VM_DOMAINS VM_CONSOLES VM_DEFINED VM_STARTED
for label in "${HOST_LABELS[@]}"; do
  VM_NAMES[$label]="dbf-v2-${SAFE_CASE_NAME}-${label}-${RUN_SUFFIX}"
  printf -v "VM_MACS[$label]" '52:54:00:%02x:%02x:%02x' \
    "$((RANDOM % 256))" "$((RANDOM % 256))" "$((RANDOM % 256))"
  VM_DEFINED[$label]=0
  VM_STARTED[$label]=0
done

log() {
  printf '[integration:%s] %s\n' "$CASE_NAME" "$*"
}

fail() {
  printf '[integration:%s] ERROR: %s\n' "$CASE_NAME" "$*" >&2
  return 1
}

virsh_system() {
  if [[ -n "$LIBVIRT_URI" ]]; then
    virsh --connect "$LIBVIRT_URI" "$@"
  else
    sudo virsh --connect qemu:///system "$@"
  fi
}

infer_remote_hypervisor() {
  case "$LIBVIRT_URI" in
    qemu+ssh://*|qemu+libssh://*|qemu+libssh2://*)
      local rest="${LIBVIRT_URI#*://}"
      printf '%s\n' "${rest%%/*}"
      ;;
  esac
}

is_remote_libvirt() {
  [[ -n "$REMOTE_HYPERVISOR" ]]
}

remote_exec() {
  if is_remote_libvirt; then
    ssh "$REMOTE_HYPERVISOR" "$@"
  else
    "$@"
  fi
}

remote_write_file() {
  local path=$1
  if is_remote_libvirt; then
    ssh "$REMOTE_HYPERVISOR" "cat > '$path'"
  else
    cat >"$path"
  fi
}

remote_read_file() {
  local path=$1
  if is_remote_libvirt; then
    ssh "$REMOTE_HYPERVISOR" "cat '$path'"
  else
    cat "$path"
  fi
}

pool_path() {
  virsh_system pool-dumpxml "$REMOTE_POOL" | python3 -c '
import sys
import xml.etree.ElementTree as ET

root = ET.fromstring(sys.stdin.read())
target = root.find("target")
path = target.findtext("path") if target is not None else None
if not path:
    raise SystemExit("pool target path not found")
print(path)
'
}

emulator_path() {
  if [[ -n "${DBF_INTEGRATION_EMULATOR:-}" ]]; then
    printf '%s\n' "$DBF_INTEGRATION_EMULATOR"
    return
  fi
  if remote_exec command -v qemu-system-x86_64 >/dev/null 2>&1; then
    remote_exec command -v qemu-system-x86_64
    return
  fi
  printf '/usr/bin/qemu-system-x86_64\n'
}

prepare_vm_paths() {
  local pool_dir=""
  if is_remote_libvirt; then
    pool_dir="$(pool_path)"
    REMOTE_TMP_DIR="$pool_dir/.dbf-v2-${SAFE_CASE_NAME}-${RUN_SUFFIX}"
    if [[ -z "$REMOTE_BASE_IMAGE" ]]; then
      REMOTE_BASE_IMAGE="$pool_dir/$(basename "$BASE_IMAGE")"
    fi
    log "remote libvirt URI: $LIBVIRT_URI"
    log "remote hypervisor: $REMOTE_HYPERVISOR"
    log "remote storage pool: $REMOTE_POOL ($pool_dir)"
    remote_exec mkdir -p "$REMOTE_TMP_DIR"
    if ! remote_exec test -f "$REMOTE_BASE_IMAGE"; then
      log "copying verified Debian base image to $REMOTE_HYPERVISOR:$REMOTE_BASE_IMAGE"
      scp -q "$BASE_IMAGE" "$REMOTE_HYPERVISOR:$REMOTE_BASE_IMAGE"
      remote_exec chmod 0644 "$REMOTE_BASE_IMAGE"
    fi
  fi

  for label in "${HOST_LABELS[@]}"; do
    if is_remote_libvirt; then
      VM_DISKS[$label]="$pool_dir/${VM_NAMES[$label]}.qcow2"
      VM_SEEDS[$label]="$pool_dir/${VM_NAMES[$label]}-seed.img"
      VM_CONSOLES[$label]="$pool_dir/${VM_NAMES[$label]}-console.log"
    else
      VM_DISKS[$label]="$CASE_WORK/vm-${label}.qcow2"
      VM_SEEDS[$label]="$CASE_WORK/seed-${label}.img"
      VM_CONSOLES[$label]="$CASE_WORK/console-${label}.log"
    fi
    VM_DOMAINS[$label]="$CASE_WORK/domain-${label}.xml"
  done
}

cleanup_vm_files() {
  if is_remote_libvirt; then
    for label in "${HOST_LABELS[@]}"; do
      remote_exec rm -f "${VM_DISKS[$label]}" "${VM_SEEDS[$label]}" "${VM_CONSOLES[$label]}" >/dev/null 2>&1 || true
    done
    remote_exec rm -rf "$REMOTE_TMP_DIR" >/dev/null 2>&1 || true
  fi
}

ssh_host() {
  local host=$1
  shift
  ssh \
    -F "$DBF_HOME/.ssh/config" \
    -o BatchMode=yes \
    -o ConnectTimeout=5 \
    "$host" "$@"
}

dbf() {
  HOME="$DBF_HOME" DBF_SSH_CONFIG="$DBF_HOME/.ssh/config" "$DBF_BIN" "$@"
}

run_remote() {
  local host description command
  if (( $# == 3 )); then
    host=$1
    description=$2
    command=$3
  elif (( $# == 2 )); then
    host=wg-a
    description=$1
    command=$2
  else
    fail "run_remote expects HOST DESCRIPTION COMMAND or DESCRIPTION COMMAND"
  fi
  log "ACTION[$host]: $description"
  ssh_host "$host" "$command"
}

assert_remote() {
  local host description command
  if (( $# == 3 )); then
    host=$1
    description=$2
    command=$3
  elif (( $# == 2 )); then
    host=wg-a
    description=$1
    command=$2
  else
    fail "assert_remote expects HOST DESCRIPTION COMMAND or DESCRIPTION COMMAND"
  fi
  ASSERTION_COUNT=$((ASSERTION_COUNT + 1))
  log "ASSERT ${ASSERTION_COUNT}[$host]: $description"
  if ! ssh_host "$host" "$command"; then
    fail "$description"
  fi
}

collect_diagnostics() {
  mkdir -p "$ARTIFACT_DIR"
  virsh_system list --all >"$ARTIFACT_DIR/virsh-list.txt" 2>&1 || true
  virsh_system net-list --all >"$ARTIFACT_DIR/virsh-net-list.txt" 2>&1 || true
  for label in "${HOST_LABELS[@]}"; do
    local vm_name="${VM_NAMES[$label]}"
    virsh_system dumpxml "$vm_name" >"$ARTIFACT_DIR/domain-${label}.xml" 2>&1 || true
    virsh_system domifaddr "$vm_name" --source lease >"$ARTIFACT_DIR/domifaddr-${label}.txt" 2>&1 || true
    if is_remote_libvirt; then
      ssh "$REMOTE_HYPERVISOR" "cat /var/log/libvirt/qemu/$vm_name.log" >"$ARTIFACT_DIR/qemu-${label}.log" 2>&1 || true
      remote_read_file "${VM_CONSOLES[$label]}" >"$ARTIFACT_DIR/console-${label}.log" 2>&1 || true
    else
      sudo sh -c "cat /var/log/libvirt/qemu/$vm_name.log" >"$ARTIFACT_DIR/qemu-${label}.log" 2>&1 || true
      sudo sh -c "cat '${VM_CONSOLES[$label]}'" >"$ARTIFACT_DIR/console-${label}.log" 2>&1 || true
    fi
  done
  if is_remote_libvirt; then
    ssh "$REMOTE_HYPERVISOR" "journalctl -u libvirtd --no-pager -n 300" >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
  else
    sudo journalctl -u libvirtd --no-pager -n 300 >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
  fi
  cp -a "$LOG_DIR" "$ARTIFACT_DIR/logs" 2>/dev/null || true
  cp -a "$CASE_DIR" "$ARTIFACT_DIR/scenario" 2>/dev/null || true
}

cleanup() {
  local status=$?
  trap - EXIT

  if (( status != 0 )); then
    for label in "${HOST_LABELS[@]}"; do
      if (( VM_DEFINED[$label] == 1 )); then
        log "case failed; collecting diagnostics in $ARTIFACT_DIR"
        collect_diagnostics
        break
      fi
    done
  fi
  for label in "${HOST_LABELS[@]}"; do
    if (( VM_STARTED[$label] == 1 )); then
      virsh_system destroy "${VM_NAMES[$label]}" >/dev/null 2>&1 || true
    fi
  done
  for label in "${HOST_LABELS[@]}"; do
    if (( VM_DEFINED[$label] == 1 )); then
      virsh_system undefine "${VM_NAMES[$label]}" --nvram >/dev/null 2>&1 ||
        virsh_system undefine "${VM_NAMES[$label]}" >/dev/null 2>&1 || true
    fi
  done
  if (( NETWORK_DEFINED == 1 )); then
    virsh_system net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
    virsh_system net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
  fi
  cleanup_vm_files

  exit "$status"
}
trap cleanup EXIT

wait_for_vm_ip() {
  local label=$1
  local vm_name="${VM_NAMES[$label]}"
  local deadline=$((SECONDS + 240))
  local ip
  while (( SECONDS < deadline )); do
    ip="$(
      virsh_system domifaddr "$vm_name" --source lease 2>/dev/null |
        awk '$3 == "ipv4" { split($4, address, "/"); print address[1]; exit }'
    )"
    if [[ -n "$ip" ]]; then
      VM_IPS[$label]="$ip"
      printf '%s\n' "$ip"
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_for_ssh() {
  local host=$1
  local deadline=$((SECONDS + 300))
  while (( SECONDS < deadline )); do
    if ssh_host "$host" true >/dev/null 2>&1; then
      return 0
    fi
    sleep 3
  done
  return 1
}

run_hook() {
  local hook=$1
  log "running $(basename "$hook")"
  source "$hook"
}

source "$SCRIPT_DIR/network.sh"

write_seed() {
  local label=$1
  local seed_image="${VM_SEEDS[$label]}"
  local user_data="$CASE_WORK/user-data-$label"
  local meta_data="$CASE_WORK/meta-data-$label"
  local network_config="$CASE_WORK/network-config-$label"
  if is_remote_libvirt; then
    user_data="$REMOTE_TMP_DIR/user-data-$label"
    meta_data="$REMOTE_TMP_DIR/meta-data-$label"
    network_config="$REMOTE_TMP_DIR/network-config-$label"
  fi

  remote_write_file "$user_data" <<EOF
#cloud-config
disable_root: false
ssh_pwauth: false
users:
  - name: root
    lock_passwd: true
    ssh_authorized_keys:
      - $PUBLIC_KEY
runcmd:
  - [sh, -c, "touch /run/debianform-cloud-init-ready"]
EOF

  remote_write_file "$meta_data" <<EOF
instance-id: ${VM_NAMES[$label]}
local-hostname: ${VM_NAMES[$label]}
EOF

  remote_write_file "$network_config" <<EOF
version: 2
ethernets:
  primary:
    match:
      macaddress: "${VM_MACS[$label]}"
    dhcp4: true
EOF

  remote_exec cloud-localds \
    --network-config="$network_config" \
    "$seed_image" \
    "$user_data" \
    "$meta_data"
}

write_domain() {
  local label=$1
  local domain_xml="${VM_DOMAINS[$label]}"

  cat >"$domain_xml" <<EOF
<domain type='$VIRT_TYPE'>
  <name>${VM_NAMES[$label]}</name>
  <memory unit='MiB'>1024</memory>
  <vcpu>2</vcpu>
  <os firmware='efi'>
    <type arch='x86_64' machine='q35'>hvm</type>
    <firmware>
      <feature enabled='no' name='enrolled-keys'/>
      <feature enabled='no' name='secure-boot'/>
    </firmware>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
  </features>
  $CPU_XML
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <emulator>$EMULATOR_PATH</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='${VM_DISKS[$label]}'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='${VM_SEEDS[$label]}'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
    <interface type='network'>
      <mac address='${VM_MACS[$label]}'/>
      <source network='$NETWORK_NAME'/>
      <model type='virtio'/>
    </interface>
    <serial type='pty'>
      <log file='${VM_CONSOLES[$label]}' append='off'/>
      <target port='0'/>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
  </devices>
</domain>
EOF
}

mkdir -p "$CASE_WORK" "$CASE_DIR" "$LOG_DIR" "$DBF_HOME/.ssh"
cp -a "$CASE_SOURCE/." "$CASE_DIR/"
chmod 0700 "$DBF_HOME" "$DBF_HOME/.ssh"

REMOTE_HYPERVISOR="${DBF_INTEGRATION_HYPERVISOR:-$(infer_remote_hypervisor)}"
prepare_vm_paths

if [[ "${DBF_INTEGRATION_DISABLE_KVM:-0}" != "1" ]] &&
  remote_exec test -r /dev/kvm &&
  remote_exec test -w /dev/kvm; then
  VIRT_TYPE="kvm"
fi

ssh-keygen -q -t ed25519 -N "" -f "$SSH_KEY"
cp "$SSH_KEY" "$CASE_DIR/id_ed25519"
chmod 0600 "$CASE_DIR/id_ed25519"
PUBLIC_KEY="$(cat "$SSH_KEY.pub")"

for label in "${HOST_LABELS[@]}"; do
  write_seed "$label"
  remote_exec qemu-img create -q -f qcow2 -F qcow2 -b "${REMOTE_BASE_IMAGE:-$BASE_IMAGE}" "${VM_DISKS[$label]}" 12G
done
if is_remote_libvirt; then
  for label in "${HOST_LABELS[@]}"; do
    remote_exec chmod 0644 "${VM_SEEDS[$label]}"
    remote_exec chmod 0666 "${VM_DISKS[$label]}"
  done
else
  chmod 0755 "$CASE_WORK"
  for label in "${HOST_LABELS[@]}"; do
    chmod 0644 "${VM_SEEDS[$label]}"
    chmod 0666 "${VM_DISKS[$label]}"
  done
fi

CPU_XML=""
if [[ "$VIRT_TYPE" == "kvm" ]]; then
  CPU_XML="<cpu mode='host-passthrough' check='none'/>"
fi
EMULATOR_PATH="$(emulator_path)"
for label in "${HOST_LABELS[@]}"; do
  write_domain "$label"
done

log "starting fresh Debian VMs using $VIRT_TYPE"
dbf_integration_start_network "$CASE_WORK/network.xml"
for label in "${HOST_LABELS[@]}"; do
  virsh_system define "${VM_DOMAINS[$label]}" >/dev/null
  VM_DEFINED[$label]=1
done
for label in "${HOST_LABELS[@]}"; do
  virsh_system start "${VM_NAMES[$label]}" >/dev/null
  VM_STARTED[$label]=1
done

for label in "${HOST_LABELS[@]}"; do
  wait_for_vm_ip "$label" >/dev/null
  log "VM $label acquired address ${VM_IPS[$label]}"
done

{
  for i in "${!HOST_LABELS[@]}"; do
    label="${HOST_LABELS[$i]}"
    alias="${HOST_ALIASES[$i]}"
    cat <<EOF
Host $alias
  HostName ${VM_IPS[$label]}
  User root
  IdentityFile $SSH_KEY
$(if is_remote_libvirt; then printf '  ProxyCommand ssh -W %%h:%%p %s\n' "$REMOTE_HYPERVISOR"; fi)
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null

EOF
  done
  cat <<EOF
Host cihost
  HostName ${VM_IPS[a]}
  User root
  IdentityFile $SSH_KEY
$(if is_remote_libvirt; then printf '  ProxyCommand ssh -W %%h:%%p %s\n' "$REMOTE_HYPERVISOR"; fi)
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
EOF
} >"$DBF_HOME/.ssh/config"
chmod 0600 "$DBF_HOME/.ssh/config"

for alias in "${HOST_ALIASES[@]}"; do
  wait_for_ssh "$alias"
  ssh_host "$alias" "cloud-init status --wait >/dev/null && test -e /run/debianform-cloud-init-ready"
  ssh_host "$alias" ". /etc/os-release && test \"\$ID\" = debian && test \"\$VERSION_ID\" = 13"
done

DBF_WG_A_HOST="${VM_IPS[a]}"
DBF_WG_B_HOST="${VM_IPS[b]}"
DBF_WG_C_HOST="${VM_IPS[c]}"
if is_remote_libvirt; then
  DBF_WG_A_HOST="wg-a"
  DBF_WG_B_HOST="wg-b"
  DBF_WG_C_HOST="wg-c"
fi
while IFS= read -r config; do
  sed -i \
    -e "s/__DBF_WG_A_SSH_HOST__/$DBF_WG_A_HOST/g" \
    -e "s/__DBF_WG_B_SSH_HOST__/$DBF_WG_B_HOST/g" \
    -e "s/__DBF_WG_C_SSH_HOST__/$DBF_WG_C_HOST/g" \
    -e "s/__DBF_WG_A_VM_IP__/${VM_IPS[a]}/g" \
    -e "s/__DBF_WG_B_VM_IP__/${VM_IPS[b]}/g" \
    -e "s/__DBF_WG_C_VM_IP__/${VM_IPS[c]}/g" \
    -e "s/__DBF_VM_IP__/$DBF_WG_A_HOST/g" \
    "$config"
done < <(find "$CASE_DIR" -maxdepth 3 -type f \( -name '[0-9]*.dbf.hcl' -o -name '*.conf' -o -name '*.sh' \))

declare -a CONFIGS=()
next_step=1
while [[ -f "$CASE_DIR/$next_step.dbf.hcl" ]]; do
  CONFIGS+=("$CASE_DIR/$next_step.dbf.hcl")
  next_step=$((next_step + 1))
done
config_count="$(find "$CASE_DIR" -maxdepth 1 -type f -name '[0-9]*.dbf.hcl' | wc -l | tr -d '[:space:]')"
if (( config_count != ${#CONFIGS[@]} )); then
  fail "numbered configs must start at 1 and be contiguous"
fi
if (( ${#CONFIGS[@]} < 2 )); then
  fail "case must contain at least an apply config and a final destroy config"
fi

for config in "${CONFIGS[@]}"; do
  filename="$(basename "$config")"
  CURRENT_STEP="${filename%%.dbf.hcl}"
  check_hook="$CASE_DIR/$CURRENT_STEP.check.sh"
  drift_hook="$CASE_DIR/$CURRENT_STEP.drift.sh"

  if [[ ! -f "$check_hook" ]]; then
    fail "missing post-apply checks: $check_hook"
  fi

  log "step $CURRENT_STEP: validating $filename"
  dbf validate -f "$config" | tee "$LOG_DIR/$CURRENT_STEP.validate.log"

  if [[ -f "$drift_hook" ]]; then
    run_hook "$drift_hook"
    log "step $CURRENT_STEP: verifying dbf check rejects drift"
    if dbf check -f "$config" >"$LOG_DIR/$CURRENT_STEP.drift-check.log" 2>&1; then
      cat "$LOG_DIR/$CURRENT_STEP.drift-check.log"
      fail "dbf check unexpectedly accepted drift for step $CURRENT_STEP"
    fi
    cat "$LOG_DIR/$CURRENT_STEP.drift-check.log"
  fi

  log "step $CURRENT_STEP: applying"
  dbf apply -f "$config" --parallel 3 --auto-approve | tee "$LOG_DIR/$CURRENT_STEP.apply.log"

  log "step $CURRENT_STEP: checking convergence"
  dbf check -f "$config" | tee "$LOG_DIR/$CURRENT_STEP.check.log"

  run_hook "$check_hook"
done

if (( ASSERTION_COUNT == 0 )); then
  fail "case must run at least one explicit assertion"
fi
log "case passed with $ASSERTION_COUNT explicit assertions"
