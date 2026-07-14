#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_SOURCE="${1:?usage: run-two-host-case.sh CASE_DIR}"
CASE_NAME="$(basename "$CASE_SOURCE")"
DBF_BIN="${DBF_INTEGRATION_DBF_BIN:?missing DBF_INTEGRATION_DBF_BIN}"
BASE_IMAGE="${DBF_INTEGRATION_BASE_IMAGE:?missing DBF_INTEGRATION_BASE_IMAGE}"
BASE_IMAGE_DIGEST="${DBF_INTEGRATION_BASE_IMAGE_DIGEST:?missing DBF_INTEGRATION_BASE_IMAGE_DIGEST}"
BASE_IMAGE_DIGEST_ALGORITHM="${DBF_INTEGRATION_BASE_IMAGE_DIGEST_ALGORITHM:?missing DBF_INTEGRATION_BASE_IMAGE_DIGEST_ALGORITHM}"
CASE_WORK="${DBF_INTEGRATION_CASE_WORK:?missing DBF_INTEGRATION_CASE_WORK}"
ARTIFACT_DIR="${DBF_INTEGRATION_CASE_ARTIFACTS:?missing DBF_INTEGRATION_CASE_ARTIFACTS}"
EXPECTED_DISTRIBUTION="${DBF_INTEGRATION_TARGET_DISTRIBUTION:?missing DBF_INTEGRATION_TARGET_DISTRIBUTION}"
EXPECTED_VERSION="${DBF_INTEGRATION_TARGET_VERSION:?missing DBF_INTEGRATION_TARGET_VERSION}"
EXPECTED_CODENAME="${DBF_INTEGRATION_TARGET_CODENAME:?missing DBF_INTEGRATION_TARGET_CODENAME}"
EXPECTED_ARCHITECTURE="${DBF_INTEGRATION_TARGET_ARCHITECTURE:?missing DBF_INTEGRATION_TARGET_ARCHITECTURE}"
TARGET_SLUG="${DBF_INTEGRATION_TARGET_SLUG:?missing DBF_INTEGRATION_TARGET_SLUG}"
CASE_DIR="$CASE_WORK/scenario"
LOG_DIR="$CASE_WORK/logs"
DBF_HOME="$CASE_WORK/home"
SSH_KEY="$CASE_WORK/id_ed25519"
LIBVIRT_URI="${DBF_LIBVIRT_URI:-${VIRSH_DEFAULT_CONNECT_URI:-${LIBVIRT_DEFAULT_URI:-}}}"
REMOTE_HYPERVISOR=""
REMOTE_TMP_DIR=""
REMOTE_POOL="${DBF_INTEGRATION_POOL:-vm}"
REMOTE_BASE_IMAGE="${DBF_INTEGRATION_REMOTE_BASE_IMAGE:-}"
VM_A_DISK="$CASE_WORK/vm-a.qcow2"
VM_A_SEED="$CASE_WORK/seed-a.img"
VM_A_DOMAIN="$CASE_WORK/domain-a.xml"
VM_A_CONSOLE="$CASE_WORK/console-a.log"
VM_B_DISK="$CASE_WORK/vm-b.qcow2"
VM_B_SEED="$CASE_WORK/seed-b.img"
VM_B_DOMAIN="$CASE_WORK/domain-b.xml"
VM_B_CONSOLE="$CASE_WORK/console-b.log"

RUN_SUFFIX="${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}-${RANDOM}"
SAFE_CASE_NAME="$(tr -c 'a-zA-Z0-9' '-' <<<"$CASE_NAME" | sed 's/-*$//')"
VM_A_NAME="dbf-test-${TARGET_SLUG}-${SAFE_CASE_NAME}-a-${RUN_SUFFIX}"
VM_B_NAME="dbf-test-${TARGET_SLUG}-${SAFE_CASE_NAME}-b-${RUN_SUFFIX}"
NETWORK_NAME="dbf-test-${TARGET_SLUG}-${SAFE_CASE_NAME}-${RUN_SUFFIX}-net"
BRIDGE_NAME=""
SUBNET_OCTET=""
printf -v VM_A_MAC '52:54:00:%02x:%02x:%02x' \
  "$((RANDOM % 256))" "$((RANDOM % 256))" "$((RANDOM % 256))"
