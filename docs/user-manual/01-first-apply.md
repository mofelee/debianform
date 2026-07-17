<p align="right">
  <strong>English</strong> | <a href="01-first-apply.zh.md">简体中文</a>
</p>

# 01. Prepare a Test Host and Complete the First Apply/Check

This chapter completes the smallest DebianForm workflow: connect to a Debian test host, write one
file, and run `validate`, `plan`, `apply`, and `check`.

The example has been verified on a Debian 13 amd64 test host.

## Prerequisites

Prepare a disposable Debian 13 amd64 host. DebianForm currently requires root SSH because it writes
under `/etc`, manages systemd, installs packages, and writes remote state and locks.

First define a stable alias in `~/.ssh/config` on the control machine. This manual consistently uses
`manual1`:

```sshconfig
Host manual1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
```

If the test host is behind a jump host, add `ProxyJump`:

```sshconfig
Host manual1
  HostName 192.0.2.10
  User root
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump jump-host
```

Confirm SSH access:

```bash
ssh manual1 'cat /etc/debian_version && dpkg --print-architecture && id -u'
```

Expected output resembles:

```text
13.5
amd64
0
```

`id -u` must print `0`.

## Create a Working Directory

Use a separate directory for each chapter. This keeps the copied commands independent from files
created by other chapters.

```bash
mkdir -p debianform-manual/01-first-apply
cd debianform-manual/01-first-apply
```

## Write the First Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/01-state.json"
    lock_path = "/var/lock/debianform/manual/01-state.lock"
  }

  files {
    file "/etc/debianform-manual/hello.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "hello from DebianForm\n"
    }
  }
}
```

This configuration does three things:

- Manages the host `manual1`.
- Uses a chapter-specific state file so later chapters do not interfere with it.
- Writes `/etc/debianform-manual/hello.txt` on the remote host.

`files.file` creates its parent directories automatically, so there is no need to declare
`/etc/debianform-manual` separately.

## Validate Locally

Run:

```bash
dbf validate
```

Expected output:

```text
configuration is valid: 1 host(s)
```

`validate` parses and validates configuration without connecting to the host.

## Plan Offline

Run:

```bash
dbf plan --offline
```

The result should contain one create:

```text
Summary: 1 create, 0 update, 0 delete, 0 no-op, 0 operations
```

An offline plan does not connect to the host or read remote state. Use it to inspect resource
addresses and the general shape of changes first.

## Plan Online

Run:

```bash
dbf plan
```

The remote file does not exist on the first run, so the result is still one create:

```text
Summary: 1 create, 0 update, 0 delete, 0 no-op, 0 operations
```

An online plan reads host facts, remote state, and the actual file state over SSH. The diff therefore
includes observed details such as `exists: false` and `sha256: ""`.

## Apply the Change

For commands that are easy to copy, this manual uses `--auto-approve` to skip interactive
confirmation:

```bash
dbf apply --auto-approve
```

Successful output ends with:

```text
apply complete
```

`apply` first prints an unlocked preview. It then acquires the remote lock, recomputes and prints the
actual execution plan, and only then changes resources and writes state. In interactive mode, if the
two plans differ, DebianForm requests confirmation again while still holding the lock.

## Plan and Check Again

Run:

```bash
dbf plan
dbf check
```

Both commands should display:

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 1 no-op, 0 operations
```

This proves that configuration, remote state, and actual host state agree.

## Verify the Remote Result over SSH

Run:

```bash
ssh manual1 'cat /etc/debianform-manual/hello.txt && stat -c %a:%U:%G /etc/debianform-manual/hello.txt'
```

Expected output:

```text
hello from DebianForm
644:root:root
```

## Complete Chapter Command Sequence

The following block can be copied and run as one sequence:

```bash
mkdir -p debianform-manual/01-first-apply
cd debianform-manual/01-first-apply

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/01-state.json"
    lock_path = "/var/lock/debianform/manual/01-state.lock"
  }

  files {
    file "/etc/debianform-manual/hello.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "hello from DebianForm\n"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf plan
dbf apply --auto-approve
dbf plan
dbf check
ssh manual1 'cat /etc/debianform-manual/hello.txt && stat -c %a:%U:%G /etc/debianform-manual/hello.txt'
```

## Cleanup

To remove only the remote files created by this chapter, run:

```bash
ssh manual1 'rm -rf /etc/debianform-manual /var/lib/debianform/manual/01-state.json /var/lock/debianform/manual/01-state.lock /var/lock/debianform/manual/01-state.lock.d'
```

Later chapters use separate state paths. You do not need to clean up this chapter before continuing.
