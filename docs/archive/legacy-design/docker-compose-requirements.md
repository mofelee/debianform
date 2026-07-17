<p align="right"><strong>English</strong> | <a href="docker-compose-requirements.zh.md">简体中文</a></p>

# DebianForm Docker / Docker Compose Management Requirements

For the implementation breakdown and current status, see `docs/docker-compose-implementation-plan.md`.

Current implementation status: Compose systemd units initially use a fixed template that generates `Type=oneshot`, `RemainAfterExit=yes`, `ExecStart=docker compose ... up -d`, and `ExecStop=docker compose ... stop`. The MVP does not yet allow Compose units to customize `Type`, `Restart`, `User`, `Group`, or additional `Exec*` lines. Real apply/check operations require the target host to be able to access Docker's official APT repository, the Docker GPG key URL, and the image registry used by the Compose project; offline plans do not download or install Docker.

## 1. Background

DebianForm is a declarative configuration tool for Debian system operations. Its design draws on Terraform's `plan / apply / state` workflow and the declarative system-configuration experience of NixOS.

Docker and Docker Compose are common ways to deploy applications on Debian servers. DebianForm needs a concise, declarative, and composable Docker management syntax that lets users:

* Install and enable Docker Engine
* Use Docker's official APT repository instead of the older `docker.io` package from the Debian repository
* Manage Docker daemon configuration
* Manage Docker Compose projects
* Generate systemd-managed services
* Clearly express changes during `plan / apply / check`
* Reuse native Docker and Compose formats wherever possible instead of reinventing the Compose schema

## 2. Design Goals

### 2.1 User Experience Goals

The minimum usable syntax should be simple:

```hcl
docker {
  enable = true
}
```

This configuration should mean:

* Install Docker's official APT repository
* Install the official Docker packages
* Install the Docker Compose plugin
* Enable `docker.service`
* Start `docker.service`

Users should not need to declare `package.source = "official"` separately to obtain the official Docker packages.

### 2.2 Default Behavior Goals

`docker { enable = true }` uses Docker's official repository by default, not `docker.io` from the Debian repository.

The default package set is:

```text
docker-ce
docker-ce-cli
containerd.io
docker-buildx-plugin
docker-compose-plugin
```

The default enabled service is:

```text
docker.service
```

The default Compose command is:

```bash
docker compose
```

Not the legacy command:

```bash
docker-compose
```

### 2.3 Abstraction-Level Goals

DebianForm should not imitate the Terraform Docker provider by directly exposing low-level resources:

```hcl
docker_container "nginx" {}
docker_image "nginx" {}
docker_network "frontend" {}
docker_volume "data" {}
```

The first version should not directly manage individual containers, images, networks, or volumes.

DebianForm should manage two high-level objects:

1. Docker as a Debian system capability
2. A Compose project as an application deployment unit

In other words, the core semantics of Docker management should be:

```hcl
docker {
  enable = true

  compose "app" {
    state = "running"
  }
}
```

Rather than:

```hcl
docker {
  container "app" {}
}
```

## 3. Non-Goals

The first version does not implement:

* A redesigned, complete Compose schema
* Direct management of individual Docker containers
* Direct management of individual Docker images
* Direct management of individual Docker networks
* Direct management of individual Docker volumes
* Swarm or Kubernetes abstractions
* Complete private-registry lifecycle management
* A replacement for `compose.yaml`
* A DebianForm-specific container orchestration DSL

These capabilities can be added in later versions, but they are outside the MVP.

## 4. Top-Level Docker Syntax

### 4.1 Minimal Syntax

```hcl
docker {
  enable = true
}
```

Equivalent to:

```hcl
docker {
  enable = true

  package {
    source  = "official"
    channel = "stable"
  }

  service {
    enable = true
    state  = "running"
  }
}
```

### 4.2 Full Syntax Draft

```hcl
docker {
  enable = true

  package {
    source  = "official"
    channel = "stable"
    version = null

    remove_conflicts = "auto"
  }

  service {
    enable = true
    state  = "running"
  }

  daemon {
    settings = {
      "log-driver" = "json-file"
      "log-opts" = {
        "max-size" = "100m"
        "max-file" = "3"
      }
    }
  }

  users = ["deploy"]
}
```

## 5. Semantics of the `enable` Field

