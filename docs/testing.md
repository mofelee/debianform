# Testing DebianForm

## Unit tests

Run the Go unit tests and race detector:

```bash
make test-unit
```

The GitHub Actions unit job also runs `gofmt`, `go vet`, and a CLI build.

CI runs for pushes, pull requests, manual dispatches, and every Monday so the moving Debian
`trixie/latest` image is tested regularly.

## Debian 13 integration test

The integration test uses libvirt to boot the latest official Debian 13
`debian-13-genericcloud-amd64.qcow2` image from:

```text
https://cloud.debian.org/images/cloud/trixie/latest/
```

It downloads `SHA512SUMS` from the same Debian directory and verifies the image before booting it.

The test creates an isolated NAT network and qcow2 overlay, injects a temporary root SSH key with
cloud-init, and exercises:

1. `dbf validate`
2. Initial `dbf plan`
3. `dbf apply` with an SSH-backed locked state
4. `dbf check`
5. A no-op second plan
6. Remote drift detection
7. Drift repair
8. Handler deduplication and change-only execution

Run it on an x86_64 Linux host:

```bash
sudo apt-get install \
  cloud-image-utils curl dnsmasq-base libvirt-clients \
  libvirt-daemon-system openssh-client qemu-system-x86 qemu-utils

sudo systemctl start libvirtd
make test-integration
```

KVM is used when `/dev/kvm` is writable. Otherwise the test falls back to QEMU software emulation.

Useful environment variables:

| Variable | Purpose |
| --- | --- |
| `DBF_INTEGRATION_WORKDIR` | Use a specific work directory |
| `DBF_INTEGRATION_KEEP_WORKDIR=1` | Preserve generated VM files after the test |
| `DBF_INTEGRATION_ARTIFACT_DIR` | Write failure diagnostics to a specific directory |
