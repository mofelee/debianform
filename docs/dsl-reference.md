# DebianForm DSL Reference

<p align="right"><strong>English</strong> | <a href="dsl-reference.zh.md">简体中文</a></p>

This document lists the directives, fields, defaults, and constraints supported
by the current `.dbf.hcl` implementation. See the
[user manual](user-manual/README.md) for tutorials, the [CLI manual](cli.md) for
command-line options, and the [support matrix](support-matrix.md) for capability
status.

Examples marked with `<!-- dbf-test:... -->` are extracted into temporary
directories by `go test ./cmd/dbf`, which runs the commands in each marker.

## Top-Level Blocks

| Block | Purpose |
| --- | --- |
| `locals` | Define `local.<name>` values referenced by later expressions in the file. Nested blocks are unsupported. |
| `variable "<name>"` | Declare an external variable assigned through the CLI, environment, or a variable file. |
| `profile "<name>"` | Reusable host-configuration fragment imported by a profile or host. |
| `component "<name>"` | Reusable resource template with typed inputs and optional artifact installation. |
| `host "<name>"` | Target-host configuration entry point. |

The `<name>` of a `profile`, `component`, `host`, or explicit `host.component`
instance must be a valid HCL native identifier. Unicode identifiers and hyphens
after the first character are allowed, for example `主机一` and `edge-1`. Empty
strings, `.`, `..`, FQDNs, slashes, quotes, and whitespace are invalid labels.
A host label is a stable logical name used in resource addresses and default
state/lock filenames. Put a remote FQDN or IP address in `ssh.host`:

```hcl
host "web-prod" {
  ssh {
    host = "web.example.com"
  }
}
```

Both `profile` and `host` support `imports = [profile.name]`. Imported content
comes first and the current block overlays it. Maps merge recursively, lists
append with deduplication, and later scalar values replace earlier ones. A
`profile` cannot declare `system.hostname`, `platform.distribution`,
`platform.version`, `platform.architecture`, or `platform.codename`, and cannot
mount a component.

A `host` supports two component-mount forms. The shorthand uses the component
name as the instance name; the explicit form chooses an instance name and
passes inputs.

<!-- dbf-test:name=component-mount-syntax;commands=validate,plan-offline,component-inspect:base -->

```hcl
component "base" {
  input "key" {
    type    = string
    default = "default"
  }

  files {
    file "/etc/component-mount.txt" {
      content = input.key
    }
  }
}

host "short_component_mount" {
  components = [component.base]
}

host "custom_component_mount" {
  component "custom_name" {
    source = component.base
    inputs = {
      key = "value"
    }
  }
}
```

`assert` may appear in a `profile` or `host`:

<!-- dbf-test:name=assert-syntax;commands=validate,plan-offline -->

```hcl
host "assert_example" {
  system {
    hostname = "assert-example"
  }

  assert {
    condition = self.system.hostname != ""
    message   = "hostname must be set"
  }
}
```

Assertions evaluate against the merged host spec. They can read only `self` and
currently support `contains()`.

## Variables and Expressions

`variable` fields:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `type` | Yes | None | `string`, `number`, `bool`, `any`, `list(T)`, `set(T)`, `map(T)`, `object({...})`, or `tuple([...])`. |
| `default` | No | None | Without a default, an external value is required. |
| `description` | No | `""` | Description used in inspect output. |
| `nullable` | No | `true` | Reject `null` when false. |
| `sensitive` | No | `false` | Redact in plans, state, and errors. |
| `ephemeral` | No | `false` | Each resource field defines its boundary. `files.file.content` uses write-only semantics with `content_version`; APT source/signing-key and nftables content currently reject ephemeral values. |
| `const` | No | `false` | Currently emitted as variable metadata; do not rely on it to prevent external overrides. |
| `deprecated` | No | `""` | Emit a warning when assigned explicitly from outside. |
| `validation` | No | None | Contains `condition` and `error_message`. May read only the current `var.<name>`. |

A `component input` has similar fields but does not support `ephemeral` or
`const`, and its validation may read only the current `input.<name>`. Component
resource expressions may read `input.<name>`, top-level `var.<name>`, and
`target`. `target` is the merged spec of the mounting host; common fields
include `target.name`, `target.platform.distribution`,
`target.platform.version`, `target.platform.architecture`, and
`target.platform.codename`. The old `target.system.architecture` and
`target.system.codename` have been removed; using them fails with migration
guidance toward `target.platform.*`.

Object types may use `optional(T)` or `optional(T, default)`:

<!-- dbf-test:name=variable-object-syntax;commands=validate,plan-offline,variable-inspect -->

