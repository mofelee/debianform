#!/usr/bin/env bash

set -euo pipefail

readonly DEBIAN_CLOUD_URL="https://cloud.debian.org/images/cloud/trixie/latest"
readonly DEBIAN_CLOUD_IMAGE="debian-13-genericcloud-amd64.qcow2"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
WORK_DIR="${DBF_INTEGRATION_WORKDIR:-$(mktemp -d "${TMPDIR:-/tmp}/debianform-integration.XXXXXX")}"
VM_NAME="dbf-ci-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}"
ARTIFACT_DIR="${DBF_INTEGRATION_ARTIFACT_DIR:-${TMPDIR:-/tmp}/debianform-integration-artifacts.$VM_NAME}"
NETWORK_NAME="${VM_NAME}-net"
BRIDGE_NAME="virbr-dbf-${RANDOM}"
printf -v MAC_ADDRESS '52:54:00:%02x:%02x:%02x' \
  "$((RANDOM % 256))" "$((RANDOM % 256))" "$((RANDOM % 256))"
SSH_KEY="$WORK_DIR/id_ed25519"
BASE_IMAGE="$WORK_DIR/$DEBIAN_CLOUD_IMAGE"
VM_DISK="$WORK_DIR/debianform-ci.qcow2"
SEED_IMAGE="$WORK_DIR/seed.img"
DBF_BIN="$WORK_DIR/dbf"
DBF_HOME="$WORK_DIR/home"
CONFIG_FILE="$WORK_DIR/integration.dbf.hcl"
VM_IP=""
VIRT_TYPE="qemu"
NETWORK_DEFINED=0
VM_DEFINED=0
VM_STARTED=0

mkdir -p "$WORK_DIR"

log() {
  printf '[integration] %s\n' "$*"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

virsh_system() {
  sudo virsh --connect qemu:///system "$@"
}

collect_diagnostics() {
  mkdir -p "$ARTIFACT_DIR"
  virsh_system list --all >"$ARTIFACT_DIR/virsh-list.txt" 2>&1 || true
  virsh_system net-list --all >"$ARTIFACT_DIR/virsh-net-list.txt" 2>&1 || true
  virsh_system dumpxml "$VM_NAME" >"$ARTIFACT_DIR/domain.xml" 2>&1 || true
  virsh_system domifaddr "$VM_NAME" --source lease >"$ARTIFACT_DIR/domifaddr.txt" 2>&1 || true
  sudo journalctl -u libvirtd --no-pager -n 300 >"$ARTIFACT_DIR/libvirtd.log" 2>&1 || true
  sudo sh -c "cat /var/log/libvirt/qemu/$VM_NAME.log" >"$ARTIFACT_DIR/qemu.log" 2>&1 || true
  sudo sh -c "cat '$WORK_DIR/console.log'" >"$ARTIFACT_DIR/console.log" 2>&1 || true
}

cleanup() {
  local status=$?
  trap - EXIT

  if (( status != 0 && VM_DEFINED == 1 )); then
    log "integration test failed; collecting diagnostics in $ARTIFACT_DIR"
    collect_diagnostics
  fi

  if (( VM_STARTED == 1 )); then
    virsh_system destroy "$VM_NAME" >/dev/null 2>&1 || true
  fi
  if (( VM_DEFINED == 1 )); then
    virsh_system undefine "$VM_NAME" --nvram >/dev/null 2>&1 || \
      virsh_system undefine "$VM_NAME" >/dev/null 2>&1 || true
  fi
  if (( NETWORK_DEFINED == 1 )); then
    virsh_system net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
    virsh_system net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
  fi

  if [[ "${DBF_INTEGRATION_KEEP_WORKDIR:-0}" != "1" ]]; then
    rm -rf "$WORK_DIR"
  else
    log "preserving work directory: $WORK_DIR"
  fi

  exit "$status"
}
trap cleanup EXIT

ssh_vm() {
  ssh \
    -i "$SSH_KEY" \
    -o BatchMode=yes \
    -o ConnectTimeout=5 \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    "root@$VM_IP" "$@"
}

dbf() {
  HOME="$DBF_HOME" "$DBF_BIN" "$@"
}

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

for command in cloud-localds curl go qemu-img sha512sum ssh ssh-keygen sudo virsh; do
  require_command "$command"
done

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  printf 'libvirt integration tests require Linux x86_64\n' >&2
  exit 1
fi

if [[ "${DBF_INTEGRATION_DISABLE_KVM:-0}" != "1" && -r /dev/kvm && -w /dev/kvm ]]; then
  VIRT_TYPE="kvm"
fi

log "building dbf"
(
  cd "$ROOT_DIR"
  go build -trimpath -o "$DBF_BIN" ./cmd/dbf
)

log "fetching Debian 13 genericcloud image checksum"
curl --fail --location --retry 3 --show-error \
  --silent \
  "$DEBIAN_CLOUD_URL/SHA512SUMS" \
  --output "$WORK_DIR/SHA512SUMS"
EXPECTED_SHA512="$(
  awk -v image="$DEBIAN_CLOUD_IMAGE" '$2 == image { print $1; exit }' \
    "$WORK_DIR/SHA512SUMS"
)"
test -n "$EXPECTED_SHA512"

