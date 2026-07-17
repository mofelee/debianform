<p align="right">
  <strong>English</strong> | <a href="README.zh.md">简体中文</a>
</p>

# DebianForm User Manual

This directory contains a tutorial series for DebianForm users. Every chapter is written to be run
directly: it includes a complete `.dbf.hcl` example, verification commands, expected results, and
cleanup or rollback guidance. The series begins with the smallest complete workflow and gradually
introduces common operations tasks.

Every chapter example must:

- Run on a low-risk Debian 13 amd64 test host.
- Begin in an empty working directory so it does not depend on files the reader already has.
- Use `.dbf.hcl`, shell, and verification commands that have been tested on a real host.
- State up front when a chapter needs a public package repository or external download.
- Fix the implementation or documentation before moving to the next chapter when actual DebianForm
  behavior does not match the tutorial's goal.

## Chapter List

- [x] [01. Prepare a test host and complete the first apply/check](01-first-apply.md)
- [x] [02. Manage files and directories and repair drift](02-files-and-drift.md)
- [x] [03. Manage users, groups, and SSH authorized keys](03-users-and-ssh-keys.md)
- [x] [04. Install packages and configure APT sources](04-apt-and-packages.md)
- [x] [05. Manage systemd service units and service state](05-systemd-service.md)
- [x] [06. Manage kernel modules, sysctl, and BBR](06-kernel-and-sysctl.md)
- [x] [07. Manage an nftables firewall](07-nftables.md)
- [x] [08. Manage Docker Engine, daemon configuration, and user access](08-docker-engine.md)
- [x] [09. Deploy a Docker Compose project](09-docker-compose.md)
- [x] [10. Use profiles, variables, and per-environment parameters](10-profiles-and-variables.md)
- [x] [11. Use components to install prebuilt or source-built tools](11-components.md)
- [x] [12. Daily operations: plan review, drift, locks, state, and recovery](12-operations.md)

When extending the manual, add the new chapter to this list before writing and validating chapters
in order.

## Reading Order

Read from Chapter 1 in sequence. Later chapters reuse the workflow established by earlier ones:

```text
mkdir workdir
cat > site.dbf.hcl
dbf validate
dbf plan --offline
dbf plan
dbf apply --auto-approve
dbf plan
dbf check
ssh host '...'
```

Each chapter remains as independent as possible so a reader can copy only that chapter's files into
a new directory and run them.

## Test Convention

The manual validates examples on disposable libvirt Debian test hosts. Validation uses root SSH
because DebianForm currently needs to install packages, write under `/etc`, manage systemd, and
write state and locks under `/var/lib/debianform` and `/var/lock/debianform`.

Every example uses the hostname `manual1`. Point `manual1` at your own test host in
`~/.ssh/config`.
