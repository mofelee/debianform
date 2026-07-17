<p align="right">
  <strong>English</strong> | <a href="11-components.zh.md">简体中文</a>
</p>

# 11. Use Components to Install Prebuilt or Source-Built Tools

This chapter demonstrates DebianForm `component` declarations. A component packages a group of
resources into a reusable template and attaches it to a host. The example uses a source component
to compile a small C program from a remote local `file://` source and installs it as
`/usr/local/bin/hello-from-source`.

The example has been verified on a Debian 13 amd64 test host. It installs `gcc` through APT as a
build package. The same DebianForm configuration writes the source file to the target first, so no
external download site is required.

## Create a Working Directory

```bash
mkdir -p debianform-manual/11-components
cd debianform-manual/11-components
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
locals {
  hello_source = <<EOF
#include <stdio.h>

int main(void) {
  puts("hello from DebianForm component");
  return 0;
}
EOF
}

component "hello_from_source" {
  type    = "source"
  version = "1.0.0"

  source {
    url    = "file:///var/lib/debianform/manual/component-source/hello.c"
    sha256 = "c584e04a304b156188a80e373adb473ee26826652e39ca9b6a4b0321e3c85dc4"
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-Wall", "-o", "hello-from-source", "hello.c"],
    ]
    output      = "hello-from-source"
    source_name = "hello.c"
  }

  install {
    path  = "/usr/local/bin/hello-from-source"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/11-state.json"
    lock_path = "/var/lock/debianform/manual/11-state.lock"
  }

  directories {
    directory "/var/lib/debianform/manual/component-source" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/var/lib/debianform/manual/component-source/hello.c" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = local.hello_source
    }
  }

  components = [
    component.hello_from_source,
  ]
}
```

The configuration has two parts:

- The host first manages `/var/lib/debianform/manual/component-source/hello.c`.
- The component reads source from `file:///var/lib/debianform/manual/component-source/hello.c`,
  verifies its SHA-256, installs the build package, runs `cc`, and installs the result at
  `/usr/local/bin/hello-from-source`.

`source.sha256` must be a 64-character SHA-256. The value above matches the complete content of
`hello_source`.

## Inspect the Component

Run:

```bash
dbf validate
dbf component inspect hello_from_source
```

`component inspect` currently focuses on component inputs. This component has no inputs, so output
resembles:

```json
{
  "name": "hello_from_source",
  "inputs": []
}
```

Artifact download, build, and install resources expand in the plan.

## Plan Offline

Run:

```bash
dbf plan --offline
```

The first plan should contain six creates:

```text
Summary: 6 create, 0 update, 0 delete, 0 no-op, 0 operations
```

Important resource addresses include:

- `host.manual1.components.hello_from_source.build.package["gcc"]`
- `host.manual1.components.hello_from_source.artifact.download["default"]`
- `host.manual1.components.hello_from_source.artifact.build[...]`
- `host.manual1.components.hello_from_source.artifact.install["/usr/local/bin/hello-from-source"]`

The `artifact.build[...]` address contains the build-output cache path, which changes with the
source SHA and build command.

## Apply the Configuration

Run:

```bash
dbf apply --auto-approve
dbf check
```

The first apply installs `gcc`, then verifies, builds, and installs the source. `check` should return
to no changes:

```text
Summary: 0 create, 0 update, 0 delete, 6 no-op, 0 operations
```

## Verify the Installation

Run:

```bash
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G %n" /usr/local/bin/hello-from-source /var/lib/debianform/manual/component-source/hello.c; sha256sum /var/lib/debianform/manual/component-source/hello.c'
```

Expected output resembles:

```text
hello from DebianForm component
755 root root /usr/local/bin/hello-from-source
644 root root /var/lib/debianform/manual/component-source/hello.c
c584e04a304b156188a80e373adb473ee26826652e39ca9b6a4b0321e3c85dc4  /var/lib/debianform/manual/component-source/hello.c
```

Confirm that state records download, build-package, build, and install resources:

