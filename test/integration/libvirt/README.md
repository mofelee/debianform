# Libvirt Integration Tests

These tests boot fresh Debian 13 cloud VMs with libvirt and exercise
`validate`, `apply`, and `check` against real remote state.

Run all cases:

```bash
make test-integration
```

Run one case:

```bash
make test-integration-case CASE=apt-source
make test-integration-case CASE=bbr
make test-integration-case CASE=component-inputs
make test-integration-case CASE=files
make test-integration-case CASE=multi-directory
make test-integration-case CASE=nftables
make test-integration-case CASE=shadowsocks-rust
make test-integration-case CASE=systemd-service-unit
make test-integration-case CASE=wireguard
```

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
| `DBF_INTEGRATION_CASE` | Run a single case directory. |
| `DBF_INTEGRATION_WORKDIR` | Override the temporary work directory. |
| `DBF_INTEGRATION_KEEP_WORKDIR=1` | Preserve the work directory after the run. |
| `DBF_INTEGRATION_ARTIFACT_DIR` | Directory for failure diagnostics. |
| `DBF_INTEGRATION_IMAGE_CACHE` | Cache directory for the Debian cloud image. |
| `DBF_INTEGRATION_DISABLE_KVM=1` | Force QEMU software emulation. |
| `DBF_LIBVIRT_URI` / `LIBVIRT_DEFAULT_URI` | Libvirt URI. Single-host cases support remote `qemu+ssh://...` by creating VM disk/seed files on the hypervisor. |
| `DBF_INTEGRATION_HYPERVISOR` | SSH host for the remote libvirt filesystem when it cannot be inferred from the URI. |
| `DBF_INTEGRATION_POOL` | Libvirt storage pool used for remote single-host VM assets, default `vm`. |
| `DBF_INTEGRATION_REMOTE_BASE_IMAGE` | Hypervisor-side Debian base image path for remote single-host cases. |