### 5.1 `enable = true`

```hcl
docker {
  enable = true
}
```

This means that the current host should provide Docker capability.

DebianForm should generate the following resources:

```text
apt.keyring["/etc/apt/keyrings/docker.asc"]
apt.repository["docker-official"]
apt.package["docker-ce"]
apt.package["docker-ce-cli"]
apt.package["containerd.io"]
apt.package["docker-buildx-plugin"]
apt.package["docker-compose-plugin"]
systemd.service["docker"]
```

It must also ensure that:

* Docker's official repository is configured
* The official Docker packages are installed
* `docker.service` is enabled
* `docker.service` is running
* The `docker compose` command is available

### 5.2 `enable = false`

```hcl
docker {
  enable = false
}
```

This means that DebianForm does not manage Docker.

By default, DebianForm should neither uninstall Docker nor stop an existing Docker service.

If uninstall behavior is supported later, it should be expressed through an explicit field such as:

```hcl
docker {
  enable = false
  purge  = true
}
```

This capability is outside the MVP.

## 6. Docker Installation Source Design

### 6.1 Default Installation Source

Default:

```hcl
package {
  source = "official"
}
```

Meaning:

* Use Docker's official APT repository
* Install the official Docker packages
* Do not use `docker.io` from the Debian repository

### 6.2 Supported Source Types

```hcl
package {
  source = "official"
}
```

Uses Docker's official repository.

```hcl
package {
  source = "debian"
}
```

Uses Docker packages from the official Debian repository.

```hcl
package {
  source = "none"
}
```

Does not install Docker packages and manages only configuration, services, or Compose projects.

```hcl
package {
  source = "custom"
}
```

Lets the user declare the repository, keyring, and packages.

### 6.3 Official Repository Syntax

```hcl
docker {
  enable = true

  package {
    source  = "official"
    channel = "stable"
  }
}
```

Installed by default:

```text
docker-ce
docker-ce-cli
containerd.io
docker-buildx-plugin
docker-compose-plugin
```

### 6.4 Debian Repository Syntax

```hcl
docker {
  enable = true

  package {
    source = "debian"
  }
}
```

The default package set can be:

```text
docker.io
docker-compose-plugin
```

This mode is primarily for users who want to follow the official Debian repositories exclusively.

It is not the default.

### 6.5 Do Not Install Packages

```hcl
docker {
  enable = true

  package {
    source = "none"
  }
}
```

Semantics:

* Do not add Docker's official repository
* Do not install Docker packages
* Continue to allow management of `/etc/docker/daemon.json`
* Continue to allow management of `docker.service`
* Continue to allow management of Compose projects

This mode suits production machines where Docker has already been installed manually.

## 7. Conflicting Package Handling

Docker's official packages can conflict with Docker-related packages already installed from Debian.

DebianForm should support conflict detection.

Proposed field:

```hcl
package {
  remove_conflicts = "auto"
}
```

Allowed values:

```text
auto
true
false
```

### 7.1 `remove_conflicts = "auto"`

This is the default.

Semantics:

* Detect conflicting packages during `plan`
* Clearly show the packages that will be replaced
* Remove conflicting packages as needed during `apply`
* Do not silently delete packages already installed by the user

Possible conflicting packages include:

```text
docker.io
docker-doc
docker-compose
podman-docker
containerd
runc
```

### 7.2 Plan Example

```text
~ docker.package
  source: debian -> official

- apt.package docker.io
+ apt.package docker-ce
+ apt.package docker-ce-cli
+ apt.package containerd.io
+ apt.package docker-buildx-plugin
+ apt.package docker-compose-plugin
```

## 8. Docker Daemon Configuration

### 8.1 Syntax

```hcl
docker {
  enable = true

  daemon {
    settings = {
      "log-driver" = "json-file"
      "log-opts" = {
        "max-size" = "100m"
        "max-file" = "3"
      }
      "registry-mirrors" = [
        "https://mirror.example.com"
      ]
    }
  }
}
```

### 8.2 Semantics

`daemon.settings` maps to:

```text
/etc/docker/daemon.json
```

DebianForm should:

* Generate the JSON file
* Validate the JSON
* Set file permissions
* Detect drift
* Reload or restart Docker after the configuration changes

Default file permissions:

```text
owner: root
group: root
mode: 0644
```

