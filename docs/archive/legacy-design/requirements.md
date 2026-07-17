<p align="right"><strong>English</strong> | <a href="requirements.zh.md">简体中文</a></p>

# DebianForm Requirements

## Background

DebianForm is the current mainline and can adopt a breaking redesign.

The goal of the current design is not to continue piling features onto the old experimental syntax. It is to redesign a user-facing system configuration DSL that lets users describe what a system should look like, as they would in a NixOS `configuration.nix`, and then have the compiler translate that description into an executable low-level resource graph.

Core direction:

```text
用户写 host/profile/component
编译器合并 profile 和 host override
编译器在最终 host 上实例化 component
生成中间表达 HostSpec
编译成低阶 ResourceGraph
调度器根据 DAG 自动决定执行顺序和并行层
```

## Design Principles

The current design should follow these principles:

- The user layer does not expose the name of the `low-level provider resource` provider.
- A `host` is the central object and represents the desired state of a Debian host.
- A `profile` is a reusable configuration fragment, similar to a simplified NixOS module.
- Default merge behavior should suit additive configuration; only an explicit `force()` clears and replaces existing values.
- Users express desired state and necessary dependencies, while the scheduler determines the execution layers that can run in parallel.
- The intermediate representation and resource graph are internal compilation boundaries that support validation, explainable plans, and feature extension.
- For system components that already have stable native configuration formats, DebianForm does not reinvent a high-level abstraction. The primary path should be a thin wrapper that manages files, validation, activation, dependencies, diffs, and composition. Examples include `nftables`, `systemd.networkd`, systemd units, sysctl, and modules-load.
- Compatibility with the old experimental configuration syntax is not required; state and resource addresses may be redesigned.

## Non-Goals

The current design does not provide:

- A complete NixOS module system.
- A Nix store, generations, or atomic rollback.
- Gradual migration compatibility for the old experimental configuration.
- Public low-level `low-level provider resource` resources as the primary syntax.
- An arbitrary option-priority system.
- Numeric priorities through which users control execution order.
- SSH commands as a user-layer abstraction.

## Top-Level Syntax

The top level retains four main objects:

```hcl
locals {}

profile "name" {}

component "name" {}

host "name" {}
```

`host` is the only object that is actually applied. Neither `profile` nor `component` executes independently:

- A `profile` contributes mergeable host baseline configuration.
- A `component` describes a reusable deployment unit that expands for a target architecture after a host explicitly attaches it.

Example:

```hcl
profile "base" {
  packages {
    install = ["curl", "vim", "ca-certificates"]
  }
}

profile "bbr" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}

host "ksvm213" {
  imports = [
    profile.base,
    profile.bbr,
  ]
}
```

## Host

`host "name"` represents a target host.

Default rules:

```text
host label = 主机名
host label = 默认 SSH host
state 使用默认路径
```

```hcl
host "ksvm213" {}
```

Equivalent meaning:

```text
host.name       = ksvm213
ssh.host        = ksvm213
state.path      = /var/lib/debianform/state/ksvm213.json
state.lock_path = /var/lock/debianform/state/ksvm213.lock
```

If the SSH target is different, it can be specified explicitly:

```hcl
host "edge-1" {
  ssh {
    host          = "10.0.0.11"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }
}
```

## First-Level Host Structure

The organization inside `host` follows NixOS-style domains while retaining DebianForm's own semantics:

```hcl
host "ksvm213" {
  imports = []
  components = []

  state {}
  ssh {}
  system {}

  kernel {}
  packages {}
  apt {}
  files {}
  secrets {}
  directories {}
  users {}
  groups {}
  services {}
  systemd {}
  nftables {}
  networking {}
  security {}
}
```

Current mainline domain scope:

- `imports`
- `components`
- `state`
- `ssh`
- `system`
- `kernel`
- `packages`
- `apt`
- `files`
- `secrets`
- `directories`
- `users`
- `groups`
- `services`
- `systemd`
- `nftables`

`networking` and `security` can be deferred to a later phase. `nftables` is a first-class domain in the target DSL; the initial implementation manages native nftables files rather than inventing a generic firewall abstraction.

The complete target syntax can also include `environment`, `sshd`, `nftables`, `docker`, and repeated `assert` blocks. They are used in [examples/fleet.dbf.hcl](../../../examples/fleet.dbf.hcl) to check whether the combined DSL is coherent, but this does not mean all of them belong to the first implementation phase.

For collections with stable identities, such as file paths, user names, and unit names, the user syntax consistently uses a domain container with labeled object blocks:

```hcl
files {
  file "/etc/motd" {}
}

users {
  user "deploy" {}
}
```

The compiler normalizes them into maps by block label. Syntax such as `files { "/etc/motd" = {} }` cannot be used because a quoted path is not a valid HCL attribute name.

## System

`system` describes the fundamental system configuration the user wants DebianForm to manage. `hostname` is desired state; when declared, it means the remote hostname should be set. Host architecture and Debian codename are runtime facts and generally do not need to be written in the DSL:

```hcl
system {
  timezone = "Asia/Tokyo"
  locale   = "en_US.UTF-8"
}
```

Requirements:

- `hostname` defaults to the host label; when explicitly declared, it means the remote hostname should converge to that value.
- `architecture` uses a canonical DebianForm architecture name such as `amd64` or `arm64` and is discovered after online `plan`, `check`, or `apply` connects to the target; when explicitly declared, it must match the discovered value.
- `codename` is a Debian release codename such as `bookworm` or `trixie`, discovered by online `plan`, `check`, or `apply` from `/etc/os-release` or `lsb_release`.
- Discovered `architecture`, `codename`, and the current remote hostname are written to top-level state under `facts.system`. Here, `facts.system.hostname` is the observed value and does not override the desired `system.hostname` value.
- `target.system.hostname` comes from the configured desired hostname; `target.system.architecture` and `target.system.codename` come from explicit assertions or online fact discovery.
- Offline `validate` does not depend on SSH; `plan --offline` generates only a purely local preview.
- `timezone` and `locale` can be provided by a profile.
- `hostname`, `architecture`, and `codename` can be declared only in a host; declaring them in a profile should produce an error.

## State

DebianForm manages ordinary Debian hosts and needs its own remote state file to record managed resources, orphan cleanup, and destroy order.

