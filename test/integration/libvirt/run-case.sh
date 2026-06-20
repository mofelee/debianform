#!/usr/bin/env bash

set -euo pipefail

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

RUN_SUFFIX="${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}-${RANDOM}"
SAFE_CASE_NAME="$(tr -c 'a-zA-Z0-9' '-' <<<"$CASE_NAME" | sed 's/-*$//')"
VM_NAME="dbf-v2-${SAFE_CASE_NAME}-${RUN_SUFFIX}"
NETWORK_NAME="${VM_NAME}-net"
BRIDGE_NAME="virbr-dbf-${RANDOM}"
SUBNET_OCTET=$((100 + RANDOM % 100))
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
  sudo virsh --connect qemu:///system "$@"
}

ssh_vm() {
  ssh \
    -F "$DBF_HOME/.ssh/config" \
    -o BatchMode=yes \
    -o ConnectTimeout=5 \
    cihost "$@"
}

dbf() {
  HOME="$DBF_HOME" "$DBF_BIN" "$@"
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
  sudo journalctl -u libvirtd --no-pager -n 300 >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
  sudo sh -c "cat /var/log/libvirt/qemu/$VM_NAME.log" >"$ARTIFACT_DIR/qemu.log" 2>&1 || true
  sudo sh -c "cat '$CASE_WORK/console.log'" >"$ARTIFACT_DIR/console.log" 2>&1 || true
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

mkdir -p "$CASE_WORK" "$CASE_DIR" "$LOG_DIR" "$DBF_HOME/.ssh"
cp -a "$CASE_SOURCE/." "$CASE_DIR/"
chmod 0700 "$DBF_HOME" "$DBF_HOME/.ssh"

if [[ "${DBF_INTEGRATION_DISABLE_KVM:-0}" != "1" && -r /dev/kvm && -w /dev/kvm ]]; then
  VIRT_TYPE="kvm"
fi

ssh-keygen -q -t ed25519 -N "" -f "$SSH_KEY"
cp "$SSH_KEY" "$CASE_DIR/id_ed25519"
chmod 0600 "$CASE_DIR/id_ed25519"
PUBLIC_KEY="$(cat "$SSH_KEY.pub")"

cat >"$CASE_WORK/user-data" <<EOF
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

cat >"$CASE_WORK/meta-data" <<EOF
instance-id: $VM_NAME
local-hostname: debianform-ci
EOF

cat >"$CASE_WORK/network-config" <<EOF
version: 2
ethernets:
  primary:
    match:
      macaddress: "$MAC_ADDRESS"
    dhcp4: true
EOF

cloud-localds \
  --network-config="$CASE_WORK/network-config" \
  "$SEED_IMAGE" \
  "$CASE_WORK/user-data" \
  "$CASE_WORK/meta-data"
qemu-img create -q -f qcow2 -F qcow2 -b "$BASE_IMAGE" "$VM_DISK" 12G
chmod 0755 "$CASE_WORK"
chmod 0644 "$SEED_IMAGE"
chmod 0666 "$VM_DISK"

cat >"$CASE_WORK/network.xml" <<EOF
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

CPU_XML=""
if [[ "$VIRT_TYPE" == "kvm" ]]; then
  CPU_XML="<cpu mode='host-passthrough' check='none'/>"
fi

cat >"$CASE_WORK/domain.xml" <<EOF
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
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
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
      <log file='$CASE_WORK/console.log' append='off'/>
      <target port='0'/>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
  </devices>
</domain>
EOF

log "starting fresh Debian VM using $VIRT_TYPE"
virsh_system net-define "$CASE_WORK/network.xml" >/dev/null
NETWORK_DEFINED=1
virsh_system net-start "$NETWORK_NAME" >/dev/null
virsh_system define "$CASE_WORK/domain.xml" >/dev/null
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
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
EOF
chmod 0600 "$DBF_HOME/.ssh/config"

wait_for_ssh
ssh_vm "cloud-init status --wait >/dev/null && test -e /run/debianform-cloud-init-ready"
ssh_vm ". /etc/os-release && test \"\$ID\" = debian && test \"\$VERSION_ID\" = 13"

while IFS= read -r config; do
  sed -i "s/__DBF_VM_IP__/$VM_IP/g" "$config"
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
