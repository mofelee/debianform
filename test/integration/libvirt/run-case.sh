#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CASE_SOURCE="${1:?usage: run-case.sh CASE_DIR}"
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
VM_NAME="dbf-test-${TARGET_SLUG}-${SAFE_CASE_NAME}-${RUN_SUFFIX}"
NETWORK_NAME="${VM_NAME}-net"
BRIDGE_NAME=""
SUBNET_OCTET=""
printf -v MAC_ADDRESS '52:54:00:%02x:%02x:%02x' \
  "$((RANDOM % 256))" "$((RANDOM % 256))" "$((RANDOM % 256))"

VM_IP=""
VIRT_TYPE="qemu"
NETWORK_DEFINED=0
VM_DEFINED=0
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
  VM_DISK="$pool_dir/$VM_NAME.qcow2"
  SEED_IMAGE="$pool_dir/$VM_NAME-seed.img"
  CONSOLE_LOG="$pool_dir/$VM_NAME-console.log"
  DOMAIN_XML="$CASE_WORK/domain.xml"
  REMOTE_TMP_DIR="$pool_dir/.$VM_NAME"
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
    remote_exec rm -rf "$VM_DISK" "$SEED_IMAGE" "$CONSOLE_LOG" "$REMOTE_TMP_DIR" >/dev/null 2>&1 || true
  fi
}

ssh_vm() {
  ssh \
    -F "$DBF_HOME/.ssh/config" \
    -o BatchMode=yes \
    -o ConnectTimeout=5 \
    -o ServerAliveInterval=5 \
    -o ServerAliveCountMax=1 \
    cihost "$@"
}