`state` belongs under `host`:

```hcl
host "ksvm213" {
  state {
    path      = "/var/lib/debianform/state/ksvm213.json"
    lock_path = "/var/lock/debianform/state/ksvm213.lock"
  }
}
```

Defaults:

```text
state.path      = /var/lib/debianform/state/<host>.json
state.lock_path = /var/lock/debianform/state/<host>.lock
```

Requirements:

- When the user does not configure `state`, default values must be filled automatically.
- `state.path` and `state.lock_path` must be absolute paths.
- Each host has independent state, avoiding the lock and destroy-boundary problems of shared multi-host state.
- State addresses can be redesigned and do not need to remain compatible with old state addresses.

### State File Contents

State records the facts under DebianForm's management on a remote host. It is not a copy of the user configuration.

State uses machine-written canonical JSON:

```json
{
  "version": 2,
  "host": "ksvm213",
  "serial": 17,
  "updated_at": "2026-06-19T12:00:00Z",
  "resources": {
    "host.ksvm213.packages.install[\"curl\"]": {
      "host": "ksvm213",
      "kind": "package",
      "provider_type": "package",
      "provider_address": "package.ksvm213_curl",
      "ownership": "managed",
      "desired": {
        "ensure": "present",
        "name": "curl"
      },
      "desired_digest": "sha256-summary",
      "observed": {
        "installed": true
      },
      "updated_at": "2026-06-19T12:00:00Z",
      "order": 0
    }
  }
}
```

`ownership` determines whether a resource is destroyed after it is removed from the configuration:

```text
created   DebianForm 创建或安装的资源；从配置删除后应销毁。
adopted   DebianForm 接管时已经存在的资源；从配置删除后默认只解除管辖。
external  只作为依赖或观测对象记录；不销毁。
```

Removing an object from the configuration and explicitly declaring `ensure = "absent"` have different semantics:

- Removing it from the configuration means DebianForm no longer manages the object; state ownership and lifecycle determine whether it is destroyed.
- `ensure = "absent"` means the user explicitly requires that the remote object not exist; unless blocked by `prevent_destroy`, the plan should generate a delete action.

Default removal policy:

| kind | Removed from configuration with ownership=created | Removed from configuration with ownership=adopted/external |
| --- | --- | --- |
| package | Uninstall | Release management |
| file/secret/nftables file/systemd unit | Delete the remote file | Release management |
| service state | Stop managing enabled/running state | Release management |
| user/group/directory | May be destroyed, but must be highlighted in the plan; users should set `prevent_destroy` on critical objects | Release management |
| operation node | Not destroyed as a long-lived resource | Not destroyed as a long-lived resource |

DebianForm provides minimal lifecycle protection without introducing Terraform's complete lifecycle semantics:

```hcl
files {
  file "/etc/nftables.conf" {
    content = file("nftables.conf")

    lifecycle {
      prevent_destroy = true
    }
  }
}
```

Requirements:

- `lifecycle.prevent_destroy` defaults to false.
- If removing a resource from the configuration, setting `ensure = "absent"`, or replacing it requires the old object to be destroyed while `prevent_destroy = true`, the plan must fail and point to the source location.
- `prevent_destroy` blocks only destroy, delete, and replace actions, not ordinary updates.
- Documentation can recommend `prevent_destroy` for high-risk domains, but the default must be explicit.
- Future capabilities such as `ignore_changes` and `replace_triggered_by` must be designed separately; they are not part of the first version.

State should store at least:

- The state format version.
- The host name.
- A monotonically increasing serial.
- The address of every managed resource.
- The provider kind and remote identity.
- Ownership.
- A summary of the most recently applied desired state.
- Necessary observed information such as a package version, file hash, path, or service name.
- Resource creation, adoption, and most recent application times.

State should not store:

- SSH private key contents.
- Plaintext secrets.
- Lock leases.
- Arbitrary command output logs.

### State Comparison Model

Plan, check, and apply must use three kinds of state input rather than comparing only the current configuration with the last written state:

```text
desired   当前 HCL 经过解析、profile 合并、component 展开后生成的 HostSpec 和 ResourceGraph。
state     远端 state 文件中记录的 DebianForm 管辖事实。
observed  provider 在计划或执行前从目标主机实时读取的实际状态。
```

Their responsibilities differ:

- `desired` is the desired state declared by the user in the current configuration, produced from current configuration files; it is not a complete copy of persistent state.
- `state` contains DebianForm's resource-management facts saved after the last apply. It determines ownership, orphan cleanup, destroy order, and summaries of the last desired and observed values.
- `observed` is the target host's current actual state, read on demand by providers. It is used to detect drift; choose create, update, delete, destroy, replace, or no-op plan actions; and determine state semantics such as adoption or release from management.

Plan/check comparison rules:

- Desired exists and state does not: read observed; plan create when the remote object does not exist, or follow provider policy to plan an update or record adopted ownership when it does.
- Desired exists and observed differs from desired: plan an update; if state shows that the values matched previously, the update also represents drift remediation.
- State exists and desired does not: plan destroy according to ownership and lifecycle; adopted and external resources are released from management by default and their state records are cleaned without modifying the remote objects.
- Desired, state, and observed all agree: plan no-op.
- If reading observed fails, the provider must return an explicit diagnostic and must not silently assume that remote state equals stored state.

`check` must reuse the same state-comparison logic. It returns nonzero if drift exists or if the plan contains any create, update, delete, destroy, replace, or run action.

### Lock File Contents

The lock file indicates only that an apply or plan currently holds the state write lock; it does not store desired state.

The lock also uses machine-written JSON:

```json
{
  "owner": "dbf",
  "pid": "12345",
  "token": "random-128-bit-token",
  "expires_at": "2026-06-19T12:05:00Z",
  "expires_at_unix": 1781870700
}
```

Requirements:

- Acquiring a lock must be an atomic remote operation.
- Releasing a lock must validate the token to avoid deleting another process's lock.
- A new operation can take over after a lock expires, but output must report the stale lock.
- The lock file is excluded from plan diffs and state.

## SSH

