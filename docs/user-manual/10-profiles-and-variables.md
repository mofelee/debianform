<p align="right">
  <strong>English</strong> | <a href="10-profiles-and-variables.zh.md">简体中文</a>
</p>

# 10. Use Profiles, Variables, and Per-Environment Parameters

This chapter demonstrates two reuse mechanisms:

- A `profile` captures reusable configuration and is imported by a host through `imports`.
- A `variable` renders the same configuration with different environment parameters.

The example has been verified on a Debian 13 amd64 test host. It writes only under
`/etc/debianform-manual/profile-demo`; it does not install packages or change global system
settings.

## Create a Working Directory

```bash
mkdir -p debianform-manual/10-profiles-and-variables
cd debianform-manual/10-profiles-and-variables
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
variable "environment" {
  type    = string
  default = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "owner" {
  type    = string
  default = "platform"
}

variable "feature_flags" {
  type    = list(string)
  default = []
}

variable "settings" {
  type = object({
    color  = string
    reload = optional(bool, false)
  })

  default = {
    color = "blue"
  }
}

profile "manual_base" {
  directories {
    directory "/etc/debianform-manual/profile-demo" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/profile-demo/common.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        environment = var.environment
        owner       = var.owner
        flags       = var.feature_flags
        settings    = var.settings
      })
    }
  }
}

profile "manual_service" {
  imports = [profile.manual_base]

  files {
    file "/etc/debianform-manual/profile-demo/service.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by service profile\n"
    }
  }
}

host "manual1" {
  imports = [profile.manual_service]

  state {
    path      = "/var/lib/debianform/manual/10-state.json"
    lock_path = "/var/lock/debianform/manual/10-state.lock"
  }

  files {
    file "/etc/debianform-manual/profile-demo/host.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        host        = "manual1"
        environment = var.environment
        owner       = var.owner
        reload      = var.settings.reload
      })
    }
  }
}
```

Create two variable files as well.

`dev.dbfvars`:

```hcl
environment   = "dev"
owner         = "dev-team"
feature_flags = ["debug", "local-cache"]

settings = {
  color  = "green"
  reload = false
}
```

`prod.dbfvars`:

```hcl
environment   = "prod"
owner         = "platform-team"
feature_flags = ["audit", "alerts"]

settings = {
  color  = "red"
  reload = true
}
```

## Profile Merge Rules

`profile.manual_service` imports the base profile through `imports = [profile.manual_base]`.
`host.manual1` then imports the service profile through `imports = [profile.manual_service]`.

Remember these merge rules:

- An imported profile is merged first; the current profile or host is merged afterward.
- Maps merge recursively.
- Lists append with duplicates removed.
- Scalar values from the later side replace earlier values.
- A profile cannot declare host-only fields such as `system.hostname`, `platform.distribution`,
  `platform.version`, `platform.architecture`, or `platform.codename`.

In this chapter, `common.json` comes from `manual_base`, `service.txt` comes from
`manual_service`, and `host.json` comes directly from the host.

## Inspect Effective Variable Values

Run:

```bash
dbf variable inspect -var-file prod.dbfvars
```

The output lists the effective value for each variable. An excerpt is:

```json
{
  "name": "environment",
  "type": "string",
  "default": "prod"
}
```

The current output field is named `default`, but it contains the effective value after variable
sources are merged.

## Preview Two Environments Offline

Preview dev first:

```bash
dbf plan --offline -var-file dev.dbfvars
```

File content in the plan should include:

```json
{"environment":"dev","flags":["debug","local-cache"],"owner":"dev-team","settings":{"color":"green","reload":false}}
```

Then preview prod:

```bash
dbf plan --offline -var-file prod.dbfvars
```

File content in the plan should include:

```json
{"environment":"prod","flags":["audit","alerts"],"owner":"platform-team","settings":{"color":"red","reload":true}}
```

## Apply the Prod Parameters

Run:

```bash
dbf validate -var-file prod.dbfvars
dbf apply --auto-approve -var-file prod.dbfvars
dbf check -var-file prod.dbfvars
```

The first apply should create four resources:

```text
Summary: 4 create, 0 update, 0 delete, 0 no-op, 0 operations
```

## Verify the Remote Files

Run:

```bash
ssh manual1 'cat /etc/debianform-manual/profile-demo/common.json; printf "\n"; cat /etc/debianform-manual/profile-demo/service.txt; cat /etc/debianform-manual/profile-demo/host.json; printf "\n"; stat -c "%a %U %G %n" /etc/debianform-manual/profile-demo /etc/debianform-manual/profile-demo/common.json /etc/debianform-manual/profile-demo/service.txt /etc/debianform-manual/profile-demo/host.json'
```

