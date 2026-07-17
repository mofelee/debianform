<p align="right">
  <strong>English</strong> | <a href="04-apt-and-packages.zh.md">简体中文</a>
</p>

# 04. Install Packages and Configure APT Sources

This chapter demonstrates two common APT operations:

- Installing a package from the official Debian repositories.
- Managing a deb822 `.sources` file and repairing it after drift.

The example has been verified on a Debian 13 amd64 test host. It installs `jq`, so the test host
must be able to reach Debian package repositories.

## Create a Working Directory

```bash
mkdir -p debianform-manual/04-apt-and-packages
cd debianform-manual/04-apt-and-packages
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/04-state.json"
    lock_path = "/var/lock/debianform/manual/04-state.lock"
  }

  apt {
    source_file "manual-disabled" {
      path = "/etc/apt/sources.list.d/debianform-manual-disabled.sources"

      content = <<-EOF
        Types: deb
        URIs: https://deb.debian.org/debian
        Suites: trixie
        Components: main
        Enabled: no
        Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
      EOF

      on_destroy = "restore"
    }
  }

  packages {
    install = ["jq"]
  }
}
```

This APT source file contains `Enabled: no`. It demonstrates how DebianForm manages a source file
and triggers `apt-get update` without enabling a duplicate repository.

`on_destroy = "restore"` means that when DebianForm stops managing the source file, it makes a best
effort to restore the content that existed before adoption. If the file did not exist before
adoption, DebianForm removes it.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

The offline plan should show two creates and one operation:

```text
Summary: 2 create, 0 update, 0 delete, 0 no-op, 1 operations
```

The operation is:

```text
! host.manual1.apt.cache_refresh
  refresh apt package cache
  command: apt-get update
```

A changed APT source file triggers one cache refresh so package installation can use current
metadata.

## Verify the Result

Check `jq`:

```bash
ssh manual1 'jq --version'
```

Expected output resembles:

```text
jq-1.7
```

Inspect the source file managed by DebianForm:

```bash
ssh manual1 'sed -n "1,8p" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

Expected output:

```text
Types: deb
URIs: https://deb.debian.org/debian
Suites: trixie
Components: main
Enabled: no
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
```

## Introduce Source-File Drift

Change `Enabled: no` to `Enabled: yes` manually:

```bash
ssh manual1 'sed -i "s/Enabled:.*/Enabled: yes/" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

Run:

```bash
dbf check
```

The command should fail, report an update to the source file, and plan another APT cache refresh:

```text
Summary: 0 create, 1 update, 0 delete, 1 no-op, 1 operations
dbf: remote state does not match configuration
```

`check` only reports drift. It does not run `apt-get update` or modify the file.

## Repair the Drift

Run:

```bash
dbf apply --auto-approve
dbf check
```

After repair, the result should be:

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 2 no-op, 0 operations
```

Confirm that the file was restored:

```bash
ssh manual1 'grep -F "Enabled: no" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/04-apt-and-packages
cd debianform-manual/04-apt-and-packages

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/04-state.json"
    lock_path = "/var/lock/debianform/manual/04-state.lock"
  }

  apt {
    source_file "manual-disabled" {
      path = "/etc/apt/sources.list.d/debianform-manual-disabled.sources"

      content = <<-EOT
        Types: deb
        URIs: https://deb.debian.org/debian
        Suites: trixie
        Components: main
        Enabled: no
        Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
      EOT

      on_destroy = "restore"
    }
  }

  packages {
    install = ["jq"]
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'jq --version'
ssh manual1 'sed -n "1,8p" /etc/apt/sources.list.d/debianform-manual-disabled.sources'

ssh manual1 'sed -i "s/Enabled:.*/Enabled: yes/" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F "Enabled: no" /etc/apt/sources.list.d/debianform-manual-disabled.sources'
```

## Cleanup

To remove the package installed by this chapter and delete the source file:

```bash
ssh manual1 'rm -f /etc/apt/sources.list.d/debianform-manual-disabled.sources /var/lib/debianform/manual/04-state.json /var/lock/debianform/manual/04-state.lock; rm -rf /var/lock/debianform/manual/04-state.lock.d; DEBIAN_FRONTEND=noninteractive apt-get remove -y jq libjq1 libonig5'
```

You do not need to clean up before continuing to later chapters.