`ssh` describes how to connect to the target host as root. DebianForm's current execution model is root-only: `plan`, `apply`, and `check` write directly to `/etc`, `/usr/local`, systemd, APT, nftables, sysctl/module configuration, and remote state. There is no sudo, become, or non-root management connection. This boundary is intentional: given the project's size, reliable support for the primary Debian path takes priority over maintaining a complex privilege-escalation matrix.

```hcl
host "edge-1" {
  ssh {
    host          = "10.0.0.11"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }
}
```

Requirements:

- `ssh.host` defaults to the host label.
- `ssh.port` is optional.
- `ssh.user` is optional and defaults to `root` when omitted. When explicitly configured, it must be `"root"`.
- `ssh.identity_file` is optional.
- The target host must allow root login with an SSH key.
- Sudo privilege escalation, sudoers management, `become`, and non-root management connections are unsupported.
- Managed service processes can still run with reduced privileges through a systemd service's `user` and `group` fields. This affects only the managed service, not DebianForm's management connection.
- A future version can support `ssh.config_host` to reference a local SSH config host directly, but the connection user must still be root.

## Profile

A `profile` is a reusable configuration fragment:

```hcl
profile "base" {
  packages {
    install = ["curl", "vim", "ca-certificates"]
  }
}
```

A `host` includes profiles through `imports`:

```hcl
host "ksvm213" {
  imports = [
    profile.base,
    profile.bbr,
  ]
}
```

Requirements:

- A `profile` can contain the same mergeable domain blocks as a `host`, but it cannot contain `ssh` or `state`; within `system`, it can provide only `timezone` and `locale`, not `hostname`, `architecture`, or `codename`.
- A `profile` is not applied independently.
- `imports` must contain static profile references.
- `imports` are merged in declaration order.
- The host's own configuration is merged last and takes precedence over all profiles.
- A profile can import another profile, but import cycles must be detected.

## Component

A `component` is a deployment unit that can be attached to multiple hosts. It is suitable for third-party binaries, application archives, CA certificates, and the users, directories, configuration files, and systemd units that accompany a component.

Its boundary differs from a `profile`:

```text
profile   合并主机领域配置，例如基础包、BBR、SSH 策略。
component 封装一个有稳定身份和版本的部署单元，例如 rclone、BIRD2 或 myapp。
host      组合 profile/component，并声明主机特有配置。
```

Minimal example:

```hcl
component "rclone" {
  type    = "binary"
  version = "1.66.0"

  source "amd64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-amd64.zip"
    sha256 = "REPLACE_WITH_RCLONE_AMD64_SHA256"
  }

  source "arm64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-arm64.zip"
    sha256 = "REPLACE_WITH_RCLONE_ARM64_SHA256"
  }

  extract {
    format           = "zip"
    strip_components = 1
    include          = "rclone"
  }

  install {
    path = "/usr/local/bin/rclone"
    mode = "0755"
  }
}

host "server1" {
  components = [
    component.rclone,
  ]
}
```

Parameterized example:

```hcl
component "myapp" {
  input "listen_addr" {
    type    = string
    default = "127.0.0.1:8080"
  }

  input "data_dir" {
    type    = string
    default = "/var/lib/myapp"
  }

  files {
    file "/etc/myapp/config.yaml" {
      content = <<-EOF
        listen: ${input.listen_addr}
        data_dir: ${input.data_dir}
      EOF
    }
  }
}

host "server1" {
  component "myapp" {
    source = component.myapp

    inputs = {
      listen_addr = "127.0.0.1:9090"
      data_dir    = "/srv/myapp"
    }
  }
}
```

The first set of proposed component artifact types is:

```text
binary          下载并安装单个可执行文件；source 可以是直接文件或压缩文件。
archive         下载并展开完整目录树。
file            下载并安装普通文件。
ca_certificate  安装 CA 证书，并在内容变化后生成 update-ca-certificates 激活动作。
```

Requirements:

- Component labels are unique within a program.
- A component cannot contain `ssh`, `state`, `imports`, or host-selection logic.
- A component is instantiated only when referenced by a host's `components` or by an in-host `component "<instance>"` block.
- `components = [component.rclone]` is shorthand for one parameterless instance; the instance name defaults to the template name.
- Arguments or multiple instances of the same template require an in-host `component "<instance>"` block.
- Component instance labels are unique within each host and become part of the address.
- Component inputs must declare their types explicitly. The current types are `string`, `number`, `bool`, `any`, `list(T)`, `set(T)`, `map(T)`, `object({ ... })`, `tuple([ ... ])`, and `optional(T)` / `optional(T, default)` inside object attributes.
- Every instance must provide a value for an input that has no default.
- Validate reports an error if an instance provides an unknown input or omits a required one.
- Expressions inside a component access arguments through read-only `input.<name>` values.
- Component expressions can access the host after profile merging and default application through the read-only `target`, such as `target.system.codename`; they cannot modify the host through this context.
- A component can contain only domain objects and need not declare a downloadable artifact.
- When a component declares an artifact, the label of `source "<arch>"` uses a canonical DebianForm architecture name. The first version supports at least `amd64` and `arm64`.
- An unlabeled `source` is architecture-independent.
- A component cannot declare both an unlabeled source and architecture-labeled sources.
- When a host architecture has a matching source, it must select that source exactly; validate reports an error if no source matches.
- Every remote download must have a content checksum. In the first version, `sha256` is 64 lowercase hexadecimal characters. If SRI is supported later, it should use a separate `checksum` field rather than allowing one field to accept multiple formats.
- `extract.format` can be declared explicitly; when omitted, it can be inferred only unambiguously from the URL suffix.
- A binary artifact's `extract.format` supports `zip`, `tar.gz`, `tar.xz`, `bz2`, and `gz`; `bz2` and `gz` represent a compressed single executable.
- `strip_components` must be greater than or equal to 0.
- For `zip`, `tar.gz`, and `tar.xz` binary extraction, `include` must ultimately match exactly one regular file; `bz2` and `gz` single-file extraction does not support `include`.
- `install.path` must be an absolute path.
- The checksum must be verified before extraction; a checksum failure must not touch the target path.
- Extraction must reject absolute paths, `..` path traversal, and symlinks that escape the staging directory.
- Binary and file artifacts use a temporary file in the same directory, set owner, group, and mode, and then rename it atomically.
- Archive artifacts are completely expanded into a staging directory before the target directory is replaced; failure preserves the previous version.
- By default, an archive's owner and group are applied recursively to the content extracted in this operation, not to files outside the target directory.
- Users do not need to manually declare implementation tools such as `curl`, `tar`, `unzip`, or `bzip2` for the artifact pipeline. Providers should use built-in implementations or show missing tools clearly as internal dependencies in the plan.
- A component can contain domain blocks such as `apt`, `packages`, `services`, `groups`, `users`, `directories`, `files`, and `systemd`; they use the same field semantics as host blocks.
- The compiler automatically infers dependencies inside a component, such as repository -> package -> service and group -> user -> directory/file -> systemd unit -> service.
- A conflict must be reported when a component and a host or profile ultimately produce the same remote identity; declaration order must not silently override it.
- Components do not allow general-purpose `before_install` or `after_install` shell hooks. Activation actions should be expressed through semantic resource types or explicit systemd services and timers.