Expected content includes:

```text
{"environment":"prod","flags":["audit","alerts"],"owner":"platform-team","settings":{"color":"red","reload":true}}
managed by service profile
{"environment":"prod","host":"manual1","owner":"platform-team","reload":true}
```

Modes and ownership resemble:

```text
755 root root /etc/debianform-manual/profile-demo
644 root root /etc/debianform-manual/profile-demo/common.json
644 root root /etc/debianform-manual/profile-demo/service.txt
644 root root /etc/debianform-manual/profile-demo/host.json
```

## Override Temporarily with `-var`

`-var` has higher precedence than `-var-file`. Run:

```bash
dbf plan --offline -var-file prod.dbfvars -var owner=release-team
```

The plan should show `owner` changed to `release-team`:

```json
{"environment":"prod","host":"manual1","owner":"release-team","reload":true}
```

If this is not a real desired change, run only the offline plan; do not apply it.

## Verify a Variable-Validation Failure

Run:

```bash
dbf validate -var environment=qa
```

Expected failure:

```text
dbf: site.dbf.hcl:5:variable["environment"].validation[0]: validation failed for variable "environment": environment must be dev, staging, or prod.
```

## Variable Source Precedence

Common variable sources, from lowest to highest precedence, are:

- Environment variable `DBF_VAR_name=value`.
- `debianform.dbfvars` and `debianform.dbfvars.json`.
- `*.auto.dbfvars` and `*.auto.dbfvars.json`.
- Explicit `-var-file path`.
- Command-line `-var name=value`.

With configuration from several directories, automatic variable files load in order of each
configuration directory's first appearance. A later directory can replace a same-named variable
from an earlier one. For tutorials and CI, this manual recommends an explicit `-var-file` so command
inputs are easier to review.

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/10-profiles-and-variables
cd debianform-manual/10-profiles-and-variables

cat > site.dbf.hcl <<'EOF'
variable "environment" {
  type    = string
  default = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "owner" {
  type    = string
  default = "platform"
}

variable "feature_flags" {
  type    = list(string)
  default = []
}

variable "settings" {
  type = object({
    color  = string
    reload = optional(bool, false)
  })

  default = {
    color = "blue"
  }
}

profile "manual_base" {
  directories {
    directory "/etc/debianform-manual/profile-demo" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/profile-demo/common.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        environment = var.environment
        owner       = var.owner
        flags       = var.feature_flags
        settings    = var.settings
      })
    }
  }
}

profile "manual_service" {
  imports = [profile.manual_base]

  files {
    file "/etc/debianform-manual/profile-demo/service.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by service profile\n"
    }
  }
}

host "manual1" {
  imports = [profile.manual_service]

  state {
    path      = "/var/lib/debianform/manual/10-state.json"
    lock_path = "/var/lock/debianform/manual/10-state.lock"
  }

  files {
    file "/etc/debianform-manual/profile-demo/host.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        host        = "manual1"
        environment = var.environment
        owner       = var.owner
        reload      = var.settings.reload
      })
    }
  }
}
EOF

cat > dev.dbfvars <<'EOF'
environment   = "dev"
owner         = "dev-team"
feature_flags = ["debug", "local-cache"]

settings = {
  color  = "green"
  reload = false
}
EOF

cat > prod.dbfvars <<'EOF'
environment   = "prod"
owner         = "platform-team"
feature_flags = ["audit", "alerts"]

settings = {
  color  = "red"
  reload = true
}
EOF

dbf variable inspect -var-file prod.dbfvars
dbf plan --offline -var-file dev.dbfvars
dbf plan --offline -var-file prod.dbfvars
dbf validate -var-file prod.dbfvars
dbf apply --auto-approve -var-file prod.dbfvars
dbf check -var-file prod.dbfvars
ssh manual1 'cat /etc/debianform-manual/profile-demo/common.json; printf "\n"; cat /etc/debianform-manual/profile-demo/service.txt; cat /etc/debianform-manual/profile-demo/host.json; printf "\n"; stat -c "%a %U %G %n" /etc/debianform-manual/profile-demo /etc/debianform-manual/profile-demo/common.json /etc/debianform-manual/profile-demo/service.txt /etc/debianform-manual/profile-demo/host.json'
dbf plan --offline -var-file prod.dbfvars -var owner=release-team
dbf validate -var environment=qa || true
```

## Cleanup

To remove the remote files and state created by this chapter:

```bash
ssh manual1 'rm -rf /etc/debianform-manual/profile-demo /var/lib/debianform/manual/10-state.json /var/lock/debianform/manual/10-state.lock /var/lock/debianform/manual/10-state.lock.d'
```
