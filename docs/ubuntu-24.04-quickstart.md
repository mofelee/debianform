<p align="right">
  <strong>English</strong> | <a href="ubuntu-24.04-quickstart.zh.md">简体中文</a>
</p>

# Ubuntu 24.04 Preview Quickstart

This is the shortest path to the first `validate/plan/apply/check` on Ubuntu 24.04 LTS (`noble`)
amd64 Server. The target is currently **Preview**, not Beta, GA, or production-ready. Debian 13
amd64 remains DebianForm's default and highest-priority target.

Ubuntu 26.04 LTS amd64 is a separate Preview path with independent verification; results from this
page cannot substitute for 26.04 evidence. Current Ubuntu support excludes 22.04, arm64, desktop
environments, Snap, PPAs, Ubuntu Pro, cloud-init lifecycle management, sudo/become, and connections
through the default `ubuntu` user. DebianForm continues to use the same `dbf` CLI, `*.dbf.hcl` DSL,
and state. There is no separate UbuntuForm.

## 1. Install the CLI

This document tracks current `main`. Releases v0.6.0 and earlier do not include Ubuntu Preview.
After release notes explicitly list Ubuntu 24.04 LTS amd64 Preview, install through Homebrew or the
installation script:

```bash
brew install mofelee/debianform/dbf
dbf version
```

Or:

```bash
curl -fsSL https://raw.githubusercontent.com/mofelee/debianform/main/scripts/install.sh | sh
dbf version
```

Before such a release, build from current source and add the repository-root `dbf` to this shell's
`PATH`:

```bash
git clone https://github.com/mofelee/debianform.git
cd debianform
make build
export PATH="$PWD:$PATH"
dbf version
```

## 2. Prepare the Target Host

Prepare a fresh, low-risk Ubuntu Server 24.04 LTS amd64 VM. The management connection must be root
SSH. DebianForm does not connect as the default `ubuntu` user and then run sudo. Define a stable
alias in `~/.ssh/config` on the control machine:

```sshconfig
Host ubuntu24_preview
  HostName 192.0.2.24
  User root
  IdentityFile ~/.ssh/id_ed25519
```

Confirm SSH and the target tuple:

```bash
ssh ubuntu24_preview 'set -eu; . /etc/os-release; printf "%s %s %s\n" "$ID" "$VERSION_ID" "$VERSION_CODENAME"; dpkg --print-architecture'
```

Expected output includes `ubuntu 24.04 noble` and `amd64`. Online fact validation rejects any other
tuple.

## 3. Validate the Example and Plan Offline

[`examples/ubuntu-24.04-preview.dbf.hcl`](../examples/ubuntu-24.04-preview.dbf.hcl) explicitly
declares the complete platform tuple required by an offline plan:

```hcl
platform {
  distribution = "ubuntu"
  version      = "24.04"
  architecture = "amd64"
  codename     = "noble"
}
```

Validate and preview locally:

```bash
dbf validate -f examples/ubuntu-24.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-24.04-preview.dbf.hcl --offline
```

`validate` does not connect to the target. `plan --offline` does not read remote facts. Whenever an
Ubuntu configuration depends on distribution dispatch, it must explicitly provide `distribution`,
`version`, `architecture`, and `codename`; DebianForm does not infer the distribution from `noble`.

## 4. Plan Online, Apply, Verify No-op, and Check

After confirming that the local preview contains only `/etc/debianform-ubuntu-preview.txt`, run:

```bash
dbf plan -f examples/ubuntu-24.04-preview.dbf.hcl
dbf apply -f examples/ubuntu-24.04-preview.dbf.hcl
dbf plan -f examples/ubuntu-24.04-preview.dbf.hcl
dbf check -f examples/ubuntu-24.04-preview.dbf.hcl
```

In a temporary test environment, add `--auto-approve` to apply. The first online plan should contain
one file create. The second plan after apply should be no-op and `check` should return 0. Online
commands discover all four platform facts and reject a mismatch between declared and detected
identity before provider observation or mutation.

## 5. Network Ownership Boundary

This example does not declare `systemd.networkd` or manage files under `/etc/systemd/network/`, so
it does not trigger the network ownership preflight. DebianForm does not generate or read Netplan
YAML content, modify or delete Netplan YAML, or invoke `netplan`. An ordinary Ubuntu host can retain
its existing Netplan-managed network.

If configuration declares structured `systemd.networkd` resources or manages a raw file under
`/etc/systemd/network/`, an operator must first prepare the target outside DebianForm as a stable
native-networkd host that remains reachable after reboot. DebianForm performs a read-only check for
active ownership under `/etc/netplan/*.yaml`. A conflict fails before any network write, service
enable, or reload. DebianForm does not migrate, adopt, or remove Netplan configuration.

## 6. Preview Boundaries

- Ubuntu 24.04 LTS amd64 has an independent, blocking 20-case matrix. Preview still means less user
  feedback and release history than Beta.
- Docker's official source selects `https://download.docker.com/linux/ubuntu` from the validated
  `platform.distribution`. A custom Debian mirror URL is not rewritten automatically for Ubuntu.
- A new platform report should include the complete tuple, `dbf version`, a minimal redacted
  configuration, and plan/check output. Never publish SSH keys, tokens, plaintext state, or internal
  host information.

See the [Support Matrix](support-matrix.md) and
[Ubuntu 24.04 Support Contract](ubuntu-24.04-support-contract.md) for the complete capability and
exclusion list. Ubuntu 26.04 users should begin with the
[Ubuntu 26.04 Preview Quickstart](ubuntu-26.04-quickstart.md).

## Documentation Verification Record

On 2026-07-14, a fresh VM was created from Ubuntu's official
`noble-server-cloudimg-amd64.img` using implementation baseline
[`49c741e848bbbcc7c95f1969bf8952d5cc425e7d`](https://github.com/mofelee/debianform/commit/49c741e848bbbcc7c95f1969bf8952d5cc425e7d).
The image SHA-256 was
`ffe6203da54deeb6db5d2a98a83f9ec8e55f149d3f7ba622e1abe5fa966ee3d6`. The example on this page
passed validate, offline plan, online plan, apply, no-op plan, and check. Its final summary was
`0 create, 0 update, 0 delete, 1 no-op, 0 operations`. The test domain, disk, seed, console log, and
temporary SSH configuration were removed afterward.