A host retains `components` in declaration order for stable plan presentation, but correctness cannot depend on this order; execution order is still determined by the compiled resource graph.

See [examples/fleet.dbf.hcl](../../../examples/fleet.dbf.hcl) for a complete composition example.

## Assertions

A host can declare repeated `assert` blocks to validate the final host configuration after profile merging:

```hcl
assert {
  condition = contains(self.kernel.modules, "tcp_bbr")
  message   = "BBR requires the tcp_bbr kernel module."
}
```

Requirements:

- `condition` is an HCL boolean expression, not a string waiting to be parsed a second time.
- `self` points to the host view after profile merging and default application.
- Assertions are evaluated before ResourceGraph compilation; a failed assertion stops `validate`, `plan`, and `apply`.
- Assertions cannot read remote runtime state; runtime preconditions belong to provider checks.
- `message` must be a non-empty string.

## Merge Rules

Default merge rules:

```text
imports 按顺序合并
profile 先合并
host 自身配置最后合并

scalar: 后者覆盖前者
map:    深度合并，同 key 后者覆盖前者
list:   默认 append，保持顺序并去重
```

Labeled domain objects are normalized into maps by `(block type, label)` before merging. Objects with the same type and label undergo field-level deep merging, so a host can append supplementary groups to a profile's `user "deploy"`; objects with different labels remain distinct.

For example, a labeled package block is normalized to `packages.package["bird2"]` before merging:

```hcl
profile "bird" {
  packages {
    package "bird2" {
      repositories = ["base_repo"]
    }
  }
}

host "router1" {
  imports = [profile.bird]

  packages {
    package "bird2" {
      repositories = ["host_repo"]
    }
  }
}
```

Effective result:

```text
packages.package["bird2"].repositories = ["base_repo", "host_repo"]
```

`components` is not an ordinary domain list. Duplicate references to the same component are deduplicated; different components do not undergo field merging, and remote-identity conflicts are checked after instantiation.

Example:

```hcl
profile "base" {
  packages {
    install = ["curl", "vim"]
  }
}

host "ksvm213" {
  imports = [profile.base]

  packages {
    install = ["htop"]
  }
}
```

Effective result:

```text
packages.install = ["curl", "vim", "htop"]
```

Map override:

```hcl
profile "bbr" {
  kernel {
    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}

host "ksvm214" {
  imports = [profile.bbr]

  kernel {
    sysctl = {
      "net.core.default_qdisc" = "fq_codel"
    }
  }
}
```

Effective result:

```text
kernel.sysctl = {
  "net.core.default_qdisc" = "fq_codel"
  "net.ipv4.tcp_congestion_control" = "bbr"
}
```

## Merge Modifiers

The current design needs a small set of explicit merge modifiers:

```hcl
force(value)   # 丢弃之前所有定义，只使用当前值
before(value)  # list 值插到已有值前面
after(value)   # list 值追加到已有值后面，默认行为
unset()        # 删除 map key 或禁用继承来的 option
```

Example:

```hcl
host "ksvm213" {
  imports = [profile.base]

  packages {
    install = force(["curl"])
  }

  kernel {
    modules = force([])
  }
}
```

Effective result:

```text
packages.install = ["curl"]
kernel.modules   = []
```

Removing an inherited map key:

```hcl
host "ksvm214" {
  imports = [profile.bbr]

  kernel {
    sysctl = {
      "net.ipv4.tcp_congestion_control" = unset()
    }
  }
}
```

Requirements:

- Merge modifiers exist only in the merge layer.
- The intermediate representation no longer contains `force`, `before`, `after`, or `unset`.
- Using `unset()` on a list should produce an error; use `force([])` to clear a list.
- `force()` can be used with scalars, maps, and lists.

Incorrect usage:

```hcl
host "ksvm215" {
  packages {
    install = unset()
  }
}
```

Use this instead:

```hcl
host "ksvm215" {
  packages {
    install = force([])
  }
}
```

## Kernel Features

`kernel` manages kernel modules and sysctls.

```hcl
host "ksvm213" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}
```

Requirements:

- `modules` persist to modules-load configuration by default.
- `sysctl` persists to sysctl.d and applies runtime values immediately by default.
- `modules` supports list shorthand.
- A later version can support an object form:

```hcl
kernel {
  module "br_netfilter" {
    persist = true
    ensure  = "present"
  }
}
```

BBR semantic dependency:

- If `kernel.sysctl["net.ipv4.tcp_congestion_control"] = "bbr"`, and
- the same host configures `kernel.modules` to contain `tcp_bbr`,
- the compiler must automatically generate an edge from the sysctl to the module.

## Package Features

`packages` manages system packages.

```hcl
packages {
  install = ["curl", "vim", "ca-certificates"]
}
```

Requirements:

- `install` is the managed set of installed packages for which DebianForm is responsible.
- If DebianForm installs a package and it is later removed from `install`, the plan should generate an uninstall action.
- If a package was already installed before DebianForm adopted it, DebianForm can record it as adopted; later removal from `install` releases it from management by default without uninstalling it.
- The first phase does not provide a `remove` field, avoiding conflation between desired absence and removal from the managed set.
- Lists append and deduplicate by default during merging.
- Package names must be non-empty.
- A later version can support object forms such as pinned versions, but the first phase does not provide version locking.

## File Features

`files` manages regular files.