IMAGE_CACHE_DIR="${DBF_INTEGRATION_IMAGE_CACHE:-}"
CACHED_IMAGE="${IMAGE_CACHE_DIR:+$IMAGE_CACHE_DIR/$DEBIAN_CLOUD_IMAGE}"

if [[ -n "$CACHED_IMAGE" && -f "$CACHED_IMAGE" ]] &&
  printf '%s  %s\n' "$EXPECTED_SHA512" "$CACHED_IMAGE" | sha512sum --check --status; then
  log "using cached Debian 13 genericcloud image from $IMAGE_CACHE_DIR"
  cp "$CACHED_IMAGE" "$BASE_IMAGE"
else
  log "downloading latest official Debian 13 genericcloud image"
  curl --fail --location --retry 3 --show-error \
    --silent \
    "$DEBIAN_CLOUD_URL/$DEBIAN_CLOUD_IMAGE" \
    --output "$BASE_IMAGE"
fi

printf '%s  %s\n' "$EXPECTED_SHA512" "$BASE_IMAGE" | sha512sum --check

if [[ -n "$CACHED_IMAGE" && ! -f "$CACHED_IMAGE" ]]; then
  mkdir -p "$IMAGE_CACHE_DIR"
  cp "$BASE_IMAGE" "$CACHED_IMAGE.partial"
  mv "$CACHED_IMAGE.partial" "$CACHED_IMAGE"
fi

log "creating cloud-init seed and qcow2 overlay"
ssh-keygen -q -t ed25519 -N "" -f "$SSH_KEY"
PUBLIC_KEY="$(cat "$SSH_KEY.pub")"

cat >"$WORK_DIR/user-data" <<EOF
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

cat >"$WORK_DIR/meta-data" <<EOF
instance-id: $VM_NAME
local-hostname: debianform-ci
EOF

cat >"$WORK_DIR/network-config" <<'EOF'
version: 2
ethernets:
  primary:
    match:
      macaddress: "__MAC_ADDRESS__"
    dhcp4: true
EOF
sed -i "s/__MAC_ADDRESS__/$MAC_ADDRESS/" "$WORK_DIR/network-config"

cloud-localds \
  --network-config="$WORK_DIR/network-config" \
  "$SEED_IMAGE" \
  "$WORK_DIR/user-data" \
  "$WORK_DIR/meta-data"
qemu-img create -q -f qcow2 -F qcow2 -b "$BASE_IMAGE" "$VM_DISK" 12G
chmod 0755 "$WORK_DIR"
chmod 0644 "$BASE_IMAGE" "$SEED_IMAGE"
chmod 0666 "$VM_DISK"
mkdir -p "$DBF_HOME/.ssh"
chmod 0700 "$DBF_HOME" "$DBF_HOME/.ssh"

