<p align="right">
  <strong>English</strong> | <a href="02-files-and-drift.zh.md">简体中文</a>
</p>

# 02. Manage Files and Directories and Repair Drift

Building on Chapter 1, this chapter manages a directory and several files, then demonstrates real
drift. After a manual remote edit, `dbf check` fails and `dbf apply` restores the file to the state
declared in configuration.

The example has been verified on a Debian 13 amd64 test host.

## Create a Working Directory

```bash
mkdir -p debianform-manual/02-files-and-drift
cd debianform-manual/02-files-and-drift
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/02-state.json"
    lock_path = "/var/lock/debianform/manual/02-state.lock"
  }

  directories {
    directory "/etc/debianform-manual/app" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/app/app.env" {
      owner   = "root"
      group   = "root"
      mode    = "0640"
      content = <<-EOF
        APP_ENV=prod
        APP_PORT=8080
      EOF
    }

    file "/etc/debianform-manual/app/banner.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by DebianForm\n"
    }
  }
}
```

The configuration declares:

- One directory, `/etc/debianform-manual/app`.
- A more sensitive environment file, `app.env`, with mode `0640`.
- An ordinary text file, `banner.txt`, with mode `0644`.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

The offline plan should show three creates:

```text
Summary: 3 create, 0 update, 0 delete, 0 no-op, 0 operations
```

Inspect the remote files:

```bash
ssh manual1 'find /etc/debianform-manual/app -maxdepth 1 -type f -printf "%f:%m:%u:%g\n" | sort'
```

Expected output:

```text
app.env:640:root:root
banner.txt:644:root:root
```

Permission output from `stat` and `find` usually omits the leading zero, so `0640` is displayed as
`640`.

## Introduce Drift

Assume someone manually changes the remote environment file:

```bash
ssh manual1 'printf "APP_ENV=dev\nAPP_PORT=9090\n" > /etc/debianform-manual/app/app.env'
```

Run:

```bash
dbf check
```

This command should fail and end with output similar to:

```text
Summary: 0 create, 1 update, 0 delete, 2 no-op, 0 operations
dbf: remote state does not match configuration
```

`check` does not repair the host. It only checks whether remote state agrees with configuration and
returns a non-zero exit status when it finds a difference.

## Inspect the Drift

The `check` output shows that `app.env` must be restored to its configured content:

```text
~ host.manual1.files.file["/etc/debianform-manual/app/app.env"]
  update file /etc/debianform-manual/app/app.env
```

The observed `sha256` also differs from desired `summary.sha256`. For an ordinary file, DebianForm
uses the content hash, owner, group, and mode to detect drift.

## Repair the Drift

Run:

```bash
dbf apply --auto-approve
dbf check
```

After repair, `check` should show:

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 3 no-op, 0 operations
```

Confirm that file content was restored:

```bash
ssh manual1 'cat /etc/debianform-manual/app/app.env'
```

Expected output:

```text
APP_ENV=prod
APP_PORT=8080
```

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/02-files-and-drift
cd debianform-manual/02-files-and-drift

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/02-state.json"
    lock_path = "/var/lock/debianform/manual/02-state.lock"
  }

  directories {
    directory "/etc/debianform-manual/app" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/app/app.env" {
      owner   = "root"
      group   = "root"
      mode    = "0640"
      content = <<-EOT
        APP_ENV=prod
        APP_PORT=8080
      EOT
    }

    file "/etc/debianform-manual/app/banner.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by DebianForm\n"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'find /etc/debianform-manual/app -maxdepth 1 -type f -printf "%f:%m:%u:%g\n" | sort'

ssh manual1 'printf "APP_ENV=dev\nAPP_PORT=9090\n" > /etc/debianform-manual/app/app.env'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'cat /etc/debianform-manual/app/app.env'
```

## Cleanup

```bash
ssh manual1 'rm -rf /etc/debianform-manual/app /var/lib/debianform/manual/02-state.json /var/lock/debianform/manual/02-state.lock /var/lock/debianform/manual/02-state.lock.d'
```

You do not need to clean up before continuing to later chapters.