```bash
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/11-state.json", encoding="utf-8") as f:
    resources = json.load(f).get("resources", {})

for key in [
    "host.manual1.components.hello_from_source.artifact.download[\"default\"]",
    "host.manual1.components.hello_from_source.artifact.install[\"/usr/local/bin/hello-from-source\"]",
    "host.manual1.components.hello_from_source.build.package[\"gcc\"]",
]:
    assert key in resources, key

assert any(k.startswith("host.manual1.components.hello_from_source.artifact.build[") for k in resources), resources
print("state resources ok")
PY'
```

## Introduce Installation-Target Drift

Delete the installed binary:

```bash
ssh manual1 'rm -f /usr/local/bin/hello-from-source'
```

Run:

```bash
dbf check
```

The command should fail and show only the install resource to be recreated:

```text
+ host.manual1.components.hello_from_source.artifact.install["/usr/local/bin/hello-from-source"]
  install component binary /usr/local/bin/hello-from-source

Summary: 1 create, 0 update, 0 delete, 5 no-op, 0 operations
dbf: remote state does not match configuration
```

## Repair the Drift

Run:

```bash
dbf apply --auto-approve
dbf check
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G" /usr/local/bin/hello-from-source'
```

DebianForm reinstalls the target from the component build cache, and `check` returns to no changes.

## When to Use a Component

Good candidates for a component include:

- A group of files, users, services, and systemd units that always appear together.
- An external binary that needs a pinned version and SHA-256.
- A tool that must be downloaded, built from source, and installed.
- A service template that should be reused across hosts through input parameters.

Do not immediately extract a component for:

- One or two simple settings used by only one host.
- Configuration that is still changing rapidly and whose boundaries are not stable.

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/11-components
cd debianform-manual/11-components

cat > site.dbf.hcl <<'EOF'
locals {
  hello_source = <<SRC
#include <stdio.h>

int main(void) {
  puts("hello from DebianForm component");
  return 0;
}
SRC
}

component "hello_from_source" {
  type    = "source"
  version = "1.0.0"

  source {
    url    = "file:///var/lib/debianform/manual/component-source/hello.c"
    sha256 = "c584e04a304b156188a80e373adb473ee26826652e39ca9b6a4b0321e3c85dc4"
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-Wall", "-o", "hello-from-source", "hello.c"],
    ]
    output      = "hello-from-source"
    source_name = "hello.c"
  }

  install {
    path  = "/usr/local/bin/hello-from-source"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/11-state.json"
    lock_path = "/var/lock/debianform/manual/11-state.lock"
  }

  directories {
    directory "/var/lib/debianform/manual/component-source" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/var/lib/debianform/manual/component-source/hello.c" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = local.hello_source
    }
  }

  components = [
    component.hello_from_source,
  ]
}
EOF

dbf validate
dbf component inspect hello_from_source
dbf plan --offline
dbf apply --auto-approve
dbf check
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G %n" /usr/local/bin/hello-from-source /var/lib/debianform/manual/component-source/hello.c; sha256sum /var/lib/debianform/manual/component-source/hello.c'
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/11-state.json", encoding="utf-8") as f:
    resources = json.load(f).get("resources", {})

for key in [
    "host.manual1.components.hello_from_source.artifact.download[\"default\"]",
    "host.manual1.components.hello_from_source.artifact.install[\"/usr/local/bin/hello-from-source\"]",
    "host.manual1.components.hello_from_source.build.package[\"gcc\"]",
]:
    assert key in resources, key

assert any(k.startswith("host.manual1.components.hello_from_source.artifact.build[") for k in resources), resources
print("state resources ok")
PY'

ssh manual1 'rm -f /usr/local/bin/hello-from-source'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G" /usr/local/bin/hello-from-source'
```

## Cleanup

To remove the source, build cache, installed result, and state created by this chapter:

```bash
ssh manual1 'rm -f /usr/local/bin/hello-from-source; rm -rf /var/lib/debianform/manual/component-source /var/cache/debianform/components/hello_from_source /var/lib/debianform/manual/11-state.json /var/lock/debianform/manual/11-state.lock /var/lock/debianform/manual/11-state.lock.d'
```

If no other work needs the `gcc` package installed by this chapter, remove it as well:

```bash
ssh manual1 'DEBIAN_FRONTEND=noninteractive apt-get remove -y gcc'
```
