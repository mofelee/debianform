<p align="right">
  <strong>English</strong> | <a href="05-systemd-service.zh.md">简体中文</a>
</p>

# 05. Manage a systemd Service Unit and Service State

This chapter deploys a low-privilege systemd service with DebianForm: it creates the runtime user,
writes a worker script, generates a `.service` unit, and enables and starts the service. It then
stops the service manually and uses `check` and `apply` to detect and repair the drift.

The example has been verified on a Debian 13 amd64 test host.

## Create a Working Directory

```bash
mkdir -p debianform-manual/05-systemd-service
cd debianform-manual/05-systemd-service
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/05-state.json"
    lock_path = "/var/lock/debianform/manual/05-state.lock"
  }

  groups {
    group "manualsvc" {
      system = true
    }
  }

  users {
    user "manualsvc" {
      system = true
      group  = "manualsvc"
      home   = "/var/lib/manualsvc"
      shell  = "/usr/sbin/nologin"
    }
  }

  directories {
    directory "/var/lib/manualsvc" {
      owner = "manualsvc"
      group = "manualsvc"
      mode  = "0750"
    }
  }

  files {
    file "/usr/local/bin/manualsvc-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOF
        #!/usr/bin/env sh
        set -eu
        while :; do
          date -Is >> /var/lib/manualsvc/heartbeat.log
          sleep 10
        done
      EOF
    }
  }

  systemd {
    service_unit "manualsvc" {
      description = "DebianForm manual service"
      run         = ["/usr/local/bin/manualsvc-worker"]
      user        = "manualsvc"
      group       = "manualsvc"
      working_dir = "/var/lib/manualsvc"
      restart     = "always"
      stdout      = "journal"
      stderr      = "journal"
    }
  }

  services {
    service "manualsvc" {
      enabled = true
      state   = "running"
    }
  }
}
```

This configuration generates `/etc/systemd/system/manualsvc.service` and runs the service under the
`manualsvc` account. The worker appends one timestamp to `/var/lib/manualsvc/heartbeat.log` every ten
seconds.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

The offline plan contains six resources and one operation:

```text
Summary: 6 create, 0 update, 0 delete, 0 no-op, 1 operations
```

The operation is a systemd daemon reload. After changing a unit file, DebianForm runs:

```text
systemctl daemon-reload
```

## Verify the Service

Run:

```bash
ssh manual1 'sleep 1; systemctl is-active manualsvc.service; systemctl is-enabled manualsvc.service; test -s /var/lib/manualsvc/heartbeat.log && tail -n 1 /var/lib/manualsvc/heartbeat.log'
```

Expected output resembles:

```text
active
enabled
2026-06-24T04:50:09+00:00
```

This confirms that the service is running, starts at boot, and writes heartbeats.

## Introduce Service Drift

Stop the service manually:

```bash
ssh manual1 'systemctl stop manualsvc.service'
```

Run:

```bash
dbf check
```

The command should fail and show that the service must start again:

```text
~ host.manual1.services.service["manualsvc"]
  start service manualsvc.service

Summary: 0 create, 1 update, 0 delete, 5 no-op, 0 operations
dbf: remote state does not match configuration
```

`check` still detects only; it does not start the service.

## Repair Service Drift

Run:

```bash
dbf apply --auto-approve
dbf check
```

After repair, the result should be:

```text
Plan:
  No changes.

Summary: 0 create, 0 update, 0 delete, 6 no-op, 0 operations
```

Confirm recovery:

```bash
ssh manual1 'systemctl is-active manualsvc.service'
```

Expected output:

```text
active
```

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/05-systemd-service
cd debianform-manual/05-systemd-service

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/05-state.json"
    lock_path = "/var/lock/debianform/manual/05-state.lock"
  }

  groups {
    group "manualsvc" {
      system = true
    }
  }

  users {
    user "manualsvc" {
      system = true
      group  = "manualsvc"
      home   = "/var/lib/manualsvc"
      shell  = "/usr/sbin/nologin"
    }
  }

  directories {
    directory "/var/lib/manualsvc" {
      owner = "manualsvc"
      group = "manualsvc"
      mode  = "0750"
    }
  }

  files {
    file "/usr/local/bin/manualsvc-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOT
        #!/usr/bin/env sh
        set -eu
        while :; do
          date -Is >> /var/lib/manualsvc/heartbeat.log
          sleep 10
        done
      EOT
    }
  }

  systemd {
    service_unit "manualsvc" {
      description = "DebianForm manual service"
      run         = ["/usr/local/bin/manualsvc-worker"]
      user        = "manualsvc"
      group       = "manualsvc"
      working_dir = "/var/lib/manualsvc"
      restart     = "always"
      stdout      = "journal"
      stderr      = "journal"
    }
  }

  services {
    service "manualsvc" {
      enabled = true
      state   = "running"
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'sleep 1; systemctl is-active manualsvc.service; systemctl is-enabled manualsvc.service; test -s /var/lib/manualsvc/heartbeat.log && tail -n 1 /var/lib/manualsvc/heartbeat.log'

ssh manual1 'systemctl stop manualsvc.service'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'systemctl is-active manualsvc.service'
```

## Cleanup

```bash
ssh manual1 'systemctl disable --now manualsvc.service 2>/dev/null || true; rm -f /etc/systemd/system/manualsvc.service /usr/local/bin/manualsvc-worker; systemctl daemon-reload; userdel manualsvc 2>/dev/null || true; groupdel manualsvc 2>/dev/null || true; rm -rf /var/lib/manualsvc /var/lib/debianform/manual/05-state.json /var/lock/debianform/manual/05-state.lock /var/lock/debianform/manual/05-state.lock.d'
```

You do not need to clean up before continuing to later chapters.