Proposed syntax:

```hcl
files {
  file "/etc/motd" {
    content = "hello\n"
    mode    = "0644"
    owner   = "root"
    group   = "root"
  }
}
```

Requirements:

- The file label is the absolute remote path and is also the object's stable identity during merging and in the resource graph.
- Exactly one of `content` and `source` is set.
- `owner` defaults to `root`.
- `group` defaults to `root`.
- `mode` defaults to `0644`.
- `ensure` defaults to `present` and supports `absent`.
- `sensitive` defaults to false. When true, plans, logs, and state cannot record plaintext content and may record only non-sensitive summaries such as hashes and lengths.
- There can be only one final definition for a path after merging.

## Secret Features

`secrets` manages local secret inputs as sensitive remote files. It is a clear semantic entry point for `files.file sensitive = true` and suits WireGuard private keys, restic environments, application keys, and similar content.

Proposed syntax:

```hcl
secrets {
  file "/etc/wireguard/private.key" {
    source = "secrets/server1-wireguard.key"
    owner  = "root"
    group  = "systemd-network"
    mode   = "0640"
  }
}
```

Requirements:

- By default, a secret file label is its absolute remote path and stable identity.
- When a component needs to reuse the same block label but generate a different target path from its inputs, it can set `path` explicitly. Resource identity and conflict detection then use the resolved `path`:

```hcl
secrets {
  file "wireguard_private_key" {
    path   = "/etc/wireguard/${input.interface.name}.key"
    source = input.private_key_source
  }
}
```

- `source` is a local file path resolved relative to the current configuration file's directory.
- The first version supports only local file sources; external secret providers can be added later.
- `owner` defaults to `root`, `group` defaults to `root`, and `mode` defaults to `0600`.
- Secret file content can be used for rendering and upload within the current process, but plans, logs, and state cannot record plaintext.
- Plans can show only whether a secret changed and non-sensitive summaries such as its hash and length.
- State can record only the remote path, content hash, mode, owner/group, and ownership.
- `secrets.file` and `files.file` cannot manage the same remote path.
- The `examples/secrets/` directory in examples must be ignored by `.gitignore`; documentation and examples must not commit real secrets.
- Future external secret providers must follow the same prohibition against plaintext in plans and state.

## Directory Features

`directories` manages directories.

```hcl
directories {
  directory "/opt/app" {
    owner = "app"
    group = "app"
    mode  = "0755"
  }
}
```

Requirements:

- The directory label is the absolute remote path and stable identity.
- `owner` defaults to `root`.
- `group` defaults to `root`.
- `mode` defaults to `0755`.
- `ensure` defaults to `present` and supports `absent`.
- Recursive behavior is deferred; the first phase does not recursively modify existing contents.

## Group Features

`groups` manages Unix groups.

```hcl
groups {
  group "deploy" {
    gid    = 1500
    system = false
  }
}
```

Requirements:

- The group label is the group name.
- `gid` is optional.
- `system` defaults to false.
- `ensure` defaults to `present` and supports `absent`.
- Group names must not be empty.

## User Features

`users` manages Unix users.

```hcl
users {
  user "deployer" {
    uid   = 1500
    group = "deploy"
    groups = [
      "docker",
    ]
    home  = "/home/deployer"
    shell = "/bin/bash"
  }
}
```

Requirements:

- The user label is the user name.
- `uid` is optional.
- `group` is optional and represents the primary group; it can reference a group declared on the same host.
- `groups` appends and deduplicates by default.
- `system` defaults to false.
- `home` is optional.
- `shell` is optional.
- `ssh_authorized_keys` appends and deduplicates by default and compiles into independent authorized-key resources.
- `ensure` defaults to `present` and supports `absent`.
- If `group` references a group declared in the same configuration, the compiler automatically generates a dependency from the user to the group.

## Systemd Features

`systemd` manages systemd unit files.

```hcl
systemd {
  units = {
    "app.service" = {
      content = <<-EOF
        [Service]
        ExecStart=/usr/local/bin/app
      EOF
    }
  }
}
```

Requirements:

- Unit names must not be empty.
- Exactly one of `content` and `source` is set.
- Units are written to `/etc/systemd/system/<unit-name>` by default.
- Changes to or deletion of unit content require daemon-reload.
- A service with the same name in `services` automatically depends on the unit.

After raw unit support is stable, DebianForm can add structured syntax close to native systemd fields:

```hcl
systemd {
  service "app" {
    enable = true
    state  = "running"

    wanted_by = ["multi-user.target"]
    after     = ["network-online.target"]

    service_config = {
      ExecStart = "/usr/local/bin/app"
      Restart   = "on-failure"
    }
  }

  timer "app" {
    enable = true
    state  = "running"

    timer_config = {
      OnCalendar = "daily"
      Persistent = true
    }
  }
}
```

Structured syntax requirements:

- `service "app"` generates `app.service`, and `timer "app"` generates `app.timer`.
- Field names retain native systemd option names wherever possible instead of inventing parallel names with similar meanings.
- `enable` and `state` are DebianForm management state and are not written directly into unit sections.
- Maps such as `service_config` and `timer_config` generate their corresponding sections.
- A timer automatically associates with a service of the same name, but the resource graph still expresses execution dependencies.
- Structured syntax must emit canonical text and support previewing; it must not degrade into running `systemctl edit` or arbitrary shell commands.

For systemd-networkd, resolved, and journald, the target syntax likewise uses structured serialization of native configuration instead of inventing a high-level network model. For example:

```hcl
systemd {
  networkd {
    netdev "20-wg0" {
      netdev = {
        Name = "wg0"
        Kind = "wireguard"
      }

      wireguard = {
        PrivateKeyFile = "/etc/wireguard/private.key"
        RouteTable     = "off"
      }

      wireguard_peer "server2" {
        PublicKey = "..."
        AllowedIPs = [
          "10.100.0.2/32",
        ]
      }
    }
  }
}
```

This generates `[NetDev]`, `[WireGuard]`, and repeated `[WireGuardPeer]` sections respectively. `RouteTable = "off"` retains native systemd-networkd semantics: routes are not created automatically from `AllowedIPs`. Users who need routes must declare them explicitly in a `.network` file or leave them to an external routing system. The compiler should validate option support against the target systemd version.