printf -v VM_B_MAC '52:54:00:%02x:%02x:%02x' \
  "$((RANDOM % 256))" "$((RANDOM % 256))" "$((RANDOM % 256))"

VM_A_IP=""
VM_B_IP=""
VIRT_TYPE="qemu"
NETWORK_DEFINED=0
VM_A_DEFINED=0
VM_B_DEFINED=0
INTERRUPTED=0
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

sync_remote_base_image() {
  local remote_sha=""
  remote_sha="$(remote_exec "${BASE_IMAGE_DIGEST_ALGORITHM}sum" "$REMOTE_BASE_IMAGE" 2>/dev/null | awk '{print $1}')" || true
  if [[ "$remote_sha" == "$BASE_IMAGE_DIGEST" ]]; then
    return
  fi

  log "copying verified $DBF_INTEGRATION_TARGET base image to $REMOTE_HYPERVISOR:$REMOTE_BASE_IMAGE"
  local partial="$REMOTE_TMP_DIR/base-image.partial"
  scp -q "$BASE_IMAGE" "$REMOTE_HYPERVISOR:$partial"
  remote_exec chmod 0644 "$partial"
  remote_exec mv -f "$partial" "$REMOTE_BASE_IMAGE"
}

prepare_vm_paths() {
  if ! is_remote_libvirt; then
    return
  fi

  local pool_dir
  pool_dir="$(pool_path)"
  VM_A_DISK="$pool_dir/$VM_A_NAME.qcow2"
  VM_A_SEED="$pool_dir/$VM_A_NAME-seed.img"
  VM_A_CONSOLE="$pool_dir/$VM_A_NAME-console.log"
  VM_B_DISK="$pool_dir/$VM_B_NAME.qcow2"
  VM_B_SEED="$pool_dir/$VM_B_NAME-seed.img"
  VM_B_CONSOLE="$pool_dir/$VM_B_NAME-console.log"
  REMOTE_TMP_DIR="$pool_dir/.dbf-test-${TARGET_SLUG}-${SAFE_CASE_NAME}-${RUN_SUFFIX}"
  if [[ -z "$REMOTE_BASE_IMAGE" ]]; then
    REMOTE_BASE_IMAGE="$pool_dir/$(basename "$BASE_IMAGE")"
  fi

  log "remote libvirt URI: $LIBVIRT_URI"
  log "remote hypervisor: $REMOTE_HYPERVISOR"
  log "remote storage pool: $REMOTE_POOL ($pool_dir)"
  remote_exec mkdir -p "$REMOTE_TMP_DIR"
  sync_remote_base_image
}

