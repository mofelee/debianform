# Libvirt Integration Tests

These tests boot fresh Debian 12 or Debian 13 amd64 cloud VMs with libvirt and
exercise the full managed-target lifecycle against real remote state. Debian 13
is the default and primary target; both versions are blocking CI targets.

Run all cases on the default Debian 13 target or explicitly select either
supported version:

```bash
make test-integration
make test-integration DEBIAN_VERSION=12
make test-integration DEBIAN_VERSION=13
```

Run one case:

```bash
make test-integration-case CASE=apt-source
make test-integration-case CASE=apt-source DEBIAN_VERSION=12
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

`DEBIAN_VERSION` accepts only `12` or `13`. The runner maps these values to the
official Bookworm or Trixie genericcloud amd64 image and asserts the guest's
Debian ID, version, codename, and architecture before executing a case.

Every numbered case step runs `validate`, an online JSON plan, drift rejection
when the case provides a drift hook, `apply`, an asserted JSON no-op plan,
`check`, and its case-specific post-apply checks. CI currently discovers 19
case directories and expands them across both versions: 2 versions x 19 cases,
or 38 blocking jobs. The `Managed target matrix gate` requires the complete
matrix to pass.

`wireguard` case uses the two-host runner. It boots two Debian VMs on the same
temporary libvirt network and verifies a native systemd-networkd WireGuard
tunnel over `wg0`.

Validate the case layout without starting a VM:

```bash
make test-integration-layout
```

Useful environment variables:

| Variable | Purpose |
| --- | --- |
| `DEBIAN_VERSION` | Public Make variable selecting `12` or `13`; defaults to `13`. |
| `DBF_INTEGRATION_DEBIAN_VERSION` | Internal runner equivalent selecting `12` or `13`; used by CI and direct script invocations. |
| `DBF_INTEGRATION_CASE` | Run a single case directory. |
| `DBF_INTEGRATION_WORKDIR` | Override the temporary work directory. |
| `DBF_INTEGRATION_KEEP_WORKDIR=1` | Preserve the work directory after the run. |
| `DBF_INTEGRATION_ARTIFACT_DIR` | Directory for failure diagnostics. |
| `DBF_INTEGRATION_IMAGE_CACHE` | Cache directory for the Debian cloud image. |
| `DBF_INTEGRATION_DISABLE_KVM=1` | Force QEMU software emulation. |
| `DBF_LIBVIRT_URI` / `LIBVIRT_DEFAULT_URI` | Libvirt URI. Single-, two-, and three-host cases support remote `qemu+ssh://...` by creating VM assets on the hypervisor. |
| `DBF_INTEGRATION_HYPERVISOR` | SSH host for the remote libvirt filesystem when it cannot be inferred from the URI. |
| `DBF_INTEGRATION_POOL` | Libvirt storage pool used for remote VM assets, default `vm`. |
| `DBF_INTEGRATION_REMOTE_BASE_IMAGE` | Hypervisor-side Debian base image path. Its SHA512 must match the verified official image; stale or corrupt copies are replaced atomically. |
