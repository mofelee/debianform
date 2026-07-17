<p align="right">
  <strong>English</strong> | <a href="03-users-and-ssh-keys.zh.md">简体中文</a>
</p>

# 03. Manage Users, Groups, and SSH Authorized Keys

This chapter creates a system group, system user, home directory, and SSH authorized key. It also
shows how `check` and `apply` detect and repair a manually removed authorized key.

The example has been verified on a Debian 13 amd64 test host. Work on this chapter also fixed a
system issue: when a directory's owner or group refers to a user or group created by the same
configuration, the resource graph now makes the directory depend on that user and group. Apply no
longer attempts chown before creating them.

## Create a Working Directory

```bash
mkdir -p debianform-manual/03-users-and-ssh-keys
cd debianform-manual/03-users-and-ssh-keys
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/03-state.json"
    lock_path = "/var/lock/debianform/manual/03-state.lock"
  }

  groups {
    group "manualapp" {
      system = true
    }
  }

  users {
    user "manualapp" {
      system = true
      group  = "manualapp"
      home   = "/var/lib/manualapp"
      shell  = "/usr/sbin/nologin"

      ssh_authorized_keys = [
        "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMANUALUSERKEY000000000000000000000000000000000000 manual@example",
      ]
    }
  }

  directories {
    directory "/var/lib/manualapp" {
      owner = "manualapp"
      group = "manualapp"
      mode  = "0750"
    }
  }
}
```

This configuration declares:

- The `manualapp` system group.
- The `manualapp` system user with `manualapp` as its primary group.
- The `/var/lib/manualapp` home directory, owned by `manualapp:manualapp` with mode `0750`.
- One authorized key.

The key in this example is intentionally fake and exists only to demonstrate resource management.
Replace it with a real public key in actual use.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

The offline plan should show four creates:

```text
Summary: 4 create, 0 update, 0 delete, 0 no-op, 0 operations
```

Inspect remote state:

```bash
ssh manual1 'getent passwd manualapp; getent group manualapp; stat -c %a:%U:%G /var/lib/manualapp; cat /var/lib/manualapp/.ssh/authorized_keys'
```

Expected output resembles:

```text
manualapp:x:999:989::/var/lib/manualapp:/usr/sbin/nologin
manualapp:x:989:
750:manualapp:manualapp
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMANUALUSERKEY000000000000000000000000000000000000 manual@example
```

The uid and gid may differ depending on system users and groups that already exist on the test host.

## Introduce Authorized-Key Drift

Delete the remote authorized key:

```bash
ssh manual1 'sed -i /MANUALUSERKEY/d /var/lib/manualapp/.ssh/authorized_keys'
```

Run:

```bash
dbf check
```

The command should fail and show that it will add the key again:

```text
+ host.manual1.users.user["manualapp"].ssh_authorized_key["3a13cdc31d27ffb8"]
  add authorized key for manualapp

Summary: 1 create, 0 update, 0 delete, 3 no-op, 0 operations
dbf: remote state does not match configuration
```

An authorized key is a separate resource, so DebianForm repairs only the key without recreating the
user.

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

Summary: 0 create, 0 update, 0 delete, 4 no-op, 0 operations
```

Confirm that the key is present again:

```bash
ssh manual1 'grep -F manual@example /var/lib/manualapp/.ssh/authorized_keys'
```

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/03-users-and-ssh-keys
cd debianform-manual/03-users-and-ssh-keys

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/03-state.json"
    lock_path = "/var/lock/debianform/manual/03-state.lock"
  }

  groups {
    group "manualapp" {
      system = true
    }
  }

  users {
    user "manualapp" {
      system = true
      group  = "manualapp"
      home   = "/var/lib/manualapp"
      shell  = "/usr/sbin/nologin"

      ssh_authorized_keys = [
        "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMANUALUSERKEY000000000000000000000000000000000000 manual@example",
      ]
    }
  }

  directories {
    directory "/var/lib/manualapp" {
      owner = "manualapp"
      group = "manualapp"
      mode  = "0750"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'getent passwd manualapp; getent group manualapp; stat -c %a:%U:%G /var/lib/manualapp; cat /var/lib/manualapp/.ssh/authorized_keys'

ssh manual1 'sed -i /MANUALUSERKEY/d /var/lib/manualapp/.ssh/authorized_keys'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F manual@example /var/lib/manualapp/.ssh/authorized_keys'
```

## Cleanup

```bash
ssh manual1 'userdel manualapp 2>/dev/null || true; groupdel manualapp 2>/dev/null || true; rm -rf /var/lib/manualapp /var/lib/debianform/manual/03-state.json /var/lock/debianform/manual/03-state.lock /var/lock/debianform/manual/03-state.lock.d'
```

You do not need to clean up before continuing to later chapters.
