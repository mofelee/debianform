#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_SOURCE="${1:?usage: run-case.sh CASE_DIR}"
CASE_NAME="$(basename "$CASE_SOURCE")"
DBF_BIN="${DBF_INTEGRATION_DBF_BIN:?missing DBF_INTEGRATION_DBF_BIN}"
BASE_IMAGE="${DBF_INTEGRATION_BASE_IMAGE:?missing DBF_INTEGRATION_BASE_IMAGE}"
CASE_WORK="${DBF_INTEGRATION_CASE_WORK:?missing DBF_INTEGRATION_CASE_WORK}"
ARTIFACT_DIR="${DBF_INTEGRATION_CASE_ARTIFACTS:?missing DBF_INTEGRATION_CASE_ARTIFACTS}"
CASE_DIR="$CASE_WORK/scenario"
LOG_DIR="$CASE_WORK/logs"
DBF_HOME="$CASE_WORK/home"
SSH_KEY="$CASE_WORK/id_ed25519"
VM_DISK="$CASE_WORK/vm.qcow2"
SEED_IMAGE="$CASE_WORK/seed.img"
CONSOLE_LOG="$CASE_WORK/console.log"
DOMAIN_XML="$CASE_WORK/domain.xml"
LIBVIRT_URI="${DBF_LIBVIRT_URI:-${VIRSH_DEFAULT_CONNECT_URI:-${LIBVIRT_DEFAULT_URI:-}}}"
REMOTE_HYPERVISOR=""
REMOTE_TMP_DIR=""
REMOTE_POOL="${DBF_INTEGRATION_POOL:-vm}"
REMOTE_BASE_IMAGE="${DBF_INTEGRATION_REMOTE_BASE_IMAGE:-}"

RUN_SUFFIX="${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}-${RANDOM}"
SAFE_CASE_NAME="$(tr -c 'a-zA-Z0-9' '-' <<<"$CASE_NAME" | sed 's/-*$//')"
VM_NAME="dbf-core-${SAFE_CASE_NAME}-${RUN_SUFFIX}"
NETWORK_NAME="${VM_NAME}-net"
BRIDGE_NAME=""
SUBNET_OCTET=""
printf -v MAC_ADDRESS '52:54:00:%02x:%02x:%02x' \
  "$((RANDOM % 256))" "$((RANDOM % 256))" "$((RANDOM % 256))"

VM_IP=""
VIRT_TYPE="qemu"
NETWORK_DEFINED=0
VM_DEFINED=0
VM_STARTED=0
CURRENT_STEP=""
ASSERTION_COUNT=0

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
  if ! is_remote_libvirt; then
    return
  fi

  local pool_dir
  pool_dir="$(pool_path)"
  VM_DISK="$pool_dir/$VM_NAME.qcow2"
  SEED_IMAGE="$pool_dir/$VM_NAME-seed.img"
  CONSOLE_LOG="$pool_dir/$VM_NAME-console.log"
  DOMAIN_XML="$CASE_WORK/domain.xml"
  REMOTE_TMP_DIR="$pool_dir/.dbf-core-$VM_NAME"
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
}

cleanup_vm_files() {
  if is_remote_libvirt; then
    remote_exec rm -rf "$VM_DISK" "$SEED_IMAGE" "$CONSOLE_LOG" "$REMOTE_TMP_DIR" >/dev/null 2>&1 || true
  fi
}

ssh_vm() {
  ssh \
    -F "$DBF_HOME/.ssh/config" \
    -o BatchMode=yes \
    -o ConnectTimeout=5 \
    cihost "$@"
}

dbf() {
  HOME="$DBF_HOME" DBF_SSH_CONFIG="$DBF_HOME/.ssh/config" "$DBF_BIN" "$@"
}

run_remote() {
  local description=$1
  local command=$2
  log "ACTION: $description"
  ssh_vm "$command"
}

assert_remote() {
  local description=$1
  local command=$2
  ASSERTION_COUNT=$((ASSERTION_COUNT + 1))
  log "ASSERT $ASSERTION_COUNT: $description"
  if ! ssh_vm "$command"; then
    fail "$description"
  fi
}

collect_diagnostics() {
  mkdir -p "$ARTIFACT_DIR"
  virsh_system list --all >"$ARTIFACT_DIR/virsh-list.txt" 2>&1 || true
  virsh_system net-list --all >"$ARTIFACT_DIR/virsh-net-list.txt" 2>&1 || true
  virsh_system dumpxml "$VM_NAME" >"$ARTIFACT_DIR/domain.xml" 2>&1 || true
  virsh_system domifaddr "$VM_NAME" --source lease >"$ARTIFACT_DIR/domifaddr.txt" 2>&1 || true
  if is_remote_libvirt; then
    ssh "$REMOTE_HYPERVISOR" "journalctl -u libvirtd --no-pager -n 300" >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
    ssh "$REMOTE_HYPERVISOR" "cat /var/log/libvirt/qemu/$VM_NAME.log" >"$ARTIFACT_DIR/qemu.log" 2>&1 || true
    remote_read_file "$CONSOLE_LOG" >"$ARTIFACT_DIR/console.log" 2>&1 || true
  else
    sudo journalctl -u libvirtd --no-pager -n 300 >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
    sudo sh -c "cat /var/log/libvirt/qemu/$VM_NAME.log" >"$ARTIFACT_DIR/qemu.log" 2>&1 || true
    sudo sh -c "cat '$CONSOLE_LOG'" >"$ARTIFACT_DIR/console.log" 2>&1 || true
  fi
  cp -a "$LOG_DIR" "$ARTIFACT_DIR/logs" 2>/dev/null || true
  cp -a "$CASE_DIR" "$ARTIFACT_DIR/scenario" 2>/dev/null || true
}

