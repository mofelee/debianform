# Libvirt Integration Tests

These tests boot fresh Debian 12, Debian 13, or Ubuntu 24.04 amd64 cloud VMs
with libvirt and exercise the full managed-target lifecycle against real remote
state. Debian 13 is the default and primary target. Debian 12, Debian 13, and
Ubuntu 24.04 each have a blocking CI matrix; Ubuntu's initial support tier is
Preview.

Run all cases on the default Debian 13 target or explicitly select either
supported version:

```bash
make test-integration
make test-integration DEBIAN_VERSION=12
make test-integration DEBIAN_VERSION=13
make test-integration TARGET=ubuntu-24.04
```

Run one case:

```bash
make test-integration-case CASE=apt-source
make test-integration-case CASE=apt-source DEBIAN_VERSION=12
make test-integration-case CASE=files TARGET=ubuntu-24.04
make test-integration-case CASE=bbr
make test-integration-case CASE=component-inputs
make test-integration-case CASE=files
make test-integration-case CASE=multi-directory
make test-integration-case CASE=nftables
make test-integration-case CASE=shadowsocks-rust
make test-integration-case CASE=script-on-change
make test-integration-case CASE=systemd-service-unit
make test-integration-case CASE=wireguard
```

`TARGET` accepts `debian-12`, `debian-13`, or `ubuntu-24.04`. When `TARGET` is
omitted, the compatibility variable `DEBIAN_VERSION=12|13` selects Debian and
defaults to Debian 13. The target descriptor resolves an official cloud image,
verifies its SHA256 or SHA512 publisher checksum, and asserts the guest's ID,
version, codename, and architecture before executing a case.

Every numbered case step runs `validate`, an online JSON plan, drift rejection
when the case provides a drift hook, `apply`, an asserted JSON no-op plan,
`check`, and its case-specific post-apply checks. CI currently discovers 20
case directories. The unchanged Debian matrix expands to 2 versions x 20 cases,
or 40 jobs guarded by `Managed target matrix gate`. The separate Ubuntu matrix
runs the same 20 cases and is guarded by `Ubuntu 24.04 target matrix gate`.

`wireguard` uses the two-host runner and `wireguard-three-host` uses the
three-host runner. On Ubuntu, these cases and `shared-script-networkd` carry a
`native-networkd.case` marker. The fixture disables future cloud-init network
rendering, removes Netplan ownership, installs a persistent networkd uplink,
and reboots before DebianForm runs. This fixture preparation is not a DebianForm
Netplan capability.

Validate the case layout without starting a VM:

```bash
make test-integration-layout
```

Useful environment variables:

| Variable | Purpose |
| --- | --- |
| `TARGET` | Public target selector: `debian-12`, `debian-13`, or `ubuntu-24.04`. |
| `DEBIAN_VERSION` | Public Make variable selecting `12` or `13`; defaults to `13`. |
| `DBF_INTEGRATION_TARGET` | Internal generic target selector used by CI and direct script invocations. |
| `DBF_INTEGRATION_CASE` | Run a single case directory. |
| `DBF_INTEGRATION_WORKDIR` | Override the temporary work directory. |
| `DBF_INTEGRATION_KEEP_WORKDIR=1` | Preserve the work directory after the run. |
| `DBF_INTEGRATION_ARTIFACT_DIR` | Directory for failure diagnostics. |
| `DBF_INTEGRATION_IMAGE_CACHE` | Cache directory for the selected cloud image. |
| `DBF_INTEGRATION_DISABLE_KVM=1` | Force QEMU software emulation. |
| `DBF_LIBVIRT_URI` / `LIBVIRT_DEFAULT_URI` | Libvirt URI. Single-, two-, and three-host cases support remote `qemu+ssh://...` by creating VM assets on the hypervisor. |
| `DBF_INTEGRATION_HYPERVISOR` | SSH host for the remote libvirt filesystem when it cannot be inferred from the URI. |
| `DBF_INTEGRATION_POOL` | Libvirt storage pool used for remote VM assets, default `vm`. |
| `DBF_INTEGRATION_REMOTE_BASE_IMAGE` | Hypervisor-side base image path. Its digest must match the verified official image; stale or corrupt copies are replaced atomically. |