### 8.3 Compiled Resources

```text
file["/etc/docker/daemon.json"]
systemd.service["docker"]
```

When the daemon configuration changes, it should trigger:

```text
systemctl restart docker
```

A more granular reload behavior can be considered later, but the MVP can consistently restart the service.

## 9. Docker User Group

### 9.1 Syntax

```hcl
docker {
  enable = true

  users = ["deploy", "app"]
}
```

### 9.2 Semantics

Adds the users to the `docker` group.

Compiled resources:

```text
group["docker"]
user_group_membership["deploy:docker"]
user_group_membership["app:docker"]
```

Notes:

* DebianForm is responsible only for declaring that a user belongs to the docker group
* It does not refresh the group membership of the user's current login shell
* Plan/apply output should tell the user that permissions take effect after logging in again

## 10. Compose Project Management

### 10.1 Design Principles

DebianForm manages Compose projects rather than individual containers.

Native Compose configuration continues to be expressed by `compose.yaml`.

DebianForm is responsible for:

* Writing Compose files
* Writing env files
* Validating Compose configuration
* Generating systemd units
* Starting, stopping, or deleting Compose projects
* Integrating with plan, apply, check, and drift detection

### 10.2 Minimal Syntax

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }
  }
}
```

### 10.3 Full Syntax Draft

```hcl
docker {
  enable = true

  compose "app" {
    enable    = true
    state     = "running"
    directory = "/opt/app"
    project   = "app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
      owner   = "root"
      group   = "root"
      mode    = "0644"
    }

    env_file "app" {
      path    = "/opt/app/.env"
      content = file("./.env")
      owner   = "root"
      group   = "root"
      mode    = "0600"
    }

    pull           = "missing"
    recreate       = "auto"
    remove_orphans = false

    service {
      enable = true
      name   = "debianform-compose-app"
    }

    after     = ["docker.service", "network-online.target"]
    wanted_by = ["multi-user.target"]
  }
}
```

## 11. Compose Field Semantics

### 11.1 `compose "<name>"`

```hcl
compose "app" {}
```

`app` is DebianForm's internal identifier for the Compose project.

By default:

```hcl
project = "app"
```

### 11.2 `enable`

```hcl
compose "app" {
  enable = true
}
```

This tells DebianForm to manage the Compose project.

Default:

```hcl
enable = true
```

### 11.3 `state`

```hcl
state = "running"
```

Allowed values:

```text
running
stopped
absent
```

Meaning:

| state   | Behavior                            |
| ------- | ----------------------------------- |
| running | Run `docker compose up -d`          |
| stopped | Run `docker compose stop`           |
| absent  | Run `docker compose down`           |

Default:

```hcl
state = "running"
```

### 11.4 `directory`

```hcl
directory = "/opt/app"
```

The working directory of the Compose project.

DebianForm should ensure that this directory exists.

Default permissions:

```text
owner: root
group: root
mode: 0755
```

### 11.5 `project`

```hcl
project = "app"
```

The Compose project name.

If it is not set, the block label is used by default.

### 11.6 `file`

```hcl
file {
  path    = "/opt/app/compose.yaml"
  content = file("./compose.yaml")
}
```

Declares a Compose file managed by DebianForm.

`file.content` can come from:

```hcl
content = file("./compose.yaml")
```

It can also be inlined:

```hcl
content = <<-YAML
services:
  web:
    image: nginx:1.27-alpine
    ports:
      - "8080:80"
