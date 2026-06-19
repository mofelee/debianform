# DebianForm v2 Requirements

## Goal

DebianForm v2 should provide a NixOS-inspired declarative system configuration
experience for ordinary Debian hosts managed over SSH.

Users should describe what a system should look like, not hand-write low-level
provider resources for every underlying operation.

The architecture should keep three layers separate:

```text
user layer      system / profile / kernel / packages / services / files
compile layer   expand user configuration into an internal resource DAG
apply layer     SSH apply + state + DAG wave scheduler
```

## NixOS Inspiration

NixOS organizes configuration around system domains such as:

```text
boot.*
networking.*
environment.*
users.*
services.*
systemd.*
security.*
```

DebianForm v2 should borrow that first-level shape, while keeping the language
and implementation simpler:

- HCL remains the configuration language.
- `system` is the primary object.
- `profile` provides reusable configuration fragments.
- Merge modifiers are explicit and limited.
- Low-level Debian provider resources remain available as an escape hatch.

DebianForm should not try to copy the Nix store, generations, or the full NixOS
module system.

## Top-Level Objects

v2 should keep the top level small:

```hcl
locals {}

profile "name" {}

system "host" {}
```

`system "host"` is the core object. Its label defaults to the SSH target name:

```hcl
system "ksvm213" {
  imports = [profile.base, profile.bbr]
}
```

This means:

```text
system name = ksvm213
SSH target  = ksvm213
state       = default state path for ksvm213
```

If the SSH target differs from the system name, the system can override it:

```hcl
system "edge-1" {
  ssh {
    host          = "10.0.0.11"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }
}
```

## System Shape

The first-level shape inside `system` should be domain-oriented:

```hcl
system "ksvm213" {
  imports = []

  state {}
  ssh {}

  kernel {}
  packages {}
  apt {}
  files {}
  directories {}
  users {}
  groups {}
  services {}
  systemd {}
  networking {}
  security {}
}
```

The first v2 implementation can start with this subset:

```text
imports
state
ssh
kernel
packages
files
directories
users
groups
services
systemd
```

## State

NixOS does not have a DebianForm-style remote apply state file. NixOS has
`system.stateVersion`, but that is a compatibility version for data migrations,
not a database of applied resources.

DebianForm manages ordinary Debian systems through SSH, so it still needs state
to track managed resources, orphan cleanup, and destroy order.

In v2, state belongs under each `system`:

```hcl
system "ksvm213" {
  state {
    path      = "/var/lib/debianform/state/ksvm213.json"
    lock_path = "/var/lock/debianform/state/ksvm213.lock"
  }
}
```

Defaults:

```text
state.path      = /var/lib/debianform/state/<system>.json
state.lock_path = /var/lock/debianform/state/<system>.lock
```

Most users should not need to configure `state`.

## Profiles

`profile` is a reusable configuration fragment:

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
```

A system imports profiles:

```hcl
system "ksvm213" {
  imports = [
    profile.base,
    profile.bbr,
  ]
}
```

Profiles contribute definitions. The final system configuration is produced by
merging imported profiles first, then merging the system body last.

## Merge Rules

Default merge behavior:

```text
imports are merged in declaration order
profile definitions merge first
system-local definitions merge last

scalar values: later definition wins
maps:          deep merge, same key later definition wins
lists:         append by default, preserving order and removing duplicates
```

Example:

```hcl
profile "base" {
  packages {
    install = ["curl", "vim"]
  }
}

system "ksvm213" {
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

Map values merge by key:

```hcl
profile "bbr" {
  kernel {
    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}

system "ksvm214" {
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

v2 should provide a small set of explicit merge modifiers:

```hcl
force(value)   # discard earlier definitions and use value
before(value)  # put list values before existing values
after(value)   # put list values after existing values; default behavior
unset()        # remove a map key or disable an inherited option
```

Example:

```hcl
system "ksvm213" {
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
system "ksvm214" {
  imports = [profile.bbr]

  kernel {
    sysctl = {
      "net.ipv4.tcp_congestion_control" = unset()
    }
  }
}
```

## BBR Example

Single-host BBR should be concise:

```hcl
system "ksvm213" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}
```

Reusable profile form:

```hcl
profile "bbr" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc" = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}

system "ksvm213" {
  imports = [profile.bbr]
}
```

The compiler should expand this into internal resources:

```text
system.ksvm213.kernel.module["tcp_bbr"]
system.ksvm213.kernel.sysctl["net.core.default_qdisc"]
system.ksvm213.kernel.sysctl["net.ipv4.tcp_congestion_control"]
```

Those internal resources can map to the existing providers:

```text
debian_kernel_module
debian_sysctl
```

The user should not need to write an explicit dependency for BBR. The compiler
or engine should infer that the `bbr` congestion-control sysctl depends on the
`tcp_bbr` module on the same system.

## Scheduling Semantics

Users should express dependencies and target state. The scheduler should decide
which tasks can safely run together.

Default behavior:

```text
explicit dependencies become graph edges
semantic dependencies become graph edges
compiled resources form a DAG
the scheduler computes executable waves from the DAG
```

Stages are not required in the first v2 version. If introduced later, explicit
stages should act as additional graph constraints, not as a replacement for
dependencies.

Recommended default concurrency:

```text
multiple systems may run in parallel
one system runs serially by default
future resource classes may opt into safe same-system parallelism
```

This avoids common remote conflicts such as apt locks, systemd reload races, and
concurrent writes to related files.

## Low-Level Escape Hatch

v1-style Debian resources should remain available for advanced or unsupported
cases, but they should no longer be the primary user-facing syntax.

Proposed escape hatch:

```hcl
resource "debian_file" "custom" {
  system  = "ksvm213"
  path    = "/etc/custom.conf"
  content = "..."
}
```

Long-term direction:

```text
v2 user syntax: system/profile/domain blocks
internal IR:    low-level Debian resources
escape hatch:   explicit resource blocks
```

## Compatibility

v2 should coexist with v1 during migration.

Requirements:

- Existing `debian_*` configurations continue to load.
- v2 `system` syntax can be introduced incrementally.
- Generated internal resource addresses should be stable.
- State migration should be explicit if resource addresses change.
- Plan output should make compiled resources understandable to users.

## Non-Goals

v2 should not attempt to implement:

- the full NixOS module system
- Nix store semantics
- system generations and atomic rollback
- arbitrary priority values for every option
- implicit magic that makes plan output hard to explain

DebianForm v2 should stand on NixOS's design lessons while staying appropriate
for SSH-managed Debian hosts.
