<p align="right">
  <strong>English</strong> | <a href="quickstart.zh.md">简体中文</a>
</p>

# DebianForm Quickstart

This is the shortest path from zero to the first apply/check on a low-risk Debian 13 amd64 host,
DebianForm's highest-priority target. Debian 12 amd64 also has Beta support. DebianForm remains in
public preview / beta, so do not use a production host for the first trial. For the separately
validated Ubuntu Preview paths, see the
[Ubuntu 24.04 Preview Quickstart](ubuntu-24.04-quickstart.md) and
[Ubuntu 26.04 Preview Quickstart](ubuntu-26.04-quickstart.md).

## 1. Install the CLI

On macOS or Linux, install with Homebrew:

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

Prepare a low-risk Debian 13 amd64 host and give it a stable name in `~/.ssh/config` on the control
machine. For a local-only platform assertion and offline smoke test for Debian 12 bookworm amd64,
see [`examples/debian12-amd64.dbf.hcl`](../examples/debian12-amd64.dbf.hcl).

Root access is required. DebianForm installs packages, writes under `/etc`, manages systemd, and
writes state and locks under `/var/lib/debianform` and `/var/lock/debianform`. Sudo, become, and
non-root management connections are unsupported; the user in SSH config must be root.

```sshconfig
Host server1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
```

Confirm that ordinary `ssh` works. DebianForm treats `host "server1"` as `ssh server1` by default,
so keep connection details in SSH config when possible:

```bash
ssh server1 'cat /etc/debian_version && uname -m'
```

## 3. Write the First Configuration

Create and enter a configuration directory. Initially, put only one `site.dbf.hcl` in it:

```bash
mkdir debianform-demo
cd debianform-demo
```

Create `site.dbf.hcl`:

```hcl
host "server1" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires the tcp_bbr kernel module."
  }
}
```

This configuration omits `ssh` and `state`:

- `host "server1"` connects through `ssh server1` by default.
- State defaults to `/var/lib/debianform/state/server1.json` on the target.
- The lock defaults to `/var/lock/debianform/state/server1.lock` on the target.

It does not need a `platform` block either. Online `plan`, `apply`, and `check` discover the real
host's distribution, version, architecture, and codename. Declare platform facts only when an
offline preview depends on them or when a fixture such as the Debian 12 smoke test explicitly
asserts them. An offline Ubuntu distribution dispatch requires the complete four-part tuple.

Add an `ssh` or `state` block only when overriding the connection name, port, identity file, or
state path.

The commands below omit `-f`. Without `-f`, `dbf` reads every `*.dbf.hcl` file in the current
working directory, sorts by filename, and processes them together. Since this directory contains
only `site.dbf.hcl`, the commands remain concise.

## 4. Validate Locally

`validate` parses and validates configuration without connecting to the target:

```bash
dbf validate
```

Expected output resembles:

```text
configuration is valid: 1 host(s)
```

The success message retains a historical format name. This output means the current configuration
is valid.

## 5. Preview the Plan Locally

`--offline` does not connect to the target and is useful for inspecting resource addresses and the
shape of changes:

```bash
dbf plan --offline
```

The plan should create the `tcp_bbr` module resource and two sysctl resources.

## 6. Plan Online

An online plan reads runtime facts, remote state, and observed state over SSH:

```bash
dbf plan
```

The first run normally contains create or update actions. It does not modify the target.

## 7. Apply

After confirming that the plan is correct, run:

```bash
dbf apply
```

In CI or a temporary test environment, skip interactive confirmation with:

```bash
dbf apply --auto-approve
```

Apply first generates an unlocked online preview. It then acquires the target's state lock, rereads
state and observed state, and prints the real execution plan. In interactive mode, a changed plan
requires confirmation again. Only after approval does DebianForm execute the resource graph and
write state.

## 8. Verify No-op and Check

After a successful apply, run the online plan again:

```bash
dbf plan
```

Create, update, delete, and operations should all be zero in the summary.

Finally, run:

```bash
dbf check
```

When the target agrees with configuration, `check` returns 0. If someone changes remote state
manually, it prints a plan and returns a non-zero status.

## Common Failures

See the [Operations Runbook](operations-runbook.md) for complete recovery procedures.

- `ssh: connect ...`: troubleshoot the network, SSH config, keys, and root login with ordinary
  `ssh server1` first.
- `offline plan cannot resolve runtime facts`: the configuration depends on remote facts. Use an
  online plan, or explicitly declare the required `platform.distribution`, `platform.version`,
  `platform.architecture`, and `platform.codename` in a fixture.
- `remote state does not match configuration`: `check` found drift or unapplied changes. Read the
  plan before deciding whether to apply, update configuration, or restore remote state.