cat >"$WORK_DIR/network.xml" <<EOF
<network>
  <name>$NETWORK_NAME</name>
  <forward mode='nat'/>
  <bridge name='$BRIDGE_NAME' stp='on' delay='0'/>
  <ip address='192.168.124.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.124.10' end='192.168.124.250'/>
    </dhcp>
  </ip>
</network>
EOF

CPU_XML=""
if [[ "$VIRT_TYPE" == "kvm" ]]; then
  CPU_XML="<cpu mode='host-passthrough' check='none'/>"
fi

cat >"$WORK_DIR/domain.xml" <<EOF
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
      <log file='$WORK_DIR/console.log' append='off'/>
      <target port='0'/>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
  </devices>
</domain>
EOF

log "starting libvirt network and Debian VM using $VIRT_TYPE"
virsh_system net-define "$WORK_DIR/network.xml" >/dev/null
NETWORK_DEFINED=1
virsh_system net-start "$NETWORK_NAME" >/dev/null
virsh_system define "$WORK_DIR/domain.xml" >/dev/null
VM_DEFINED=1
virsh_system start "$VM_NAME" >/dev/null
VM_STARTED=1

wait_for_vm_ip
log "VM acquired address $VM_IP"
wait_for_ssh
ssh_vm "cloud-init status --wait >/dev/null && test -e /run/debianform-cloud-init-ready"
ssh_vm ". /etc/os-release && test \"\$ID\" = debian && test \"\$VERSION_ID\" = 13"

cat >"$CONFIG_FILE" <<EOF
host "debian_ci" {
  address       = "$VM_IP"
  identity_file = "$SSH_KEY"
}

state "ssh" {
  host      = "debian_ci"
  path      = "/var/lib/debianform-ci/state.json"
  lock_path = "/var/lock/debianform-ci/state.lock"
}

handler "record_change" {
  host    = "debian_ci"
  command = "echo handler >> /var/lib/debianform-ci/handler.log"
}

locals {
  files = {
    primary   = "managed primary\n"
    secondary = "managed secondary\n"
  }
}

debian_group "deploy" {
  host = "debian_ci"
  name = "debianform-deploy"
  gid  = 4242
}

debian_user "app" {
  host  = "debian_ci"
  name  = "debianform-app"
  uid   = 4250
  gid   = "debianform-deploy"
  home  = "/home/debianform-app"
  shell = "/usr/sbin/nologin"

  depends_on = [
    debian_group.deploy,
  ]
}

debian_directory "managed" {
  host  = "debian_ci"
  path  = "/var/lib/debianform-ci/managed"
  owner = "root"
  group = "root"
  mode  = "0750"
}

debian_file "managed" {
  for_each = local.files

  host    = "debian_ci"
  path    = each.key == "primary" ? "/var/lib/debianform-ci/managed/primary.conf" : "/var/lib/debianform-ci/managed/secondary.conf"
  content = each.value
  owner   = "root"
  group   = "root"
  mode    = each.key == "primary" ? "0640" : "0600"

  depends_on = [
    debian_directory.managed,
  ]

  notify = [
    handler.record_change,
  ]
}
EOF

log "validating and planning initial configuration"
dbf validate -f "$CONFIG_FILE"
INITIAL_PLAN="$(dbf plan -f "$CONFIG_FILE")"
printf '%s\n' "$INITIAL_PLAN"
grep -q "debian_directory.managed" <<<"$INITIAL_PLAN"
grep -q 'debian_file.managed\["primary"\]' <<<"$INITIAL_PLAN"
grep -q 'debian_file.managed\["secondary"\]' <<<"$INITIAL_PLAN"
grep -q "debian_group.deploy" <<<"$INITIAL_PLAN"
grep -q "debian_user.app" <<<"$INITIAL_PLAN"
grep -q "handler.record_change" <<<"$INITIAL_PLAN"

log "applying initial configuration"
dbf apply -f "$CONFIG_FILE" --auto-approve
dbf check -f "$CONFIG_FILE"