## Service Features

`services` manages systemd service state.

```hcl
services {
  service "nginx" {
    package = "nginx"
    enabled = true
    state   = "running"
  }
}
```

Requirements:

- The service label is the service name; when the suffix is omitted, a `.service` unit is used by default.
- `package` is optional; if the same host manages that package, the service automatically depends on it.
- `enabled` is optional; omission means DebianForm does not manage enablement state.
- `state` is optional and supports `running`, `stopped`, `restarted`, and `reloaded`.
- If a systemd unit with the same name exists on the same host, the service automatically depends on it.

## APT Features

An APT repository is a host domain object, not a top-level component. Place it on the host when configuring one machine directly, in a profile when reusing repository policy, and inside a component when reusing the repository together with software and services as a whole.

```hcl
apt {
  repository "cznic_bird2" {
    uris       = ["https://pkg.labs.nic.cz/bird2"]
    suites     = ["trixie"]
    components = ["main"]

    signing_key {
      url    = "https://pkg.labs.nic.cz/gpg"
      sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      path   = "/etc/apt/keyrings/cznic-bird2.asc"
    }
  }
}
```

Repository requirements:

- A repository label is a stable logical name within one host.
- Deb822 `.sources` is the default output format.
- `uris`, `suites`, and `components` use lists to avoid breaking syntax when a field evolves from a single value to multiple values.
- `signing_key` is optional; when declared, exactly one of `url` and `content` is set.
- A remote signing key must declare `sha256`; fingerprint validation can be added later.
- `sha256` must be 64 hexadecimal characters; if `sha256` is declared for inline `content`, it must match the content.
- `path` defaults to `/etc/apt/keyrings/<repository>.asc`.
- A source automatically references its own signing-key path.
- After a signing key or source changes, the compiler generates a host-scoped APT cache-refresh node.
- When multiple repositories change together, each host runs `apt-get update` at most once.
- A repository with `ensure = "absent"` removes the source and key and triggers an APT cache refresh.

Package list shorthand remains appropriate for ordinary packages from the official Debian repositories:

```hcl
packages {
  install = ["curl", "vim"]
}
```

Use a package object when the source relationship must be declared:

```hcl
packages {
  package "bird2" {
    repositories = ["cznic_bird2"]
  }
}
```

Requirements:

- `package "name"` and `install = ["name"]` normalize into the same PackageItem type.
- The same package cannot be declared in both list and object form.
- `repositories` references repository labels in the same host's final configuration.
- `repositories` can reference only repositories that exist and have `ensure = "present"`.
- A package depends only on its explicitly referenced repositories and the host-scoped cache refresh into which changes to those repositories converge.
- The broad rule from the old experimental version, where every package automatically depended on every repository on the same host, is not retained.
- A package without `repositories` uses the currently configured APT sources but does not gain dependency edges when unrelated repositories change.

BIRD2 is best packaged as a complete component rather than promoting the repository itself into a component:

```hcl
component "bird2" {
  apt {
    repository "cznic_bird2" {
      uris       = ["https://pkg.labs.nic.cz/bird2"]
      suites     = [target.system.codename]
      components = ["main"]

      signing_key {
        url    = "https://pkg.labs.nic.cz/gpg"
        sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      }
    }
  }

  packages {
    package "bird2" {
      repositories = ["cznic_bird2"]
    }
  }

  services {
    service "bird" {
      package = "bird2"
      enabled = true
      state   = "running"
    }
  }
}
```

See [examples/bird2.dbf.hcl](../../../examples/bird2.dbf.hcl) for a complete example.

## Nftables Features

The current model should expose nftables directly instead of reducing the firewall to low-capability fields such as `allowed_tcp_ports` and `allowed_udp_ports`. Nftables is already a stable native configuration language on Debian. DebianForm is responsible only for file management, validation, activation, dependencies, and plan diffs.

Proposed syntax:

```hcl
nftables {
  enable = true

  main {
    path     = "/etc/nftables.conf"
    validate = true
    activate = true

    content = <<-EOF
      flush ruleset

      include "/etc/nftables.d/*.nft"
    EOF
  }

  file "20-services" {
    path = "/etc/nftables.d/20-services.nft"

    content = <<-EOF
      table inet filter {
        chain input {
          type filter hook input priority 0; policy drop;

          ct state established,related accept
          iifname "lo" accept
          tcp dport { 22, 80, 443 } accept
          udp dport 51820 accept
          counter drop
        }
      }
    EOF
  }
}
```

Requirements:

- `nftables.enable = true` means installing and enabling the nftables runtime; package installation can be handled by `packages` or an internal dependency generated by the compiler.
- `main` represents the main ruleset and defaults to `/etc/nftables.conf`.
- `file "<label>"` represents a snippet managed by DebianForm and defaults to `/etc/nftables.d/<label>.nft`.
- Exactly one of `content` and `source` is set.
- `validate` defaults to true; DebianForm must run `nft -c -f /etc/nftables.conf` or an equivalent validation before activation.
- `activate` defaults to true; after validation passes, DebianForm runs `nft -f /etc/nftables.conf`.
- When several nftables files change, the main ruleset is validated and activated only once on that host.
- Plans must show text diffs for nft files; sensitive snippets do not reveal plaintext.
- Profiles can contribute snippets, but final paths must not conflict on a host; component-contributed nftables snippets are deferred to a later component-domain extension.
- The first version does not provide a generic top-level `firewall` block. If helpers are added later, they can compile only into explicit nftables snippets and cannot become a second set of semantics replacing nftables.

Recommended composition constraints:

- `main` is responsible only for `flush ruleset` and `include "/etc/nftables.d/*.nft"`.
- A base snippet such as `10-base` defines the table, chain, hook, and default policy.
- Other snippets should prefer `add rule inet filter input ...` to append rules and avoid defining the same table or chain repeatedly.
- Snippet labels should use numeric prefixes to express load order, such as `10-base`, `20-wireguard`, and `30-services`.
- DebianForm does not parse the nft language deeply or try to predict every semantic conflict. `nft -c -f /etc/nftables.conf` is the authoritative validation.
- If several components need to open ports, they should each contribute their own snippet rather than modify one shared file.

See [examples/nftables.dbf.hcl](../../../examples/nftables.dbf.hcl) for a complete example.