```hcl
variable "settings" {
  type = object({
    port = number
    tls  = optional(bool, false)
  })

  default = {
    port = 8080
  }
}

host "variable_object_example" {
  files {
    file "/etc/settings.json" {
      content = jsonencode(var.settings)
    }
  }
}
```

See [Common Configuration Selection](cli.md#common-configuration-selection)
in the CLI manual for top-level variable sources, precedence, and sensitive
runtime sources.

Ordinary expressions support `path.module`, `local.<name>`, `var.<name>`, and
the functions `file()`, `templatefile()`, `jsonencode()`, and `toset()`. When a
`profile` or `host` overrides imported content, `force()` replaces a list,
`before()` / `after()` control list merge order, and `unset()` removes a map or
object field.

<!-- dbf-test:name=expression-and-merge-syntax;commands=validate,plan-offline;files=template.txt -->

```hcl
locals {
  modules = ["loop"]
}

variable "message" {
  type    = string
  default = "hello"
}

profile "base" {
  kernel {
    modules = ["tcp_bbr"]
    sysctl = {
      "net.core.default_qdisc" = "fq"
    }
  }
}

host "expression_example" {
  imports = [profile.base]

  kernel {
    modules = after(local.modules)
    sysctl = {
      "net.core.default_qdisc" = unset()
    }
  }

  files {
    file "/etc/expression-example.json" {
      content = jsonencode({
        module_dir = path.module
        message    = templatefile("template.txt", { message = var.message })
        modules    = toset(local.modules)
      })
    }
  }
}

host "force_example" {
  imports = [profile.base]

  kernel {
    modules = force(local.modules)
  }
}
```

## Host Domains

Unless noted otherwise, the following domains may appear in `host` and
`profile`. A `component` may use only `apt`, `packages`, `files`, `secrets`,
`directories`, `groups`, `users`, `systemd`, and `services`.

### ssh

Available in `host` and `profile`. Defaults are
`host = host block label`, `port = 22`, and `user = "root"`. `user` may only be
omitted, set to an empty string, or set to `"root"`.

| Field | Description |
| --- | --- |
| `host` | SSH connection name or address. |
| `port` | SSH port. |
| `user` | Management user; only root is currently supported. |
| `identity_file` | Path to an SSH identity file. |

### state

Available in `host` and `profile`. Defaults:

```text
/var/lib/debianform/state/<host>.json
/var/lock/debianform/state/<host>.lock
```

| Field | Description |
| --- | --- |
| `path` | Remote state JSON path. |
| `lock_path` | Remote state-lock path. |

### system

Available in `host`. A `profile` may set only `timezone` and `locale`;
`hostname` is host-only. `hostname` is the desired managed system hostname. If
omitted, DebianForm does not manage it. `timezone` and `locale` are also desired
state: once explicitly declared, online `plan` / `apply` / `check` observes and
converges the target. Omitted values are unmanaged and do not reset the target
to defaults. `architecture` and `codename` do not belong to `system`. The old
`system.architecture` and `system.codename` have been removed and fail with
migration guidance toward `platform`.

| Field | Description |
| --- | --- |
| `hostname` | Desired managed system hostname; omission means hostname is unmanaged. |
| `timezone` | Desired system timezone, such as `UTC` or `Asia/Shanghai`; must be a relative name present under the target's `/usr/share/zoneinfo`. |
| `locale` | Desired system default locale, the `LANG` in `/etc/default/locale`, such as `C.UTF-8` or `en_US.UTF-8`. DebianForm installs/generates it when necessary and preserves unmanaged `LC_*` lines. |

### platform

Available in `host`, not `profile`. Describes target platform facts used mainly
for offline plans, assertions, target allowlist validation, Docker official
sources, and architecture-specific component sources. Online `plan` / `apply` /
`check` discovers these facts, so real-host configuration normally omits them.
An explicitly declared field is an assertion; a mismatch with discovery fails
before provider observation or mutation.

| Field | Description |
| --- | --- |
| `distribution` | `/etc/os-release` identity such as `debian` or `ubuntu`; it is never inferred from codename. |
| `version` | Distribution version such as Debian `12`/`13` or Ubuntu `24.04`/`26.04`. |
| `architecture` | dpkg architecture such as `amd64` or `arm64`. |
| `codename` | Distribution codename such as `bookworm`, `trixie`, `noble`, or `resolute`. |

An offline plan that dispatches for Ubuntu must declare the complete tuple:

```hcl
platform {
  distribution = "ubuntu"
  version      = "24.04"
  architecture = "amd64"
  codename     = "noble"
}
```

Ubuntu 26.04 uses the same fields with `version/codename` set to
`26.04/resolute`. For compatibility with existing Debian fixtures, offline
configuration that omits `distribution/version` retains historical Debian
repository defaults. This compatibility never infers Ubuntu from a codename
such as `noble` or `resolute`.

### kernel

Available in `host` and `profile`.

| Field | Description |
| --- | --- |
| `modules` | List of non-empty strings; load and persist each kernel module. |
| `sysctl` | `map(string)`; persist each sysctl and apply its runtime value. |

### packages

Available in `host`, `profile`, and `component`.

| Form | Description |
| --- | --- |
| `install = ["curl"]` | Install a package. |
| `package "<name>" { repositories = ["repo"] }` | Install a package with dependencies on named `apt.repository` resources. |

A `package` block may include `lifecycle { prevent_destroy = true }`.

### apt

Available in `host`, `profile`, and `component`.

Fields of `repository "<name>"`:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `uris` | When present | None | Non-empty string list. |
| `suites` | When present | None | Non-empty string list. |
| `components` | When present | None | Non-empty string list. |
| `architectures` | No | `[]` | deb822 Architectures. |
| `ensure` | No | `"present"` | `"present"` or `"absent"`. |
| `signing_key` | No | None | Nested block. When declared on a present repository, requires `url` or `content`. |

`signing_key` fields:

| Field | Description |
| --- | --- |
| `url` | Download the key; requires `sha256`. |
| `content` | Inline key content. References to sensitive values are redacted automatically; ephemeral values are currently unsupported. When `sha256` is also supplied, validate the content digest. |
| `sha256` | 64-character hexadecimal value. |
| `path` | Keyring path; defaults to `/etc/apt/keyrings/<safe-name>.asc`. |

Fields of `source_file "<label>"`:

| Field | Default | Description |
| --- | --- | --- |
| `path` | None | Must be an absolute path. |
| `content` / `source` | None | Exactly one is required when present. References to sensitive `content` are redacted automatically; ephemeral values are currently unsupported. Relative `source` paths resolve against the configuration-file directory. |
| `owner` | `"root"` | File owner. |
| `group` | `"root"` | File group. |
| `mode` | `"0644"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |
| `on_destroy` | `"keep"` | `"keep"` or `"restore"`. Sensitive content cannot use `"restore"`, preventing restoration plaintext from being stored in state. |

Both `repository` and `source_file` support
`lifecycle { prevent_destroy = true }`.

### files

Available in `host`, `profile`, and `component`.

Fields of `file "<path-or-label>"`:

| Field | Default | Description |
| --- | --- | --- |
| `path` | Label | Required explicitly when the label is not an absolute path. The final path must be absolute. |
| `content` / `source` | None | Exactly one is required when present. |
| `content_version` | `""` | Required when `content` contains an ephemeral value; used for write-only change detection. |
| `owner` | `"root"` | File owner. |
| `group` | `"root"` | File group. |
| `mode` | `"0644"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |
| `sensitive` | `false` | Request explicit redaction; content containing sensitive or ephemeral values is redacted automatically. |
| `on_change` | None | Component-only. May reference a component-local or root script, generating and executing an operation after an actual change. |

Supports `lifecycle { prevent_destroy = true }`.

### secrets

Available in `host`, `profile`, and `component`, but retained for compatibility
with the legacy form. New configuration should prefer
`variable + files.file content + content_version`.

Fields of `file "<path-or-label>"`:

| Field | Default | Description |
| --- | --- | --- |
| `path` | Label | The final path must be absolute. |
| `source` | None | Required when present; relative paths resolve against the configuration-file directory. |
| `owner` | `"root"` | File owner. |
| `group` | `"root"` | File group. |
| `mode` | `"0600"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |

Supports `lifecycle { prevent_destroy = true }`. Use emits a deprecation warning.

### directories

Available in `host`, `profile`, and `component`.

Fields of `directory "<absolute-path>"`:

| Field | Default |
| --- | --- |
| `owner` | `"root"` |
| `group` | `"root"` |
| `mode` | `"0755"` |
| `ensure` | `"present"` |

Supports `lifecycle { prevent_destroy = true }`.

### groups

Available in `host`, `profile`, and `component`.

Fields of `group "<name>"`:

| Field | Default | Description |
| --- | --- | --- |
| `gid` | `""` | Optional GID. |
| `system` | `false` | Create a system group. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |

Supports `lifecycle { prevent_destroy = true }`.

### users

Available in `host`, `profile`, and `component`.

Fields of `user "<name>"`:

| Field | Default | Description |
| --- | --- | --- |
| `uid` | `""` | Optional UID. |
| `home` | `""` | Optional home directory. |
| `shell` | `""` | Optional shell. |
| `group` | `""` | Primary group; a group with a different name must be declared. |
| `groups` | `[]` | Supplementary groups. |
| `system` | `false` | Create a system user. |
| `ssh_authorized_keys` | `[]` | Authorized-key list. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |

Supports `lifecycle { prevent_destroy = true }`.

### systemd

Available in `host`, `profile`, and `component`.

`unit "<name>"` manages a raw unit file at
`/etc/systemd/system/<name>`.

| Field | Default | Description |
| --- | --- | --- |
| `content` / `source` | None | Exactly one is required when present. |
| `owner` | `"root"` | File owner. |
| `group` | `"root"` | File group. |
| `mode` | `"0644"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |

`service_unit "<name>"` generates or manages a `.service` unit and appends
`.service` to the name automatically.

| Field | Default | Description |
| --- | --- | --- |
| `content` / `source` | None | Raw-unit mode; cannot combine with structured fields. |
| `description` | Unit name without `.service` | `[Unit] Description=`. |
| `run` | None | Required in structured mode; a string or string argv list. |
| `type` | None | `simple`, `exec`, `forking`, `oneshot`, `dbus`, `notify`, `notify-reload`, or `idle`. |
| `user` / `group` | None | `[Service] User=` / `Group=`. |
| `working_dir` | None | `[Service] WorkingDirectory=`. |
| `environment` | `{}` | `map(string)`, rendered as `Environment=`. |
| `restart` | None | `no`, `on-success`, `on-failure`, `on-abnormal`, `on-watchdog`, `on-abort`, or `always`. |
| `restart_delay` | None | `[Service] RestartSec=`. |
| `wants` / `after` | `[]` | `[Unit] Wants=` / `After=`. |
| `wanted_by` | `["multi-user.target"]` | `[Install] WantedBy=`; an empty list omits the Install section. |
| `stdout` / `stderr` | None | StandardOutput/StandardError. |
| `owner` | `"root"` | Unit-file owner. |
| `file_group` | `"root"` | Unit-file group. |
| `mode` | `"0644"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |

Both `unit` and `service_unit` support
`lifecycle { prevent_destroy = true }`.

#### systemd.networkd

`systemd { networkd { ... } }` generates networkd files and manages
`systemd-networkd.service`.

| Field | Default | Description |
| --- | --- | --- |
| `enable` | `null` | Whether to enable systemd-networkd. |

Fields of `netdev "<label>"`:

| Field | Default | Description |
| --- | --- | --- |
| `path` | `/etc/systemd/network/<label>.netdev` | Must be an absolute path. |
| `netdev` | `{}` | Render as `[NetDev]`; when present, must contain `Name` and `Kind`. |
| `wireguard` | `{}` | Render as `[WireGuard]`; inline `PrivateKey` is forbidden, so use `PrivateKeyFile`. |
| `wireguard_peer "<label>"` | None | Nested block rendered as `[WireGuardPeer]`; inline `PresharedKey` is forbidden. |
| `owner` / `group` | `"root"` / `"root"` | File owner/group. |
| `mode` | `"0644"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |

Fields of `network "<label>"`:

| Field | Default | Description |
| --- | --- | --- |
| `path` | `/etc/systemd/network/<label>.network` | Must be an absolute path. |
| `match` | `{}` | Render as `[Match]`; required when present. |
| `network` | `{}` | Render as `[Network]`; required when present. |
| `owner` / `group` | `"root"` / `"root"` | File owner/group. |
| `mode` | `"0644"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |

Networkd section values may be strings, numbers, booleans, or lists of those
types; booleans render as `yes`/`no`. Both `netdev` and `network` support
`lifecycle { prevent_destroy = true }`.

### services

Available in `host`, `profile`, and `component`.

Fields of `service "<name>"`:

| Field | Default | Description |
| --- | --- | --- |
| `package` | `""` | Optional package dependency. |
| `enabled` | `null` | Manage enablement when true/false; omission leaves it unmanaged. |
| `state` | `""` | `running`, `stopped`, `restarted`, or `reloaded`; omission leaves runtime state unmanaged. |

The service-unit name gains `.service` automatically. Supports
`lifecycle { prevent_destroy = true }`.

### nftables

Available in `host` and `profile`.

| Field | Default | Description |
| --- | --- | --- |
| `enable` | `null` | Whether to enable the nftables service. |

`main { ... }` manages `/etc/nftables.conf`, and `file "<label>" { ... }`
manages `/etc/nftables.d/<label>.nft` by default. Both use the same fields:

| Field | Default | Description |
| --- | --- | --- |
| `path` | As above | Must be an absolute path. |
| `content` / `source` | None | Exactly one is required when present. References to sensitive `content` are redacted automatically; ephemeral values are currently unsupported. |
| `owner` | `"root"` | File owner. |
| `group` | `"root"` | File group. |
| `mode` | `"0644"` | Four-digit octal string. |
| `ensure` | `"present"` | `"present"` or `"absent"`. |
| `sensitive` | `false` | Explicitly require output redaction; automatically enabled when `content` references a sensitive value. |
| `validate` | `true` | Trigger `nft -c -f`. |
| `activate` | `true` | Trigger nftables reload/activation. |

Supports `lifecycle { prevent_destroy = true }`.

### docker

Available in `host`.

Top-level fields:

| Field | Default | Description |
| --- | --- | --- |
| `enable` | `false` | Enable Docker Engine management. |
| `users` | `[]` | Users to add to the `docker` supplementary group. |

Fields of the `package` block:

| Field | Default | Description |
| --- | --- | --- |
| `source` | `"official"` | `"official"`, `"none"`, or `"custom"`. Omission uses Docker's official APT repository. |
| `channel` | `"stable"` | Only stable is currently used. |
| `version` | `null` | Parsed, but version pinning is not yet implemented. |
| `repository_url` | Select `linux/debian` or `linux/ubuntu` from the validated distribution | Available only with `source = "official"`; replaces the Docker official APT repository base URL. |
| `gpg_url` | Select the matching official repository's `/gpg` from the validated distribution | Available only with `source = "official"`; replaces the Docker official APT signing-key URL. |
| `gpg_sha256` | Official key SHA-256 for the default `gpg_url`; empty for a custom `gpg_url` | Available only with `source = "official"`; optional. A custom `gpg_url` does not inherit the official SHA, but this field enables checksum validation explicitly. |
| `remove_conflicts` | `"auto"` | `"auto"`, `true`/`"true"`, or `false`/`"false"`. |

Aliyun mirror example for Docker's official APT source:

```hcl
docker {
  package {
    repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
    gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
  }
}
```

The default official source selects Debian or Ubuntu URLs using online facts or
a complete offline platform tuple; it never infers distribution from codename.
Explicit `repository_url` / `gpg_url` values are not rewritten from a Debian
mirror to Ubuntu automatically. Ubuntu 26.04 must select `linux/ubuntu` with the
`resolute` suite and must not fall back silently to Ubuntu 24.04 `noble`.

These fields control the Docker Engine official APT source and signing key, not
a Docker registry mirror. They have a similar objective to
`get.docker.com --mirror`, namely using a mirror for Docker installation, but
DebianForm never runs the `get.docker.com` script. It declaratively manages the
APT source, key, packages, service, and state. With a custom `gpg_url` and no
`gpg_sha256`, DebianForm does not validate the key-file checksum. Set
`gpg_sha256` when content validation and key-content drift detection are needed.

Fields of the `service` block:

| Field | Default | Description |
| --- | --- | --- |
| `enable` | `true` | Manage docker.service enablement. |
| `state` | `"running"` | `"running"` or `"stopped"`. |

`daemon { settings = {...} }` generates `/etc/docker/daemon.json`. Settings
must be a JSON-compatible map and cannot contain sensitive or ephemeral values.

Fields of `compose "<name>"`:

| Field | Default | Description |
| --- | --- | --- |
| `enable` | `true` | Whether to manage the Compose project. |
| `state` | `"running"` | `"running"`, `"stopped"`, or `"absent"`. |
| `directory` | None | Must be an absolute path when enabled. |
| `project` | Compose label | Docker Compose project name. |
| `pull` | `"missing"` | `"never"`, `"missing"`, or `"always"`. |
| `recreate` | `"auto"` | `"auto"`, `"always"`, or `"never"`. |
| `remove_orphans` | `false` | Whether to remove orphan containers. |
| `after` | `["docker.service", "network-online.target"]` | After= for the generated service unit. |
| `wanted_by` | `["multi-user.target"]` | WantedBy= for the generated service unit. |

`compose.file` must appear exactly once without a label. Multiple
`env_file "<label>"` blocks are allowed. Both use these fields:

| Field | Default | Description |
| --- | --- | --- |
| `path` | None | Must be an absolute path. |
| `content` / `source` | None | Exactly one is required. |
| `owner` | `"root"` | File owner. |
| `group` | `"root"` | File group. |
| `mode` | File `"0644"`; env_file `"0600"` | Four-digit octal string. |

Fields of `compose.service`:

| Field | Default | Description |
| --- | --- | --- |
| `enable` | `true` | Whether to generate and manage the systemd unit/service. |
| `name` | `debianform-compose-<compose-label>` | Unit base name; `.service` is appended automatically. |

Compose labels, project names, and service names must start with a letter or
digit. Subsequent characters may be letters, digits, `_`, `.`, `@`, `%`, `+`,
or `-`.

## Component Artifacts

A `component` may contain only resources, or it may declare artifact
download/build/installation. Artifact fields:

| Field/block | Description |
| --- | --- |
| `type` | `"binary"`, `"archive"`, `"file"`, `"ca_certificate"`, or `"source"`. Required once source/extract/build/install/version is declared. |
| `version` | Version metadata included in plan/state. |
| `source ["architecture"]` | At least one. Omit the label for an architecture-independent source, or select by `platform.architecture`. Labeled and unlabeled sources cannot be mixed. |
| `extract` | Available for `binary`, `archive`, and `source`; required for `archive`. |
| `build` | Available and required only for `source`. |
| `install` | Required for artifact components. |

`source` fields:

| Field | Description |
| --- | --- |
| `url` | Non-empty URL. |
| `sha256` | 64-character hexadecimal value. |

`extract` fields:

| Field | Default | Description |
| --- | --- | --- |
| `format` | Infer from URL | For `binary`: `zip`, `tar.gz`, `tar.xz`, `bz2`, or `gz`; for `archive`: `tar.gz`; for `source`: `zip`, `tar.gz`, or `tar.xz`. |
| `strip_components` | `0` | Must be at least 0. |
| `include` | `""` | Required for binary zip/tar formats; unsupported for `archive` and `source`. |

`build` fields:

| Field | Default | Description |
| --- | --- | --- |
| `commands` | None | At least one argv list; never joined through a shell. |
| `packages` | `[]` | Build-package list. |
| `working_dir` | `""` | Relative path. |
| `output` | None | Required relative path. |
| `source_name` | `""` | Relative filename for a single-file source build. |

`install` fields:

| Field | Default | Description |
| --- | --- | --- |
| `path` | None | Must be an absolute path. |
| `owner` | `"root"` | File owner. |
| `group` | `"root"` | File group. |
| `mode` | `"0644"` for `file`/`ca_certificate`; `"0755"` otherwise | Four-digit octal string. |

Installing a `ca_certificate` triggers an `update-ca-certificates` operation.

### script / files.file.on_change

A program-root `script "<name>"`, at the same level as `host` and `component`,
is a shared definition instantiated per target host. Files in different
components may reference the same root declaration:

<!-- dbf-test:name=shared-networkd-reload-syntax;commands=validate,plan-offline -->

```hcl
script "reload_networkd" {
  mode = "once"
  commands = [
    ["systemctl", "start", "systemd-networkd.service"],
    ["networkctl", "reload"],
  ]
}

component "wan" {
  files {
    file "/etc/systemd/network/20-wan.network" {
      content   = "[Match]\nName=enp1s0\n"
      on_change = script.reload_networkd
    }
  }
}

component "policy_route" {
  files {
    file "/etc/systemd/network/30-policy-routing.network" {
      content   = "[Match]\nName=enp2s0\n"
      on_change = script.reload_networkd
    }
  }
}

host "router1" {
  components = [component.wan, component.policy_route]
  platform {
    architecture = "amd64"
    codename     = "trixie"
  }
}
```

Reference and shadowing rules are deterministic:

- `script.<name>` resolves a declaration local to the current component first;
  without a local declaration, it resolves the root declaration.
- `global.script.<name>` always resolves the root declaration explicitly,
  disambiguating a local declaration with the same name.
- The aggregation key is resolved declaration identity, not label text or
  command content. On one host, resources referencing the same root declaration
  merge. Different declarations never merge even when commands match. The same
  declaration is instantiated separately on different hosts.

A root script currently allows only `mode = "once"` and supports the same
`interpreter`, `outputs`, `run`, `content`, and `commands` fields as a component
script. It may read `var.<name>`, `local.<name>`, and per-host `target`, but no
component-specific `input.*`. A root script is only a definition: an execution
step exists only when a referencing resource actually changes. Unreferenced or
untriggered definitions do not execute. A stable operation address resembles
`host.router1.script["reload_networkd"]`; its `depends_on` and `triggered_by`
contain all referencing resources without duplicates.

The raw-file form above is appropriate for `[Route]`, `[RoutingPolicyRule]`, and
similar content not yet covered by the structured DSL. Existing structured
`systemd.networkd` resources continue using the provider's host-level reload
aggregation and need no such hook.

Existing component-local script semantics remain unchanged:

A component-local `script "<name>"` appears beside `files`, `services`, and
other domain blocks. A `files.file.on_change` can reference it:

<!-- dbf-test:name=component-script-on-change-syntax;commands=validate,plan-offline -->

```hcl
component "managed_app" {
  input "service_name" {
    type = string
  }

  script "reload" {
    mode        = "once"
    interpreter = ["/bin/sh", "-eu"]
    outputs     = ["/etc/managed-app/rendered.env"]
    run         = "cp /etc/managed-app/config.env /etc/managed-app/rendered.env && systemctl reload ${input.service_name}.service"
  }

  files {
    file "/etc/managed-app/config.env" {
      content   = "LISTEN_ADDR=127.0.0.1:8080\n"
      on_change = script.reload
    }
  }
}

host "app1" {
  component "app" {
    source = component.managed_app

    inputs = {
      service_name = "managed-app"
    }
  }
}
```

`script` fields:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `mode` | No | `"once"` | `"once"` or `"each"`. |
| `interpreter` | No | `["/bin/sh", "-eu"]` | Non-empty string list. |
| `outputs` | No | `[]` | Paths to ordinary files generated by the script; each must be absolute. |
| `run` | Exactly one of three | None | A single script-command string. |
| `content` | Exactly one of three | None | Multiline script text. |
| `commands` | Exactly one of three | None | Command matrix such as `[["systemctl", "reload", "app.service"]]`. |

Exactly one of `run`, `content`, and `commands` is required. Component script
fields may read `input.<name>`, `var.<name>`, and `target` from the component
evaluation context. A `host` or `profile` cannot declare `script`, and
`files.file.on_change` remains component-only.

The current implementation covers DSL parsing, validation, HostSpec
compilation, ResourceGraph/plan operation presentation, apply-time execution,
`once` / `each` triggers, runtime trigger context, and output-file drift
detection. The full script is an internal provider execution payload and never
appears in plan text/JSON/HTML.

With `outputs`, apply records each output-file hash after the script completes.
A later check/plan retriggers the script when an output is missing, becomes a
directory, its hash drifts, or the script declaration changes. `outputs` only
defines the observation boundary for side effects; DebianForm does not directly
write or remove those output files.

Under `mode = "once"`, one script runs once per apply even if several files that
actually changed trigger it. Under `mode = "each"`, it runs once per changed
file. Online plans display a unique operation address for every trigger, such as:

```text
host.app1.components.app.script["reload"].trigger["host.app1.components.app.files.file[\"/etc/app.conf\"]"]
```

The script receives these environment variables:

| Environment variable | Description |
| --- | --- |
| `DBF_SCRIPT_NAME` | Script name. |
| `DBF_COMPONENT_NAME` | Component instance name; empty for a host-scoped root script. |
| `DBF_TRIGGER_ADDRESS` | Current triggering resource address; always one address in `each` mode. |
| `DBF_TRIGGER_PATH` | Current triggering file path; always one path in `each` mode. |
| `DBF_TRIGGER_ADDRESSES` | Newline-separated addresses that triggered this script in the current run. |
| `DBF_TRIGGER_PATHS` | Newline-separated file paths that triggered this script in the current run. |

Use `mode = "each"` with `DBF_TRIGGER_PATH` when the script must process files
individually. For an ordinary service reload or restart, use the default
`mode = "once"`.

## Complete Example

The following example covers the primary current DSL directives. It is intended
only for local `validate` and `plan --offline` and does not connect to a remote
host.

<!-- dbf-test:name=dsl-reference;commands=validate,plan-offline;files=token.txt,local-source.txt -->

```hcl
locals {
  app_dir = "/opt/reference-app"
}

variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "dev"
  nullable    = false

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "runtime_token" {
  type        = string
  sensitive   = true
  ephemeral   = true
  default     = "not-a-real-secret-token"
  description = "Write-only runtime token."
}

profile "base" {
  system {
    timezone = "UTC"
    locale   = "C.UTF-8"
  }

  directories {
    directory "/opt/reference-app" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/reference-app/config.json" {
      content = jsonencode({
        environment = var.environment
        app_dir     = local.app_dir
      })
      mode = "0644"
    }
  }

  assert {
    condition = self.system.timezone == "UTC"
    message   = "base directory must be declared"
  }
}

