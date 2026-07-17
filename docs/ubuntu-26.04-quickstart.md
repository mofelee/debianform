<p align="right">
  <strong>English</strong> | <a href="ubuntu-26.04-quickstart.zh.md">简体中文</a>
</p>

# Ubuntu 26.04 Preview Quickstart

This is the shortest path to the first `validate/plan/apply/check` on Ubuntu 26.04 LTS (`resolute`)
amd64 Server. The target is currently **Preview**, not Beta, GA, or production-ready. Debian 13
amd64 remains DebianForm's default and highest-priority target.

The support scope excludes Ubuntu 26.04 arm64, desktop environments, NetworkManager management,
Snap, PPAs, Ubuntu Pro, cloud-init lifecycle management, sudo/become, connections through the
default `ubuntu` user, and in-place upgrades from Ubuntu 24.04 to 26.04. DebianForm continues to use
the same `dbf` CLI, `*.dbf.hcl` DSL, and state. There is no separate UbuntuForm.

## 1. Install the CLI

This document tracks current `main`. Before a release whose notes include Ubuntu 26.04 Preview,
build from current source:

```bash
git clone https://github.com/mofelee/debianform.git
cd debianform
make build
export PATH="$PWD:$PATH"
dbf version
```

After formal release notes explicitly list Ubuntu 26.04 LTS amd64 Preview, install with Homebrew:

```bash
brew install mofelee/debianform/dbf
dbf version
```

Or use the installation script:

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
dbf version
```

## 2. Prepare the Target Host

Prepare a fresh, low-risk Ubuntu Server 26.04 LTS amd64 VM. The management connection must be root
SSH. DebianForm does not connect as the default `ubuntu` user and then run sudo. Define a stable
alias in `~/.ssh/config` on the control machine:

```sshconfig
Host ubuntu26_preview
  HostName 192.0.2.26
  User root
  IdentityFile ~/.ssh/id_ed25519
```

Confirm SSH and the complete target tuple:

```bash
ssh ubuntu26_preview 'set -eu; . /etc/os-release; printf "%s %s %s\n" "$ID" "$VERSION_ID" "${VERSION_CODENAME:-$UBUNTU_CODENAME}"; dpkg --print-architecture'
```

Expected output includes `ubuntu 26.04 resolute` and `amd64`. Any other tuple is rejected before
provider observation or mutation.

## 3. Validate the Example and Plan Offline

[`examples/ubuntu-26.04-preview.dbf.hcl`](../examples/ubuntu-26.04-preview.dbf.hcl) explicitly
declares the complete platform tuple required by an offline plan:

```hcl
platform {
  distribution = "ubuntu"
  version      = "26.04"
  architecture = "amd64"
  codename     = "resolute"
}
```

Validate and preview locally:

```bash
dbf validate -f examples/ubuntu-26.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-26.04-preview.dbf.hcl --offline
```

`validate` does not connect to the target. `plan --offline` does not read remote facts. Whenever an
Ubuntu configuration depends on distribution dispatch, it must explicitly provide `distribution`,
`version`, `architecture`, and `codename`; DebianForm does not infer the distribution from
`resolute`.

## 4. Plan Online, Apply, Verify No-op, and Check

After confirming that the local preview contains only `/etc/debianform-ubuntu26-preview.txt`, run:

```bash
dbf plan -f examples/ubuntu-26.04-preview.dbf.hcl
dbf apply -f examples/ubuntu-26.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-26.04-preview.dbf.hcl
dbf check -f examples/ubuntu-26.04-preview.dbf.hcl
```

In a temporary test environment, add `--auto-approve` to apply. The first online plan should contain
one file create. The second plan after apply should be no-op and `check` should return 0. Online
commands discover all four platform facts and reject a mismatch between declared and detected
identity before provider observation or mutation.

## 5. APT and Docker Distribution Boundaries

Ubuntu 26.04 uses the `resolute` archive and security suites. Docker's official source must select
`https://download.docker.com/linux/ubuntu` and the `resolute` suite from the validated tuple.
DebianForm does not fall back to an Ubuntu 24.04 `noble` repository, package, or fixture. An explicit
custom repository URL is not rewritten automatically across distributions either.

## 6. Network Ownership Boundary

This example does not declare `systemd.networkd` or manage files under `/etc/systemd/network/`, so
it does not trigger the network ownership preflight. DebianForm does not generate or read Netplan
YAML content, modify or delete Netplan YAML, or invoke `netplan`. An ordinary Ubuntu host can retain
its existing Netplan-managed network.

If configuration declares structured `systemd.networkd` resources or manages a raw file under
`/etc/systemd/network/`, an operator must first prepare the target outside DebianForm as a stable
native-networkd host that remains reachable after reboot. DebianForm performs a read-only Netplan
ownership check. A conflict fails before any network write, service enable, or reload. DebianForm
does not migrate, adopt, or remove Netplan configuration.

## 7. Preview Boundaries

- Ubuntu 26.04 LTS amd64 has an independent, blocking 20-case matrix and
  `Ubuntu 26.04 target matrix gate`. Preview describes maturity; it does not make CI regressions
  acceptable.
- Ubuntu 24.04 LTS amd64 is a separate Preview path with independent verification. Matrix results
  cannot be shared between the two versions.
- Ubuntu 26.04 arm64, desktop/NetworkManager, Netplan management, sudo/become, the default `ubuntu`
  user, Snap, PPAs, Ubuntu Pro, cloud-init lifecycle management, and in-place upgrades remain
  Unsupported.
- A new platform report should include the complete tuple, `dbf version`, a minimal redacted
  configuration, and plan/check output. Never publish SSH keys, tokens, plaintext state, or internal
  host information.

See the [Support Matrix](support-matrix.md),
[Platform Support Strategy](platform-support-strategy.md), and
[Ubuntu 26.04 Support Contract](ubuntu-26.04-support-contract.md) for complete capabilities and
exclusions.

## Documentation Verification Record

On 2026-07-15, a fresh VM was created from Ubuntu's official released image
`ubuntu-26.04-server-cloudimg-amd64.img` using the CLI built at implementation baseline
[`0211ab2c98d674182dc91a9af7bd887dc91e5539`](https://github.com/mofelee/debianform/commit/0211ab2c98d674182dc91e5539),
together with the new example. The image SHA-256 was
`0826c5005ebc70edcfc4519e5d65eca766782f16426231c4c3e92b811ba8df0b`. Detected target facts were
`ubuntu/26.04/resolute/amd64`.

The example passed validate, offline plan, online plan, apply, the second online plan, and check.
The first online plan contained `1 create`; the post-apply plan and check both reported
`0 create, 0 update, 0 delete, 1 no-op, 0 operations`. The SHA-256 of
`/etc/netplan/50-cloud-init.yaml` remained
`e2aabf4fd72d957351ea7150be9d477e3bafb4654906198da9840a58bdba5e50` before and after execution,
and no networkd configuration was created. The test domain, disk, seed, console, NVRAM, remote
temporary directory, temporary SSH config, and known-hosts artifact were all removed afterward.

On the same implementation baseline,
[CI run 29418825778](https://github.com/mofelee/debianform/actions/runs/29418825778) also proved
Debian 12, Debian 13, Ubuntu 24.04, and Ubuntu 26.04 at `20/20` each, all three aggregate gates, and
`84/84` jobs including unit tests.