ssh_vm "test \"\$(cat /var/lib/debianform-ci/managed/primary.conf)\" = 'managed primary'"
ssh_vm "test \"\$(cat /var/lib/debianform-ci/managed/secondary.conf)\" = 'managed secondary'"
ssh_vm "test \"\$(stat -c %a /var/lib/debianform-ci/managed)\" = 750"
ssh_vm "test \"\$(stat -c %a /var/lib/debianform-ci/managed/primary.conf)\" = 640"
ssh_vm "test \"\$(stat -c %a /var/lib/debianform-ci/managed/secondary.conf)\" = 600"
ssh_vm "test \"\$(wc -l < /var/lib/debianform-ci/handler.log)\" = 1"
ssh_vm "test -s /var/lib/debianform-ci/state.json && test -e /var/lock/debianform-ci/state.lock"
ssh_vm "grep -q 'debian_file.managed' /var/lib/debianform-ci/state.json"
ssh_vm "getent group debianform-deploy >/dev/null"
ssh_vm "test \"\$(getent group debianform-deploy | cut -d: -f3)\" = 4242"
ssh_vm "getent passwd debianform-app >/dev/null"
ssh_vm "test \"\$(getent passwd debianform-app | cut -d: -f3)\" = 4250"
ssh_vm "test \"\$(id -gn debianform-app)\" = debianform-deploy"
ssh_vm "test \"\$(getent passwd debianform-app | cut -d: -f7)\" = /usr/sbin/nologin"

NOOP_PLAN="$(dbf plan -f "$CONFIG_FILE")"
printf '%s\n' "$NOOP_PLAN"
grep -qx "No changes." <<<"$NOOP_PLAN"
ssh_vm "test \"\$(wc -l < /var/lib/debianform-ci/handler.log)\" = 1"

log "introducing drift and verifying check failure"
ssh_vm "printf 'drift\n' > /var/lib/debianform-ci/managed/primary.conf"
if dbf check -f "$CONFIG_FILE" >"$WORK_DIR/drift-check.log" 2>&1; then
  printf 'dbf check unexpectedly accepted drift\n' >&2
  exit 1
fi
cat "$WORK_DIR/drift-check.log"
grep -q 'debian_file.managed\["primary"\]' "$WORK_DIR/drift-check.log"

log "repairing drift"
dbf apply -f "$CONFIG_FILE" --auto-approve
dbf check -f "$CONFIG_FILE"
ssh_vm "test \"\$(cat /var/lib/debianform-ci/managed/primary.conf)\" = 'managed primary'"
ssh_vm "test \"\$(wc -l < /var/lib/debianform-ci/handler.log)\" = 2"

log "introducing group drift and verifying repair"
ssh_vm "groupmod -g 4243 debianform-deploy"
if dbf check -f "$CONFIG_FILE" >"$WORK_DIR/group-drift-check.log" 2>&1; then
  printf 'dbf check unexpectedly accepted group drift\n' >&2
  exit 1
fi
cat "$WORK_DIR/group-drift-check.log"
grep -q "debian_group.deploy" "$WORK_DIR/group-drift-check.log"
dbf apply -f "$CONFIG_FILE" --auto-approve
dbf check -f "$CONFIG_FILE"
ssh_vm "test \"\$(getent group debianform-deploy | cut -d: -f3)\" = 4242"

log "introducing user drift and verifying repair"
ssh_vm "usermod -s /bin/sh debianform-app"
if dbf check -f "$CONFIG_FILE" >"$WORK_DIR/user-drift-check.log" 2>&1; then
  printf 'dbf check unexpectedly accepted user drift\n' >&2
  exit 1
fi
cat "$WORK_DIR/user-drift-check.log"
grep -q "debian_user.app" "$WORK_DIR/user-drift-check.log"
dbf apply -f "$CONFIG_FILE" --auto-approve
dbf check -f "$CONFIG_FILE"
ssh_vm "test \"\$(getent passwd debianform-app | cut -d: -f7)\" = /usr/sbin/nologin"

log "integration test passed"