YAML
```

### 11.7 `env_file`

```hcl
env_file "app" {
  path    = "/opt/app/.env"
  content = file("./.env")
  mode    = "0600"
}
```

Declares an env file used by Compose and managed by DebianForm.

Multiple env files can be supported:

```hcl
env_file "app" {}
env_file "secret" {}
```

### 11.8 `pull`

```hcl
pull = "missing"
```

Allowed values:

```text
never
missing
always
```

Meaning:

| pull    | Behavior                         |
| ------- | -------------------------------- |
| never   | Do not proactively pull images  |
| missing | Pull an image when it is missing |
| always  | Pull before every apply          |

Proposed default:

```hcl
pull = "missing"
```

### 11.9 `recreate`

```hcl
recreate = "auto"
```

Allowed values:

```text
auto
always
never
```

Meaning:

| recreate | Behavior                                         |
| -------- | ------------------------------------------------ |
| auto     | Let Docker Compose decide whether to recreate    |
| always   | Force recreation                                 |
| never    | Do not recreate                                  |

Default:

```hcl
recreate = "auto"
```

### 11.10 `remove_orphans`

```hcl
remove_orphans = true
```

Appends the following option when running Compose up:

```bash
--remove-orphans
```

Default:

```hcl
remove_orphans = false
```

## 12. systemd Integration

### 12.1 Goal

Each Compose project should be able to generate a systemd unit so that systemd manages startup at boot and service supervision.

### 12.2 Default Unit Name

```text
debianform-compose-<name>.service
```

For example:

```hcl
compose "app" {}
```

Generates by default:

```text
debianform-compose-app.service
```

### 12.3 Syntax

```hcl
compose "app" {
  service {
    enable = true
    name   = "debianform-compose-app"
  }

  after     = ["docker.service", "network-online.target"]
  wanted_by = ["multi-user.target"]
}
```

### 12.4 Default Dependencies

Defaults:

```hcl
after = ["docker.service", "network-online.target"]
wanted_by = ["multi-user.target"]
```

### 12.5 Generated Unit Example

```ini
[Unit]
Description=DebianForm Compose Project app
Requires=docker.service
After=docker.service network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/app
ExecStart=/usr/bin/docker compose -p app -f /opt/app/compose.yaml up -d
ExecStop=/usr/bin/docker compose -p app -f /opt/app/compose.yaml stop
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
```

When `state = "absent"`, DebianForm can run:

```bash
docker compose -p app -f /opt/app/compose.yaml down
```

## 13. Compose Validation

DebianForm should validate the Compose configuration before apply.

Proposed command:

```bash
docker compose -p <project> -f <file> config
```

When validation fails:

* Stop apply
* Report the error
* Do not start the service
* Do not modify systemd state

## 14. Plan Behavior

### 14.1 Adding Docker

Input:

```hcl
docker {
  enable = true
}
```

Example plan output:

```text
+ docker
  + apt keyring /etc/apt/keyrings/docker.asc
  + apt repository docker-official
  + package docker-ce
  + package docker-ce-cli
  + package containerd.io
  + package docker-buildx-plugin
  + package docker-compose-plugin
  + service docker.service enabled
  + service docker.service running
```

### 14.2 Adding a Compose Project

Input:

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }
  }
}
```

Example plan output:

```text
+ docker.compose["app"]
  + directory /opt/app
  + file /opt/app/compose.yaml
  + systemd unit debianform-compose-app.service
  + validate compose config
  + start compose project app
```

### 14.3 Modifying Daemon Configuration

```text
~ docker.daemon
  ~ /etc/docker/daemon.json
  ~ restart docker.service
```

### 14.4 Modifying a Compose File

```text
~ docker.compose["app"]
  ~ file /opt/app/compose.yaml
  ~ validate compose config
  ~ docker compose up -d
```

## 15. Apply Behavior

`apply` should run in the following order:

1. Configure Docker's official repository
2. Install Docker packages
3. Create the docker group
4. Configure user group memberships
5. Write `/etc/docker/daemon.json`
6. Enable and start `docker.service`
7. Create the Compose working directory
8. Write the Compose file
9. Write env files
10. Run `docker compose config`
11. Write the systemd unit
12. Run `systemctl daemon-reload`
13. Enable and start the Compose systemd unit
14. Run `up`, `stop`, or `down` according to `state`

## 16. Check Behavior

`check` should verify:

* Docker's official repository exists
* Docker packages are installed
* The Docker version satisfies the declaration
* `docker.service` is enabled
* `docker.service` is running
* The content of `/etc/docker/daemon.json` matches the declaration
* The Compose working directory exists
* The Compose file content matches the declaration
* Env file content matches the declaration
* The systemd unit content matches the declaration
* The Compose project is in the desired state

Example output:

```text
✓ docker official repository
✓ docker packages
✓ docker.service running
✓ /etc/docker/daemon.json
✓ compose app config
✓ compose app systemd unit
✓ compose app running
```

## 17. Drift Detection

DebianForm should detect the following drift:

