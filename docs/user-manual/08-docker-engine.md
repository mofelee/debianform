<p align="right">
  <strong>English</strong> | <a href="08-docker-engine.zh.md">简体中文</a>
</p>

# 08. Manage Docker Engine, Daemon Configuration, and User Access

This chapter demonstrates DebianForm's high-level Docker support: installing Docker's official APT
repository and packages, enabling the Docker service, writing `/etc/docker/daemon.json`, and adding
a managed user to the `docker` group.

The example has been verified on a Debian 13 amd64 test host. The first run downloads and installs
Docker Engine from Docker's official APT repository and therefore requires network access.

Work on this chapter fixed a system issue as well: when `docker.users` refers to a user created by
the same configuration, online plan now permits the membership to be planned. Apply then follows
the dependency order, creating the user before adding it to the Docker group.

## Create a Working Directory

```bash
mkdir -p debianform-manual/08-docker-engine
cd debianform-manual/08-docker-engine
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/08-state.json"
    lock_path = "/var/lock/debianform/manual/08-state.lock"
  }

  users {
    user "dockerdeploy" {
      system = true
      home   = "/var/lib/dockerdeploy"
      shell  = "/usr/sbin/nologin"
    }
  }

  docker {
    enable = true
    users  = ["dockerdeploy"]

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "10m"
          "max-file" = "2"
        }
      }
    }
  }
}
```

`docker { enable = true }` expands into several resources:

- Docker's official APT signing key.
- Docker's official APT repository.
- An APT cache refresh.
- Removal of conflicting Docker packages.
- `docker-ce`, `docker-ce-cli`, `containerd.io`, buildx, and the Compose plugin.
- The `docker` group.
- `docker.service`.
- Optional daemon configuration and a restart operation.
- Optional supplementary-group membership for users.

Docker's official APT repository uses target platform facts. Online `dbf plan` discovers these facts
automatically. For a purely offline preview, temporarily add this to the host:

```hcl
platform {
  architecture = "amd64"
  codename     = "trixie"
}
```

## Use a Mirror of Docker's Official APT Repository

The default `docker.package.source = "official"` uses:

- `repository_url = "https://download.docker.com/linux/debian"`
- `gpg_url = "https://download.docker.com/linux/debian/gpg"`

To use Aliyun's mirror of Docker's official APT repository, override the URLs under `package`:

```hcl
host "manual1" {
  docker {
    enable = true

    package {
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
      gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
```

`gpg_sha256` is optional. DebianForm automatically uses the official Docker key SHA-256 for the
default official GPG URL. When `gpg_url` is customized, it does not apply the official hash to the
mirror URL. To pin the mirror's key content, add:

```hcl
gpg_sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

When a custom `gpg_url` omits `gpg_sha256`, DebianForm does not verify the key file's checksum. Set
`gpg_sha256` when content verification and key-content drift detection are required.

These fields are valid only with `package.source = "official"`; setting them with `none` or
`custom` is an error. Like `get.docker.com --mirror`, they change the source used to install Docker,
but DebianForm does not execute the `get.docker.com` script. It manages the APT source, key,
packages, service, and state itself.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan
dbf apply --auto-approve
```

The first plan contains many resources. Its summary resembles:

```text
Summary: 13 create, 0 update, 0 delete, 0 no-op, 2 operations
```

The two operations are:

- `apt-get update`
- `systemctl restart docker.service`

The first installation may take tens of seconds.

## Verify Docker

Run:

```bash
ssh manual1 'docker --version; docker compose version; systemctl is-active docker; systemctl is-enabled docker; id dockerdeploy; cat /etc/docker/daemon.json'
```

Expected output resembles:

```text
Docker version 29.6.0, build fb59821
Docker Compose version v5.1.4
active
enabled
uid=994(dockerdeploy) gid=986(dockerdeploy) groups=986(dockerdeploy),1000(docker)
{
  "log-driver": "json-file",
  "log-opts": {
    "max-file": "2",
    "max-size": "10m"
  }
}
```

Version numbers, uid, and gid may differ.

`docker.users` changes supplementary-group membership. A user who already has a login session must
log in again before that session gains the new group permission.

## Introduce Daemon-Configuration Drift

Change `max-size` to `20m` manually:

```bash
ssh manual1 'sed -i "s/10m/20m/" /etc/docker/daemon.json'
```

Run:

```bash
dbf check
```

The command should fail:

```text
~ host.manual1.docker.daemon.file["/etc/docker/daemon.json"]
  update file /etc/docker/daemon.json

! host.manual1.docker.daemon.restart
  restart docker service

Summary: 0 create, 1 update, 0 delete, 12 no-op, 1 operations
dbf: remote state does not match configuration
```

After daemon configuration changes, DebianForm rewrites the file and restarts Docker.

## Repair the Drift

Run:

```bash
dbf apply --auto-approve
dbf check
```

Confirm that configuration was restored and Docker remains active:

```bash
ssh manual1 'grep -F "max-size" /etc/docker/daemon.json; systemctl is-active docker'
```

Expected output:

```text
    "max-size": "10m"
active
```

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/08-docker-engine
cd debianform-manual/08-docker-engine

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/08-state.json"
    lock_path = "/var/lock/debianform/manual/08-state.lock"
  }

  users {
    user "dockerdeploy" {
      system = true
      home   = "/var/lib/dockerdeploy"
      shell  = "/usr/sbin/nologin"
    }
  }

  docker {
    enable = true
    users  = ["dockerdeploy"]

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "10m"
          "max-file" = "2"
        }
      }
    }
  }
}
EOF

dbf validate
dbf plan
dbf apply --auto-approve
ssh manual1 'docker --version; docker compose version; systemctl is-active docker; systemctl is-enabled docker; id dockerdeploy; cat /etc/docker/daemon.json'

ssh manual1 'sed -i "s/10m/20m/" /etc/docker/daemon.json'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F "max-size" /etc/docker/daemon.json; systemctl is-active docker'
```

## Cleanup

To uninstall Docker and remove this chapter's state:

```bash
ssh manual1 'systemctl disable --now docker.service 2>/dev/null || true; systemctl disable --now containerd.service 2>/dev/null || true; userdel dockerdeploy 2>/dev/null || true; rm -rf /var/lib/dockerdeploy /var/lib/debianform/manual/08-state.json /var/lock/debianform/manual/08-state.lock /var/lock/debianform/manual/08-state.lock.d; rm -f /etc/docker/daemon.json /etc/apt/sources.list.d/docker_official.sources /etc/apt/keyrings/docker.asc; DEBIAN_FRONTEND=noninteractive apt-get remove -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin'
```

Do not clean up before Chapter 9, because its Docker Compose example reuses Docker Engine.