prepare_native_networkd() {
  if [[ "$EXPECTED_DISTRIBUTION" != "ubuntu" || ! -f "$CASE_SOURCE/native-networkd.case" ]]; then
    return
  fi
  local boot_id deadline current
  boot_id="$(ssh_vm cat /proc/sys/kernel/random/boot_id)"
  log "preparing explicit native-networkd Ubuntu fixture"
  ssh_vm 'set -eu
iface="$(ip -o route show default | awk "NR == 1 { print \$5 }")"
test -n "$iface"
install -d -m 0755 /etc/systemd/network /etc/cloud/cloud.cfg.d
printf "[Match]\nName=%s\n\n[Network]\nDHCP=yes\nIPv6AcceptRA=yes\n" "$iface" > /etc/systemd/network/10-dbf-uplink.network
printf "network: {config: disabled}\n" > /etc/cloud/cloud.cfg.d/99-disable-network-config.cfg
rm -f /etc/netplan/*.yaml
systemctl enable systemd-networkd.service
sync
systemctl reboot' >/dev/null 2>&1 || true

  deadline=$((SECONDS + 330))
  while (( SECONDS < deadline )); do
    current="$(ssh_vm cat /proc/sys/kernel/random/boot_id 2>/dev/null || true)"
    if [[ -n "$current" && "$current" != "$boot_id" ]]; then
      if ssh_vm 'systemctl is-active --quiet systemd-networkd.service && test -z "$(find /etc/netplan -maxdepth 1 -type f -name "*.yaml" -print -quit 2>/dev/null)" && test -z "$(find /run/systemd/network -maxdepth 1 -type f -name "*netplan*" -print -quit 2>/dev/null)" && ip route show default | grep -q .'; then
        return
      fi
    fi
    sleep 3
  done
  fail "native-networkd fixture did not become ready after reboot"
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

assert_remote_eventually() {
  local description=$1
  local command=$2
  local timeout="${3:-60}"
  local interval="${4:-2}"
  local deadline=$((SECONDS + timeout))

  ASSERTION_COUNT=$((ASSERTION_COUNT + 1))
  log "ASSERT $ASSERTION_COUNT: $description"
  while (( SECONDS < deadline )); do
    if ssh_vm "$command" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$interval"
  done

  log "ASSERT $ASSERTION_COUNT: final failure output for: $description"
  ssh_vm "$command" || true
  fail "$description did not pass within ${timeout}s"
}

collect_guest_diagnostics() {
  if [[ -z "$VM_IP" || ! -f "$DBF_HOME/.ssh/config" ]]; then
    return
  fi

  local guest_dir="$ARTIFACT_DIR/guest"
  mkdir -p "$guest_dir"
  ssh_vm "set +e; hostnamectl; printf '\n'; uname -a; printf '\n'; uptime; printf '\n'; cat /etc/os-release" >"$guest_dir/system.txt" 2>&1 || true
  ssh_vm "systemctl --failed --no-pager --full" >"$guest_dir/systemctl-failed.txt" 2>&1 || true
  ssh_vm "systemctl list-units --all --no-pager --full 'debianform*' 'dbf*' '*docker*' '*networkd*' 'NetworkManager*'" >"$guest_dir/systemd-units.txt" 2>&1 || true
  ssh_vm 'set +e
printf "===== Netplan ownership paths =====\n"
for path in /lib/netplan/*.yaml /etc/netplan/*.yaml /run/netplan/*.yaml /run/systemd/network/*netplan* /run/NetworkManager/system-connections/netplan-*; do
  test -f "$path" || continue
  printf "%s\n" "$path"
done
printf "\n===== Network management commands =====\n"
command -v netplan || true
command -v networkctl || true
command -v nmcli || true
printf "\n===== Network services =====\n"
systemctl is-enabled systemd-networkd.service NetworkManager.service
systemctl is-active systemd-networkd.service NetworkManager.service
printf "\n===== Links and routes =====\n"
ip -brief link
ip -brief address
ip route show table all
printf "\n===== networkctl =====\n"
networkctl --no-pager --full status
' >"$guest_dir/network-ownership.txt" 2>&1 || true
  ssh_vm "dpkg-query -W | LC_ALL=C sort" >"$guest_dir/packages.txt" 2>&1 || true
  ssh_vm "journalctl --no-pager -n 500" >"$guest_dir/journal.log" 2>&1 || true
  ssh_vm "journalctl -u docker.service --no-pager -n 300" >"$guest_dir/docker.service.log" 2>&1 || true
  ssh_vm 'set +e
for state_path in /var/lib/debianform-integration/*.json; do
  test -f "$state_path" || continue
  printf "===== %s =====\n" "$state_path"
  cat "$state_path"
  printf "\n"
done' >"$guest_dir/state.json.log" 2>&1 || true
  ssh_vm "set +e
for unit_path in /etc/systemd/system/debianform*.service /etc/systemd/system/dbf*.service; do
  test -e \"\$unit_path\" || continue
  unit=\"\$(basename \"\$unit_path\")\"
  printf '===== %s status =====\n' \"\$unit\"
  systemctl status --no-pager --full \"\$unit\"
  printf '\n===== %s journal =====\n' \"\$unit\"
  journalctl -u \"\$unit\" --no-pager -n 300
  printf '\n'
done" >"$guest_dir/managed-services.log" 2>&1 || true
  ssh_vm "set +e
if command -v docker >/dev/null 2>&1; then
  docker version
  printf '\n===== docker info =====\n'
  docker info
  printf '\n===== docker compose version =====\n'
  docker compose version
  printf '\n===== docker compose projects =====\n'
  docker compose ls --all
  printf '\n===== docker containers =====\n'
  docker ps -a --no-trunc
  printf '\n===== docker images =====\n'
  docker images
  printf '\n===== recent docker events =====\n'
  timeout 10s docker events --since 20m
fi" >"$guest_dir/docker.log" 2>&1 || true
}

collect_diagnostics() {
  mkdir -p "$ARTIFACT_DIR"
  virsh_system list --all >"$ARTIFACT_DIR/virsh-list.txt" 2>&1 || true
  virsh_system net-list --all >"$ARTIFACT_DIR/virsh-net-list.txt" 2>&1 || true
  virsh_system dumpxml "$VM_NAME" >"$ARTIFACT_DIR/domain.xml" 2>&1 || true
  virsh_system domifaddr "$VM_NAME" --source lease >"$ARTIFACT_DIR/domifaddr.txt" 2>&1 || true
  collect_guest_diagnostics
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
  rm -f "$ARTIFACT_DIR/scenario/id_ed25519"
  rm -rf "$ARTIFACT_DIR/scenario/secrets"
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM

  if (( status != 0 && VM_DEFINED == 1 && INTERRUPTED == 0 )); then
    log "case failed; collecting diagnostics in $ARTIFACT_DIR"
    collect_diagnostics
  fi
  virsh_system destroy "$VM_NAME" >/dev/null 2>&1 || true
  virsh_system undefine "$VM_NAME" --nvram >/dev/null 2>&1 ||
    virsh_system undefine "$VM_NAME" >/dev/null 2>&1 || true
  virsh_system net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
  virsh_system net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
  cleanup_vm_files

  exit "$status"
}
trap cleanup EXIT
trap 'INTERRUPTED=1; exit 130' INT TERM

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

load_step_source_args() {
  local step=$1
  local default_config=$2
  local out_name=$3
  local -n out=$out_name
  local source_file="$CASE_DIR/$step.sources"
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
    out+=("-f" "$CASE_DIR/$source")
    count=$((count + 1))
  done <"$source_file"
  if (( count == 0 )); then
    fail "$step.sources must contain at least one source"
  fi
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
    <disk type='file' device='disk'>
      <driver name='qemu' type='raw'/>
      <source file='$SEED_IMAGE'/>
      <target dev='vdb' bus='virtio'/>
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

log "starting fresh $DBF_INTEGRATION_TARGET VM using $VIRT_TYPE"
dbf_integration_start_network "$CASE_WORK/network.xml"
virsh_system define "$DOMAIN_XML" >/dev/null
VM_DEFINED=1
virsh_system start "$VM_NAME" >/dev/null

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
ssh_vm ". /etc/os-release && test \"\$ID\" = '$EXPECTED_DISTRIBUTION' && test \"\$VERSION_ID\" = '$EXPECTED_VERSION' && test \"\${VERSION_CODENAME:-\${UBUNTU_CODENAME:-}}\" = '$EXPECTED_CODENAME' && test \"\$(dpkg --print-architecture)\" = '$EXPECTED_ARCHITECTURE'"
prepare_native_networkd

DBF_CONFIG_HOST="$VM_IP"
if is_remote_libvirt; then
  DBF_CONFIG_HOST="cihost"
fi
while IFS= read -r config; do
  sed -i \
    -e "s|__DBF_VM_IP__|$DBF_CONFIG_HOST|g" \
    -e "s|__DBF_TARGET_CODENAME__|$EXPECTED_CODENAME|g" \
    -e "s|__DBF_TARGET_APT_MIRROR__|$DBF_INTEGRATION_TARGET_APT_MIRROR|g" \
    -e "s|__DBF_TARGET_APT_SECURITY_MIRROR__|$DBF_INTEGRATION_TARGET_APT_SECURITY_MIRROR|g" \
    -e "s|__DBF_TARGET_APT_COMPONENTS__|$DBF_INTEGRATION_TARGET_APT_COMPONENTS|g" \
    "$config"
done < <(find "$CASE_DIR" -maxdepth 3 -type f \( -name '*.dbf.hcl' -o -name '*.dbfvars' -o -name '*.dbfvars.json' \))

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
  declare -a source_args=()
  load_step_source_args "$CURRENT_STEP" "$config" source_args

  if [[ ! -f "$check_hook" ]]; then
    fail "missing post-apply checks: $check_hook"
  fi

  log "step $CURRENT_STEP: validating $filename"
  dbf validate "${source_args[@]}" | tee "$LOG_DIR/$CURRENT_STEP.validate.log"

  log "step $CURRENT_STEP: planning before apply"
  dbf plan "${source_args[@]}" --format json | tee "$LOG_DIR/$CURRENT_STEP.pre-apply-plan.json"

  if [[ -f "$drift_hook" ]]; then
    run_hook "$drift_hook"
    log "step $CURRENT_STEP: verifying dbf check rejects drift"
    if dbf check "${source_args[@]}" >"$LOG_DIR/$CURRENT_STEP.drift-check.log" 2>&1; then
      cat "$LOG_DIR/$CURRENT_STEP.drift-check.log"
      fail "dbf check unexpectedly accepted drift for step $CURRENT_STEP"
    fi
    cat "$LOG_DIR/$CURRENT_STEP.drift-check.log"
  fi

  log "step $CURRENT_STEP: applying"
  dbf apply "${source_args[@]}" --auto-approve | tee "$LOG_DIR/$CURRENT_STEP.apply.log"

  log "step $CURRENT_STEP: verifying no-op plan after apply"
  dbf plan "${source_args[@]}" --format json | tee "$LOG_DIR/$CURRENT_STEP.noop-plan.json"
  python3 "$SCRIPT_DIR/assert-noop-plan.py" "$LOG_DIR/$CURRENT_STEP.noop-plan.json"

  log "step $CURRENT_STEP: checking convergence"
  dbf check "${source_args[@]}" | tee "$LOG_DIR/$CURRENT_STEP.check.log"

  run_hook "$check_hook"
done

if (( ASSERTION_COUNT == 0 )); then
  fail "case must run at least one explicit assertion"
fi
log "case passed with $ASSERTION_COUNT explicit assertions"