* A user manually modified `/etc/docker/daemon.json`
* A user manually modified the Compose file
* A user manually modified an env file
* A user manually disabled `docker.service`
* A user manually stopped `docker.service`
* A user manually modified the systemd unit
* The Compose project is not running
* The Compose project contains orphan containers

Example drift output:

```text
! drift detected

~ /etc/docker/daemon.json
  expected hash: abc123
  actual hash:   def456

~ docker.compose["app"]
  expected state: running
  actual state:   stopped
```

## 18. Relationship to the Existing DebianForm Architecture

The Docker DSL is a high-level DebianForm domain block.

The user writes:

```hcl
host "server1" {
  docker {
    enable = true
  }
}
```

The compiler should expand it into a low-level resource graph:

```text
apt_repository
apt_keyring
apt_package
file
directory
group
user_group_membership
systemd_service
operation
```

Users do not directly manipulate low-level resources unless an escape hatch is provided later.

## 19. Recommended MVP Scope

The first version implements only the following capabilities:

```hcl
docker {
  enable = true
}
```

```hcl
docker {
  enable = true

  daemon {
    settings = {}
  }
}
```

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }
  }
}
```

The MVP must support:

* Docker's official APT repository
* Official Docker packages
* Enabling and starting the Docker service
* `/etc/docker/daemon.json`
* Writing Compose files
* Validating Compose configuration
* Compose project up, stop, and down operations
* Generating systemd units
* Plan, apply, and check output

The MVP does not yet support:

* Automatically generating Compose YAML from HCL objects
* Image lifecycle management
* Volume lifecycle management
* Network lifecycle management
* Registry login
* Secret management
* Swarm
* Rootless Docker
* A Podman backend

## 20. Future Extensions

### 20.1 Generate Compose YAML from HCL

Later versions can support:

```hcl
docker {
  compose "app" {
    spec = {
      services = {
        web = {
          image = "nginx:1.27-alpine"
          ports = ["8080:80"]
        }
      }
    }
  }
}
```

This capability is only syntax sugar.

Native Compose files should remain the primary path:

```hcl
file {
  content = file("./compose.yaml")
}
```

### 20.2 Registry Login

Later versions can support:

```hcl
docker {
  registry "ghcr.io" {
    username = "user"
    password = secret("ghcr_token")
  }
}
```

This is outside the MVP.

### 20.3 Rootless Docker

Later versions can support:

```hcl
docker {
  rootless {
    enable = true
    user   = "deploy"
  }
}
```

This is outside the MVP.

### 20.4 Docker Mirror

Later versions can support:

```hcl
docker {
  package {
    source = "official"

    repository {
      mirror = "https://mirror.example.com/docker"
    }
  }
}
```

This is outside the MVP.

## 21. Final Recommended Syntax

Minimal Docker:

```hcl
docker {
  enable = true
}
```

Docker daemon:

```hcl
docker {
  enable = true

  daemon {
    settings = {
      "log-driver" = "json-file"
      "log-opts" = {
        "max-size" = "100m"
        "max-file" = "3"
      }
    }
  }
}
```

Docker Compose:

```hcl
docker {
  enable = true

  compose "app" {
    state     = "running"
    directory = "/opt/app"

    file {
      path    = "/opt/app/compose.yaml"
      content = file("./compose.yaml")
    }

    env_file "app" {
      path    = "/opt/app/.env"
      content = file("./.env")
      mode    = "0600"
    }

    after     = ["docker.service", "network-online.target"]
    wanted_by = ["multi-user.target"]
  }
}
```

## 22. Core Conclusions

DebianForm's Docker management semantics should be:

```hcl
docker {
  enable = true
}
```

Meaning:

```text
让这台 Debian 主机具备 Docker 能力。
```

The default behavior must be:

```text
使用 Docker 官方源安装 Docker。
```

Rather than:

```text
使用 Debian 仓库中的 docker.io。
```

Compose semantics should be:

```hcl
docker {
  compose "app" {}
}
```

Meaning:

```text
管理一个 Docker Compose project。
```

Rather than:

```text
管理一组低阶 container / image / network / volume。
```

This preserves all of the following:

* DebianForm's high-level declarative system experience
* Terraform-style plan, apply, and check capabilities
* The NixOS-style enable convention
* Compatibility with the native Docker Compose ecosystem
* A concise, stable, and extensible DSL boundary
