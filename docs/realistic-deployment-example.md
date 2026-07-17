<p align="right">
  <strong>English</strong> | <a href="realistic-deployment-example.zh.md">简体中文</a>
</p>

# Realistic Deployment Template: A systemd Application

This document presents a small deployment that can be validated locally:
`examples/realistic-systemd-app.dbf.hcl`. It has no external downloads or real secrets and is a
useful starting template for a first low-risk Debian 13 host.

## Scope

The example declares a complete but compact systemd application:

- `groups.group "demoapp"`: creates a system group.
- `users.user "demoapp"`: creates a system user with `/usr/sbin/nologin`.
- `directories.directory`: manages permissions for `/etc/demoapp` and `/var/lib/demoapp`.
- `files.file`: writes a configuration file and executable script.
- `systemd.service_unit "demoapp"`: generates `/etc/systemd/system/demoapp.service`.
- `services.service "demoapp"`: declares the service enabled and running.

The management connection still uses DebianForm's current root SSH model. The service process
itself runs with the low-privilege `demoapp` account through `systemd.service_unit.user/group`.

## Local Acceptance

Run local syntax validation and preview the offline plan:

```bash
dbf validate -f examples/realistic-systemd-app.dbf.hcl
dbf plan -f examples/realistic-systemd-app.dbf.hcl --offline
```

The expected plan includes:

- Creating the `demoapp` group and user.
- Creating `/etc/demoapp` and `/var/lib/demoapp`.
- Writing `/etc/demoapp/config.env` and `/usr/local/bin/demoapp-worker`.
- Writing `demoapp.service` and triggering `systemctl daemon-reload`.
- Enabling and starting `demoapp.service`.

## Trying It on a Real Host

On a low-risk Debian 13 test host, first complete the SSH and state configuration under
`host "demoapp1"` as described in the Quickstart. Then run, in order:

```bash
dbf validate -f site.dbf.hcl
dbf plan -f site.dbf.hcl
dbf apply -f site.dbf.hcl
dbf plan -f site.dbf.hcl
dbf check -f site.dbf.hcl
```

After the first apply, the next online plan should be close to no-op and `check` should return 0. If
someone manually changes the unit, a managed file, or the service state, `check` should report drift
and return a non-zero status.

## Customization Guidance

- Replace `demoapp` with the real service name, keeping the user/group, directories, and unit name
  consistent.
- Put non-sensitive configuration in ordinary `files.file` declarations.
- For real tokens, private keys, or passwords, use the
  `variable + files.file sensitive = true` pattern instead of embedding them in example
  configuration.
- If the service needs an external binary, prefer a `component` artifact with a pinned SHA-256.
- For production data directories and high-risk services, evaluate whether
  `lifecycle.prevent_destroy` is appropriate.

Related documentation:

- [Quickstart](quickstart.md)
- [CLI Manual](cli.md)
- [Operations Runbook](operations-runbook.md)
- [Support Matrix](support-matrix.md)
- [systemd Service Units](systemd-service-units.md)
