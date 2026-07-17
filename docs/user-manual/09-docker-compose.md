<p align="right">
  <strong>English</strong> | <a href="09-docker-compose.zh.md">简体中文</a>
</p>

# 09. Deploy a Docker Compose Project

This chapter demonstrates DebianForm's Docker Compose support: it manages a project directory,
`compose.yaml`, a sensitive `.env` file, and a generated systemd unit, then converges the Compose
project to `running`.

The example has been verified on a Debian 13 amd64 test host. The first run pulls `busybox:1.36`
from Docker Hub and therefore requires network access. If Chapter 8 already installed Docker, this
chapter reuses the existing Docker Engine.

Work on this chapter fixed a system issue: Compose-project drift inspection now uses
`docker compose ps --all --format json` to include stopped containers. After a manual stop, the
plan therefore reports `stopped -> running` accurately.

## Create a Working Directory

```bash
mkdir -p debianform-manual/09-docker-compose
cd debianform-manual/09-docker-compose
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/09-state.json"
    lock_path = "/var/lock/debianform/manual/09-state.lock"
  }

  docker {
    enable = true

    compose "manualapp" {
      state          = "running"
      directory      = "/opt/debianform-compose-manual"
      project        = "manualapp"
      pull           = "missing"
      recreate       = "auto"
      remove_orphans = true

      file {
        path = "/opt/debianform-compose-manual/compose.yaml"

        content = <<-YAML
          services:
            web:
              image: busybox:1.36
              command: ["sh", "-c", "while true; do echo debianform-compose; sleep 60; done"]
              labels:
                com.example.debianform: "manual09"
              env_file:
                - .env
        YAML
      }

      env_file "app" {
        path    = "/opt/debianform-compose-manual/.env"
        content = "TOKEN=manual09-secret-value\n"
        mode    = "0600"
      }
    }
  }
}
```

`docker.compose` does not replace Compose YAML. YAML still describes the container topology.
DebianForm manages the files, runs `docker compose config`, generates a systemd unit, and converges
project state through `docker compose up/stop/down`.

Important fields:

- `directory` is the Compose project's working directory.
- `file` manages the primary `compose.yaml`; its default mode is `0644`.
- `env_file` manages sensitive files. It treats content as sensitive by default and uses mode `0600`
  in this example.
- `project` corresponds to `docker compose -p manualapp`.
- `state = "running"` requires the project to remain running.
- `pull = "missing"` pulls an image when it is absent.
- `remove_orphans = true` removes containers for extra services during convergence.

This chapter also installs Docker Engine through Docker's official repository. Online `dbf plan`
discovers platform facts automatically. For a purely offline preview, declare the facts described
in Chapter 8. Ubuntu must provide complete `platform.distribution`, `platform.version`,
`platform.architecture`, and `platform.codename` values.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan
dbf apply --auto-approve
```

The first plan includes Docker Engine, Compose files, the systemd unit, and project resources. Its
summary resembles:

```text
Summary: 15 create, 0 update, 0 delete, 0 no-op, 3 operations
```

If Chapter 8 already installed Docker, the real apply may show Docker packages and repositories as
adopt existing. That is expected: this chapter uses a separate state file and must adopt existing
remote resources into its own state.

## Verify the Compose Project

Run:

```bash
dbf plan
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
ssh manual1 'systemctl is-active debianform-compose-manualapp.service; systemctl is-enabled debianform-compose-manualapp.service'
ssh manual1 'stat -c "%a %U %G %n" /opt/debianform-compose-manual /opt/debianform-compose-manual/compose.yaml /opt/debianform-compose-manual/.env'
```

Both `dbf plan` and `dbf check` should report no changes. The Compose project should have one
running container, the systemd service should be `active` and `enabled`, and file modes should
resemble:

```text
755 root root /opt/debianform-compose-manual
644 root root /opt/debianform-compose-manual/compose.yaml
600 root root /opt/debianform-compose-manual/.env
```

Confirm that state records the Compose resource without exposing `.env` plaintext:

```bash
ssh manual1 'grep -F "host.manual1.docker.compose[\"manualapp\"].project" /var/lib/debianform/manual/09-state.json'
ssh manual1 '! grep -F "manual09-secret-value" /var/lib/debianform/manual/09-state.json'
```

## Introduce Runtime Drift

Stop the Compose project manually:

```bash
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml stop'
```

Run:

```bash
dbf check
```

The command should fail and show that the project drifted from the desired `running` state to
`stopped`:

```text
~ host.manual1.docker.compose["manualapp"].project
  converge docker compose project manualapp from stopped to running

Summary: 0 create, 1 update, 0 delete, 14 no-op, 0 operations
dbf: remote state does not match configuration
```

## Repair the Drift

Run:

```bash
dbf apply --auto-approve
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
```

`apply` runs `docker compose up -d --pull missing` again, and `check` returns to no changes.

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/09-docker-compose
cd debianform-manual/09-docker-compose

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/09-state.json"
    lock_path = "/var/lock/debianform/manual/09-state.lock"
  }

  docker {
    enable = true

    compose "manualapp" {
      state          = "running"
      directory      = "/opt/debianform-compose-manual"
      project        = "manualapp"
      pull           = "missing"
      recreate       = "auto"
      remove_orphans = true

      file {
        path = "/opt/debianform-compose-manual/compose.yaml"

        content = <<-YAML
          services:
            web:
              image: busybox:1.36
              command: ["sh", "-c", "while true; do echo debianform-compose; sleep 60; done"]
              labels:
                com.example.debianform: "manual09"
              env_file:
                - .env
        YAML
      }

      env_file "app" {
        path    = "/opt/debianform-compose-manual/.env"
        content = "TOKEN=manual09-secret-value\n"
        mode    = "0600"
      }
    }
  }
}
EOF

dbf validate
dbf plan
dbf apply --auto-approve
dbf plan
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
ssh manual1 'systemctl is-active debianform-compose-manualapp.service; systemctl is-enabled debianform-compose-manualapp.service'
ssh manual1 'stat -c "%a %U %G %n" /opt/debianform-compose-manual /opt/debianform-compose-manual/compose.yaml /opt/debianform-compose-manual/.env'
ssh manual1 'grep -F "host.manual1.docker.compose[\"manualapp\"].project" /var/lib/debianform/manual/09-state.json'
ssh manual1 '! grep -F "manual09-secret-value" /var/lib/debianform/manual/09-state.json'

ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml stop'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml ps --format json | grep -i running'
```

## Cleanup

To remove the Compose project and state created by this chapter:

```bash
ssh manual1 'docker compose -p manualapp -f /opt/debianform-compose-manual/compose.yaml down 2>/dev/null || true; systemctl disable --now debianform-compose-manualapp.service 2>/dev/null || true; rm -f /etc/systemd/system/debianform-compose-manualapp.service; systemctl daemon-reload; rm -rf /opt/debianform-compose-manual /var/lib/debianform/manual/09-state.json /var/lock/debianform/manual/09-state.lock /var/lock/debianform/manual/09-state.lock.d'
```