## Networking and Security

`networking` and `security` are reserved as future extension domains.

Requirements:

- The first phase does not design complex network abstractions.
- Do not prematurely invent another domain model over networkd or SSH hardening.
- Native `nftables` is the primary firewall path, not a generic `firewall` abstraction.
- Mature native systemd formats can have one-to-one structured serialization and a raw-file escape hatch.
- Related use cases can initially be covered through `files`, `systemd`, and `services`.

## Intermediate Representation

The current design must define an intermediate `HostSpec` representation.

Compilation pipeline:

```text
HCL AST
  -> profile/host merge
  -> component attachment and expansion
  -> HostSpec
  -> ResourceGraph
  -> Plan
  -> Apply
```

Requirements:

- All defaults are filled in within `HostSpec`.
- `HostSpec` contains no merge modifiers.
- `HostSpec` retains source locations for error messages and plan output.
- `HostSpec` retains domain structure and is not identical to provider resources.
- Only the `ResourceGraph` contains low-level provider resources and semantic operation nodes.

See [ir-requirements.md](ir-requirements.md) for the detailed design.

## Resource Addresses

The current design can redesign resource addresses.

Proposed user-layer address:

```text
host.<host>.<domain>.<kind>[<key>]
```

Examples:

```text
host.ksvm213.kernel.module["tcp_bbr"]
host.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
host.ksvm213.packages.install["curl"]
host.ksvm213.files.file["/etc/motd"]
host.ksvm213.services.service["nginx"]
host.ksvm213.components.rclone.install["/usr/local/bin/rclone"]
```

Requirements:

- Plans show user-layer addresses by default.
- Low-level resource addresses can appear as debugging information.
- State should primarily record stable addresses.
- Compatibility with old addresses is not required.

## Operation Nodes

Some actions are not long-lived remote objects but must participate in the dependency graph or they degrade into provider side effects. For example:

```text
host.server1.apt.cache_refresh
host.server1.nftables.validate
host.server1.nftables.activate
host.server1.systemd.daemon_reload
host.server1.services.service["myapp"].restart
host.server1.ca_certificates.update
```

Requirements:

- These actions are modeled uniformly as `OperationNode` objects.
- An `OperationNode` is a first-class DAG node in the ResourceGraph with a stable address.
- An `OperationNode` can be generated only from domain semantics and cannot serve as an arbitrary user shell hook.
- When several upstream changes require the same action, the compiler must converge them into one node by host or scope.
- Plans must show an operation's trigger sources and execution preview.
- Apply must execute operations according to the DAG and, after failure, stop downstream nodes that depend on them.
- State can record a summary of the most recent operation execution but cannot treat an operation as ownership of a long-lived resource.

## Scheduling Semantics

Users do not write stages by default.

Scheduling rules:

```text
用户声明目标状态
编译器生成显式依赖和语义依赖
资源图必须无环
调度器从 DAG 计算 wave
不同 wave 串行
同一 wave 内按并发策略执行
```

Wave example:

```text
wave 0:
  host.web1.packages.install["nginx"]
  host.web1.systemd.unit["nginx.service"]
  host.web2.files.file["/etc/motd"]

wave 1:
  host.web1.systemd.daemon_reload

wave 2:
  host.web1.services.service["nginx"]
```

The scheduler includes only resources and operations that need execution in this plan in active waves; no-op dependencies are considered satisfied. If a resource or operation fails, later active nodes that depend on it are skipped, while independent nodes in the same or later waves can continue and write state for their respective hosts.

Default concurrency policy:

```text
多 host 可并行
单 host 默认串行
后续允许部分资源类型声明 safe_parallel
```

`--parallel` is the global concurrency limit. By default, each host still permits only one executing node; the internal scheduler can expose a separate per-host limit for tests or a future CLI. Only resource types marked `safe_parallel` can occupy one host slot concurrently on the same host. Nodes such as operations, packages, services, users, groups, kernel resources, and sysctls touch shared system state and execute exclusively per host.

Requirements:

- Numeric priorities are not used to express dependencies.
- `depends_on` is used only as an escape hatch when semantic inference is insufficient.
- If explicit stages are introduced later, they can add graph constraints but cannot replace dependencies.
- When any resource fails during apply, downstream resources that depend on it should stop.

## Plan Output

Plans should primarily use addresses users understand, but they cannot be a flat resource list. Changes in the current design often occur deep inside domain structures. The internal plan model must retain structured field-level diffs, which separate renderers then output as terminal text, JSON, or HTML.

Example:

```text
Plan:
  ~ host.server1.systemd.networkd.netdev["10-wg0"]
    source: examples/fleet.dbf.hcl:430

    ~ wireguard_peer["server2"]
      ~ PersistentKeepalive: 15 -> 25
      ~ AllowedIPs
        + "10.200.0.0/24"

  ~ host.server1.nftables.file["20-services"]
    source: examples/fleet.dbf.hcl:560

    ~ content
      - tcp dport { 22, 80 } accept
      + tcp dport { 22, 80, 443 } accept

    validates: nft -c -f /etc/nftables.conf
    activates: nft -f /etc/nftables.conf
```

Requirements:

- Plans show create, update, delete, and no-op semantics.
- Plans show user-layer addresses.
- Debug mode can show low-level provider addresses.
- Error messages must point to the user configuration file and line number.
- The internal plan should use structured `DiffNode` objects rather than only summary strings.
- Scalar diffs show before and after values.
- Object and map diffs recurse by key.
- Set diffs show additions and removals by element identity and must not misreport an unordered set as index changes.
- Only lists where order is meaningful are diffed by index.
- Lists of labeled blocks must first be normalized into maps, such as `wireguard_peer["server2"]`.
- Text content uses line-level diffs; large files collapse surrounding context by default.
- Sensitive fields show only summaries such as `<sensitive>`, hashes, and lengths, never plaintext.
- When terminal output uses color, additions are green, deletions red, and updates yellow; environments without color must retain the `+`, `-`, and `~` symbols.
- JSON output is the stable machine interface; HTML preview is one renderer for the same structured plan.
- An HTML preview must be a standalone static file and support filtering by host, component, and action; collapsing and expanding; searching field paths; and displaying source locations.
- An interactive TUI can be added later and is not a first-version requirement.