cleanup_vm_files() {
  if is_remote_libvirt && [[ -n "$REMOTE_TMP_DIR" ]]; then
    remote_exec rm -rf \
      "$VM_A_DISK" "$VM_A_SEED" "$VM_A_CONSOLE" \
      "$VM_B_DISK" "$VM_B_SEED" "$VM_B_CONSOLE" \
      "$REMOTE_TMP_DIR" >/dev/null 2>&1 || true
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

prepare_native_networkd_host() {
  local alias=$1
  if [[ "$EXPECTED_DISTRIBUTION" != "ubuntu" || ! -f "$CASE_SOURCE/native-networkd.case" ]]; then
    return
  fi
  local boot_id deadline current
  boot_id="$(ssh_host "$alias" cat /proc/sys/kernel/random/boot_id)"
  log "preparing explicit native-networkd Ubuntu fixture on $alias"
  ssh_host "$alias" 'set -eu
iface="$(ip -o route show default | awk "NR == 1 { print \$5 }")"
test -n "$iface"
install -d -m 0755 /etc/systemd/network /etc/cloud/cloud.cfg.d
printf "[Match]\nName=%s\n\n[Network]\nDHCP=yes\nIPv6AcceptRA=yes\n" "$iface" > /etc/systemd/network/10-dbf-uplink.network
printf "network: {config: disabled}\n" > /etc/cloud/cloud.cfg.d/99-disable-network-config.cfg
rm -f /etc/netplan/*.yaml
systemctl enable systemd-networkd.service
sync
systemctl reboot' >/dev/null 2>&1 || true
  deadline=$((SECONDS + 300))
  while (( SECONDS < deadline )); do
    current="$(ssh_host "$alias" cat /proc/sys/kernel/random/boot_id 2>/dev/null || true)"
    if [[ -n "$current" && "$current" != "$boot_id" ]]; then
      ssh_host "$alias" 'systemctl is-active --quiet systemd-networkd.service && test -z "$(find /etc/netplan -maxdepth 1 -type f -name "*.yaml" -print -quit 2>/dev/null)" && test -z "$(find /run/systemd/network -maxdepth 1 -type f -name "*netplan*" -print -quit 2>/dev/null)" && ip route show default | grep -q .'
      return
    fi
    sleep 3
  done
  fail "native-networkd fixture $alias did not return after reboot"
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
  virsh_system dumpxml "$VM_A_NAME" >"$ARTIFACT_DIR/domain-a.xml" 2>&1 || true
  virsh_system dumpxml "$VM_B_NAME" >"$ARTIFACT_DIR/domain-b.xml" 2>&1 || true
  virsh_system domifaddr "$VM_A_NAME" --source lease >"$ARTIFACT_DIR/domifaddr-a.txt" 2>&1 || true
  virsh_system domifaddr "$VM_B_NAME" --source lease >"$ARTIFACT_DIR/domifaddr-b.txt" 2>&1 || true
  if is_remote_libvirt; then
    ssh "$REMOTE_HYPERVISOR" "journalctl -u libvirtd --no-pager -n 300" >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
    ssh "$REMOTE_HYPERVISOR" "cat /var/log/libvirt/qemu/$VM_A_NAME.log" >"$ARTIFACT_DIR/qemu-a.log" 2>&1 || true
    ssh "$REMOTE_HYPERVISOR" "cat /var/log/libvirt/qemu/$VM_B_NAME.log" >"$ARTIFACT_DIR/qemu-b.log" 2>&1 || true
    remote_read_file "$VM_A_CONSOLE" >"$ARTIFACT_DIR/console-a.log" 2>&1 || true
    remote_read_file "$VM_B_CONSOLE" >"$ARTIFACT_DIR/console-b.log" 2>&1 || true
  else
    sudo journalctl -u libvirtd --no-pager -n 300 >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
    sudo sh -c "cat /var/log/libvirt/qemu/$VM_A_NAME.log" >"$ARTIFACT_DIR/qemu-a.log" 2>&1 || true
    sudo sh -c "cat /var/log/libvirt/qemu/$VM_B_NAME.log" >"$ARTIFACT_DIR/qemu-b.log" 2>&1 || true
    sudo sh -c "cat '$VM_A_CONSOLE'" >"$ARTIFACT_DIR/console-a.log" 2>&1 || true
    sudo sh -c "cat '$VM_B_CONSOLE'" >"$ARTIFACT_DIR/console-b.log" 2>&1 || true
  fi
  cp -a "$LOG_DIR" "$ARTIFACT_DIR/logs" 2>/dev/null || true
  cp -a "$CASE_DIR" "$ARTIFACT_DIR/scenario" 2>/dev/null || true
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM

  if (( status != 0 && (VM_A_DEFINED == 1 || VM_B_DEFINED == 1) && INTERRUPTED == 0 )); then
    log "case failed; collecting diagnostics in $ARTIFACT_DIR"
    collect_diagnostics
  fi
  virsh_system destroy "$VM_A_NAME" >/dev/null 2>&1 || true
  virsh_system destroy "$VM_B_NAME" >/dev/null 2>&1 || true
  virsh_system undefine "$VM_A_NAME" --nvram >/dev/null 2>&1 ||
    virsh_system undefine "$VM_A_NAME" >/dev/null 2>&1 || true
  virsh_system undefine "$VM_B_NAME" --nvram >/dev/null 2>&1 ||
    virsh_system undefine "$VM_B_NAME" >/dev/null 2>&1 || true
  virsh_system net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
  virsh_system net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
  cleanup_vm_files

  exit "$status"
}
trap cleanup EXIT
trap 'INTERRUPTED=1; exit 130' INT TERM

wait_for_vm_ip() {
  local vm_name=$1
  local deadline=$((SECONDS + 240))
  local ip
  while (( SECONDS < deadline )); do
    ip="$(
      virsh_system domifaddr "$vm_name" --source lease 2>/dev/null |
        awk '$3 == "ipv4" { split($4, address, "/"); print address[1]; exit }'
    )"
    if [[ -n "$ip" ]]; then
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
  local seed_image=$2
  local mac=$3
  local host_name=$4
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
instance-id: $host_name
local-hostname: $host_name
EOF

  remote_write_file "$network_config" <<EOF
version: 2
ethernets:
  primary:
    match:
      macaddress: "$mac"
    dhcp4: true
EOF

  remote_exec cloud-localds \
    --network-config="$network_config" \
    "$seed_image" \
    "$user_data" \
    "$meta_data"
}

write_domain() {
  local vm_name=$1
  local disk=$2
  local seed=$3
  local mac=$4
  local console_log=$5
  local domain_xml=$6

  cat >"$domain_xml" <<EOF
<domain type='$VIRT_TYPE'>
  <name>$vm_name</name>
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
      <source file='$disk'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='disk'>
      <driver name='qemu' type='raw'/>
      <source file='$seed'/>
      <target dev='vdb' bus='virtio'/>
      <readonly/>
    </disk>
    <interface type='network'>
      <mac address='$mac'/>
      <source network='$NETWORK_NAME'/>
      <model type='virtio'/>
    </interface>
    <serial type='pty'>
      <log file='$console_log' append='off'/>
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

write_seed a "$VM_A_SEED" "$VM_A_MAC" "$VM_A_NAME"
write_seed b "$VM_B_SEED" "$VM_B_MAC" "$VM_B_NAME"
remote_exec qemu-img create -q -f qcow2 -F qcow2 -b "${REMOTE_BASE_IMAGE:-$BASE_IMAGE}" "$VM_A_DISK" 12G
remote_exec qemu-img create -q -f qcow2 -F qcow2 -b "${REMOTE_BASE_IMAGE:-$BASE_IMAGE}" "$VM_B_DISK" 12G
if is_remote_libvirt; then
  remote_exec chmod 0644 "$VM_A_SEED" "$VM_B_SEED"
  remote_exec chmod 0666 "$VM_A_DISK" "$VM_B_DISK"
else
  chmod 0755 "$CASE_WORK"
  chmod 0644 "$VM_A_SEED" "$VM_B_SEED"
  chmod 0666 "$VM_A_DISK" "$VM_B_DISK"
fi

CPU_XML=""
if [[ "$VIRT_TYPE" == "kvm" ]]; then
  CPU_XML="<cpu mode='host-passthrough' check='none'/>"
fi
EMULATOR_PATH="$(emulator_path)"
write_domain "$VM_A_NAME" "$VM_A_DISK" "$VM_A_SEED" "$VM_A_MAC" "$VM_A_CONSOLE" "$VM_A_DOMAIN"
write_domain "$VM_B_NAME" "$VM_B_DISK" "$VM_B_SEED" "$VM_B_MAC" "$VM_B_CONSOLE" "$VM_B_DOMAIN"

log "starting fresh $DBF_INTEGRATION_TARGET VMs using $VIRT_TYPE"
dbf_integration_start_network "$CASE_WORK/network.xml"
virsh_system define "$VM_A_DOMAIN" >/dev/null
VM_A_DEFINED=1
virsh_system define "$VM_B_DOMAIN" >/dev/null
VM_B_DEFINED=1
virsh_system start "$VM_A_NAME" >/dev/null
virsh_system start "$VM_B_NAME" >/dev/null

VM_A_IP="$(wait_for_vm_ip "$VM_A_NAME")"
VM_B_IP="$(wait_for_vm_ip "$VM_B_NAME")"
log "VM A acquired address $VM_A_IP"
log "VM B acquired address $VM_B_IP"

cat >"$DBF_HOME/.ssh/config" <<EOF
Host wg-a
  HostName $VM_A_IP
  User root
  IdentityFile $SSH_KEY
$(if is_remote_libvirt; then printf '  ProxyCommand ssh -W %%h:%%p %s\n' "$REMOTE_HYPERVISOR"; fi)
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null

Host wg-b
  HostName $VM_B_IP
  User root
  IdentityFile $SSH_KEY
$(if is_remote_libvirt; then printf '  ProxyCommand ssh -W %%h:%%p %s\n' "$REMOTE_HYPERVISOR"; fi)
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null

Host cihost
  HostName $VM_A_IP
  User root
  IdentityFile $SSH_KEY
$(if is_remote_libvirt; then printf '  ProxyCommand ssh -W %%h:%%p %s\n' "$REMOTE_HYPERVISOR"; fi)
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
EOF
chmod 0600 "$DBF_HOME/.ssh/config"

wait_for_ssh wg-a
wait_for_ssh wg-b
ssh_host wg-a "cloud-init status --wait >/dev/null && test -e /run/debianform-cloud-init-ready"
ssh_host wg-b "cloud-init status --wait >/dev/null && test -e /run/debianform-cloud-init-ready"
ssh_host wg-a ". /etc/os-release && test \"\$ID\" = '$EXPECTED_DISTRIBUTION' && test \"\$VERSION_ID\" = '$EXPECTED_VERSION' && test \"\${VERSION_CODENAME:-\${UBUNTU_CODENAME:-}}\" = '$EXPECTED_CODENAME' && test \"\$(dpkg --print-architecture)\" = '$EXPECTED_ARCHITECTURE'"
ssh_host wg-b ". /etc/os-release && test \"\$ID\" = '$EXPECTED_DISTRIBUTION' && test \"\$VERSION_ID\" = '$EXPECTED_VERSION' && test \"\${VERSION_CODENAME:-\${UBUNTU_CODENAME:-}}\" = '$EXPECTED_CODENAME' && test \"\$(dpkg --print-architecture)\" = '$EXPECTED_ARCHITECTURE'"
prepare_native_networkd_host wg-a
prepare_native_networkd_host wg-b

DBF_WG_A_HOST="$VM_A_IP"
DBF_WG_B_HOST="$VM_B_IP"
if is_remote_libvirt; then
  DBF_WG_A_HOST="wg-a"
  DBF_WG_B_HOST="wg-b"
fi
while IFS= read -r config; do
  sed -i \
    -e "s/__DBF_WG_A_SSH_HOST__/$DBF_WG_A_HOST/g" \
    -e "s/__DBF_WG_B_SSH_HOST__/$DBF_WG_B_HOST/g" \
    -e "s/__DBF_WG_A_VM_IP__/$VM_A_IP/g" \
    -e "s/__DBF_WG_B_VM_IP__/$VM_B_IP/g" \
    -e "s/__DBF_VM_IP__/$DBF_WG_A_HOST/g" \
    -e "s|__DBF_TARGET_CODENAME__|$EXPECTED_CODENAME|g" \
    -e "s|__DBF_TARGET_APT_MIRROR__|$DBF_INTEGRATION_TARGET_APT_MIRROR|g" \
    -e "s|__DBF_TARGET_APT_SECURITY_MIRROR__|$DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR|g" \
    -e "s|__DBF_TARGET_APT_COMPONENTS__|$DBF_INTEGRATION_TARGET_APT_COMPONENTS|g" \
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

  log "step $CURRENT_STEP: planning before apply"
  dbf plan -f "$config" --format json | tee "$LOG_DIR/$CURRENT_STEP.pre-apply-plan.json"

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

  log "step $CURRENT_STEP: verifying no-op plan after apply"
  dbf plan -f "$config" --format json | tee "$LOG_DIR/$CURRENT_STEP.noop-plan.json"
  python3 "$SCRIPT_DIR/assert-noop-plan.py" "$LOG_DIR/$CURRENT_STEP.noop-plan.json"

  log "step $CURRENT_STEP: checking convergence"
  dbf check -f "$config" | tee "$LOG_DIR/$CURRENT_STEP.check.log"

  run_hook "$check_hook"
done

if (( ASSERTION_COUNT == 0 )); then
  fail "case must run at least one explicit assertion"
fi
log "case passed with $ASSERTION_COUNT explicit assertions"