component "app_sidecar" {
  input "port" {
    type        = number
    default     = 8080
    nullable    = false
    description = "Sidecar listen port."

    validation {
      condition     = input.port >= 1 && input.port <= 65535
      error_message = "port must be between 1 and 65535."
    }
  }

  files {
    file "/etc/reference-app/sidecar.env" {
      content = "PORT=${input.port}\n"
      mode    = "0644"
    }
  }

  services {
    service "reference-sidecar" {
      enabled = false
      state   = "stopped"
    }
  }
}

component "local_tool" {
  type    = "file"
  version = "1.0.0"

  source {
    url    = "file://local-source.txt"
    sha256 = "dbecfcfc1c83e7491897111315e80f6b9fabab3d144695cecb21bae7aeda8ba4"
  }

  install {
    path = "/usr/local/share/reference-tool.txt"
    mode = "0644"
  }
}

host "reference1" {
  imports = [profile.base]

  ssh {
    host          = "reference1"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform/reference/state.json"
    lock_path = "/var/lock/debianform/reference/state.lock"
  }

  system {
    hostname = "reference1"
  }

  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  kernel {
    modules = ["tcp_bbr"]
    sysctl = {
      "net.core.default_qdisc" = "fq"
    }
  }

  apt {
    repository "reference" {
      uris       = ["https://repo.example.invalid/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        content = "not-real-key\n"
        sha256  = "655e13e5db5c1ade95bc939d3d55d5f6d5f8d48f49ad436d3cdef1b962df8075"
      }
    }

    source_file "local" {
      path    = "/etc/apt/sources.list.d/local.sources"
      content = "Types: deb\nURIs: https://deb.debian.org/debian\nSuites: trixie\nComponents: main\n"
    }
  }

  packages {
    install = ["curl"]

    package "reference-tool" {
      repositories = ["reference"]

      lifecycle {
        prevent_destroy = true
      }
    }
  }

  groups {
    group "reference" {
      system = true
    }
  }

  users {
    user "reference" {
      system = true
      group  = "reference"
      home   = "/var/lib/reference"
      shell  = "/usr/sbin/nologin"
    }
  }

  files {
    file "/etc/reference-app/runtime-token" {
      content         = var.runtime_token
      content_version = "v1"
      mode            = "0600"
      sensitive       = true
    }

    file "/etc/reference-app/source-copy.txt" {
      source = "local-source.txt"
      mode   = "0644"
    }
  }

  secrets {
    file "/etc/reference-app/legacy-token" {
      source = "token.txt"
    }
  }

  systemd {
    unit "reference-raw.service" {
      content = "[Unit]\nDescription=Reference Raw\n[Service]\nExecStart=/bin/true\n"
    }

    service_unit "reference-app" {
      description = "Reference App"
      run         = ["/usr/bin/sleep", "infinity"]
      user        = "reference"
      group       = "reference"
      working_dir = "/var/lib/reference"
      restart     = "always"
      environment = {
        ENVIRONMENT = var.environment
      }
    }

    networkd {
      enable = true

      netdev "10-wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }
        wireguard = {
          PrivateKeyFile = "/etc/wireguard/wg0.key"
        }
        wireguard_peer "peer-a" {
          PublicKey  = "not-a-real-public-key"
          AllowedIPs = ["10.0.0.2/32"]
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }
        network = {
          Address = ["10.0.0.1/24"]
        }
      }
    }
  }

  services {
    service "reference-app" {
      enabled = true
      state   = "running"
    }
  }

  nftables {
    enable = true

    main {
      content = "flush ruleset\ninclude \"/etc/nftables.d/*.nft\"\n"
    }

    file "20-reference" {
      content = "table inet reference { chain input { type filter hook input priority 0; policy accept; } }\n"
    }
  }

  docker {
    enable = true

    package {
      remove_conflicts = false
    }

    service {
      enable = false
      state  = "stopped"
    }

    daemon {
      settings = {
        log-driver = "json-file"
        log-opts = {
          max-size = "10m"
        }
      }
    }

    users = ["reference"]

    compose "reference" {
      directory      = "/opt/reference-compose"
      project        = "reference"
      pull           = "never"
      recreate       = "never"
      remove_orphans = true

      file {
        path    = "/opt/reference-compose/compose.yaml"
        content = "services:\n  web:\n    image: debian:trixie-slim\n    command: sleep infinity\n"
      }

      env_file "app" {
        path    = "/opt/reference-compose/.env"
        content = "ENVIRONMENT=${var.environment}\n"
      }

      service {
        enable = false
      }
    }
  }

  component "sidecar" {
    source = component.app_sidecar
    inputs = {
      port = 9000
    }
  }

  components = [
    component.local_tool,
  ]

  assert {
    condition = self.platform.architecture == "amd64"
    message   = "reference example expects amd64"
  }
}
```