The JSON format is detailed in [plan-format.md](../../plan-format.md).

## CLI Requirements

First-phase CLI:

```text
dbf validate
dbf plan
dbf plan --offline
dbf plan --format json
dbf plan --html plan.html
dbf apply
dbf check
dbf fmt
```

Requirements:

- `validate` performs HCL parsing, profile merging, HostSpec validation, and ResourceGraph validation.
- `plan` first discovers runtime facts and reads state and observed values, then displays a plan organized by address; it does not write state or execute changes.
- `plan --offline` does not connect over SSH and generates only a static local preview.
- `plan --format json` emits a structured plan for CI, auditing, and external viewers.
- `plan --html <file>` generates a static HTML preview without changing remote state.
- `apply` plans first and then executes the ResourceGraph.
- `check` returns nonzero if drift exists or the plan contains changes.
- `--host <name>` filters one host.
- `--parallel <n>` controls concurrency across hosts.
- `--dry-run` can be an alias for `plan` or a later addition.
- `plan --interactive` can be added later as a TUI viewer without blocking the first version.

## Roadmap

### Milestone 0: Design Freeze

Goal: freeze the user syntax, merge rules, and intermediate-representation boundary of the current design.

Deliverables:

- Complete the requirements document.
- Complete the intermediate-representation requirements document.
- Define the scope that is incompatible with the old experimental format.
- Confirm the first-phase domain block scope.

### Milestone 1: Parser and AST

Goal: parse the top-level syntax of the current design.

Deliverables:

- Support `host` blocks.
- Support `profile` blocks.
- Support `component` blocks and static host `components` references.
- Support static `imports` references.
- Support the minimal fields for `ssh`, `state`, `system`, `kernel`, and `packages`.
- Support AST representations for `force`, `before`, `after`, and `unset`.
- Add parser tests.

### Milestone 2: Merge Engine

Goal: implement profile/host merging.

Deliverables:

- Merge imports in order.
- Append and deduplicate lists.
- Deep-merge maps.
- Let later scalar values override earlier values.
- Support `force()` override.
- Support `before()` / `after()` list order.
- Support `unset()` map-key removal.
- Detect profile import cycles.
- Include source locations in merge errors.

### Milestone 3: HostSpec

Goal: generate and validate the intermediate representation.

Deliverables:

- Define `Program`, `HostSpec`, and domain specs.
- Fill SSH, state, and system defaults and normalize architecture.
- Normalize kernel modules and sysctls.
- Normalize the managed packages install set.
- Validate required fields and conflicts.
- Add intermediate-representation snapshot tests.

### Milestone 4: ResourceGraph Compiler

Goal: compile a low-level resource graph from HostSpec.

Deliverables:

- Compile kernel configuration into kernel-module and sysctl resources.
- Compile packages into package resources.
- Generate stable addresses.
- Generate low-level provider payloads.
- Generate source-address mappings.
- Infer the BBR module/sysctl dependency.
- Detect resource-address conflicts and dependency cycles.

### Milestone 5: Current Plan

Goal: use the ResourceGraph to generate structured plans that are readable by users and machines.

Deliverables:

- Emit addresses in plans.
- Include field-level `DiffNode` objects in the internal plan.
- Provide a terminal tree-diff renderer.
- Provide a stable JSON renderer.
- Provide a static HTML preview renderer.
- Emit low-level provider addresses in debug mode.
- Retain existing drift detection.
- Point errors to user configuration sources.
- Add BBR plan tests.

### Milestone 6: Current Apply

Goal: execute the ResourceGraph.

Deliverables:

- Maintain independent state for each host.
- Apply serially within one host.
- Apply multiple hosts concurrently.
- Stop downstream resources after an apply failure.
- Reintroduce handler or systemd reload semantics into the resource graph.
- Write addresses to updated state.

### Milestone 7: First Domain Blocks

Goal: complete everyday system-configuration capabilities.

Deliverables:

- `files`
- `directories`
- `groups`
- `users`
- `systemd`
- `services`
- `nftables`
- Infer their semantic dependencies.
- Add their plan/apply tests.

### Milestone 8: APT and Release Components

Goal: support more complete software sources and binary releases.

Deliverables:

- `apt.repository`
- Repository key management.
- Explicit package-to-repository references.
- Host-scoped APT cache-refresh nodes.
- `binary`, `archive`, `file`, and `ca_certificate` component artifacts.
- Select sources by host architecture.
- Check downloads, extract securely, and install atomically.
- Expand domain configuration inside components and detect conflicts.

### Milestone 9: Scheduler Enhancements

Goal: upgrade from concurrent hosts with serial execution per host to DAG wave scheduling.

Deliverables:

- Compute ResourceGraph waves.
- Enforce a global concurrency limit.
- Enforce a per-host concurrency limit.
- Mark resource types as safe_parallel.
- Define failure propagation rules.
- Add scheduling tests.

### Milestone 10: Documentation and Examples

Goal: make the current design usable as the main version.

Deliverables:

- Update README to the current syntax.
- Add a BBR example.
- Add an nginx example.
- Add a user/group/authorized_key example.
- Add a systemd service example.
- Add a multi-host/profile example.
- Cover BBR, APT repositories, BIRD2 components, binary components, nftables, plan previews, and systemd-networkd/WireGuard with small golden examples.
- Include golden examples in parser, HostSpec, ResourceGraph, and plan snapshot tests.
- Keep `fleet` as a composition stress example rather than the sole input to all unit tests.
- Explain only that the old experimental format is obsolete in migration notes; do not promise compatibility.

## Acceptance Criteria

The first version of the current design must at least satisfy the following:

- A `host` can configure BBR on a single machine.
- A `profile` can reuse BBR and base packages.
- A host can override maps and lists from profiles.
- `force([])` can clear an inherited list.
- The same component can be attached to multiple hosts and select exactly one source by architecture.
- Validate reports an error when a component and a host or profile produce the same remote identity.
- Plans emit user-layer addresses.
- Apply correctly executes kernel modules, sysctls, and packages.
- Every host uses independent state.
- Validate detects import cycles, field conflicts, and resource-graph cycles.
- Basic system configuration can be completed without writing any `low-level provider resource` resources.