cleanup() {
  local status=$?
  trap - EXIT

  if (( status != 0 && VM_DEFINED == 1 )); then
    log "case failed; collecting diagnostics in $ARTIFACT_DIR"
    collect_diagnostics
  fi
  if (( VM_STARTED == 1 )); then
    virsh_system destroy "$VM_NAME" >/dev/null 2>&1 || true
  fi
  if (( VM_DEFINED == 1 )); then
    virsh_system undefine "$VM_NAME" --nvram >/dev/null 2>&1 ||
      virsh_system undefine "$VM_NAME" >/dev/null 2>&1 || true
  fi
  if (( NETWORK_DEFINED == 1 )); then
    virsh_system net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
    virsh_system net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
  fi
  cleanup_vm_files

  exit "$status"
}
trap cleanup EXIT

wait_for_vm_ip() {
  local deadline=$((SECONDS + 240))
  while (( SECONDS < deadline )); do
    VM_IP="$(
      virsh_system domifaddr "$VM_NAME" --source lease 2>/dev/null |
        awk '$3 == "ipv4" { split($4, address, "/"); print address[1]; exit }'
    )"
    if [[ -n "$VM_IP" ]]; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_for_ssh() {
  local deadline=$((SECONDS + 300))
  while (( SECONDS < deadline )); do
    if ssh_vm true >/dev/null 2>&1; then
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

USER_DATA="$CASE_WORK/user-data"
META_DATA="$CASE_WORK/meta-data"
NETWORK_CONFIG="$CASE_WORK/network-config"
if is_remote_libvirt; then
  USER_DATA="$REMOTE_TMP_DIR/user-data"
  META_DATA="$REMOTE_TMP_DIR/meta-data"
  NETWORK_CONFIG="$REMOTE_TMP_DIR/network-config"
fi

remote_write_file "$USER_DATA" <<EOF
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

remote_write_file "$META_DATA" <<EOF
instance-id: $VM_NAME
local-hostname: debianform-ci
EOF

remote_write_file "$NETWORK_CONFIG" <<EOF
version: 2
ethernets:
  primary:
    match:
      macaddress: "$MAC_ADDRESS"
    dhcp4: true
EOF

remote_exec cloud-localds \
  --network-config="$NETWORK_CONFIG" \
  "$SEED_IMAGE" \
  "$USER_DATA" \
  "$META_DATA"
remote_exec qemu-img create -q -f qcow2 -F qcow2 -b "${REMOTE_BASE_IMAGE:-$BASE_IMAGE}" "$VM_DISK" 12G
if is_remote_libvirt; then
  remote_exec chmod 0644 "$SEED_IMAGE"
  remote_exec chmod 0666 "$VM_DISK"
else
  chmod 0755 "$CASE_WORK"
  chmod 0644 "$SEED_IMAGE"
  chmod 0666 "$VM_DISK"
fi

CPU_XML=""
if [[ "$VIRT_TYPE" == "kvm" ]]; then
  CPU_XML="<cpu mode='host-passthrough' check='none'/>"
fi
EMULATOR_PATH="$(emulator_path)"

cat >"$DOMAIN_XML" <<EOF
<domain type='$VIRT_TYPE'>
  <name>$VM_NAME</name>
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
      <source file='$VM_DISK'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='$SEED_IMAGE'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
    <interface type='network'>
      <mac address='$MAC_ADDRESS'/>
      <source network='$NETWORK_NAME'/>
      <model type='virtio'/>
    </interface>
    <serial type='pty'>
      <log file='$CONSOLE_LOG' append='off'/>
      <target port='0'/>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
  </devices>
</domain>
EOF

log "starting fresh Debian VM using $VIRT_TYPE"
dbf_integration_start_network "$CASE_WORK/network.xml"
virsh_system define "$DOMAIN_XML" >/dev/null
VM_DEFINED=1
virsh_system start "$VM_NAME" >/dev/null
VM_STARTED=1

wait_for_vm_ip
log "VM acquired address $VM_IP"

cat >"$DBF_HOME/.ssh/config" <<EOF
Host cihost
  HostName $VM_IP
  User root
  IdentityFile $SSH_KEY
$(if is_remote_libvirt; then printf '  ProxyCommand ssh -W %%h:%%p %s\n' "$REMOTE_HYPERVISOR"; fi)
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
EOF
chmod 0600 "$DBF_HOME/.ssh/config"

wait_for_ssh
ssh_vm "cloud-init status --wait >/dev/null && test -e /run/debianform-cloud-init-ready"
ssh_vm ". /etc/os-release && test \"\$ID\" = debian && test \"\$VERSION_ID\" = 13"

DBF_CONFIG_HOST="$VM_IP"
if is_remote_libvirt; then
  DBF_CONFIG_HOST="cihost"
fi
while IFS= read -r config; do
  sed -i "s/__DBF_VM_IP__/$DBF_CONFIG_HOST/g" "$config"
done < <(find "$CASE_DIR" -maxdepth 1 -type f -name '[0-9]*.dbf.hcl')

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
  dbf apply -f "$config" --auto-approve | tee "$LOG_DIR/$CURRENT_STEP.apply.log"

  log "step $CURRENT_STEP: checking convergence"
  dbf check -f "$config" | tee "$LOG_DIR/$CURRENT_STEP.check.log"

  run_hook "$check_hook"
done

if (( ASSERTION_COUNT == 0 )); then
  fail "case must run at least one explicit assertion"
fi
log "case passed with $ASSERTION_COUNT explicit assertions"
